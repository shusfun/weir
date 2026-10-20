"""Restore the installed lock when the database still has an administrator.

Prints only booleans, counts, and health. Never prints config values or tokens.
"""
import json
from remote import connect, run

script = r"""
python3 - <<'PY'
import gzip, hashlib, io, json, os, tarfile, pathlib, subprocess, urllib.request
config_path = pathlib.Path('/etc/migate-control/config.json')
current = json.loads(config_path.read_text(encoding='utf-8'))
def key_hash(cfg):
    value = cfg.get('encryption_key') or ''
    return hashlib.sha256(value.encode()).hexdigest() if value else ''
def read_backup(path):
    with tarfile.open(path, 'r:gz') as bundle:
        member = next(item for item in bundle.getmembers() if item.name.endswith('migate-control/config.json'))
        return json.load(io.TextIOWrapper(bundle.extractfile(member), encoding='utf-8'))
backups = sorted(pathlib.Path('/var/backups/migate').glob('migate-control-*.tar.gz'))
summaries = []
previous = None
for path in backups:
    try:
        cfg = read_backup(path)
    except Exception:
        summaries.append({'name': path.name, 'readable': False})
        continue
    item = {
        'name': path.name,
        'installed': bool(cfg.get('installed')),
        'user_set': bool((cfg.get('database') or {}).get('user')),
        'key_matches_current': key_hash(cfg) == key_hash(current),
    }
    summaries.append(item)
    if cfg.get('installed') and (cfg.get('database') or {}).get('user'):
        previous = cfg
        previous_name = path.name
backup = {'found': bool(backups), 'count': len(backups), 'latest': summaries[-3:]}
report = {
    'current_installed': bool(current.get('installed')),
    'current_user_set': bool((current.get('database') or {}).get('user')),
    'backup': backup,
}
if previous and (previous.get('database') or {}).get('user'):
    db = previous['database']
    env = os.environ.copy()
    env['PGPASSWORD'] = db.get('password') or ''
    query = subprocess.run(['docker', 'exec', '-e', 'PGPASSWORD', '1Panel-postgresql-EcCX', 'psql', '-U', db['user'], '-d', db.get('database') or 'gate', '-tAc', "select count(*) from migate.users where role='admin'"], capture_output=True, text=True, env=env)
    report['previous_admin_query_status'] = query.returncode
    if query.returncode == 0:
        report['previous_admin_count'] = int(query.stdout.strip() or '0')
    else:
        report['previous_admin_error'] = (query.stderr or '').replace(db.get('password') or '', '')[-240:]
try:
    report['health'] = urllib.request.urlopen('http://127.0.0.1:19810/healthz', timeout=3).read().decode()
except Exception as error:
    report['health'] = str(error)
print(json.dumps(report))
PY
"""
with connect('control') as client:
    code, out, err = run(client, script, timeout=90)
print(out)
if err:
    print(err)
raise SystemExit(code)
