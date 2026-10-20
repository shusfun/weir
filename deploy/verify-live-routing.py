"""A scoped test route must affect only its selected user, then restore cleanly."""
import importlib.util,json,time
from pathlib import Path
from live_api import API,state
spec=importlib.util.spec_from_file_location('client',Path(__file__).with_name('verify-live-client.py'));client=importlib.util.module_from_spec(spec);spec.loader.exec_module(client)
a=API().admin();s=state();did=s['device_id'];checks=[]
def task(op,payload):
 tid=a.request('POST',f'/api/v1/admin/devices/{did}/tasks',{'operation':op,'payload':payload,'confirm':True})['id']
 a.wait_task(tid);return tid
created=False;routed=False
try:
 task('outbound.upsert',{'id':900,'tag':'acceptance-deny','protocol':'blackhole'});created=True
 client.run_probe(s['test_users'][0],1)
 checks.append({'check':'adding outbound does not alter default direct flow','passed':True})
 task('routing.upsert',{'id':900,'outbound_id':900,'client_id':s['test_users'][1]['grant_id'],'inbound_id':1});routed=True
 link,_=client.get_link(s['test_users'][1])
 client.run_probe(s['test_users'][1],2,False,link);client.run_probe(s['test_users'][0],1)
 checks.append({'check':'route targets one grant while other shared-inbound user stays connected','passed':True})
finally:
 if routed:task('routing.delete',{'id':900})
 if created:task('outbound.delete',{'id':900})
client.run_probe(s['test_users'][1],2)
checks.append({'check':'route and outbound deletion restore real client connectivity','passed':True})
Path('docs/evidence/live-routing.json').write_text(json.dumps({'date':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),'version':a.request('GET','/api/v1/admin/overview')['version'],'checks':checks},indent=2))
print('Real scoped routing and deletion recovery passed')
