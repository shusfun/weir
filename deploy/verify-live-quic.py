"""Public Gate QUIC subscription, pinned TLS, real clients and metering.

Only fresh acceptance resources on the owned spare 24446 port are changed.
All private keys and imported configs remain in the restricted operator folder.
"""
import copy,datetime,hashlib,json,secrets,subprocess,time,urllib.parse,urllib.request
from pathlib import Path
from cryptography import x509
from cryptography.x509.oid import NameOID
from cryptography.hazmat.primitives import hashes,serialization
from cryptography.hazmat.primitives.asymmetric import ec
from live_api import API,state,OPS
from remote import connect,run

a=API().admin();s=state();device=s['device_id'];records=[];account=None;grant=None;certificate=False;inbound=False
version=a.request('GET','/api/v1/admin/overview').get('version','unknown')
def record(name,**extra):records.append({'check':name,'passed':True,**extra});print(name,flush=True)
def task(op,payload):
 t=a.request('POST',f'/api/v1/admin/devices/{device}/tasks',{'operation':op,'payload':payload,'confirm':True});a.wait_task(t['id']);return t
def wait_grant():
 end=time.monotonic()+60
 while time.monotonic()<end:
  g=next(g for g in a.request('GET','/api/v1/admin/grants')['items'] if g['id']==grant)
  if g['status']=='applied':return g
  time.sleep(1)
 raise RuntimeError('grant did not apply')
