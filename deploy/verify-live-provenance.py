"""Owned real HTTP source and duplicate manual endpoint: per-occurrence metadata."""
import json,subprocess,time
from pathlib import Path
from live_api import API,state
from remote import connect,run

a=API().admin();owned=[];checks=[];version=a.request('GET','/api/v1/admin/overview')['version']
u=API().acceptance_user(state()['test_users'][0]);before=u.request('GET','/api/v1/subscription')
def save(kind,data):
 r=a.request('POST','/api/v1/admin/resources/'+kind,{'data':data});owned.append((kind,r['id']));return r['id']
fixture="""from http.server import BaseHTTPRequestHandler,HTTPServer
class H(BaseHTTPRequestHandler):
 def do_GET(self):
  self.send_response(200);self.end_headers();self.wfile.write(b'trojan://synthetic@example.com:443#original')
 def log_message(self,*args):pass
HTTPServer(('0.0.0.0',24446),H).serve_forever()
"""
with connect('gate') as ssh:
 def shell(cmd):
  code,_,_=run(ssh,cmd)
  if code:raise RuntimeError('Owned provenance fixture operation failed')
 shell("python3 -c \"import socket;s=socket.socket();s.bind(('0.0.0.0',24446));s.close()\"")
 try:
  with ssh.open_sftp() as ftp:
   with ftp.open('/var/tmp/migate-provenance-fixture.py','w') as f:f.write(fixture)
  shell('systemd-run --unit=migate-provenance-fixture --collect /usr/bin/python3 /var/tmp/migate-provenance-fixture.py');time.sleep(1)
  sid=save('sources',{'name':'Owned provenance source','url':'http://154.9.26.146:24446/','enabled':True,'refresh_seconds':3600});a.request('POST',f'/api/v1/admin/sources/{sid}/refresh')
  nid=save('nodes',{'name':'Owned provenance manual','group':'A','url':'trojan://synthetic@example.com:443#original'})
  ops=[{'type':'sort','params':{'keys':[{'key':'group'}]}},{'type':'rename','params':{'template':{'enabled':True,'template':'{group}-{provider}-{originalName}'}}}]
  oid=save('operators',{'name':'Owned provenance chain','operators':ops})
  pid=save('profiles',{'name':'Owned provenance group','source_ids':[sid],'node_ids':[nid],'operator_ids':[oid]})
  r=subprocess.run(['node','deploy/browser-provenance.cjs'],capture_output=True,text=True)
  if r.returncode:raise RuntimeError('Actual provenance UI failed; private details omitted')
  reply=a.request('POST','/api/v1/admin/profiles/preview',{'id':pid,'target':'links'})
  assert reply['links']==['trojan://synthetic@example.com:443#A-Owned%20provenance%20manual-original','trojan://synthetic@example.com:443#Z-Owned%20provenance%20source-original']
  checks.append('real HTTP refresh and UI source-group save preserve cache; duplicate URLs keep their own source by occurrence, group sort and original-name template')
  source=next(r for r in a.request('GET','/api/v1/admin/resources/sources')['items'] if r['id']==sid)
  assert source['data']['links'] and source['data']['group']=='Z'
  u.request('POST','/api/v1/admin/profiles/preview',{'id':pid,'target':'links'},expected=(403,))
  assert u.request('GET','/api/v1/subscription')['url']==before['url'] and u.request('GET','/api/v1/subscription')['node_count']==before['node_count']
  checks.append('ordinary preview rejected; private inventory never added to own published subscription or grants')
 finally:
  for kind,id in reversed(owned):a.request('DELETE',f'/api/v1/admin/resources/{kind}/{id}')
  shell('systemctl stop migate-provenance-fixture.service || true');shell('rm -f -- /var/tmp/migate-provenance-fixture.py')
Path('docs/evidence/live-provenance.json').write_text(json.dumps({'version':version,'checks':checks,'date':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),'cleanup':'owned source/node/profile and HTTP service removed; no grants changed','visual':'four widths captured; manual review pending','limits':'name multiplier is display hint, not accounting; tags and additional upstream platform context remain gaps'},indent=2),encoding='utf-8')
print('Real source provenance, duplicate endpoint isolation, UI group and preview passed')
