"""Read the control update unit and enable it when it is not active."""
import json
from remote import connect, run

script = r"""
python3 - <<'PY'
import json, os, pathlib, subprocess
def show(unit):
    enabled = subprocess.run(['systemctl', 'is-enabled', unit], capture_output=True, text=True).stdout.strip()
    active = subprocess.run(['systemctl', 'is-active', unit], capture_output=True, text=True).stdout.strip()
    return {'unit': unit, 'enabled': enabled or 'unknown', 'active': active or 'unknown', 'file': os.path.isfile('/etc/systemd/system/' + unit)}
units = [show('migate-control-update.path'), show('migate-control-update.service')]
request = pathlib.Path('/var/lib/migate-control/release-request.json')
status = pathlib.Path('/var/lib/migate-control/release-status.json')
service = subprocess.run(['systemctl', 'is-active', 'migate-control.service'], capture_output=True, text=True).stdout.strip()
installing = subprocess.run(['bash', '-lc', 'ps -eo args | grep -E "migate-install|install.sh control" | grep -v grep || true'], capture_output=True, text=True).stdout
journal = subprocess.run(['journalctl', '-u', 'migate-control-update.service', '-n', '20', '--no-pager', '-o', 'cat'], capture_output=True, text=True).stdout
lines = []
for line in journal.splitlines():
    if any(word in line.lower() for word in ['token', 'authorization', 'bearer']):
        lines.append('[redacted]')
    else:
        lines.append(line[-240:])
version = ''
for candidate in ['/opt/migate-control/current/migate-control', '/usr/local/bin/migate-control']:
    if os.path.isfile(candidate):
        completed = subprocess.run([candidate, '--version'], capture_output=True, text=True)
        version = (completed.stdout or completed.stderr).strip().splitlines()[-1:] or ['']
        version = version[0]
        break
report = {
    'units': units,
    'request_present': request.is_file(),
    'status_present': status.is_file(),
    'installer': os.path.isfile('/usr/local/bin/migate-install'),
    'service': service,
    'installer_processes': [line[-180:] for line in installing.splitlines() if 'pgrep' not in line][:5],
    'version': version,
    'journal': lines[-20:],
}
path_unit = next(item for item in units if item['unit'].endswith('.path'))
if path_unit['active'] != 'active':
    unit_text = '''[Unit]
Description=Watch MiGate release request
[Path]
PathExists=/var/lib/migate-control/release-request.json
PathChanged=/var/lib/migate-control/release-request.json
Unit=migate-control-update.service
[Install]
WantedBy=multi-user.target
'''
    service_text = '''[Unit]
Description=MiGate verified GitHub release update
[Service]
Type=oneshot
User=root
ExecStart=/usr/local/bin/migate-install --apply-request
'''
    pathlib.Path('/etc/systemd/system/migate-control-update.service').write_text(service_text)
    pathlib.Path('/etc/systemd/system/migate-control-update.path').write_text(unit_text)
    subprocess.run(['systemctl', 'daemon-reload'], check=True)
    subprocess.run(['systemctl', 'enable', '--now', 'migate-control-update.path'], check=True)
    report['enabled_now'] = True
    report['units_after'] = [show('migate-control-update.path'), show('migate-control-update.service')]
else:
    report['enabled_now'] = False
print(json.dumps(report, ensure_ascii=False))
PY
"""
client = connect("control")
try:
    status, output, error = run(client, script, timeout=60)
finally:
    client.close()
print(output, end="")
if error.strip():
    print(error, end="", file=__import__("sys").stderr)
if status:
    raise SystemExit(status)