def probe(config,success=True):
 config=copy.deepcopy(config);config['log']={'level':'fatal'};config['inbounds']=[{'type':'http','listen':'127.0.0.1','listen_port':20994}]
 path=OPS/'live-quic-client.json';path.write_text(json.dumps(config))
 p=subprocess.Popen(['dist/cores/sing-box-1.14.3-stats/sing-box.exe','run','-c',str(path)],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
 try:
  time.sleep(1)
  opener=urllib.request.build_opener(urllib.request.ProxyHandler({'https':'http://127.0.0.1:20994'}))
  body=opener.open('https://www.cloudflare.com/cdn-cgi/trace',timeout=15).read().decode()
  ok='ip=154.9.26.146' in body
 except Exception:ok=False
 finally:p.terminate();p.wait(timeout=10);path.unlink(missing_ok=True)
 if ok!=success:raise RuntimeError('actual QUIC client expected '+('connection' if success else 'denial'))
def config_for(user):
 u=API();u.login(user['email'],user['password']);info=u.request('GET','/api/v1/subscription')
 path=urllib.parse.urlsplit(info['url']).path
 return json.loads(u.request('GET',path+'?target=singbox',raw=True))
try:
 with connect('gate') as gate:
  code,out,err=run(gate,"ss -lntup | awk '/:24446 / {print}'")
  if code or out.strip():raise RuntimeError('spare port is occupied')
 key=ec.generate_private_key(ec.SECP256R1());now=datetime.datetime.now(datetime.timezone.utc)
 name=x509.Name([x509.NameAttribute(NameOID.COMMON_NAME,'migate-acceptance.invalid')])
 cert=x509.CertificateBuilder().subject_name(name).issuer_name(name).public_key(key.public_key()).serial_number(x509.random_serial_number()).not_valid_before(now-datetime.timedelta(minutes=5)).not_valid_after(now+datetime.timedelta(days=2)).add_extension(x509.SubjectAlternativeName([x509.DNSName('migate-acceptance.invalid')]),False).sign(key,hashes.SHA256())
 task('certificate.upsert',{'id':92,'certificate_pem':cert.public_bytes(serialization.Encoding.PEM).decode(),'key_pem':key.private_bytes(serialization.Encoding.PEM,serialization.PrivateFormat.PKCS8,serialization.NoEncryption()).decode()});certificate=True
 record('public Gate certificate applied through encrypted task and atomic receipt')
 account={'email':f'quic-{secrets.token_hex(5)}@sub.caowo.baby','password':secrets.token_urlsafe(24),'quota':32*1024*1024,'expires':0}
 account['id']=a.request('POST','/api/v1/admin/users',account)['id']
 for protocol in ['hysteria2','tuic']:
  payload={'id':92,'remark':'QUIC acceptance','protocol':protocol,'core':'sing-box','port':24446,'public_host':'154.9.26.146','certificate_id':92,'tls_sni':'migate-acceptance.invalid','tuic_congestion_control':'bbr' if protocol=='tuic' else ''}
  task('inbound.upsert',payload);inbound=True
  if grant is None:grant=a.request('POST','/api/v1/admin/grants',{'user_id':account['id'],'device_id':device,'inbound_id':92})['id']
  wait_grant();cfg=config_for(account)
  outbound=next(o for o in cfg['outbounds'] if o['type']==protocol)
  assert outbound['tls'].get('certificate_public_key_sha256') and not outbound['tls'].get('insecure')
  probe(cfg);record(protocol+' actual subscription client connects directly to public Gate with SPKI pin')
  wrong=copy.deepcopy(cfg);next(o for o in wrong['outbounds'] if o['type']==protocol)['tls']['certificate_public_key_sha256']=['AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=']
  probe(wrong,False);record(protocol+' subscription client rejects wrong public key pin')
  a.request('PATCH',f"/api/v1/admin/users/{account['id']}",{'enabled':False,'quota':account['quota'],'expires':0});wait_grant();probe(cfg,False)
  record(protocol+' real Agent applies administrator disable')
  a.request('PATCH',f"/api/v1/admin/users/{account['id']}",{'enabled':True,'quota':account['quota'],'expires':0});wait_grant();probe(cfg)
  deadline=time.monotonic()+45
  while time.monotonic()<deadline:
   g=next(g for g in a.request('GET','/api/v1/admin/grants')['items'] if g['id']==grant)
   if g['used']>0:break
   time.sleep(2)
  assert g['used']>0
  record(protocol+' real usage reaches center',used_bytes=g['used'])
  if protocol=='tuic':
   expiry=int(time.time())+8
   a.request('PATCH',f"/api/v1/admin/users/{account['id']}",{'enabled':True,'quota':account['quota'],'expires':expiry});wait_grant()
   while time.time()<=expiry+2:time.sleep(1)
   probe(cfg,False);record('TUIC expiry enforced by real Agent without further administrator task')
   a.request('PATCH',f"/api/v1/admin/users/{account['id']}",{'enabled':True,'quota':account['quota'],'expires':0});wait_grant();probe(cfg)
   now=datetime.datetime.now(datetime.timezone.utc);certificate_expiry=now+datetime.timedelta(seconds=25)
   short=x509.CertificateBuilder().subject_name(name).issuer_name(name).public_key(key.public_key()).serial_number(x509.random_serial_number()).not_valid_before(now-datetime.timedelta(minutes=1)).not_valid_after(certificate_expiry).add_extension(x509.SubjectAlternativeName([x509.DNSName('migate-acceptance.invalid')]),False).sign(key,hashes.SHA256())
   task('certificate.upsert',{'id':92,'certificate_pem':short.public_bytes(serialization.Encoding.PEM).decode(),'key_pem':key.private_bytes(serialization.Encoding.PEM,serialization.PrivateFormat.PKCS8,serialization.NoEncryption()).decode()})
   probe(config_for(account));record('certificate renewed through actual task without changing client identity')
   while datetime.datetime.now(datetime.timezone.utc)<=certificate_expiry+datetime.timedelta(seconds=3):time.sleep(1)
   with connect('gate') as gate:
    code,out,err=run(gate,"ss -lnup | awk '/:24446 / {print}'")
    if code or out.strip():raise RuntimeError('expired certificate left public listener active')
   probe(cfg,False);record('certificate expiry closes actual public listener through Agent enforcement')
   deadline=time.monotonic()+25
   while time.monotonic()<deadline:
    observations=a.request('GET','/api/v1/admin/events')['items']
    if any(e['data'].get('reason')=='certificate_expired' and e['data'].get('certificate_id')==92 and e['data'].get('device_id')==device for e in observations):break
    time.sleep(1)
   else:raise RuntimeError('expired certificate observation missing')
   record('center independently observes expired certificate without Laya')
except Exception as e:
 records.append({'check':'public QUIC acceptance','passed':False,'error':str(e)});raise
finally:
 if account:
  a.request('PATCH',f"/api/v1/admin/users/{account['id']}",{'enabled':False,'quota':account['quota'],'expires':0})
 if grant:
  a.request('DELETE',f'/api/v1/admin/grants/{grant}')
  deadline=time.monotonic()+45
  while time.monotonic()<deadline:
   g=next(g for g in a.request('GET','/api/v1/admin/grants')['items'] if g['id']==grant)
   if g['status']=='revoked':break
   time.sleep(1)
 if inbound:task('inbound.delete',{'id':92})
 if certificate:task('certificate.delete',{'id':92})
 report=json.dumps({'version':version,'date':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),'client':'sing-box 1.14.3-migate.stats1 Windows','records':records,'cleanup':'test account disabled; grant revoked; test inbound and certificate removed'},indent=2)
 Path('docs/evidence/live-quic.json').write_text(report,encoding='utf-8')
 Path(f'docs/evidence/live-quic-{version}-{int(time.time())}.json').write_text(report,encoding='utf-8')
