"""Actual HTTP source usage, exclusion, exact arithmetic and scoped preview."""
import json,subprocess,time
from pathlib import Path
from live_api import API,state
from remote import connect,run
fixture=r'''
from http.server import BaseHTTPRequestHandler,HTTPServer
from pathlib import Path
class H(BaseHTTPRequestHandler):
 def do_GET(self):
  if Path('/var/tmp/migate-usage-fixture.fail').exists():self.send_response(503);self.end_headers();return
  self.send_response(200)
  if self.path=='/a':value='upload=9007199254740993;download=1;total=9223372036854775807;expire=4102444800'
  elif self.path=='/b':value='upload=2;download=3;total=9223372036854775807;expire=4000000000'
  else:value='total=100;expire=0'
  self.send_header('Subscription-Userinfo',value);self.end_headers();self.wfile.write(b'trojan://synthetic@example.com:443#fixture')
 def log_message(self,*args):pass
HTTPServer(('0.0.0.0',24446),H).serve_forever()
'''
a=API().admin();owned=[];checks=[];before=a.request('GET','/api/v1/admin/sources/usage');version=a.request('GET','/api/v1/admin/overview')['version']
def save(kind,data):
 r=a.request('POST','/api/v1/admin/resources/'+kind,{'data':data});owned.append((kind,r['id']));return r['id']
def ui(mode):
 r=subprocess.run(['node','deploy/browser-source-usage.cjs',mode],capture_output=True,text=True)
 if r.returncode:raise RuntimeError('Usage UI failed; private context omitted')
def profile():return a.request('POST','/api/v1/admin/profiles/preview',{'id':pid,'target':'links'})['upstream_usage']
def source(id):return next(r['data'] for r in a.request('GET','/api/v1/admin/resources/sources')['items'] if r['id']==id)
with connect('gate') as ssh:
 def shell(cmd):
  code,out,_=run(ssh,cmd)
  if code:raise RuntimeError('Owned usage fixture operation failed')
  return out
 shell("python3 -c \"import socket;s=socket.socket();s.bind(('0.0.0.0',24446));s.close()\"")
 try:
  with ssh.open_sftp() as ftp:
   with ftp.open('/var/tmp/migate-usage-fixture.py','w') as f:f.write(fixture)
  shell('systemd-run --unit=migate-usage-fixture --collect /usr/bin/python3 /var/tmp/migate-usage-fixture.py');time.sleep(1)
  ids=[]
  for name in ['A','B']:
   sid=save('sources',{'name':'Owned usage source '+name,'url':'http://154.9.26.146:24446/'+name.lower(),'enabled':True,'refresh_seconds':3600,'allow_stale':True});a.request('POST',f'/api/v1/admin/sources/{sid}/refresh');ids.append(sid)
  aid,bid=ids;pid=save('profiles',{'name':'Owned usage exact group','source_ids':[aid,bid]})
  r=profile();assert r['total']=='18446744073709551614' and r['upload']=='9007199254740995' and r['download']=='4' and r['expire']=='4000000000' and r['status']=='available'
  checks.append('real HTTP and PG sum beyond signed int64 and JS precision; exact strings, earliest expiry')
  ui('panel')
  ui('exclude');r=profile();assert r['included']==1 and r['excluded']==1 and r['total']=='9223372036854775807' and r['expire']=='4000000000'
  assert source(bid)['links'] and source(bid)['upstream_usage']['download']=='3'
  checks.append('UI excludes B counters while keeping earliest expiry, inventory and observation; reopen persists')
  ui('include');r=profile();assert r['included']==2 and r['total']=='18446744073709551614'
  d=source(bid);d['enabled']=False;a.request('POST','/api/v1/admin/resources/sources',{'id':bid,'data':d})
  # A disabled source cannot be previewed as a usable inventory.
  a.request('POST','/api/v1/admin/profiles/preview',{'id':pid,'target':'links'},expected=(422,))
  checks.append('disabled source inventory still refused; aggregate disabled count separate')
  d['enabled']=True;a.request('POST','/api/v1/admin/resources/sources',{'id':bid,'data':d})
  missing=save('sources',{'name':'Owned usage missing counters','url':'http://154.9.26.146:24446/missing','enabled':True,'refresh_seconds':3600});a.request('POST',f'/api/v1/admin/sources/{missing}/refresh')
  shell('touch /var/tmp/migate-usage-fixture.fail');a.request('POST',f'/api/v1/admin/sources/{aid}/refresh',expected=(400,));r=profile()
  assert r['status']=='partial' and r['total'] is None and r['upload'] is None and r['remaining'] is None and r['expire'] is None
  ui('unknown');checks.append('503 bounded node fallback never converts stale upstream counters to fresh totals; UI unknown')
  u=API().acceptance_user(state()['test_users'][0]);u.request('GET','/api/v1/admin/sources/usage',expected=(403,));API().request('GET','/api/v1/admin/sources/usage',expected=(401,))
  user=u.request('GET','/api/v1/subscription');assert user['quota']<9223372036854775807
  checks.append('ordinary and anonymous summary rejected; user quota remains independent')
 finally:
  for kind,id in reversed(owned):a.request('DELETE',f'/api/v1/admin/resources/{kind}/{id}')
  shell('systemctl stop migate-usage-fixture.service || true');shell('rm -f -- /var/tmp/migate-usage-fixture.py /var/tmp/migate-usage-fixture.fail')
after=a.request('GET','/api/v1/admin/sources/usage')
for k in ['included','excluded','disabled']:assert before[k]==after[k]
Path('docs/evidence/live-source-usage.json').write_text(json.dumps({'version':version,'date':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),'checks':checks,'cleanup':'owned source/profile inventory and HTTP service removed; original counts restored','visual':'four widths captured; manual review pending'},indent=2),encoding='utf-8')
print('Real upstream usage, UI exclusion, exact sum, unknown state and permission isolation passed')
