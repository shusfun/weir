"""Real UI enable/disable, dependency failure and two-user isolation."""
import importlib.util,json,os,secrets,subprocess,time
from pathlib import Path
from live_api import API,OPS,state
spec=importlib.util.spec_from_file_location('client',Path(__file__).with_name('verify-live-client.py'));client=importlib.util.module_from_spec(spec);spec.loader.exec_module(client)
a=API().admin();s=state();did=s['device_id'];private=OPS/'resource-state-fixture.json';checks=[];oid=None;rid=None
def task(op,payload,success=True):
 r=a.request('POST',f'/api/v1/admin/devices/{did}/tasks',{'operation':op,'payload':payload,'confirm':True})
 if success:a.wait_task(r['id'])
 return r
def ui(tab,id,enabled,success=True):
 private.write_text(json.dumps({'tab':tab,'id':id,'enabled':enabled}),encoding='utf-8');os.chmod(private,0o600)
 result=subprocess.run(['node','deploy/browser-resource-state.cjs'],capture_output=True,text=True)
 if result.returncode:raise RuntimeError('Owned resource state UI failed; private details omitted')
 tid=json.loads(private.read_text(encoding='utf-8'))['task_id']
 if success:a.wait_task(tid)
 else:
  for _ in range(100):
   found=a.request('GET',f'/api/v1/admin/tasks?q={tid}')['items']
   t=next((v for v in found if v['id']==tid),None)
   if t and t['status']=='failed':break
   if t and t['status']=='applied':raise RuntimeError('Referenced outbound disabled')
   time.sleep(1)
  else:raise RuntimeError('Dependency failure receipt missing')
def probe(blocked):
 link,_=client.get_link(s['test_users'][0]);client.run_probe(s['test_users'][0],1,not blocked,link);client.run_probe(s['test_users'][1],2)
try:
 oid=task('outbound.upsert',{'tag':'owned-state-'+secrets.token_hex(4),'protocol':'blackhole'})['resource_id']
 rid=task('routing.upsert',{'outbound_id':oid,'client_id':s['test_users'][0]['grant_id'],'inbound_id':1})['resource_id'];probe(True)
 ui('outbounds',oid,False,False);probe(True);checks.append('UI disabling referenced outbound fails; applied state and two-user isolation retained')
 ui('routing',rid,False);probe(False);checks.append('UI route disable restores selected user; peer unaffected')
 ui('outbounds',oid,False);probe(False);checks.append('UI outbound disable succeeds only after route disabled')
 # A legacy edit omitting state cannot reactivate it.
 task('outbound.upsert',{'id':oid,'tag':'owned-state-preserved','protocol':'blackhole'})
 rows=a.request('GET',f'/api/v1/admin/resources/device-outbound?owner_id={did}')['items']
 if next(r for r in rows if r['data']['id']==oid)['data']['enabled'] is not False:raise RuntimeError('Legacy edit re-enabled outbound')
 ui('outbounds',oid,True);ui('routing',rid,True);probe(True);checks.append('omitted-state edit remains disabled; UI outbound then route enable restores selected deny')
finally:
 if rid:task('routing.delete',{'id':rid})
 if oid:task('outbound.delete',{'id':oid})
 private.unlink(missing_ok=True)
probe(False)
Path('docs/evidence/live-resource-state.json').write_text(json.dumps({'version':a.request('GET','/api/v1/admin/overview')['version'],'date':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),'checks':checks,'cleanup':'owned resources removed; two real clients restored','visual':'four widths captured; human review pending'},indent=2),encoding='utf-8')
print('Real resource state UI, dependency failure, legacy edit, two-user isolation and cleanup passed')
