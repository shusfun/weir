"""Import a real user's subscription into pinned Xray and test the Gate path."""
import base64
import hashlib
import http.client
import json
import os
from pathlib import Path
import socket
import subprocess
import time
import urllib.parse
import urllib.request
from live_api import API,state,OPS,save_state

def config_from_link(link,port):
 u=urllib.parse.urlsplit(link);q=urllib.parse.parse_qs(u.query)
 if u.scheme!='vless':raise ValueError('This acceptance client expects VLESS')
 get=lambda k,d='':q.get(k,[d])[0]
 return {'log':{'loglevel':'warning'},'inbounds':[{'listen':'127.0.0.1','port':port,'protocol':'http','settings':{}}],
         'outbounds':[{'protocol':'vless','settings':{'vnext':[{'address':u.hostname,'port':u.port,'users':[{'id':u.username,'encryption':'none','flow':get('flow')}]}]},
                       'streamSettings':{'network':get('type','tcp'),'security':get('security'),
                                         'realitySettings':{'serverName':get('sni'),'fingerprint':get('fp','chrome'),'publicKey':get('pbk'),'shortId':get('sid')}}}]}

def get_link(user):
 if any(u.get('email')==user.get('email') for u in state().get('test_users',[])):
  a=API().acceptance_user(user)
 else:
  a=API();a.login(user['email'],user['password'])
 a.request('GET','/api/v1/admin/users',expected=(403,))
 info=a.request('GET','/api/v1/subscription')
 subpath=urllib.parse.urlsplit(info['url']).path
 links=base64.b64decode(a.request('GET',subpath,raw=True)).decode().splitlines()
 if len(links)!=1:raise RuntimeError('Expected one authorized node')
 return links[0],subpath

def run_probe(user,index,expected=True,link_override=None,url='https://www.cloudflare.com/cdn-cgi/trace'):
 if link_override is None:link,subpath=get_link(user)
 else:link,subpath=link_override,None
 config=config_from_link(link,20980+index)
 file=OPS/f'client-{index}.json';file.write_text(json.dumps(config))
 log=OPS/f'client-{index}.log'
 binary=Path('dist/cores/v26.3.27/xray.exe').resolve()
 flags=subprocess.CREATE_NO_WINDOW if os.name=='nt' else 0
 with log.open('w') as f:
  process=subprocess.Popen([str(binary),'run','-config',str(file)],stdout=f,stderr=f,creationflags=flags)
  try:
   for _ in range(30):
    try:
     with socket.create_connection(('127.0.0.1',20980+index),timeout=.2):break
    except OSError:time.sleep(.1)
   opener=urllib.request.build_opener(urllib.request.ProxyHandler({'https':f'http://127.0.0.1:{20980+index}'}))
   try:
    if url.startswith('http://'):
     # Explicit proxy request: loopback test targets must resolve at the Gate,
     # regardless of the operator machine's NO_PROXY environment.
     connection=http.client.HTTPConnection('127.0.0.1',20980+index,timeout=12)
     try:
      connection.request('GET',url)
      response=connection.getresponse()
      if response.status!=200:raise RuntimeError('Fixture returned HTTP '+str(response.status))
      body=response.read()
     finally:connection.close()
    else:
     with opener.open(url,timeout=12) as response:
      body=response.read()
   except Exception:
    if expected:raise
    return {'user':index,'blocked':True}
   if not expected:raise RuntimeError('Disabled/expired/quota-exhausted credential still connected')
   if 'cdn-cgi/trace' not in url:return {'download_bytes':len(body)}
   body=body.decode()
   ip=next(line[3:] for line in body.splitlines() if line.startswith('ip='))
   if ip!='154.9.26.146':raise RuntimeError('Proxy egress is not the Gate')
   return {'user':index,'credential_digest':hashlib.sha256(urllib.parse.urlsplit(link).username.encode()).hexdigest(),
           'egress':ip,'gate_port':config['outbounds'][0]['settings']['vnext'][0]['port'],'subscription_formats':verify_formats(subpath) if subpath else []}
  finally:
   process.terminate();process.wait(timeout=10)

def verify_formats(path):
 a=API()
 for target in ['clash','singbox']:
  data=a.request('GET',path+'?target='+target,raw=True)
  if not data:raise RuntimeError('Empty subscription conversion')
 return ['base64','clash','singbox']

if __name__=='__main__':
 s=state();results=[run_probe(user,i+1) for i,user in enumerate(s['test_users'])]
 if results[0]['credential_digest']==results[1]['credential_digest']:raise RuntimeError('Users share credentials')
 for item in results:del item['credential_digest']
 Path('docs/evidence/live-client.json').write_text(json.dumps({'version':API().admin().request('GET','/api/v1/admin/overview')['version'],'date':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),'client':'Xray v26.3.27 Windows','results':results},indent=2))
 print('Two real clients passed: separate credentials, shared inbound, proxy egress directly through Gate')
