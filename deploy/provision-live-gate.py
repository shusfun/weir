"""Pair the authorized Gate and provision two independent acceptance users."""
import base64
import hashlib
import json
import secrets
import time
from pathlib import Path
from cryptography.hazmat.primitives.asymmetric import ed25519,x25519
from cryptography.hazmat.primitives import serialization
from live_api import API,state,save_state
from remote import connect,run

api=API().admin()
s=state()
with connect('gate') as c:
 code,out,err=run(c,'sha256sum /opt/migate-gate/cores/xray')
 expected=hashlib.sha256(Path('dist/cores/v26.3.27/xray').read_bytes()).hexdigest()
 if code or out.split()[0]!=expected: raise RuntimeError('Uploaded core checksum mismatch')
 if 'device_id' not in s:
  code,out,err=run(c,'/opt/migate-gate/current/migate-gate pair --center https://sub.caowo.baby --center-dial-address 127.0.0.1:19814 --name gate-154')
  if code:raise RuntimeError(err)
  pair=json.loads(out)
  with c.open_sftp() as ftp:
   with ftp.open('/etc/migate-gate/agent.json') as f: config=json.load(f)
  private=base64.b64decode(config['private_key'])[:32]
  pub=ed25519.Ed25519PrivateKey.from_private_bytes(private).public_key().public_bytes(serialization.Encoding.Raw,serialization.PublicFormat.Raw)
  if hashlib.sha256(pub).hexdigest()!=pair['fingerprint']:raise RuntimeError('Device fingerprint mismatch')
  s.update(device_id=pair['device_id'],pair=pair);save_state(s)
 if s.get('pair'):
  pair=s['pair']
  api.request('POST',f"/api/v1/admin/devices/{s['device_id']}/approve",{'code':pair['pair_code'],'fingerprint':pair['fingerprint']})
  del s['pair'];save_state(s)
 code,out,err=run(c,'chown root:migate-gate /etc/migate-gate/agent.json; chmod 640 /etc/migate-gate/agent.json; systemctl enable --now migate-gate.service')
 if code:raise RuntimeError(err)
 print('Device identity verified, approved and agent started')
 if 'reality' not in s:
  key=x25519.X25519PrivateKey.generate()
  encode=lambda b:base64.urlsafe_b64encode(b).decode().rstrip('=')
  s['reality']={'private':encode(key.private_bytes(serialization.Encoding.Raw,serialization.PrivateFormat.Raw,serialization.NoEncryption())),
                'public':encode(key.public_key().public_bytes(serialization.Encoding.Raw,serialization.PublicFormat.Raw)), 'short_id':secrets.token_hex(8)}
  save_state(s)
 if not s.get('inbound_ready'):
  payload={'id':1,'remark':'Gate Reality','protocol':'vless','core':'xray','port':24445,'network':'tcp','security':'reality',
           'public_host':'154.9.26.146','tls_sni':'www.cloudflare.com','tls_fingerprint':'chrome',
           'reality_dest':'www.cloudflare.com:443','reality_server_names':'www.cloudflare.com',
           'reality_private_key':s['reality']['private'],'reality_public_key':s['reality']['public'],'reality_short_id':s['reality']['short_id']}
  task=api.request('POST',f"/api/v1/admin/devices/{s['device_id']}/tasks",{'operation':'inbound.upsert','payload':payload,'confirm':True})
  api.wait_task(task['id']);s['inbound_ready']=True;save_state(s)
  print('Independent Reality inbound applied on 24445')
 s.setdefault('test_users',[])
 while len(s['test_users'])<2:
  index=len(s['test_users'])+1
  item={'email':f'acceptance-{index}@sub.caowo.baby','password':secrets.token_urlsafe(24)}
  item['id']=api.request('POST','/api/v1/admin/users',{**item,'quota':128*1024*1024,'expires':int(time.time())+86400})['id']
  s['test_users'].append(item);save_state(s)
 for item in s['test_users']:
  if 'grant_id' not in item:
   item['grant_id']=api.request('POST','/api/v1/admin/grants',{'user_id':item['id'],'device_id':s['device_id'],'inbound_id':1})['id']
   save_state(s)
 deadline=time.monotonic()+90
 while time.monotonic()<deadline:
  grants=api.request('GET','/api/v1/admin/grants')['items']
  if all(any(g['id']==u['grant_id'] and g['status']=='applied' for g in grants) for u in s['test_users']):break
  time.sleep(1)
 else:raise RuntimeError('User authorizations were not applied')
 print('Two users authorized on the same inbound with independent credentials')
