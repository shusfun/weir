"""One cross-machine flock for deployment, rollback and real acceptance.

The remote marker survives interruption. Cleanup runs before releasing the lock;
an interrupted/failed cleanup blocks new work until --recover completes it.
This is an operator coordination boundary, not a replacement for API permissions.
"""
import argparse
import hmac
import http.server
import json
import os
import re
from pathlib import Path
import secrets
import signal
import subprocess
import sys
import threading
import time
import uuid

from remote import _connect, run

ROOT = Path(__file__).resolve().parent.parent
OPS = Path(os.environ.get('LOCALAPPDATA', str(Path.home()))) / 'MiGate/operations'
PENDING = OPS / 'production-pending.json'
MARKER = '/var/lib/migate-control/acceptance-pending.json'
LOCK = '/run/lock/migate-acceptance.lock'


def atomic(path, data):
    # Desktop may redirect LOCALAPPDATA to another drive. Resolve both names
    # before replace; mixing virtual and real paths yields ERROR_NOT_SAME_DEVICE.
    path = path.parent.resolve() / path.name
    path.parent.mkdir(parents=True, exist_ok=True)
    tmp = path.with_suffix('.tmp')
    tmp.write_text(json.dumps(data, ensure_ascii=False, indent=2), encoding='utf-8')
    # Packaged Windows apps may redirect only the newly created file, while
    # resolving its not-yet-existing destination still returns the virtual drive.
    tmp = tmp.resolve()
    path = tmp.parent / path.name
    os.chmod(tmp, 0o600)
    os.replace(tmp, path)


class RemoteLease:
    HEARTBEAT_INTERVAL = 2

    def __enter__(self):
        self.heartbeat_stop = threading.Event()
        self.heartbeat_failed = threading.Event()
        self.heartbeat_lock = threading.Lock()
        self.client = _connect('control')
        self.channel = self.client.get_transport().open_session()
        self.channel.settimeout(10)
        # Fixed paths and command; user arguments never enter this shell.
        self.channel.exec_command("flock -n " + LOCK + " python3 -u -c \"import os,select,sys; print('MIGATE_LOCKED',end='',flush=True); "
                                  "exec('while True:\\n ready=select.select([sys.stdin],[],[],20)[0]\\n if not ready or not os.read(0,4096): break')\"")
        self.last_heartbeat = 0
        response = self.channel.recv(64)
        if response != b'MIGATE_LOCKED':
            self.__exit__(None, None, None)
            raise RuntimeError('Another production operation holds the shared lock')
        # Snapshot, recovery and runtime capture can block longer than the
        # remote timeout. Keep renewing through every phase, not just the child
        # polling loop or browser requests.
        if not self.alive():
            self.__exit__(None, None, None)
            raise RuntimeError('Production lock lost during acquisition')
        self.heartbeat_thread = threading.Thread(target=self._renew, daemon=True)
        self.heartbeat_thread.start()
        return self

    def alive(self):
        with self.heartbeat_lock:
            if self.heartbeat_stop.is_set() or self.heartbeat_failed.is_set():
                return False
            try:
                transport = self.client.get_transport()
                active = transport is not None and transport.is_active() and not self.channel.exit_status_ready() and not self.channel.closed
                if active and time.monotonic() - self.last_heartbeat >= self.HEARTBEAT_INTERVAL:
                    self.channel.sendall(b'heartbeat\n')
                    self.last_heartbeat = time.monotonic()
            except Exception:
                active = False
            if not active:
                self.heartbeat_failed.set()
            return active

    def _renew(self):
        while not self.heartbeat_stop.wait(self.HEARTBEAT_INTERVAL / 2):
            if not self.alive():
                return

    def require_alive(self):
        if not self.alive():
            raise RuntimeError('Production lock lost; recovery marker must be retained')

    def marker(self):
        self.require_alive()
        code, out, _ = run(self.client, 'if [ -f ' + MARKER + ' ]; then cat ' + MARKER + '; fi')
        if code:
            raise RuntimeError('Cannot read production recovery marker')
        return json.loads(out) if out.strip() else None

    def write_marker(self, data):
        self.require_alive()
        code, _, _ = run(self.client, 'umask 077; cat > ' + MARKER + '.tmp && mv ' + MARKER + '.tmp ' + MARKER,
                          stdin=json.dumps(data))
        if code:
            raise RuntimeError('Cannot persist production recovery marker')

    def clear_marker(self):
        self.require_alive()
        code, _, _ = run(self.client, 'rm -f ' + MARKER)
        if code:
            raise RuntimeError('Cannot clear production recovery marker')

    def __exit__(self, *_):
        self.heartbeat_stop.set()
        try:
            self.channel.shutdown_write()
        except Exception:
            pass
        self.channel.close()
        self.client.close()
        if hasattr(self, 'heartbeat_thread'):
            self.heartbeat_thread.join(timeout=2)


