"""Actual systemd and Xray lifecycle in a dedicated disposable Docker container.

No host filesystem/cgroup mounts or production network credentials are used.
The center PG/Redis restore is tested separately by verify-center-lifecycle.ps1.
"""
import argparse
import json
from pathlib import Path
import subprocess
import time

p=argparse.ArgumentParser();p.add_argument('--version',required=True);args=p.parse_args()
container='migate-systemd-acceptance';results=[]
def cmd(*argv,check=True):
 r=subprocess.run(argv,text=True,encoding='utf-8',errors='replace',capture_output=True)
 if check and r.returncode:raise RuntimeError(' '.join(argv[:4])+' failed: '+r.stderr[-2000:]+r.stdout[-2000:])
 return r
def shell(script,check=True):return cmd('docker','exec',container,'sh','-eu','-c',script,check=check)
def record(name):results.append({'check':name,'passed':True});print(name,flush=True)

labels=json.loads(cmd('docker','inspect',container,'--format','{{json .Config.Labels}}').stdout)
if labels.get('migate.acceptance')!='systemd':raise RuntimeError('Refusing non-test container')
shell('test "$(cat /proc/1/comm)" = systemd; systemctl is-system-running')
cmd('docker','cp',f'dist/migate-{args.version}-linux-amd64/.',container+':/release')
cmd('docker','cp','dist/cores/v26.3.27/xray',container+':/xray-fixture')
shell('cd /release; sh install.sh gate')
shell('systemctl is-active migate-gate-helper; install -m 755 /xray-fixture /opt/migate-gate/cores/xray; printf \'{"inbounds":[],"outbounds":[{"protocol":"freedom"}]}\' > /etc/migate-gate/cores/xray.json; chmod 644 /etc/migate-gate/cores/xray.json; systemctl start migate-gate-xray; systemctl is-active migate-gate-xray; printf retained > /var/lib/migate-gate/acceptance-marker')
record('install starts real systemd helper and independent real Xray')
manage='/release/migate-manage';manifest='/etc/migate-gate/install-manifest.json'
base=f'{manage} uninstall --manifest {manifest}'
dry=json.loads(shell(base+' --dry-run').stdout)
assert not any(x['target']=='/opt/migate-gate/cores' for x in dry)
shell(base+' --confirm gate:uninstall')
shell('systemctl is-active migate-gate-xray; test "$(cat /var/lib/migate-gate/acceptance-marker)" = retained; test -f /etc/migate-gate/install-manifest.json; test ! -e /opt/migate-gate/current')
record('ordinary uninstall preserves running Xray and data; preview excludes cores')
shell('cd /release; sh install.sh gate; systemctl is-active migate-gate-helper; test "$(cat /var/lib/migate-gate/acceptance-marker)" = retained')
record('reinstall after ordinary uninstall preserves data')
dry=json.loads(shell(base+' --with-cores --dry-run').stdout)
assert any(x['target']=='/opt/migate-gate/cores' for x in dry)
shell(base+' --with-cores --confirm gate:uninstall:with-cores')
shell('test ! -e /opt/migate-gate/cores; test "$(cat /var/lib/migate-gate/acceptance-marker)" = retained; ! systemctl is-active --quiet migate-gate-xray')
record('with-cores removes only owned core and stops actual systemd unit')
shell('cd /release; sh install.sh gate')
shell(f'{manage} backup --manifest {manifest} --backup-dir /var/backups/migate-acceptance --confirm gate:backup')
snapshot=shell('find /var/backups/migate-acceptance -name "*.snapshot.json" | sort | tail -1').stdout.strip()
assert snapshot.startswith('/var/backups/migate-acceptance/')
shell(base+' --purge --backup-dir /var/backups/migate-purge --confirm gate:uninstall:purge')
shell('test ! -e /etc/migate-gate; test ! -e /var/lib/migate-gate; test ! -e /opt/migate-gate')
record('purge removes owned roots after external backup')
shell('cd /release; sh install.sh gate')
shell(f'{manage} restore --manifest {manifest} --snapshot {snapshot} --backup-dir /var/backups/migate-pre-restore --confirm gate:restore')
shell('test "$(cat /var/lib/migate-gate/acceptance-marker)" = retained; systemctl start migate-gate-helper; systemctl is-active migate-gate-helper')
record('purge reinstall and checksummed restore work with real systemd')
# A center binary that exits immediately exercises the actual installer trap.
shell('cd /release; sh install.sh control 127.0.0.1:19810 http://localhost:19810')
shell('cp -a /release /bad-release; printf "broken-acceptance\\n" > /bad-release/VERSION; printf "#!/bin/sh\\nexit 1\\n" > /bad-release/migate-control; cd /bad-release; find . -type f ! -name SHA256SUMS -print0 | sort -z | xargs -0 sha256sum > SHA256SUMS')
failed=shell('cd /bad-release; sh install.sh control 127.0.0.1:19810 http://localhost:19810',check=False)
assert failed.returncode!=0
shell(f'test "$(basename "$(readlink /opt/migate-control/current)")" = {args.version}; systemctl is-active migate-control; /opt/migate-control/current/migate-control --config /etc/migate-control/config.json --health-check')
inventory=json.loads(shell('cat /etc/migate-control/install-manifest.json').stdout)
assert inventory['current']==args.version
record('failed center upgrade restores prior symlink manifest and healthy actual service')
Path('docs/evidence/systemd-lifecycle.json').write_text(json.dumps({'date':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),'version':args.version,'environment':'isolated Debian Docker, actual systemd PID1, actual Xray v26.3.27','limitations':'Gate is unpaired; center is not initialized. Initialized PG/Redis restore is separately tested.','results':results},indent=2))
