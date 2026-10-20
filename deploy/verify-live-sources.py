"""Real source fetch/cache/preview on the deployed center; synthetic nodes only.

An owned temporary HTTP fixture on Gate port 24446 is removed in finally.
No upstream subscription credentials or ordinary-user grants are used.
"""
import json,shlex,time,subprocess
from pathlib import Path
from live_api import API
from remote import connect,run

fixture='''from http.server import BaseHTTPRequestHandler,HTTPServer
import json
from pathlib import Path
class Handler(BaseHTTPRequestHandler):
 def do_GET(self):
  if self.path=='/ua' and self.headers.get('User-Agent')!='MiGate-Acceptance/69':
   self.send_response(403);self.end_headers();return
  if Path('/var/tmp/migate-source-fixture.fail').exists():
   self.send_response(503);self.end_headers();return
  self.send_response(200)
  if self.path=='/invalid-metadata':self.send_header('Subscription-Userinfo','upload=-1;total=1024')
  elif self.path!='/no-metadata':self.send_header('Subscription-Userinfo','upload=128;download=256;total=1024;expire=4102444800;private=must-not-store')
  self.end_headers()
  if self.path=='/clash':
   self.wfile.write(('proxies:'+chr(10)+' - {name: HK, type: ss, server: example.com, port: 8388, cipher: aes-128-gcm, password: pass}').encode())
  elif self.path=='/singbox':
   self.wfile.write(json.dumps({'outbounds':[{'type':'http','tag':'HTTP','server':'example.org','server_port':8080,'username':'user','password':'synthetic'}]}).encode())
  elif self.path=='/surge':
   self.wfile.write(b'[Proxy]\\nHK = ss, example.com, 8388, encrypt-method=aes-128-gcm, password="synthetic", udp-relay=true\\n[Rule]\\nFINAL,DIRECT')
  elif self.path=='/loon':
   self.wfile.write(b'[Proxy]\\nHK = Shadowsocks, example.com, 8388, aes-128-gcm, "synthetic"')
  elif self.path=='/quanx':
   self.wfile.write(b'[server_local]\\nshadowsocks=example.com:8388, method=aes-128-gcm, password=synthetic, udp-relay=true, tag=HK')
  elif self.path=='/names':
   self.wfile.write(b'trojan://p+a%2B%2525@example.com:443?security=tls&type=ws&path=%2Fa%2Bb%252B&host=example.com#HK+%2B+%252B')
  elif self.path=='/filters':
   self.wfile.write(('trojan://synthetic@example.com:443#香港-A'+chr(10)+'trojan://synthetic@example.com:443#香港-Expire'+chr(10)+'http://u:synthetic@example.com:8080#日本-C'+chr(10)+'ss://YWVzLTEyOC1nY206c3ludGhldGlj@example.com:8388#US-D').encode())
  else:self.wfile.write(b'ss://YWVzLTEyOC1nY206cGFzcw@example.com:8388#HK')
 def log_message(self,*args):pass
HTTPServer(('0.0.0.0',24446),Handler).serve_forever()
'''
a=API().admin();owned=[];records=[];version=a.request('GET','/api/v1/admin/overview')['version']
def record(check):records.append({'check':check,'passed':True});print(check,flush=True)
def save(kind,data,id=0):
 result=a.request('POST','/api/v1/admin/resources/'+kind,{'data':data,'id':id})
 if not id:owned.append((kind,result['id']))
 return result
