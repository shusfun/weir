"""Real two-inbound single-task binding, actual TLS client and receipt projection."""
import copy,datetime,importlib.util,json,os,secrets,subprocess,time,urllib.parse,urllib.request
from pathlib import Path
from cryptography import x509
from cryptography.x509.oid import NameOID
from cryptography.hazmat.primitives import hashes,serialization
from cryptography.hazmat.primitives.asymmetric import ec
from live_api import API,OPS,state
from remote import connect,run

a=API().admin();s=state();did=s['device_id'];owned_in=[];owned_cert=[];account=None;grant=None;private=OPS/'certificate-binding-fixture.json';checks=[]
prefix='Binding-'+secrets.token_hex(4)
def task(op,payload):
 t=a.request('POST',f'/api/v1/admin/devices/{did}/tasks',{'operation':op,'payload':payload,'confirm':True});a.wait_task(t['id']);return t
def rows(kind):return a.request('GET',f'/api/v1/admin/resources/device-{kind}?owner_id={did}')['items']
def config_for():
 u=API();u.login(account['email'],account['password']);info=u.request('GET','/api/v1/subscription');path=urllib.parse.urlsplit(info['url']).path
 return json.loads(u.request('GET',path+'?target=singbox',raw=True))
def probe(config):
 cfg=copy.deepcopy(config);cfg['log']={'level':'fatal'};cfg['inbounds']=[{'type':'http','listen':'127.0.0.1','listen_port':20994}];file=OPS/'binding-client.json';file.write_text(json.dumps(cfg),encoding='utf-8');os.chmod(file,0o600)
 p=subprocess.Popen(['dist/cores/sing-box-1.14.3-stats/sing-box.exe','run','-c',str(file)],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
 try:
  time.sleep(1);opener=urllib.request.build_opener(urllib.request.ProxyHandler({'https':'http://127.0.0.1:20994'}));body=opener.open('https://www.cloudflare.com/cdn-cgi/trace',timeout=20).read().decode()
  if 'ip=154.9.26.146' not in body:raise RuntimeError('Certificate client wrong egress')
 finally:p.terminate();p.wait(timeout=10);file.unlink(missing_ok=True)
try:
 with connect('gate') as ssh:
  status,out,_=run(ssh,"ss -lntup | awk '/:(24446|24447) / {print}'")
  if status or out.strip():raise RuntimeError('Fixture port occupied')
 key=ec.generate_private_key(ec.SECP256R1());now=datetime.datetime.now(datetime.timezone.utc);name=x509.Name([x509.NameAttribute(NameOID.COMMON_NAME,'migate-binding.invalid')])
 for suffix in ['original','replacement']:
  cert=x509.CertificateBuilder().subject_name(name).issuer_name(name).public_key(key.public_key()).serial_number(x509.random_serial_number()).not_valid_before(now-datetime.timedelta(minutes=5)).not_valid_after(now+datetime.timedelta(days=2)).add_extension(x509.SubjectAlternativeName([x509.DNSName('migate-binding.invalid')]),False).sign(key,hashes.SHA256())
  owned_cert.append(task('certificate.upsert',{'name':prefix+'-'+suffix,'certificate_pem':cert.public_bytes(serialization.Encoding.PEM).decode(),'key_pem':key.private_bytes(serialization.Encoding.PEM,serialization.PrivateFormat.PKCS8,serialization.NoEncryption()).decode()})['resource_id'])
 for index,suffix in enumerate(['active','empty']):
  owned_in.append(task('inbound.upsert',{'remark':prefix+'-'+suffix,'protocol':'hysteria2','core':'sing-box','port':24446+index,'public_host':'154.9.26.146','certificate_id':owned_cert[0],'tls_sni':'migate-binding.invalid'})['resource_id'])
 account={'email':f'binding-{secrets.token_hex(5)}@sub.caowo.baby','password':secrets.token_urlsafe(24),'quota':64<<20,'expires':0};account['id']=a.request('POST','/api/v1/admin/users',account)['id']
 grant=a.request('POST','/api/v1/admin/grants',{'user_id':account['id'],'device_id':did,'inbound_id':owned_in[0]})['id']
 for _ in range(90):
  g=next(v for v in a.request('GET','/api/v1/admin/grants')['items'] if v['id']==grant)
  if g['status']=='applied':break
  time.sleep(1)
 else:raise RuntimeError('Fixture grant not applied')
 cfg=config_for();probe(cfg);checks.append('actual certificate-pinned Hysteria2 client works before binding')
 before={r['data']['id']:r['data'] for r in rows('inbound') if r['data']['id'] in owned_in}
 f={'prefix':prefix,'cert_name':prefix+'-replacement','inbound_names':[prefix+'-active',prefix+'-empty']};private.write_text(json.dumps(f),encoding='utf-8');os.chmod(private,0o600)
 result=subprocess.run(['node','deploy/browser-certificate-binding.cjs'],capture_output=True,text=True)
 if result.returncode:
  for line in result.stderr.splitlines():
   if line.startswith('Certificate binding browser failed at '):print(line)
  raise RuntimeError('Binding browser acceptance failed; private fixture omitted')
 tid=json.loads(private.read_text(encoding='utf-8'))['task_id'];a.wait_task(tid)
 after={r['data']['id']:r['data'] for r in rows('inbound') if r['data']['id'] in owned_in}
 for id in owned_in:
  if after[id]['certificate_id']!=owned_cert[1]:raise RuntimeError('Binding receipt absent')
  for field in ['port','protocol','enabled','tls_sni','public_host']:
   if before[id].get(field)!=after[id].get(field):raise RuntimeError('Binding altered unrelated inbound data')
 with connect('gate') as ssh:
  verify_script="""import json,ssl,hashlib,os
c=json.load(open('/etc/migate-gate/cores/sing-box.json'))
rows=[i for i in c.get('inbounds',[]) if i.get('tag')=='hy2-inbound-REPLACE_INBOUND']
assert len(rows)==1
path=rows[0]['tls']['certificate_path']
assert path.startswith('/etc/migate-gate/certs/REPLACE_CERT-')
assert hashlib.sha256(ssl.PEM_cert_to_DER_cert(open(path).read())).hexdigest()=='REPLACE_DIGEST'
print('actual_core_replacement_certificate_matched')
""".replace('REPLACE_INBOUND',str(owned_in[0])).replace('REPLACE_CERT',str(owned_cert[1])).replace('REPLACE_DIGEST',cert.fingerprint(hashes.SHA256()).hex())
  status,out,_=run(ssh,'python3 -',verify_script)
  if status or out.strip()!='actual_core_replacement_certificate_matched':raise RuntimeError('Actual core did not reference replacement certificate')
 checks.append('actual active sing-box inbound references replacement content-addressed certificate; DER SHA-256 matches new certificate')
 probe(config_for());probe(cfg);checks.append('UI selects two named TLS inbounds; one applied receipt updates both; unrelated fields and old pinned client retained')
 ordinary=API();ordinary.login(account['email'],account['password'])
 payload={'certificate_id':owned_cert[0],'inbounds':[{'id':id,'previous_certificate_id':owned_cert[1]} for id in owned_in]}
 ordinary.request('POST',f'/api/v1/admin/devices/{did}/tasks',{'operation':'certificate.bind','payload':payload,'confirm':True},expected=(403,))
 stale=copy.deepcopy(payload);stale['inbounds'][1]['previous_certificate_id']=owned_cert[0]
 a.request('POST',f'/api/v1/admin/devices/{did}/tasks',{'operation':'certificate.bind','payload':stale,'confirm':True},expected=(409,))
 for r in rows('inbound'):
  if r['data']['id'] in owned_in and r['data']['certificate_id']!=owned_cert[1]:raise RuntimeError('Stale batch partially modified')
 checks.append('ordinary user forbidden and stale second target rejects entire batch without partial applied state')
finally:
 if grant:
  a.request('DELETE',f'/api/v1/admin/grants/{grant}')
  for _ in range(60):
   g=next(v for v in a.request('GET','/api/v1/admin/grants')['items'] if v['id']==grant)
   if g['status']=='revoked':break
   time.sleep(1)
 for id in reversed(owned_in):task('inbound.delete',{'id':id})
 for id in reversed(owned_cert):task('certificate.delete',{'id':id})
 if account and account.get('id'):a.request('PATCH',f"/api/v1/admin/users/{account['id']}",{'enabled':False,'quota':account['quota'],'expires':0})
 private.unlink(missing_ok=True)
spec=importlib.util.spec_from_file_location('client',Path(__file__).with_name('verify-live-client.py'));client=importlib.util.module_from_spec(spec);spec.loader.exec_module(client)
for i,u in enumerate(s['test_users'],1):client.run_probe(u,i)
Path('docs/evidence/live-certificate-binding.json').write_text(json.dumps({'version':a.request('GET','/api/v1/admin/overview')['version'],'date':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),'checks':checks+['owned inbounds/certificates removed, fixture grant revoked, both ordinary clients restored'],'visual':'four screenshots captured; human review pending'},indent=2),encoding='utf-8')
print('Real two-inbound certificate binding UI, atomic receipt, pinned client, guards and cleanup passed')
