"""Retained paired Gate lifecycle using the existing labeled disposable Agent.

Only its test device/account is changed at the live center. Primary Gate, x-ui,
1Panel and production data services are never uninstalled. Secrets stay in the
restricted operator directory or disposable container, never in the report.
"""
import argparse,base64,hashlib,hmac,json,secrets,shlex,struct,subprocess,time,urllib.parse
from pathlib import Path
from live_api import API,state,save_state
from remote import connect,run

p=argparse.ArgumentParser();p.add_argument('--version',required=True);args=p.parse_args()
container='migate-quota-acceptance';records=[];authorized=None;account=None;device=None
def docker(*argv,input=None,check=True):
 r=subprocess.run(['docker',*argv],text=True,encoding='utf-8',errors='replace',capture_output=True,input=input)
 if check and r.returncode:raise RuntimeError('Isolated lifecycle command failed: '+r.stderr[-1200:]+r.stdout[-1000:])
 return r.stdout
def shell(command):return docker('exec',container,'sh','-eu','-c',command)
def record(check):records.append({'check':check,'passed':True});print(check,flush=True)
assert json.loads(docker('inspect',container,'--format','{{json .Config.Labels}}')).get('migate.acceptance')=='quota'
docker('start',container)
a=API().admin();s=state()
def stepup():
 # Avoid using a TOTP already consumed by another operator acceptance step.
 time.sleep(30-time.time()%30+1)
 secret=base64.b32decode(s['totp_secret']+'='*((8-len(s['totp_secret'])%8)%8))
 digest=hmac.new(secret,struct.pack('>Q',int(time.time())//30),hashlib.sha1).digest();o=digest[-1]&15
 return str((struct.unpack('>I',digest[o:o+4])[0]&0x7fffffff)%1000000).zfill(6)
def recover():
 reply=json.loads(shell('/opt/migate-gate/current/migate-gate recover'))
 assert reply['device_id']==device and reply['fingerprint']==fingerprint
 a.request('POST',f'/api/v1/admin/devices/{device}/approve',{'code':reply['pair_code'],'fingerprint':reply['fingerprint'],'password':s['admin_password'],'mfa_code':stepup()})
 shell('systemctl start migate-gate-helper migate-gate-xray migate-gate')
 deadline=time.monotonic()+45
 while time.monotonic()<deadline:
  found=next(d for d in a.request('GET','/api/v1/admin/devices')['items'] if d['id']==device)
  if found['status']=='approved' and (found.get('metadata') or {}).get('version')==args.version:return
  time.sleep(2)
 raise RuntimeError('Recovered Agent did not send its new-version heartbeat')
def local_grant():
 data=json.loads(shell("python3 -c \"import sqlite3,json;d=sqlite3.connect('/var/lib/migate-gate/agent.db');print(json.dumps(json.loads(d.execute('select data from state where id=1').fetchone()[0])))\""))
 return next(g for g in data['grants'] if g['grant_id']==grant)
def probe():
 user=API();user.login(account['email'],account['password'])
 info=user.request('GET','/api/v1/subscription')
 links=base64.b64decode(user.request('GET',urllib.parse.urlsplit(info['url']).path,raw=True)).decode().splitlines();assert len(links)==1
 u=urllib.parse.urlsplit(links[0]);q=urllib.parse.parse_qs(u.query);get=lambda k:q[k][0]
 config={'log':{'loglevel':'none'},'inbounds':[{'listen':'127.0.0.1','port':20991,'protocol':'http','settings':{}}],'outbounds':[{'protocol':'vless','settings':{'vnext':[{'address':'127.0.0.1','port':24446,'users':[{'id':u.username,'encryption':'none','flow':get('flow')}]}]},'streamSettings':{'network':'tcp','security':'reality','realitySettings':{'serverName':get('sni'),'fingerprint':get('fp'),'publicKey':get('pbk'),'shortId':get('sid')}}}]}
 code='''import json,subprocess,time,http.client
from pathlib import Path
Path('/var/tmp/migate-restore-client.json').write_text(json.dumps(CONFIG))
p=subprocess.Popen(['/opt/migate-gate/cores/xray','run','-config','/var/tmp/migate-restore-client.json'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
try:
 time.sleep(1)
 c=http.client.HTTPConnection('127.0.0.1',20991,timeout=12);c.request('GET','http://127.0.0.1:19816/test');r=c.getresponse();body=r.read();assert r.status==200 and len(body)==1048576
finally:p.terminate();p.wait(timeout=8);Path('/var/tmp/migate-restore-client.json').unlink(missing_ok=True)
'''.replace('CONFIG',repr(config))
 docker('exec','-i',container,'python3','-',input=code)

with connect('control') as center:
 try:
  shell('systemctl stop migate-gate; test -f /etc/migate-gate/agent.json')
  docker('cp',f'dist/migate-{args.version}-linux-amd64/.',container+':/release')
  shell('cd /release; if test -f /etc/migate-gate/install-manifest.json; then cmp -s migate-gate /opt/migate-gate/current/migate-gate; else sh install.sh gate; fi')
  identity=json.loads(shell('cat /etc/migate-gate/agent.json'));device=identity['device_id']
  fingerprint=next(d['fingerprint'] for d in a.request('GET','/api/v1/admin/devices')['items'] if d['id']==device)
  pub=shell('cat /var/lib/migate-gate/tunnel/id_ed25519.pub').strip()
  authorized='restrict,port-forwarding,permitopen="127.0.0.1:443",command="/usr/sbin/nologin" '+pub
  with center.open_sftp() as ftp:
   path='/var/lib/migate-tunnel/.ssh/authorized_keys'
   with ftp.open(path) as f:old=f.read().decode()
   with ftp.open(path,'w') as f:f.write(old.rstrip()+'\n'+authorized+'\n')
  shell('ssh -fNT -o StrictHostKeyChecking=yes -o ExitOnForwardFailure=yes -o UserKnownHostsFile=/var/lib/migate-gate/tunnel/known_hosts -i /var/lib/migate-gate/tunnel/id_ed25519 -L127.0.0.1:19814:127.0.0.1:443 migate-tunnel@106.55.5.233')
  recover()
  record('retained revoked test identity recovers only after VPS proof and administrator MFA')
  account={'email':f'lifecycle-{secrets.token_hex(4)}@sub.caowo.baby','password':secrets.token_urlsafe(24)}
  account['id']=a.request('POST','/api/v1/admin/users',{**account,'quota':256<<20})['id']
  stored=state();stored.setdefault('enforcement_users',[]).append(account);save_state(stored)
  grant=a.request('POST','/api/v1/admin/grants',{'device_id':device,'inbound_id':1,'user_id':account['id']})['id']
  deadline=time.monotonic()+60
  while time.monotonic()<deadline:
   g=next(x for x in a.request('GET','/api/v1/admin/grants')['items'] if x['id']==grant)
   if g['status']=='applied':break
   time.sleep(1)
  else:raise RuntimeError('Test grant not applied')
  fixture="from http.server import BaseHTTPRequestHandler,HTTPServer\nclass H(BaseHTTPRequestHandler):\n def do_GET(self):\n  self.send_response(200);self.end_headers();self.wfile.write(b'x'*1048576)\n def log_message(self,*args):pass\nHTTPServer(('127.0.0.1',19816),H).serve_forever()"
  shell('systemd-run --unit=migate-restore-fixture --collect /usr/bin/python3 -c '+shlex.quote(fixture))
  probe();time.sleep(2)
  before=local_grant();assert before['used']>0
  base='/release/migate-manage';manifest='/etc/migate-gate/install-manifest.json'
  uninstall=f'{base} uninstall --manifest {manifest}'
  shell(uninstall+' --confirm gate:uninstall')
  shell('systemctl is-active migate-gate-xray; test -f /var/lib/migate-gate/agent.db')
  assert next(d['status'] for d in a.request('GET','/api/v1/admin/devices')['items'] if d['id']==device)=='revoked'
  shell('cd /release; sh install.sh gate')
  recover();after=local_grant();assert after['epoch']==before['epoch'] and after['used']>=before['used']
  probe();record('paired ordinary uninstall preserves real running core; reinstall and reapproval retain meter epoch and use')
  shell(uninstall+' --with-cores --confirm gate:uninstall:with-cores')
  shell('test ! -e /opt/migate-gate/cores; test -f /var/lib/migate-gate/agent.db; cd /release; sh install.sh gate')
  docker('cp','dist/cores/v26.3.27/xray',container+':/var/tmp/xray-verified')
  shell('install -m755 /var/tmp/xray-verified /opt/migate-gate/cores/xray')
  recover();probe();record('paired with-cores uninstall removes real core; verified reinstall recovers same authorization')
  shell(f'{base} backup --manifest {manifest} --backup-dir /var/backups/paired --confirm gate:backup')
  snapshot=shell('find /var/backups/paired -name "*.snapshot.json" | sort | tail -1').strip();assert snapshot.startswith('/var/backups/paired/')
  meter=local_grant()
  shell(uninstall+' --purge --backup-dir /var/backups/paired-purge --confirm gate:uninstall:purge')
  shell('test ! -e /etc/migate-gate; test ! -e /var/lib/migate-gate; cd /release; sh install.sh gate')
  shell('install -m755 /var/tmp/xray-verified /opt/migate-gate/cores/xray')
  shell(f'{base} restore --manifest {manifest} --snapshot {snapshot} --backup-dir /var/backups/paired-pre-restore --confirm gate:restore')
  restored=local_grant();assert restored['epoch']==meter['epoch'] and restored['used']>=meter['used']
  recover();probe();record('paired purge reinstall and checksummed restore retain device ID meter epoch counters and real client access after reapproval')
  report={'version':args.version,'date':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),'environment':'isolated actual systemd Agent and Xray connected to live center; only test device and account','limitations':'temporary test SSH transport is external to application uninstall scope; production Gate never uninstalled','records':records}
  Path('docs/evidence/paired-lifecycle.json').write_text(json.dumps(report,indent=2))
 except Exception as error:
  Path(f'docs/evidence/paired-lifecycle-{args.version}-failure.json').write_text(json.dumps({'version':args.version,'records':records,'error_type':type(error).__name__,'detail':'sanitized; inspect operator console for lifecycle stage'},indent=2));raise
 finally:
  if account:a.request('PATCH',f"/api/v1/admin/users/{account['id']}",{'enabled':False})
  if device:a.request('POST',f'/api/v1/admin/devices/{device}/revoke',{})
  docker('stop',container,check=False)
  if authorized:
   with center.open_sftp() as ftp:
    path='/var/lib/migate-tunnel/.ssh/authorized_keys'
    with ftp.open(path) as f:lines=f.read().decode().splitlines()
    with ftp.open(path,'w') as f:f.write('\n'.join(x for x in lines if x!=authorized)+'\n')
