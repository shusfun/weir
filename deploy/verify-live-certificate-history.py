"""Owned certificate versions only; no CA/DNS claim, no private material logged."""
import datetime,json,secrets,shlex,subprocess,time
from pathlib import Path
from cryptography import x509
from cryptography.x509.oid import NameOID
from cryptography.hazmat.primitives import hashes,serialization
from cryptography.hazmat.primitives.asymmetric import ec
from live_api import API,state
from remote import connect,run

api=API().admin();s=state();device=s['device_id'];cid=None;checks=[]
guard='/etc/migate-gate/certs/acceptance-unregistered-'+secrets.token_hex(12)

def history():
 devices=api.request('GET','/api/v1/admin/devices')['items']
 return next(d for d in devices if d['id']==device)['metadata'].get('core_status',{}).get('certificate_history',{})
def task(op,payload):
 item=api.request('POST',f'/api/v1/admin/devices/{device}/tasks',{'operation':op,'payload':payload,'confirm':True})
 return api.wait_task(item['id'])
def wait_history(count):
 deadline=time.monotonic()+75
 while time.monotonic()<deadline:
  data=history()
  if data.get('validity')=='fresh' and sum(r['certificate_id']==cid for r in data.get('items',[]))==count:return data
  time.sleep(2)
 raise RuntimeError('Actual certificate history observation did not settle')
def upload_version():
 global cid
 key=ec.generate_private_key(ec.SECP256R1());now=datetime.datetime.now(datetime.timezone.utc)
 name=x509.Name([x509.NameAttribute(NameOID.COMMON_NAME,'history-acceptance.invalid')])
 cert=x509.CertificateBuilder().subject_name(name).issuer_name(name).public_key(key.public_key()).serial_number(x509.random_serial_number()).not_valid_before(now-datetime.timedelta(minutes=5)).not_valid_after(now+datetime.timedelta(days=2)).sign(key,hashes.SHA256())
 before={r['id'] for r in api.request('GET','/api/v1/admin/resources/device-certificate')['items']}
 task('certificate.upsert',{'id':cid or 0,'certificate_pem':cert.public_bytes(serialization.Encoding.PEM).decode(),'key_pem':key.private_bytes(serialization.Encoding.PEM,serialization.PrivateFormat.PKCS8,serialization.NoEncryption()).decode()})
 if cid is None:
  fresh=[r for r in api.request('GET','/api/v1/admin/resources/device-certificate')['items'] if r['id'] not in before and r['owner_id']==device]
  if len(fresh)!=1:raise RuntimeError('Cannot identify owned history fixture')
  cid=int(fresh[0]['data']['id'])
def pids(gate):
 code,out,_=run(gate,'systemctl show --property=MainPID --value migate-gate-xray.service migate-gate-sing-box.service relay154-x-ui.service')
 if code:raise RuntimeError('Cannot inspect owned/runtime service identity')
 return out.splitlines()

with connect('gate') as gate:
 try:
  code,_,_=run(gate,'test ! -e '+shlex.quote(guard)+' && install -m 0600 /dev/null '+shlex.quote(guard))
  if code:raise RuntimeError('Cannot create scoped unregistered guard')
  upload_version();upload_version();upload_version()
  before=wait_history(3)
  upload_version();view=wait_history(4)
  queued=api.request('POST',f'/api/v1/admin/devices/{device}/tasks',{'operation':'certificate.clean','payload':{'preview_digest':before['preview_digest']},'confirm':True})
  deadline=time.monotonic()+75
  while time.monotonic()<deadline:
   item=next(t for t in api.request('GET','/api/v1/admin/tasks')['items'] if t['id']==queued['id'])
   if item['status'] in ['failed','applied']:break
   time.sleep(1)
  if item['status']!='failed' or item.get('result',{}).get('message')!='certificate_history_changed':raise RuntimeError('Stale history snapshot not rejected by real helper')
  checks.append({'check':'four actual imported versions; stale preview queued then real helper rejects without deletion','passed':True})
  pid_before=pids(gate)
  result=subprocess.run(['node','deploy/browser-certificate-history.cjs'],capture_output=True,text=True)
  if result.returncode:raise RuntimeError('Certificate history browser acceptance failed: '+result.stderr.strip())
  evidence=json.loads(Path('docs/evidence/certificate-history-ui.json').read_text(encoding='utf-8'))
  applied=api.wait_task(evidence['task_id'])
  if applied.get('result',{}).get('removed')!=2:raise RuntimeError('Actual cleanup count mismatch')
  after=wait_history(2)
  if pids(gate)!=pid_before:raise RuntimeError('Certificate cleanup unexpectedly restarted cores/x-ui')
  if run(gate,'test -f '+shlex.quote(guard))[0]:raise RuntimeError('Unregistered guard was deleted')
  replay=json.dumps({'task_id':evidence['task_id'],'preview_digest':view['preview_digest']})
  code,out,_=run(gate,'curl --silent --fail --unix-socket /run/migate/helper.sock -H "Content-Type: application/json" --data '+shlex.quote(replay)+' http://helper/certificate-clean')
  if code or not json.loads(out).get('replayed'):raise RuntimeError('Durable cleanup replay failed')
  code,_,_=run(gate,'systemctl restart migate-gate-helper.service')
  if code:raise RuntimeError('Owned helper restart failed')
  time.sleep(2)
  code,out,_=run(gate,'curl --silent --fail --unix-socket /run/migate/helper.sock -H "Content-Type: application/json" --data '+shlex.quote(replay)+' http://helper/certificate-clean')
  if code or not json.loads(out).get('replayed'):raise RuntimeError('Cleanup receipt did not survive helper restart')
  checks.append({'check':'actual UI cleanup removes two registered retired pairs; newest two retained; unknown file untouched; core/x-ui PIDs unchanged; receipt replay survives helper restart','passed':True})
  Path('docs/evidence/live-certificate-history.json').write_text(json.dumps({'date':datetime.datetime.now(datetime.timezone.utc).isoformat(),'version':api.request('GET','/api/v1/admin/overview')['version'],'checks':checks,'before':view,'after':after,'limitation':'Self-signed imported test certificate only; no real CA/DNS issuance claimed. Retained recovery versions remain owned history after test certificate removal.'},indent=2),encoding='utf-8')
 finally:
  if cid is not None:task('certificate.delete',{'id':cid})
  run(gate,'rm -- '+shlex.quote(guard))
print('Actual certificate history snapshot rejection, registered-only cleanup, preservation and durable replay passed')