with connect('gate') as gate:
 def shell(command):
  code,out,err=run(gate,command)
  if code:raise RuntimeError('Owned fixture operation failed: '+err[-500:])
  return out
 shell("python3 -c \"import socket;s=socket.socket();s.bind(('0.0.0.0',24446));s.close()\"")
 with gate.open_sftp() as ftp:
  with ftp.open('/var/tmp/migate-source-fixture.py','w') as f:f.write(fixture)
 try:
  shell('systemd-run --unit=migate-source-fixture --collect /usr/bin/python3 /var/tmp/migate-source-fixture.py')
  shell("python3 - <<'PY'\nimport time,urllib.request\nfor i in range(30):\n try:\n  urllib.request.urlopen('http://127.0.0.1:24446/sub',timeout=1).read();break\n except Exception:\n  if i==29:raise RuntimeError('Owned source fixture failed readiness')\n  time.sleep(0.2)\nPY")
  source=save('sources',{'name':'Acceptance source fixture','url':'http://154.9.26.146:24446/sub','enabled':True,'refresh_seconds':60,'allow_stale':False})
  sid=source['id']
  group=save('profiles',{'name':'Acceptance source group','source_ids':[sid]})
  a.request('POST',f'/api/v1/admin/sources/{sid}/refresh')
  preview=a.request('POST','/api/v1/admin/profiles/preview',{'id':group['id'],'target':'clash'})
  assert preview['node_count']==1 and not preview['warnings'] and 'example.com' in preview['output']
  record('real HTTP source refresh and referenced group conversion')
  item=next(r for r in a.request('GET','/api/v1/admin/resources/sources')['items'] if r['id']==sid)
  assert item['data']['upstream_usage']=={'status':'available','upload':'128','download':'256','total':'1024','expire':'4102444800'}
  subprocess.run(['node','deploy/browser-source-metadata.cjs',str(sid),'available'],check=True)
  record('upstream-reported counters and expiry preserved through HTTP, PG and UI; private extensions excluded')
  shell('touch /var/tmp/migate-source-fixture.fail')
  a.request('POST',f'/api/v1/admin/sources/{sid}/refresh',expected=(400,))
  a.request('POST','/api/v1/admin/profiles/preview',{'id':group['id'],'target':'links'},expected=(422,))
  record('upstream 503 preserves cache but default policy refuses stale output')
  items=a.request('GET','/api/v1/admin/resources/sources')['items']
  item=next(r for r in items if r['id']==sid)
  assert item['data']['node_count']==1 and item['data']['last_error']=='refresh_failed'
  assert item['data']['upstream_usage']['download']=='256'
  subprocess.run(['node','deploy/browser-source-metadata.cjs',str(sid),'unknown'],check=True)
  record('failed refresh retains last observation but UI marks current upstream data unknown')
  data=item['data'];data['allow_stale']=True;data['max_stale_seconds']=3600
  save('sources',data,sid)
  preview=a.request('POST','/api/v1/admin/profiles/preview',{'id':group['id'],'target':'links'})
  assert preview['node_count']==1 and preview['warnings']==[f'source:{sid}:stale_cache']
  record('explicit bounded stale fallback carries visible source warning')
  shell('rm -- /var/tmp/migate-source-fixture.fail')
  deadline=time.monotonic()+90
  while time.monotonic()<deadline:
   item=next(r for r in a.request('GET','/api/v1/admin/resources/sources')['items'] if r['id']==sid)
   if not item['data']['last_error']:break
   time.sleep(3)
  else:raise RuntimeError('scheduled source refresh did not recover')
  record('background schedule retries and recovers without manual refresh')
  data=item['data'];data['url']='http://154.9.26.146:24446/different'
  save('sources',data,sid)
  item=next(r for r in a.request('GET','/api/v1/admin/resources/sources')['items'] if r['id']==sid)
  assert 'links' not in item['data'] and 'last_update' not in item['data']
  assert 'upstream_usage' not in item['data']
  record('source URL change invalidates previous inventory and cached credentials')
  a.request('POST',f'/api/v1/admin/sources/{sid}/refresh')
  subprocess.run(['node','deploy/browser-source-options.cjs',str(sid)],check=True)
  data=next(r for r in a.request('GET','/api/v1/admin/resources/sources')['items'] if r['id']==sid)['data']
  data['url']='http://154.9.26.146:24446/ua';save('sources',data,sid)
  a.request('POST',f'/api/v1/admin/sources/{sid}/refresh')
  preview=a.request('POST','/api/v1/admin/profiles/preview',{'id':group['id'],'target':'singbox'})
  assert preview['node_count']==1 and not preview['warnings']
  record('actual UI request options persist, invalidate prior cache and custom User-Agent reaches Gate HTTP upstream')
  data['url']='http://154.9.26.146:24446/names';data['plus_as_space']=False;save('sources',data,sid)
  a.request('POST',f'/api/v1/admin/sources/{sid}/refresh')
  subprocess.run(['node','deploy/browser-source-names.cjs',str(sid),str(group['id'])],check=True)
  record('actual URI name toggle invalidates cache; browser refresh/group output preserves encoded plus, credentials and path; disable restores literal plus')
  for format in ['clash','singbox','surge','loon','quanx']:
   data=item['data'];data['url']='http://154.9.26.146:24446/'+format
   save('sources',data,sid)
   a.request('POST',f'/api/v1/admin/sources/{sid}/refresh')
   preview=a.request('POST','/api/v1/admin/profiles/preview',{'id':group['id'],'target':'singbox'})
   assert preview['node_count']==1 and not preview['warnings']
   converted=json.loads(preview['output'])['outbounds'][1]
   assert converted['password']==('pass' if format=='clash' else 'synthetic')
   record('real '+format+' source import preserves credentials through conversion')
  data=item['data'];data['url']='http://154.9.26.146:24446/filters';save('sources',data,sid)
  a.request('POST',f'/api/v1/admin/sources/{sid}/refresh')
  subprocess.run(['node','deploy/browser-source-filters.cjs',str(sid),str(group['id'])],check=True)
  record('actual visual source name/protocol filters, advanced validation failure/recovery, cache retention and group conversion passed')
  for path,status in [('invalid-metadata','invalid'),('no-metadata','absent')]:
   data=item['data'];data['url']='http://154.9.26.146:24446/'+path
   save('sources',data,sid)
   a.request('POST',f'/api/v1/admin/sources/{sid}/refresh')
   item=next(r for r in a.request('GET','/api/v1/admin/resources/sources')['items'] if r['id']==sid)
   assert item['data']['upstream_usage']=={'status':status}
   subprocess.run(['node','deploy/browser-source-metadata.cjs',str(sid),'unknown'],check=True)
   record(path+' does not fabricate zero counters or mark observation healthy')
 finally:
  for kind,id in reversed(owned):a.request('DELETE',f'/api/v1/admin/resources/{kind}/{id}')
  shell('systemctl stop migate-source-fixture.service; rm -f -- /var/tmp/migate-source-fixture.py /var/tmp/migate-source-fixture.fail')
Path('docs/evidence/live-sources.json').write_text(json.dumps({'date':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),'version':version,'fixture':'temporary owned Gate HTTP service; synthetic nodes, no user grants','records':records},indent=2))
