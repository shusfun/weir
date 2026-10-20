"""Owned Reality inlet: UI state, subscription withdrawal, restart and legacy edit."""
import base64,copy,importlib.util,json,os,secrets,subprocess,time,urllib.parse
from pathlib import Path
from live_api import API,OPS,state
from remote import connect,run

a=API().admin();s=state();did=s['device_id'];iid=None;gid=None;account=None;private=OPS/'resource-state-fixture.json';checks=[]
spec=importlib.util.spec_from_file_location('client',Path(__file__).with_name('verify-live-client.py'));client=importlib.util.module_from_spec(spec);spec.loader.exec_module(client)
def task(payload):
 t=a.request('POST',f'/api/v1/admin/devices/{did}/tasks',{'operation':'inbound.upsert','payload':payload,'confirm':True});a.wait_task(t['id']);return t
def ui(enabled):
 private.write_text(json.dumps({'tab':'inbounds','id':iid,'enabled':enabled}),encoding='utf-8');os.chmod(private,0o600)
 r=subprocess.run(['node','deploy/browser-resource-state.cjs'],capture_output=True,text=True)
 if r.returncode:
  (OPS/'resource-state-browser-error.txt').write_text(r.stderr,encoding='utf-8')
  stage=next((line for line in r.stderr.splitlines() if line.startswith('Resource state UI failed at ')),'unreported stage')
  raise RuntimeError('Inbound state browser failed: '+stage+'; exit='+str(r.returncode)+'; output lengths='+str((len(r.stdout),len(r.stderr))))
 a.wait_task(json.loads(private.read_text(encoding='utf-8'))['task_id'])
def subscription_count():
 u=API();u.login(account['email'],account['password']);reply=u.request('GET','/api/v1/subscription');path=urllib.parse.urlsplit(reply['url']).path
 links=base64.b64decode(u.request('GET',path,raw=True)).decode().splitlines()
 if reply['node_count']!=len(links):raise RuntimeError('Subscription metadata count mismatch')
 return len(links)
def peer():client.run_probe(s['test_users'][0],1);client.run_probe(s['test_users'][1],2)
try:
 with connect('gate') as ssh:
  status,out,_=run(ssh,"ss -lntup | awk '/:24446 / {print}'")
  if status or out.strip():raise RuntimeError('Owned inlet test port occupied')
 payload={'remark':'Owned inlet state '+secrets.token_hex(4),'port':24446,'protocol':'vless','core':'xray','public_host':'154.9.26.146','network':'tcp','security':'reality','reality_dest':'www.cloudflare.com:443','reality_server_names':'www.cloudflare.com','tls_sni':'www.cloudflare.com','tls_fingerprint':'chrome'}
 iid=task(payload)['resource_id']
 account={'email':'inlet-'+secrets.token_hex(5)+'@sub.caowo.baby','password':secrets.token_urlsafe(24),'quota':64<<20,'expires':0};account['id']=a.request('POST','/api/v1/admin/users',account)['id']
 gid=a.request('POST','/api/v1/admin/grants',{'user_id':account['id'],'device_id':did,'inbound_id':iid})['id']
 for _ in range(90):
  if next(g for g in a.request('GET','/api/v1/admin/grants')['items'] if g['id']==gid)['status']=='applied':break
  time.sleep(1)
 else:raise RuntimeError('Fixture grant not applied')
 link,_=client.get_link(account);client.run_probe(account,3,True,link)
 ui(False)
 if subscription_count()!=0:raise RuntimeError('Stopped inlet still published')
 client.run_probe(account,3,False,link);peer();checks.append('UI stop closes owned inlet, withdraws subscription, leaves two peer users connected')
 with connect('gate') as ssh:
  status,_,_=run(ssh,'systemctl restart migate-gate.service')
  if status:raise RuntimeError('Owned Agent restart failed')
 time.sleep(3);client.run_probe(account,3,False,link)
 task({**payload,'id':iid})
 if subscription_count()!=0:raise RuntimeError('Legacy omitted-state edit republished disabled inlet')
 client.run_probe(account,3,False,link);checks.append('Agent restart and legacy edit omitting enabled retain disabled inlet and subscription withdrawal')
 ui(True)
 if subscription_count()!=1:raise RuntimeError('Enabled inlet missing subscription')
 new_link,_=client.get_link(account)
 if urllib.parse.urlsplit(link).username!=urllib.parse.urlsplit(new_link).username:raise RuntimeError('Toggle rotated credential')
 client.run_probe(account,3,True,link);peer();checks.append('UI start republishes same independent client credential and actual direct Gate connection')
finally:
 if gid:
  a.request('DELETE',f'/api/v1/admin/grants/{gid}')
  for _ in range(60):
   if next(g for g in a.request('GET','/api/v1/admin/grants')['items'] if g['id']==gid)['status']=='revoked':break
   time.sleep(1)
 if iid:
  t=a.request('POST',f'/api/v1/admin/devices/{did}/tasks',{'operation':'inbound.delete','payload':{'id':iid},'confirm':True});a.wait_task(t['id'])
 if account and account.get('id'):a.request('PATCH',f"/api/v1/admin/users/{account['id']}",{'enabled':False,'quota':account['quota'],'expires':0})
 private.unlink(missing_ok=True)
peer()
Path('docs/evidence/live-inbound-state.json').write_text(json.dumps({'version':a.request('GET','/api/v1/admin/overview')['version'],'checks':checks,'cleanup':'owned inlet removed, grant revoked and test user disabled; two peer users connect','visual':'four screenshots captured; human review pending'},indent=2),encoding='utf-8')
print('Real inlet stop/start, subscription withdrawal, Agent restart, legacy edit, credentials and isolation passed')