class Broker:
    def __init__(self, lease):
        self.lease, self.token = lease, secrets.token_hex(32)
        outer = self
        class Handler(http.server.BaseHTTPRequestHandler):
            def do_GET(self):
                valid = (self.path == '/check' and outer.lease.alive() and
                         hmac.compare_digest(self.headers.get('Authorization', ''), 'Bearer ' + outer.token))
                self.send_response(200 if valid else 403)
                self.send_header('Content-Type', 'application/json')
                self.end_headers()
                self.wfile.write(json.dumps({'active': valid}).encode())
            def do_POST(self):
                valid = (self.path == '/authorize' and outer.lease.alive() and
                         hmac.compare_digest(self.headers.get('Authorization', ''), 'Bearer ' + outer.token))
                try:
                    length = int(self.headers.get('Content-Length', '0'))
                    if not valid or length <= 0 or length > 262144:
                        raise ValueError('invalid authorization request')
                    envelope = json.loads(self.rfile.read(length))
                    authorize(envelope['method'], envelope['path'], envelope.get('data'))
                except Exception:
                    valid = False
                self.send_response(200 if valid else 403)
                self.send_header('Content-Type', 'application/json')
                self.end_headers()
                self.wfile.write(json.dumps({'active': valid}).encode())
            def log_message(self, *_):
                pass
        self.server = http.server.ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)

    def __enter__(self):
        self.thread.start()
        self.previous = {k: os.environ.get(k) for k in ['MIGATE_LEASE_ADDRESS', 'MIGATE_LEASE_TOKEN']}
        os.environ['MIGATE_LEASE_ADDRESS'] = 'http://127.0.0.1:' + str(self.server.server_port)
        os.environ['MIGATE_LEASE_TOKEN'] = self.token
        return self

    def __exit__(self, *_):
        self.server.shutdown()
        self.server.server_close()
        for k, v in self.previous.items():
            if v is None:
                os.environ.pop(k, None)
            else:
                os.environ[k] = v


def inventory(api, kind):
    out, page = [], 1
    while True:
        reply = api.request('GET', f'/api/v1/admin/resources/{kind}?page={page}&page_size=500')
        out.extend(reply['items'])
        if len(out) == reply['total']:
            return out
        if not reply['items'] or page >= 100:
            raise RuntimeError('Incomplete cleanup baseline')
        page += 1


def authorize(method, path, data):
    if method in ['GET', 'HEAD', 'OPTIONS']:
        return
    path = path.split('?', 1)[0]
    if method == 'POST' and path in ['/api/v1/auth/login', '/api/v1/auth/mfa', '/api/v1/auth/logout',
                                      '/api/v1/admin/profiles/preview', '/api/v1/admin/subscriptions/preview']:
        return
    record = json.loads(PENDING.read_text(encoding='utf-8'))
    scope = record['cleanup']
    if not scope:
        raise RuntimeError('Mutating acceptance requires a registered recovery scope')
    prefix = record['fixture_prefix']
    if scope.get('handler') == 'outbound-pool' and not path.startswith('/api/v1/admin/resources/'):
        from production_pool_scope import authorize as authorize_pool
        from live_api import API
        return authorize_pool(API().admin(), record, method, path, data)
    match = re.fullmatch(r'/api/v1/admin/resources/([a-z-]+)(?:/(\d+))?', path)
    if match:
        kind, resource_id = match.groups()
        if kind not in scope['plan']['resource_kinds']:
            raise RuntimeError('Resource kind outside scope')
        if method == 'POST' and not (data or {}).get('id'):
            if not (data or {}).get('data', {}).get('name', '').startswith(prefix):
                raise RuntimeError('New fixture must use this run prefix')
            return
        resource_id = int(resource_id or (data or {}).get('id', 0))
    else:
        match = re.fullmatch(r'/api/v1/admin/public-pages/(\d+)/guestbook/(?:config|messages)', path)
        if match:
            kind, resource_id = 'public-page', int(match.group(1))
        elif method == 'POST' and re.fullmatch(r'/api/v1/pages/' + re.escape(prefix) + r'[a-z0-9-]+/guestbook', path):
            return
        else:
            raise RuntimeError('Operation has no recovery contract')
    if resource_id in {x['id'] for x in scope['baseline'][kind]}:
        raise RuntimeError('Cannot edit pre-existing inventory in this scope')
    from live_api import API
    found = next((x for x in inventory(API().admin(), kind) if x['id'] == resource_id), None)
    # Repeated deletes may legitimately address an already removed owned fixture.
    # Only cleanup calls with a known ID are used; unknown IDs fail closed.
    if not found or not found.get('data', {}).get('name', '').startswith(prefix):
        raise RuntimeError('Resource does not belong to this run')


