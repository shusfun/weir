"""Actual source protection with an owned bounded HTTP fixture; no user grants."""
import json,subprocess,time
from pathlib import Path
from live_api import API
from remote import connect,run

fixture=r'''
from http.server import BaseHTTPRequestHandler,HTTPServer
from pathlib import Path
import json,urllib.parse
class H(BaseHTTPRequestHandler):
 def do_GET(self):
  mode=Path('/var/tmp/migate-cache-fixture-mode').read_text().strip()
  q=urllib.parse.parse_qs(urllib.parse.urlsplit(self.path).query)
  Path('/var/tmp/migate-cache-fixture-request').write_text(json.dumps({'timestamp':bool(q.get('_t')),'preserved':q.get('fixture')==['a+b']}))
  if mode=='fail':self.send_response(503);self.end_headers();return
  self.send_response(200);self.send_header('Subscription-Userinfo','download='+('10' if mode=='10' else '99')+';total=1024');self.end_headers()
  n=int(mode)
  self.wfile.write(('\n'.join('trojan://synthetic@'+('127.0.0.1' if n==0 else 'example.com')+':443#node-'+str(i) for i in range(max(n,1)))).encode())
 def log_message(self,*args):pass
HTTPServer(('0.0.0.0',24446),H).serve_forever()
'''

a=API().admin();owned=[];checks=[];version=a.request('GET','/api/v1/admin/overview')['version']
def save(kind,data):
 r=a.request('POST','/api/v1/admin/resources/'+kind,{'data':data});owned.append((kind,r['id']));return r['id']
def ui(enabled):
 r=subprocess.run(['node','deploy/browser-source-protection.cjs',str(sid),'on' if enabled else 'off'],capture_output=True,text=True)
 if r.returncode:raise RuntimeError('Protective cache browser workflow failed; private details omitted')
def data():return next(r['data'] for r in a.request('GET','/api/v1/admin/resources/sources')['items'] if r['id']==sid)
def preview(expected=(200,)):
 return a.request('POST','/api/v1/admin/profiles/preview',{'id':pid,'target':'links'},expected=expected)
with connect('gate') as ssh:
 def shell(cmd):
  code,out,_=run(ssh,cmd)
  if code:raise RuntimeError('Owned protective fixture operation failed')
  return out
 def mode(value):
  with ssh.open_sftp() as f:
   with f.open('/var/tmp/migate-cache-fixture-mode','w') as o:o.write(str(value))
 def refresh(ok=True):return a.request('POST',f'/api/v1/admin/sources/{sid}/refresh',expected=(200,) if ok else (400,))
 def request_flags():return json.loads(shell('cat /var/tmp/migate-cache-fixture-request'))
 shell("python3 -c \"import socket;s=socket.socket();s.bind(('0.0.0.0',24446));s.close()\"")
 try:
  with ssh.open_sftp() as f:
   with f.open('/var/tmp/migate-cache-fixture.py','w') as o:o.write(fixture)
  mode(10);shell('systemd-run --unit=migate-cache-fixture --collect /usr/bin/python3 /var/tmp/migate-cache-fixture.py');time.sleep(1)
  sid=save('sources',{'name':'Owned protective cache fixture','url':'http://154.9.26.146:24446/sub?fixture=a%2Bb','enabled':False,'allow_stale':False,'refresh_seconds':3600})
  # Disabled periodic refresh avoids background races; enable after UI save.
  pid=save('profiles',{'name':'Owned cache group','source_ids':[sid]});ui(True)
  d=data();d['enabled']=True;a.request('POST','/api/v1/admin/resources/sources',{'id':sid,'data':d});refresh();good=data()
  assert good['node_count']==10 and request_flags()=={'timestamp':True,'preserved':True}
  checks.append('actual UI enable/save/reopen; HTTP _t bypass and original query preserved')
  for value in [2,0,'fail']:
   mode(value);refresh(False);d=data();p=preview()
   assert d['node_count']==10 and d['last_update']==good['last_update'] and d['upstream_usage']==good['upstream_usage'] and p['node_count']==10 and p['warnings']==[f'source:{sid}:stale_cache']
  checks.append('10 to 2 shrink, local-only placeholder and HTTP 503 retain actual PG cache/usage with explicit stale warning')
  assert data()['last_error']=='refresh_failed'
  # The application must reject fallback once the real 60-second deadline passes.
  deadline=time.monotonic()+65
  while time.monotonic()<deadline:time.sleep(1)
  preview((422,));checks.append('real elapsed cache deadline refuses stale output; no timestamp injection')
  mode(3);refresh();d=data();assert d['node_count']==3 and not d['last_error'] and not preview()['warnings']
  checks.append('exact 30 percent boundary accepted and fresh state recovers')
  ui(False);mode('fail');refresh(False);preview((422,));assert request_flags()=={'timestamp':False,'preserved':True}
  checks.append('UI disable restores no-cache-bypass and default stale refusal')
  a.request('GET','/api/v1/admin/resources/sources')
 finally:
  for kind,id in reversed(owned):a.request('DELETE',f'/api/v1/admin/resources/{kind}/{id}')
  shell('systemctl stop migate-cache-fixture.service || true')
  shell('rm -f -- /var/tmp/migate-cache-fixture.py /var/tmp/migate-cache-fixture-mode /var/tmp/migate-cache-fixture-request')
Path('docs/evidence/live-source-protection.json').write_text(json.dumps({'version':version,'date':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),'checks':checks,'cleanup':'owned HTTP service and inventory resources removed','visual':'four widths captured; manual review pending'},indent=2),encoding='utf-8')
print('Real protective source cache, shrink/placeholder/503, deadline and UI state passed')
