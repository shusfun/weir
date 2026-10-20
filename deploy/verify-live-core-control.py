"""Scoped owned-core lifecycle checks. Always restore service before exiting."""
import importlib.util,json,time
from pathlib import Path
from live_api import API,state
from remote import connect,run

spec=importlib.util.spec_from_file_location('clients',Path(__file__).with_name('verify-live-client.py'));clients=importlib.util.module_from_spec(spec);spec.loader.exec_module(clients)
a=API().admin();s=state();did=s['device_id'];checks=[];version=a.request('GET','/api/v1/admin/overview')['version']
def task(op,core=None):
 t=a.request('POST',f'/api/v1/admin/devices/{did}/tasks',{'operation':op,'payload':{'core':core} if core else {},'confirm':True});return a.wait_task(t['id'])
def service(c,core,expected):
 code,out,err=run(c,'systemctl is-active migate-gate-'+core+'.service')
 if out.strip()!=expected:raise RuntimeError('Unexpected managed core state: '+out.strip())
link,_=clients.get_link(s['test_users'][0])
with connect('gate') as c:
 for core in ['sing-box','xray']:
  try:
   task('core.stop',core);service(c,core,'inactive')
   if core=='xray':clients.run_probe(s['test_users'][0],1,False,link)
   else:clients.run_probe(s['test_users'][0],1)
   code,out,err=run(c,'systemctl restart migate-gate.service')
   if code:raise RuntimeError('Cannot restart owned Agent')
   time.sleep(4);service(c,core,'inactive')
   checks.append({'check':core+' stop persists across actual Agent restart','passed':True})
   task('core.start',core);time.sleep(3);service(c,core,'active')
   clients.run_probe(s['test_users'][0],1)
   checks.append({'check':core+' start restores real client path; other owned core preserved','passed':True})
  finally:
   task('core.start',core)
 observed=task('core.status')['result']
 if len(observed.get('items',[]))!=2 or not observed.get('observed_at'):raise RuntimeError('Missing real core observation')
 code,out,err=run(c,'systemctl is-active relay154-x-ui.service')
 if code or out.strip()!='active':raise RuntimeError('Existing x-ui unavailable')
 checks.append({'check':'core status contains two real version/service observations; x-ui remains active','passed':True})
Path('docs/evidence/live-core-control.json').write_text(json.dumps({'version':version,'date':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),'checks':checks},indent=2),encoding='utf-8')
print('Actual core stop/start, Agent restart persistence, client recovery and x-ui checks passed')
