"""Two real Agents/cores consume one center account, including duplicate reports.

Second Agent runs in the explicitly labeled disposable Docker container.
Only its temporary SSH public key is appended to the dedicated tunnel account.
"""
import argparse,base64,concurrent.futures,hashlib,hmac,struct,importlib.util,json,secrets,shlex,subprocess,time,urllib.parse
from pathlib import Path
from cryptography.hazmat.primitives.asymmetric import ed25519
from cryptography.hazmat.primitives import serialization
from live_api import API,ForwardedHTTPS,state,save_state,OPS
from remote import connect,run

parser=argparse.ArgumentParser();parser.add_argument('--version',required=True);args=parser.parse_args()
container='migate-quota-acceptance'
def docker(*args):
 r=subprocess.run(['docker',*args],capture_output=True,text=True,encoding='utf-8',errors='replace')
 if r.returncode:raise RuntimeError('Docker acceptance command failed: '+r.stderr[-1200:])
 return r.stdout
def shell(cmd):return docker('exec',container,'sh','-eu','-c',cmd)
assert json.loads(docker('inspect',container,'--format','{{json .Config.Labels}}'))['migate.acceptance']=='quota'
docker('start',container)
spec=importlib.util.spec_from_file_location('client',Path(__file__).with_name('verify-live-client.py'));client=importlib.util.module_from_spec(spec);spec.loader.exec_module(client)
api=API().admin();s=state();checks=[];device=None;account=None;authorized=None
fixture='''from http.server import BaseHTTPRequestHandler,HTTPServer
class H(BaseHTTPRequestHandler):
 def do_GET(self):
  data=b'x'*(2<<20);self.send_response(200);self.send_header('Content-Length',str(len(data)));self.end_headers();self.wfile.write(data)
 def log_message(self,*args):pass
HTTPServer(('127.0.0.1',19816),H).serve_forever()
'''
with connect('control') as center,connect('gate') as gate:
 try:
  docker('cp',f'dist/migate-{args.version}-linux-amd64/.',container+':/release')
  docker('cp','dist/cores/v26.3.27/xray',container+':/xray-fixture')
  shell('cd /release; if test -f /etc/migate-gate/install-manifest.json; then cmp -s migate-gate /opt/migate-gate/current/migate-gate; else sh install.sh gate; fi; install -m755 /xray-fixture /opt/migate-gate/cores/xray; install -d -m700 /var/lib/migate-gate/tunnel; test -f /var/lib/migate-gate/tunnel/id_ed25519 || ssh-keygen -q -t ed25519 -N "" -f /var/lib/migate-gate/tunnel/id_ed25519')
  pub=shell('cat /var/lib/migate-gate/tunnel/id_ed25519.pub').strip()
  authorized='restrict,port-forwarding,permitopen="127.0.0.1:443",command="/usr/sbin/nologin" '+pub
  with center.open_sftp() as ftp:
   path='/var/lib/migate-tunnel/.ssh/authorized_keys'
   with ftp.open(path,'r') as f:old=f.read().decode()
   with ftp.open(path,'w') as f:f.write(old.rstrip()+'\n'+authorized+'\n')
   with ftp.open('/etc/ssh/ssh_host_ed25519_key.pub') as f:hostkey=f.read().decode().strip()
  shell('printf %s '+shlex.quote('106.55.5.233 '+hostkey+'\n')+' > /var/lib/migate-gate/tunnel/known_hosts')
  shell('ssh -fNT -o StrictHostKeyChecking=yes -o ExitOnForwardFailure=yes -o UserKnownHostsFile=/var/lib/migate-gate/tunnel/known_hosts -i /var/lib/migate-gate/tunnel/id_ed25519 -L127.0.0.1:19814:127.0.0.1:443 migate-tunnel@106.55.5.233')
  pair=json.loads(shell('if test -f /etc/migate-gate/agent.json; then /opt/migate-gate/current/migate-gate recover; else /opt/migate-gate/current/migate-gate pair --center https://sub.caowo.baby --center-dial-address 127.0.0.1:19814 --name quota-isolated; fi'))
  config=json.loads(shell('cat /etc/migate-gate/agent.json'));device=pair['device_id']
  private=ed25519.Ed25519PrivateKey.from_private_bytes(base64.b64decode(config['private_key'])[:32])
  public=private.public_key().public_bytes(serialization.Encoding.Raw,serialization.PublicFormat.Raw)
  assert hashlib.sha256(public).hexdigest()==pair['fingerprint']
  # Retained interrupted fixtures recover through actual VPS proof and admin MFA.
  while time.time()%30>1:time.sleep(.5)
  secret=base64.b32decode(s['totp_secret']+'='*((8-len(s['totp_secret'])%8)%8))
  signature=hmac.new(secret,struct.pack('>Q',int(time.time())//30),hashlib.sha1).digest();offset=signature[-1]&15
  mfa=str((struct.unpack('>I',signature[offset:offset+4])[0]&0x7fffffff)%1000000).zfill(6)
  api.request('POST',f'/api/v1/admin/devices/{device}/approve',{'code':pair['pair_code'],'fingerprint':pair['fingerprint'],'password':s['admin_password'],'mfa_code':mfa})
  shell('chown root:migate-gate /etc/migate-gate/agent.json; chmod 640 /etc/migate-gate/agent.json; systemctl start migate-gate')
  deadline=time.monotonic()+45
  while time.monotonic()<deadline:
   observed=next(d for d in api.request('GET','/api/v1/admin/devices')['items'] if d['id']==device)
   if (observed.get('metadata') or {}).get('version')==args.version:break
   time.sleep(2)
  else:raise RuntimeError('Isolated Agent version heartbeat mismatch')
  inbound={'id':1,'remark':'Isolated quota Reality','protocol':'vless','core':'xray','port':24446,'network':'tcp','security':'reality','public_host':'127.0.0.1','tls_sni':'www.cloudflare.com','tls_fingerprint':'chrome','reality_dest':'www.cloudflare.com:443','reality_server_names':'www.cloudflare.com','reality_private_key':s['reality']['private'],'reality_public_key':s['reality']['public'],'reality_short_id':s['reality']['short_id']}
  task=api.request('POST',f'/api/v1/admin/devices/{device}/tasks',{'operation':'inbound.upsert','payload':inbound,'confirm':True});api.wait_task(task['id'])
  account={'email':f'quota-shared-{secrets.token_hex(4)}@sub.caowo.baby','password':secrets.token_urlsafe(24)}
  account['id']=api.request('POST','/api/v1/admin/users',{**account,'quota':64<<20})['id']
  s=state();s.setdefault('enforcement_users',[]).append(account);save_state(s)
  def grant(d):return api.request('POST','/api/v1/admin/grants',{'device_id':d,'inbound_id':1,'user_id':account['id']})['id']
  with concurrent.futures.ThreadPoolExecutor(2) as pool:gids=list(pool.map(grant,[s['device_id'],device]))
  deadline=time.monotonic()+75
  while time.monotonic()<deadline:
   grants=[g for g in api.request('GET','/api/v1/admin/grants')['items'] if g['id'] in gids]
   if len(grants)==2 and all(g['status']=='applied' for g in grants):break
   time.sleep(1)
  else:raise RuntimeError('Two real Agent grants did not apply')
  assert sum(g['allocated'] for g in grants)==64<<20
  user=API();user.login(account['email'],account['password']);info=user.request('GET','/api/v1/subscription')
  links=base64.b64decode(user.request('GET',urllib.parse.urlsplit(info['url']).path,raw=True)).decode().splitlines();assert len(links)==2
  command='systemd-run --unit=migate-acceptance-shared --collect /usr/bin/python3 -c '+shlex.quote(fixture)
  code,_,_=run(gate,command)
  if code:raise RuntimeError('Gate download fixture failed')
  shell(command)
  with concurrent.futures.ThreadPoolExecutor(2) as pool:
   futures=[pool.submit(client.run_probe,account,7+i,True,link,'http://127.0.0.1:19816/payload') for i,link in enumerate(links)]
   transferred=[f.result()['download_bytes'] for f in futures]
  assert transferred==[2<<20,2<<20]
  deadline=time.monotonic()+65
  while time.monotonic()<deadline:
   grants=[g for g in api.request('GET','/api/v1/admin/grants')['items'] if g['id'] in gids]
   if all(g['used']>=2<<20 for g in grants):break
   time.sleep(2)
  else:raise RuntimeError('Two real meter reports not received')
  assert sum(g['allocated'] for g in grants)==64<<20
  def user_used():return next(u['used'] for u in api.request('GET','/api/v1/admin/users')['items'] if u['id']==account['id'])
  assert user_used()==sum(g['used'] for g in grants)
  checks.append({'check':'two independent real Agents and Xray clients consume shared account concurrently','download_bytes':transferred,'grants':[{k:g[k] for k in ['device_id','allocated','used']} for g in grants],'passed':True})
  shell('systemctl stop migate-gate')
  local=json.loads(shell("python3 -c \"import sqlite3,json;d=sqlite3.connect('/var/lib/migate-gate/agent.db');print(json.dumps(json.loads(d.execute('select data from state where id=1').fetchone()[0])))\""))
  meters=[{k:g[k] for k in ['grant_id','sequence','epoch','used']} for g in local['grants']]
  challenge=API().request('POST','/api/v1/devices/challenge',{'device_id':device})['challenge']
  bearer=API().request('POST','/api/v1/devices/auth',{'device_id':device,'challenge':challenge,'signature':base64.b64encode(private.sign(challenge.encode())).decode()})['token']
  def report():
   c=ForwardedHTTPS('sub.caowo.baby',timeout=30);c.request('POST','/api/v1/agent/meter',json.dumps({'items':meters}),headers={'Content-Type':'application/json','Authorization':'Bearer '+bearer});r=c.getresponse();r.read();c.close();assert r.status==200
  report();before=user_used();report();assert user_used()==before
  checks.append({'check':'duplicate actual Agent persisted meter does not charge account again','passed':True})
  shell('systemctl start migate-gate')
 finally:
  if account:api.request('PATCH',f"/api/v1/admin/users/{account['id']}",{'enabled':False})
  if device:api.request('POST',f'/api/v1/admin/devices/{device}/revoke',{})
  run(gate,'systemctl stop migate-acceptance-shared.service')
  subprocess.run(['docker','stop',container],capture_output=True)
  if authorized:
   with center.open_sftp() as ftp:
    path='/var/lib/migate-tunnel/.ssh/authorized_keys'
    with ftp.open(path) as f:lines=f.read().decode().splitlines()
    with ftp.open(path,'w') as f:f.write('\n'.join(x for x in lines if x!=authorized)+'\n')
Path('docs/evidence/live-shared-quota.json').write_text(json.dumps({'version':args.version,'date':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),'checks':checks,'second_agent':'isolated Docker systemd and real core; no production inbound added'},indent=2))
print('Two real Agents shared-quota and duplicate-meter acceptance passed')
