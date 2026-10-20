"""Owned two-rule priority exercise; actual clients before/after and restart."""
import importlib.util,json,subprocess,time
from pathlib import Path
from live_api import API,state
from remote import connect,run
spec=importlib.util.spec_from_file_location('client',Path(__file__).with_name('verify-live-client.py'));client=importlib.util.module_from_spec(spec);spec.loader.exec_module(client)
a=API().admin();s=state();did=s['device_id'];owned=[];checks=[];routes=[];baseline={}

def task(op,payload):
 result=a.request('POST',f'/api/v1/admin/devices/{did}/tasks',{'operation':op,'payload':payload,'confirm':True})
 a.wait_task(result['id']);return result

try:
 for _ in range(40):
  devices=a.request('GET','/api/v1/admin/devices')['items'];device=next(d for d in devices if d['id']==did)
  if 'routing.order.v1' in device.get('metadata',{}).get('capabilities',[]):break
  time.sleep(1)
 else:raise RuntimeError('Agent has not advertised route-order capability')
 before=a.request('GET','/api/v1/admin/resources/device-routing')['items'];routes=[r for r in before if r.get('owner_id')==did]
 if len(routes)>50:raise RuntimeError('Too many existing routes for scoped live priority exercise')
 baseline={int(r['data']['id']):int(r['data'].get('sort',0)) for r in routes}
 minsort=min([r['data'].get('sort',0) for r in routes]+[0])-20
 for tag,protocol in [('acceptance-order-deny','blackhole'),('acceptance-order-direct','freedom')]:
  result=task('outbound.upsert',{'id':0,'tag':tag+'-'+str(int(time.time())),'protocol':protocol})
  owned.append(('outbound',result['resource_id']))
 deny_out,direct_out=owned[0][1],owned[1][1]
 for outbound,priority in [(deny_out,minsort),(direct_out,minsort+10)]:
  result=task('routing.upsert',{'id':0,'outbound_id':outbound,'client_id':s['test_users'][1]['grant_id'],'inbound_id':1,'domain':'full:www.cloudflare.com','sort':priority})
  owned.append(('routing',result['resource_id']))
 direct_rule=owned[-1][1]
 link,_=client.get_link(s['test_users'][1]);client.run_probe(s['test_users'][1],2,False,link)
 client.run_probe(s['test_users'][0],1)
 checks.append('deny priority blocks only selected user; other shared-inbound user still exits through Gate')
 subprocess.run(['node','deploy/browser-route-order.cjs',str(direct_rule)],check=True)
 client.run_probe(s['test_users'][1],2);client.run_probe(s['test_users'][0],1)
 checks.append('UI priority reorder applied; both independent real Xray clients exit directly via Gate')
 # A service restart reconstructs State from SQLite and reapplies actual configs.
 with connect('gate') as c:
  code,out,err=run(c,'systemctl restart migate-gate.service && systemctl is-active migate-gate.service',timeout=45)
  if code or out.strip()!='active':raise RuntimeError('Agent restart failed')
 for _ in range(15):
  try:client.run_probe(s['test_users'][1],2);break
  except Exception:
   if _==14:raise
   time.sleep(1)
 checks.append('Agent restart preserves priority from SQLite and actual client remains connected')
finally:
 # Restore even when a UI/client assertion fails. Never skip owned cleanup
 # because one restoration failed; report every incomplete operation.
 cleanup_errors=[]
 for r in routes:
  try:task('routing.upsert',{**r['data'],'sort':baseline[int(r['data']['id'])]})
  except Exception as e:cleanup_errors.append(type(e).__name__+' restoring existing route')
 for kind,id in reversed(owned):
  try:task(kind+'.delete',{'id':id})
  except Exception as e:cleanup_errors.append(type(e).__name__+' removing owned '+kind)
 if cleanup_errors:raise RuntimeError('; '.join(cleanup_errors))
client.run_probe(s['test_users'][0],1);client.run_probe(s['test_users'][1],2)
checks.append('owned routes/outbounds removed, original priorities restored, both users reconnect')
Path('docs/evidence/live-route-order.json').write_text(json.dumps({'date':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),'version':a.request('GET','/api/v1/admin/overview')['version'],'checks':checks},indent=2),encoding='utf-8')
print('Real priority switch, user isolation, Agent restart and scoped restoration passed')
