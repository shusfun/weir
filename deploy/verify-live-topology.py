"""Named scoped route from UI; actual clients and retained graph relationships."""
import importlib.util,json,os,secrets,subprocess,time
from pathlib import Path
from live_api import API,OPS,state
spec=importlib.util.spec_from_file_location('client',Path(__file__).with_name('verify-live-client.py'));client=importlib.util.module_from_spec(spec);spec.loader.exec_module(client)
a=API().admin();s=state();did=s['device_id'];private=OPS/'topology-fixture.json';f={'tag':'owned-topology-'+secrets.token_hex(4),'grant_id':s['test_users'][0]['grant_id'],'other_grant_id':s['test_users'][1]['grant_id'],'user_email':s['test_users'][0]['email']};oid=None
def task(op,payload):
 r=a.request('POST',f'/api/v1/admin/devices/{did}/tasks',{'operation':op,'payload':payload,'confirm':True});a.wait_task(r['id']);return r
def browser(mode):
 f['mode']=mode;private.write_text(json.dumps(f),encoding='utf-8');os.chmod(private,0o600)
 result=subprocess.run(['node','deploy/browser-topology.cjs'],capture_output=True,text=True);f.update(json.loads(private.read_text(encoding='utf-8')))
 if result.returncode:
  for line in result.stderr.splitlines():
   if line.startswith('Topology workflow failed at '):print(line)
  raise RuntimeError('Topology real browser failed; private details omitted')
try:
 oid=task('outbound.upsert',{'tag':f['tag'],'protocol':'blackhole'})['resource_id']
 rows=a.request('GET',f'/api/v1/admin/resources/device-inbound?owner_id={did}')['items'];f['inbound_name']=next(r['data']['remark'] for r in rows if r['data']['id']==1)
 browser('create-route');a.wait_task(f['task_id'])
 link,_=client.get_link(s['test_users'][0]);client.run_probe(s['test_users'][0],1,False,link);client.run_probe(s['test_users'][1],2)
 before=a.request('GET','/api/v1/admin/tasks')['total'];browser('canvas');after=a.request('GET','/api/v1/admin/tasks')['total']
 if before!=after:raise RuntimeError('Read-only canvas created task')
finally:
 if f.get('route_id'):task('routing.delete',{'id':f['route_id']})
 if oid:task('outbound.delete',{'id':oid})
 private.unlink(missing_ok=True)
client.run_probe(s['test_users'][0],1)
Path('docs/evidence/live-topology.json').write_text(json.dumps({'version':a.request('GET','/api/v1/admin/overview')['version'],'date':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),'checks':['UI search email, select Gate/inbound/grant/outbound by name; changing inlet clears grant','actual selected-user deny and peer connection passed','actual graph edge belongs only to chosen grant; missing up/down shown unknown','zoom in/out, fit, layout, filter, keyboard Enter and resource navigation passed','canvas is read only; task count unchanged','owned route/outbound removed; connection restored'],'visual':'four widths captured; human review pending'},indent=2),encoding='utf-8')
print('Real named client routing, graph read-only interactions, two-user isolation and cleanup passed')
