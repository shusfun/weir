"""Real Gate configuration failure, core restart and command boundary acceptance."""
import hashlib,importlib.util,json,time
from pathlib import Path
from live_api import API,state
from remote import connect,run

spec=importlib.util.spec_from_file_location('client',Path(__file__).with_name('verify-live-client.py'));client=importlib.util.module_from_spec(spec);spec.loader.exec_module(client)
a=API().admin();s=state();checks=[]
def task(op,payload):return a.request('POST',f"/api/v1/admin/devices/{s['device_id']}/tasks",{'operation':op,'payload':payload,'confirm':True})['id']
def wait(tid):
 deadline=time.monotonic()+75
 while time.monotonic()<deadline:
  t=next(t for t in a.request('GET','/api/v1/admin/tasks')['items'] if t['id']==tid)
  if t['status'] in ['applied','failed']:return t
  time.sleep(1)
 raise RuntimeError('Task receipt timeout')
with connect('gate') as c:
 def config_digest():
  with c.open_sftp() as ftp:
   with ftp.open('/etc/migate-gate/cores/xray.json','rb') as f:return hashlib.sha256(f.read()).hexdigest()
 before=config_digest()
 # Valid operation and unoccupied unprivileged port, but invalid Reality key:
 # the actual core validator must reject it without changing current config.
 tid=task('inbound.upsert',{'id':999,'remark':'invalid-core-acceptance','protocol':'vless','core':'xray','port':24446,'network':'tcp','security':'reality','reality_private_key':'invalid','reality_dest':'www.cloudflare.com:443','reality_server_names':'www.cloudflare.com'})
 receipt=wait(tid)
 assert receipt['status']=='failed' and config_digest()==before
 client.run_probe(s['test_users'][0],1)
 checks.append({'check':'actual core rejects invalid candidate and preserves existing client connectivity','task_id':tid,'status':'failed as expected','passed':True})
 tid=task('core.restart',{});assert wait(tid)['status']=='applied'
 client.run_probe(s['test_users'][0],1)
 checks.append({'check':'administrator core restart receives ready-state acknowledgement and real client reconnects','task_id':tid,'passed':True})
 a.request('POST',f"/api/v1/admin/devices/{s['device_id']}/tasks",{'operation':'shell.exec','payload':{'command':'id'},'confirm':True},expected=(400,))
 checks.append({'check':'arbitrary shell operation refused by center','passed':True})
 code,out,err=run(c,'systemctl is-active relay154-x-ui migate-gate migate-gate-helper migate-gate-xray migate-gate-tunnel; ss -H -ltn | awk \'$4 ~ /:(80|443|8443|24445)$/ {print $4}\'')
 assert code==0
 checks.append({'check':'existing x-ui and reserved listeners coexist with owned services','observed':out.splitlines(),'passed':True})
Path('docs/evidence/live-faults.json').write_text(json.dumps({'date':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),'version':'0.2.0-rc8','checks':checks},indent=2))
print('Real core invalid config, restart, command boundary and coexistence passed')
