"""Capture redacted actual runtime/inventory evidence without reading secrets."""
import json,time
from pathlib import Path
from remote import connect,run
from live_api import API

report={'date':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),'servers':{}}
for role in ['control','gate']:
 with connect(role) as c:
  entry={}
  commands={
   'current':f'readlink /opt/migate-{role}/current',
   'previous':f'readlink /opt/migate-{role}/previous',
   'service':f'systemctl is-active migate-{role}',
   'manifest':f"python3 -c \"import json;d=json.load(open('/etc/migate-{role}/install-manifest.json'));print(json.dumps({{k:d[k] for k in ['role','version','current','previous'] if k in d}}))\"",
   'binary_sha256':f'sha256sum /opt/migate-{role}/current/migate-{role}',
   'latest_backup_snapshots':f"find /var/backups/migate -mindepth 1 -maxdepth 1 -type f -name 'migate-{role}-*.snapshot.json' -printf '%f\\n' | sort | tail -n 3",
  }
  if role=='gate':commands.update({'owned_core_services':'systemctl is-active migate-gate-helper migate-gate-xray migate-gate-tunnel','existing_x_ui':'systemctl is-active relay154-x-ui','spare_port':"ss -lntup | awk '/:24446 / {print}'"})
  for label,command in commands.items():
   status,out,err=run(c,command);entry[label]={'exit_code':status,'output':out.strip()}
  report['servers'][role]=entry
try:
 report['application_version']=API().admin().request('GET','/api/v1/admin/overview').get('version')
except Exception:
 report['application_version']=None
 report['application_observation']='unavailable'
from production_run_support import atomic_evidence
atomic_evidence(Path('docs/evidence/deployment.json'),report)
from status import generate
generate()
print('Redacted runtime, manifests, rollback versions and backup locations recorded')