def snapshot(scope):
    if scope == 'none':
        return {}
    from live_api import API
    api = API().admin()
    plan = json.loads((ROOT / 'deploy/acceptance-scopes.json').read_text(encoding='utf-8'))[scope]
    if plan.get('handler') == 'outbound-pool':
        from production_pool_scope import snapshot as snapshot_pool
        result=snapshot_pool(api)
        result['plan']=plan
        return result
    baseline = {kind: inventory(api, kind) for kind in plan['resource_kinds']}
    return {'plan': plan, 'baseline': baseline}


def cleanup(record):
    data = record['cleanup']
    if not data:
        return
    from live_api import API
    api = API().admin()
    if data.get('handler') == 'outbound-pool':
        from production_pool_scope import cleanup as cleanup_pool
        return cleanup_pool(api, record)
    prefix = record['fixture_prefix']
    for kind in reversed(data['plan']['resource_kinds']):
        baseline = {x['id']: x for x in data['baseline'][kind]}
        for item in inventory(api, kind):
            if item['id'] in baseline:
                # Never overwrite a concurrent user's change with an old baseline.
                if item != baseline[item['id']]:
                    raise RuntimeError('Pre-existing inventory changed; recovery requires review')
            else:
                if not item.get('data', {}).get('name', '').startswith(prefix):
                    raise RuntimeError('Unowned new inventory found; refusing automatic removal')
                api.request('DELETE', f"/api/v1/admin/resources/{kind}/{item['id']}", expected=(200, 404))
        after = {x['id']: x for x in inventory(api, kind)}
        if after != baseline:
            raise RuntimeError('Cleanup baseline verification failed')


