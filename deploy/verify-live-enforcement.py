"""Real-core disable, expiry and offline quota checks, scoped to test users."""
import json
from pathlib import Path
import secrets
import shlex
import time
from live_api import API,state,save_state
from remote import connect,run
import importlib.util
spec=importlib.util.spec_from_file_location('live_client',Path(__file__).with_name('verify-live-client.py'))
client=importlib.util.module_from_spec(spec);spec.loader.exec_module(client)
get_link,run_probe=client.get_link,client.run_probe

api=API().admin();s=state();results=[];version=api.request('GET','/api/v1/admin/overview')['version']
def wait_grant(gid):
 until=time.monotonic()+60
 while time.monotonic()<until:
  g=next(x for x in api.request('GET','/api/v1/admin/grants')['items'] if x['id']==gid)
  if g['status']=='applied':return
  if g['status']=='failed':raise RuntimeError('Grant application failed')
  time.sleep(1)
 raise TimeoutError('Grant application timeout')
def check_tasks():
 until=time.monotonic()+60
 while time.monotonic()<until:
  tasks=api.request('GET','/api/v1/admin/tasks')['items']
  if not any(t['device_id']==s['device_id'] and t['status'] in ['pending','running'] for t in tasks):return
  time.sleep(1)
 raise TimeoutError('Tasks did not settle')
def test_user(label,quota,expires=0):
 entry={'email':f'{label}-{secrets.token_hex(3)}@sub.caowo.baby','password':secrets.token_urlsafe(24)}
 entry['id']=api.request('POST','/api/v1/admin/users',{**entry,'quota':quota,'expires':expires})['id']
 entry['grant_id']=api.request('POST','/api/v1/admin/grants',{'user_id':entry['id'],'device_id':s['device_id'],'inbound_id':1})['id']
 s.setdefault('enforcement_users',[]).append(entry);save_state(s)
 wait_grant(entry['grant_id']);return entry

user=s['test_users'][1];link,_=get_link(user)
try:
 api.request('PATCH',f"/api/v1/admin/users/{user['id']}",{'enabled':False});check_tasks()
 run_probe(user,2,False,link);run_probe(s['test_users'][0],1)
 results.append({'check':'disabled credential rejected while other shared-inbound user works','passed':True})
finally:
 api.request('PATCH',f"/api/v1/admin/users/{user['id']}",{'enabled':True});check_tasks()
run_probe(user,2)
expiry=test_user('expiry',32<<20,int(time.time())+20);expiry_link,_=get_link(expiry)
quota=test_user('quota',256<<10);quota_link,_=get_link(quota)
with connect('gate') as gate:
 code,out,err=run(gate,"ss -H -lnt 'sport = :24446'")
 if code or out.strip():raise RuntimeError('Acceptance fixture port 24446 is occupied')
 fixture_path='/acceptance-'+secrets.token_urlsafe(24)
 fixture='''from http.server import BaseHTTPRequestHandler,HTTPServer
class Handler(BaseHTTPRequestHandler):
 def do_GET(self):
  if self.path != FIXTURE_PATH:self.send_error(404);return
  data=b'x'*(1<<20)
  self.send_response(200);self.send_header('Content-Length',str(len(data)));self.end_headers()
  try:self.wfile.write(data)
  except (BrokenPipeError,ConnectionResetError):pass
 def log_message(self,*args):pass
HTTPServer(('0.0.0.0',24446),Handler).serve_forever()
'''
 code,out,err=run(gate,'systemd-run --unit=migate-acceptance-download --collect --property=User=nobody /usr/bin/python3 -c '+shlex.quote('FIXTURE_PATH='+repr(fixture_path)+'\n'+fixture))
 if code:raise RuntimeError('Cannot start isolated download fixture')
 def meter():
  script="import sqlite3,json;d=sqlite3.connect('/var/lib/migate-gate/agent.db');s=json.loads(d.execute('select data from state where id=1').fetchone()[0]);g=next(x for x in s['grants'] if x['grant_id']=="+str(quota['grant_id'])+");print(json.dumps({k:g[k] for k in ['used','allocated','enabled']}))"
  code,out,err=run(gate,'python3 -c '+shlex.quote(script))
  if code:raise RuntimeError('Cannot read local meter evidence')
  return json.loads(out)
 code,out,err=run(gate,'systemctl stop migate-gate-tunnel.service')
 if code:raise RuntimeError(err)
 try:
  # Agent remains alive; only the center transport is disconnected.
  transfer_error=None
  try:run_probe(quota,4,True,quota_link,'http://154.9.26.146:24446'+fixture_path)
  except Exception as error:transfer_error=type(error).__name__
  deadline=time.monotonic()+12
  while time.monotonic()<deadline:
   measured=meter()
   if measured['used']>=measured['allocated'] and not measured['enabled']:break
   time.sleep(1)
  else:raise RuntimeError('Quota was not exhausted: '+json.dumps(measured)+'; transfer error='+str(transfer_error))
  run_probe(quota,4,False,quota_link)
  results.append({'check':'offline local quota exhaustion rejects new real client connections','passed':True,'quota_bytes':256<<10,'local_meter':measured,'sampling_error':'one-second sampling plus in-flight traffic'})
  time.sleep(22)
  run_probe(expiry,3,False,expiry_link)
  results.append({'check':'offline local expiry rejects real client connection','passed':True})
  code,out,err=run(gate,'systemctl restart migate-gate.service; systemctl is-active migate-gate.service')
  if code:raise RuntimeError(err)
  time.sleep(3)
  run_probe(quota,4,False,quota_link)
  results.append({'check':'offline Agent restart preserves exhausted budget','passed':True})
 finally:
  code,out,err=run(gate,'systemctl start migate-gate-tunnel.service')
  run(gate,'systemctl stop migate-acceptance-download.service')
  for item in [expiry,quota]:api.request('PATCH',f"/api/v1/admin/users/{item['id']}",{'enabled':False})
  if code:raise RuntimeError(err)
for item in [expiry,quota]:api.request('PATCH',f"/api/v1/admin/users/{item['id']}",{'enabled':False})
check_tasks()
run_probe(s['test_users'][0],1)
results.append({'check':'transport reconnect and unaffected user recovery','passed':True})
Path('docs/evidence/live-enforcement.json').write_text(json.dumps({'version':version,'date':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),'results':results},indent=2))
print('Live disable, expiry, offline quota, restart and reconnect acceptance passed')
