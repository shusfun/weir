"""Read-only view of the control listener and setup state."""
import json
from remote import connect, run

script = r"""
python3 - <<'PY'
import json, os, datetime, hashlib, hmac, pathlib, subprocess
path='/etc/migate-control/config.json'
info={'config_exists': os.path.isfile(path)}
if info['config_exists']:
    data=json.load(open(path, encoding='utf-8'))
    info['installed']=bool(data.get('installed'))
    info['listen']=data.get('listen')
    info['public_url']=data.get('public_url')
    info['setup_expires']=data.get('setup_expires')
    now = datetime.datetime.now(datetime.timezone.utc)
    info['server_time_utc'] = now.isoformat()
    expires = datetime.datetime.fromisoformat(data['setup_expires'].replace('Z', '+00:00'))
    info['setup_remaining_seconds'] = int((expires - now).total_seconds())
    token_path = pathlib.Path('/etc/migate-control/setup-token')
    info['token_file_exists'] = token_path.is_file()
    token = token_path.read_text().strip() if token_path.is_file() else ''
    info['token_file_matches_config'] = bool(token) and hmac.compare_digest(hashlib.sha256(token.encode()).hexdigest(), data.get('setup_hash', ''))
    info['config_modified_utc'] = datetime.datetime.fromtimestamp(os.stat(path).st_mtime, datetime.timezone.utc).isoformat()
    manifest_path = pathlib.Path('/etc/migate-control/install-manifest.json')
    if manifest_path.is_file():
        manifest = json.loads(manifest_path.read_text())
        info['release'] = {key: manifest.get(key) for key in ['version', 'current']}
    info['service_started'] = subprocess.run(['systemctl', 'show', 'migate-control', '--property=ExecMainStartTimestamp', '--value'], capture_output=True, text=True, check=True).stdout.strip()
    pid = subprocess.run(['systemctl', 'show', 'migate-control', '--property=MainPID', '--value'], capture_output=True, text=True, check=True).stdout.strip()
    if pid.isdecimal() and pid != '0':
        arguments = pathlib.Path('/proc/' + pid + '/cmdline').read_bytes().decode().split('\x00')
        config_argument = '/etc/migate-control/config.json'
        for index, argument in enumerate(arguments):
            if argument in ['--config', '-config'] and index + 1 < len(arguments):
                config_argument = arguments[index + 1]
            elif argument.startswith(('--config=', '-config=')):
                config_argument = argument.split('=', 1)[1]
        info['process_uses_inspected_config'] = os.path.realpath(config_argument) == os.path.realpath(path)
    initial_path = pathlib.Path('/etc/migate-control/initialization.txt')
    info['initialization_file_exists'] = initial_path.is_file()
    info['initialization_file_authoritative'] = False
print(json.dumps(info))
import urllib.request, urllib.error
public_origin = 'https://sub.caowo.baby'
# OPTIONS exercises the origin middleware without submitting setup credentials
# or invoking a state-changing handler. Never follow a redirect for this probe.
class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None
opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
for method, endpoint, origin in [
    ('GET', '/healthz', None),
    ('OPTIONS', '/api/v1/setup/verify', public_origin),
    ('OPTIONS', '/api/v1/setup/verify', info.get('public_url')),
]:
    headers = {'Origin': origin} if origin else {}
    request = urllib.request.Request(public_origin + endpoint, headers=headers, method=method)
    try:
        response = opener.open(request, timeout=10)
    except urllib.error.HTTPError as error:
        response = error
    with response:
        body = response.read(512).decode('utf-8', 'replace')
        print(json.dumps({'probe': method + ' ' + endpoint, 'origin': origin,
                          'status': response.status, 'body': body}))
# Verification is read-only; send the current file token only to the fixed local
# listener and the verified HTTPS origin. Never print token or request headers.
if info.get('token_file_matches_config') and not info.get('installed'):
    for base in ['http://127.0.0.1:19810', public_origin]:
        request = urllib.request.Request(base + '/api/v1/setup/verify', data=b'{}',
            headers={'Origin': public_origin, 'Sec-Fetch-Site': 'same-origin',
                     'Content-Type': 'application/json', 'Authorization': 'Bearer ' + token}, method='POST')
        try:
            response = opener.open(request, timeout=10)
        except urllib.error.HTTPError as error:
            response = error
        with response:
            print(json.dumps({'probe': 'current_token_verify', 'base': base,
                              'status': response.status, 'body': response.read(512).decode('utf-8', 'replace')}))
PY
echo '---LISTEN---'
ss -lntup 2>/dev/null | awk 'NR==1 || /:80 |:443 |:19810 /'
echo '---SERVICE---'
systemctl is-active migate-control || true
echo '---HTTP---'
curl -sS -m 5 -D - -o /dev/null http://127.0.0.1/ || true
echo
curl -sS -m 5 http://127.0.0.1:19810/healthz || true
echo
"""
with connect('control') as client:
    code, out, err = run(client, script, timeout=30)
print(out)
if err:
    print(err)
raise SystemExit(code)