def stop(child):
    if child.poll() is not None:
        return
    if os.name == 'nt':
        subprocess.run(['taskkill', '/PID', str(child.pid), '/T', '/F'], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    else:
        os.killpg(child.pid, signal.SIGTERM)
    try:
        child.wait(timeout=10)
    except subprocess.TimeoutExpired:
        if os.name != 'nt':
            os.killpg(child.pid, signal.SIGKILL)
        child.wait(timeout=5)


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--scope', choices=['none', 'guestbook', 'outbound-pool'], default='none')
    p.add_argument('--label', required=True)
    p.add_argument('--impact', choices=['observe', 'page', 'business', 'kernel', 'lifecycle', 'final'], default='observe')
    p.add_argument('--recover', action='store_true')
    p.add_argument('command', nargs=argparse.REMAINDER)
    args = p.parse_args()
    command = args.command[1:] if args.command[:1] == ['--'] else args.command
    if not command and not args.recover:
        p.error('A command is required')
    if command:
        program = Path(command[0]).stem.lower()
        scripts = [Path(v) for v in command[1:] if v.endswith(('.py', '.cjs'))]
        if program not in ['python', 'python3', 'node'] or len(scripts) != 1:
            p.error('Use a registered Python/Node deployment or acceptance entrypoint')
        script = scripts[0]
        script_index = command.index(str(script)) if str(script) in command else next(i for i,v in enumerate(command) if Path(v) == script)
        if command[1:script_index] not in [[], ['-X', 'utf8'], ['-u']]:
            p.error('Interpreter options outside the registered entrypoint contract')
        if (ROOT / script).resolve().parent != ROOT / 'deploy':
            p.error('Entrypoint must belong to this workspace deploy directory')
        contracts = json.loads((ROOT / 'deploy/production-commands.json').read_text(encoding='utf-8'))
        contract = contracts.get(script.name)
        if not contract or contract['scope'] != args.scope or args.impact not in contract['impacts']:
            p.error('Entrypoint lacks a matching recovery/impact contract; register the scope before production work')
    with RemoteLease() as lease, Broker(lease):
        old = lease.marker()
        if old:
            if not args.recover or not PENDING.exists():
                raise RuntimeError('Previous production cleanup incomplete; run --recover with its local journal')
            record = json.loads(PENDING.read_text(encoding='utf-8'))
            if old['run_id'] != record['run_id']:
                raise RuntimeError('Recovery journal does not belong to the remote pending run')
            recovery = {'run_id': uuid.uuid4().hex, 'label': args.label, 'scope': record['cleanup'].get('scope', 'recovery'),
                        'impact': 'observe', 'recovery_of': record['run_id'],
                        'started_at': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime())}
            restored = False
            try:
                lease.require_alive()
                cleanup(record)
                lease.clear_marker()
                PENDING.unlink()
                restored = True
            finally:
                recovery.update(finished_at=time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()),
                                cleanup_verified=restored, result='passed' if restored else 'failed')
                write_report(recovery, lease)
            print('Interrupted production scope restored and verified')
        elif PENDING.exists():
            raise RuntimeError('Local recovery journal requires review before new production work')
        if not command:
            return 0
        run_id = uuid.uuid4().hex
        record = {'run_id': run_id, 'label': args.label, 'started_at': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()),
                  'fixture_prefix': 'migate-acceptance-' + run_id[:12] + '-', 'cleanup': snapshot(args.scope)}
        atomic(PENDING, record)
        lease.write_marker({k: record[k] for k in ['run_id', 'label', 'started_at']})
        env = dict(os.environ, MIGATE_FIXTURE_PREFIX=record['fixture_prefix'], MIGATE_ACCEPTANCE_SCOPE=args.scope,
                   MIGATE_RELEASE_IMPACT=args.impact, MIGATE_RUNNER_PID=str(os.getpid()))
        child = None
        outcome, cleaned = 1, False
        try:
            child = subprocess.Popen(command, cwd=ROOT, env=env, start_new_session=os.name != 'nt',
                                     creationflags=subprocess.CREATE_NEW_PROCESS_GROUP if os.name == 'nt' else 0)
            while child.poll() is None:
                if not lease.alive():
                    raise RuntimeError('SSH lock lost; stopping operation and retaining recovery journal')
                time.sleep(0.25)
            outcome = child.returncode
        finally:
            if child:
                stop(child)
            report = {k: record[k] for k in ['run_id', 'label', 'started_at']}
            try:
                lease.require_alive()
                cleanup(record)
                lease.clear_marker()
                PENDING.unlink()
                cleaned = True
            finally:
                # Also record failed cleanup; both recovery journals remain in
                # place unless cleanup and baseline verification succeeded.
                report.update(finished_at=time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()), exit_code=outcome,
                              cleanup_verified=cleaned, scope=args.scope, impact=args.impact,
                              result='passed' if outcome == 0 and cleaned else 'failed')
                write_report(report, lease)
        return outcome


def write_report(report, lease):
    # Recovery is a separate operation: never overwrite the original failure.
    paths = [ROOT / 'docs/evidence/production-operation.json',
             ROOT / ('docs/evidence/production-operations/' + report['run_id'] + '.json')]
    for path in paths:
        atomic(path, report)
    if lease.alive():
        captured = subprocess.run([sys.executable, '-X', 'utf8', 'deploy/capture-deployment.py'], cwd=ROOT)
        if captured.returncode:
            raise RuntimeError('Production finished but runtime evidence capture failed; current status is stale')
        observed = json.loads((ROOT / 'docs/evidence/deployment.json').read_text(encoding='utf-8'))
        report['server_versions'] = {role: Path(item['current']['output']).name
                                    if item['current']['exit_code'] == 0 else None
                                    for role, item in observed['servers'].items()}
        report['version'] = report['server_versions'].get('control')
        report['observed_at'] = observed['date']
        for path in paths:
            atomic(path, report)
    from status import generate
    generate()


if __name__ == '__main__':
    try:
        sys.exit(main())
    except KeyboardInterrupt:
        print('Production interrupted; cleanup attempted before unlock', file=sys.stderr)
        sys.exit(130)
    except Exception as e:
        print(str(e), file=sys.stderr)
        sys.exit(1)
