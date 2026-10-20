"""Print control service state and redacted recent logs."""
from remote import connect, run

script = r"""
python3 - <<'PY'
import json, subprocess
active = subprocess.run(['systemctl', 'is-active', 'migate-control'], capture_output=True, text=True)
show = subprocess.run(['systemctl', 'show', 'migate-control', '-p', 'ExecMainStatus', '-p', 'Result', '-p', 'ActiveState'], capture_output=True, text=True)
logs = subprocess.run(['journalctl', '-u', 'migate-control', '-n', '30', '--no-pager', '-o', 'cat'], capture_output=True, text=True)
kept = []
for line in logs.stdout.splitlines():
    lowered = line.lower()
    if 'password' in lowered or 'postgres://' in lowered or 'setup-token' in lowered:
        continue
    kept.append(line[-240:])
print(json.dumps({'active': active.stdout.strip(), 'show': show.stdout.strip(), 'log': kept[-12:]}, ensure_ascii=False))
PY
"""
with connect('control') as client:
    code, out, err = run(client, script, timeout=30)
print(out)
if err:
    print(err)
raise SystemExit(code)
