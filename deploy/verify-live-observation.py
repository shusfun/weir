"""Stop only owned control transport; observe unknown then recovered data quality."""
import json,time
from pathlib import Path
from live_api import API,state
from remote import connect,run
a=API().admin();did=state()['device_id'];records=[]
def event():
 items=a.request('GET','/api/v1/admin/events')['items']
 return next((x['data'] for x in items if x['kind']=='security-event' and x['key']==f'device:{did}'),None)
with connect('gate') as c:
 code,_,_=run(c,'systemctl stop migate-gate-tunnel.service')
 if code:raise RuntimeError('Cannot stop owned control transport')
 try:
  deadline=time.monotonic()+100
  while time.monotonic()<deadline:
   observed=event()
   if observed and observed['status']=='unknown':break
   time.sleep(3)
  else:raise RuntimeError('Missing heartbeat was not marked unknown')
  records.append({'check':'missing heartbeat produces deterministic warning without Laya','observed':observed,'passed':True})
 finally:
  code,_,_=run(c,'systemctl start migate-gate-tunnel.service')
  if code:raise RuntimeError('Cannot restart owned control transport')
 deadline=time.monotonic()+65
 while time.monotonic()<deadline:
  observed=event()
  if observed and observed['status']=='resolved':break
  time.sleep(3)
 else:raise RuntimeError('Restored heartbeat did not resolve warning')
 records.append({'check':'restored heartbeat resolves observation with timestamp','observed':observed,'passed':True})
Path('docs/evidence/live-observation.json').write_text(json.dumps({'date':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),'version':'0.2.0-rc11','records':records,'laya':'not configured; deterministic observation remains operational'},indent=2))
print('Real offline and recovered observation without Laya passed')

