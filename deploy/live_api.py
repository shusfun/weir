"""Real HTTPS API acceptance, with a verified SSH transport for local ISP TLS issues.

The origin, TLS certificate validation and cookies remain the public domain.
Forward 127.0.0.1:19443 to the center's 127.0.0.1:443 before using this module.
"""
import http.client
from http.cookies import SimpleCookie
import json
import os
from pathlib import Path
import socket
import ssl
import time

ORIGIN='https://sub.caowo.baby'
OPS=Path(os.environ['LOCALAPPDATA'])/'MiGate/operations'
def state(): return json.loads((OPS/'deployment.json').read_text())
def save_state(data): (OPS/'deployment.json').write_text(json.dumps(data,indent=2),encoding='utf-8')

class ForwardedHTTPS(http.client.HTTPSConnection):
 def connect(self):
  self.sock=self._context.wrap_socket(socket.create_connection(('127.0.0.1',19443),timeout=self.timeout),server_hostname=self.host)

class API:
 def __init__(self): self.cookie=''
 def request(self,method,path,data=None,expected=(200,201,202),raw=False):
  from production_guard import check
  check(method,path,data)
  c=ForwardedHTTPS('sub.caowo.baby',timeout=35,context=ssl.create_default_context())
  headers={'Origin':ORIGIN,'Content-Type':'application/json','Cookie':self.cookie}
  c.request(method,path,None if data is None else json.dumps(data),headers)
  r=c.getresponse();body=r.read();status=r.status
  for key,value in r.getheaders():
   if key.lower()=='set-cookie':
    cookie=SimpleCookie();cookie.load(value)
    if 'migate_session' in cookie:self.cookie='migate_session='+cookie['migate_session'].value
  c.close()
  if status not in expected:
   message=json.loads(body).get('error','request failed') if body.startswith(b'{') else 'non-JSON response'
   raise RuntimeError(f'{method} endpoint failed: HTTP {status}, {message}')
  return body if raw else json.loads(body)
 def login(self,email,password):return self.request('POST','/api/v1/auth/login',{'email':email,'password':password})
 def acceptance_user(self,account):
  # Only explicitly registered ordinary acceptance accounts may reuse a session.
  # Authentication/MFA tests continue calling login() to exercise fresh login.
  configured=state().get('test_users',[])
  if not any(u.get('email')==account.get('email') and u.get('password')==account.get('password') for u in configured):
   raise RuntimeError('Acceptance session requires an owned test account')
  cache_path=OPS/'acceptance-sessions.json'
  try:cache=json.loads(cache_path.read_text(encoding='utf-8'))
  except (FileNotFoundError,ValueError):cache={}
  self.cookie=cache.get(account['email'],'')
  if self.cookie:
   try:
    me=self.request('GET','/api/v1/me')
    if me.get('email')==account['email'] and me.get('role')=='user':return self
   except RuntimeError:pass
   self.cookie=''
  reply=self.login(account['email'],account['password'])
  if reply.get('mfa_required') or reply.get('user',{}).get('role')!='user':
   raise RuntimeError('Ordinary acceptance account requires attention')
  cache[account['email']]=self.cookie
  cache_path.write_text(json.dumps(cache),encoding='utf-8')
  os.chmod(cache_path,0o600)
  return self
 def admin(self):
  s=state()
  if s.get('admin_cookie'):
   self.cookie=s['admin_cookie']
   try:
    self.request('GET','/api/v1/me')
    return self
   except RuntimeError: self.cookie=''
  reply=self.login(s['admin_email'],s['admin_password'])
  if reply.get('mfa_required'):
   import base64,hmac,hashlib,struct
   secret=base64.b32decode(s['totp_secret']+'='*((8-len(s['totp_secret'])%8)%8))
   def code():
    digest=hmac.new(secret,struct.pack('>Q',int(time.time())//30),hashlib.sha1).digest();o=digest[-1]&15
    return str((struct.unpack('>I',digest[o:o+4])[0]&0x7fffffff)%1000000).zfill(6)
   self.request('POST','/api/v1/auth/mfa',{'challenge':reply['challenge'],'code':code()})
  s=state();s['admin_cookie']=self.cookie;save_state(s)
  return self
 def wait_task(self,tid,timeout=90):
  deadline=time.monotonic()+timeout
  while time.monotonic()<deadline:
   tasks=self.request('GET','/api/v1/admin/tasks')['items']
   t=next((x for x in tasks if x['id']==tid),None)
   if t and t['status'] in ['applied','failed']:
    if t['status']!='applied': raise RuntimeError('Task failed: '+str(t['result']))
    return t
   time.sleep(1)
  raise TimeoutError('Task not applied within acceptance deadline')
