"""Inject failed release startup on owned services, verify restoration on both VPSs."""
import argparse,json,time
import os
from pathlib import Path
from remote import connect,run

if os.environ.get('MIGATE_RELEASE_IMPACT') not in ['kernel','lifecycle','final']:
 raise RuntimeError('Full rollback rehearsal requires kernel/lifecycle/final impact; page batches retain backup/readiness checks')
parser=argparse.ArgumentParser();parser.add_argument("--version",required=True);parser.add_argument("--role",choices=["control","gate"]);args=parser.parse_args()
results=[]
roles=[args.role] if args.role else ['control','gate']
for role in roles:
 with connect(role) as c:
  version=args.version
  bad=f'{version}-rollback-{int(time.time())}'
  script=f'''set -eu
rollback_step=preflight
trap 'echo rollback-checkpoint:$rollback_step' EXIT
test "$(basename "$(readlink /opt/migate-{role}/current)")" = {version}
test ! -e /var/tmp/migate-{role}-{bad}
mkdir /var/tmp/migate-{role}-{bad}
tar -xzf /var/tmp/migate-{role}-{version}.tar.gz -C /var/tmp/migate-{role}-{bad}
cd /var/tmp/migate-{role}-{bad}
printf '%s\\n' {bad} > VERSION
printf '#!/bin/sh\\nexit 1\\n' > migate-{role}
find . -type f ! -name SHA256SUMS -print0 | sort -z | xargs -0 sha256sum > SHA256SUMS
rollback_step=install-injected-failure
if sh install.sh {role} 127.0.0.1:19810 https://sub.caowo.baby /etc/migate-control/pg-dump > /var/tmp/migate-{role}-{bad}.log 2>&1; then
 echo 'BROKEN RELEASE WAS ACCEPTED'; exit 1
fi
rollback_step=restored-version
test "$(basename "$(readlink /opt/migate-{role}/current)")" = {version}
rollback_step=restored-service
systemctl is-active migate-{role}
rollback_step=restored-manifest
python3 -c 'import json; m=json.load(open("/etc/migate-{role}/install-manifest.json")); assert m["current"]=="{version}"; print("manifest matches runtime")'
'''
  if role=='control':script+=' /opt/migate-control/current/migate-control --config /etc/migate-control/config.json --health-check\n'
  else:script+='systemctl is-active migate-gate-helper migate-gate-xray migate-gate-tunnel relay154-x-ui\nif test -f /etc/migate-gate/cores/sing-box.json; then systemctl is-active migate-gate-sing-box; fi\n'
  code,out,err=run(c,'sh -s',script,timeout=180)
  if code:raise RuntimeError(role+' rollback acceptance failed: '+out+err)
  results.append({'role':role,'restored_version':version,'injected_version':bad,'passed':True,'checks':out.splitlines(),'diagnostic_log':f'/var/tmp/migate-{role}-{bad}.log'})
  print(role+' failed-start upgrade rollback passed',flush=True)
report=json.dumps({'date':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),'results':results},indent=2)
Path('docs/evidence/live-rollback.json').write_text(report)
Path(f'docs/evidence/live-rollback-{args.version}.json').write_text(report)
