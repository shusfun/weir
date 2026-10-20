"""Owned loopback fixture, actual UI tasks and two real clients on the VPS."""
import datetime,importlib.util,json,os,secrets,shlex,subprocess,time,uuid,urllib.parse
from pathlib import Path
from cryptography import x509
from cryptography.x509.oid import NameOID
from cryptography.hazmat.primitives import hashes,serialization
from cryptography.hazmat.primitives.asymmetric import ec
from live_api import API,OPS,state
from remote import connect,run

spec=importlib.util.spec_from_file_location('live_client',Path(__file__).with_name('verify-live-client.py'));client=importlib.util.module_from_spec(spec);spec.loader.exec_module(client)
api=API().admin();s=state();did=s['device_id'];fixture={'tag':'owned-outbound-'+secrets.token_hex(4),'uuid':str(uuid.uuid4()),'password':secrets.token_urlsafe(24)}
private=OPS/'outbound-transport-fixture.json';checks=[];route_id=None
root='/var/tmp/migate-outbound-fixture'

def task(op,payload):
 result=api.request('POST',f'/api/v1/admin/devices/{did}/tasks',{'operation':op,'payload':payload,'confirm':True})
 api.wait_task(result['id']);return result

with connect('gate') as gate:
 def shell(command):
  code,out,err=run(gate,command)
  if code:raise RuntimeError('Owned outbound fixture operation failed; private diagnostics omitted')
  return out
 shell("python3 -c \"import socket;s=socket.socket();s.bind(('127.0.0.1',24446));s.close()\"")
 if not shell('/opt/migate-gate/cores/xray version').startswith('Xray 26.3.27'):raise RuntimeError('Compatible owned core version required')
 shell('test ! -e '+root+' && install -d -m 700 '+root)
 def upload(name,content):
  with gate.open_sftp() as ftp:
   with ftp.open(root+'/'+name,'w') as f:f.write(content)
   ftp.chmod(root+'/'+name,0o600)
 def server(mode):
  run(gate,'systemctl stop migate-outbound-fixture.service')
  stream={'network':'ws','wsSettings':{'path':'/fixture'}} if mode=='vmess-ws' else {'network':'grpc','grpcSettings':{'serviceName':'fixture'}}
  inbound={'listen':'127.0.0.1','port':24446,'protocol':'vmess','settings':{'clients':[{'id':fixture['uuid'],'alterId':0}]},'streamSettings':stream}
  if mode=='https':
   inbound.update(protocol='http',settings={'accounts':[{'user':'fixture','pass':fixture['password']}]},streamSettings={'network':'tcp','security':'tls','tlsSettings':{'certificates':[{'certificateFile':root+'/cert.pem','keyFile':root+'/key.pem'}]}})
  config={'log':{'loglevel':'warning'},'inbounds':[inbound],'outbounds':[{'protocol':'freedom','tag':'direct'}]}
  upload('config.json',json.dumps(config))
  shell('/opt/migate-gate/cores/xray run -test -config '+root+'/config.json >/dev/null 2>&1')
  shell('systemd-run --unit=migate-outbound-fixture --collect /opt/migate-gate/cores/xray run -config '+root+'/config.json')
  shell("python3 - <<'PY'\nimport socket,time\nfor i in range(40):\n try:\n  s=socket.create_connection(('127.0.0.1',24446),timeout=.2);s.close();break\n except OSError:\n  if i==39:raise RuntimeError('owned fixture readiness failed')\n  time.sleep(.2)\nPY")
 def browser(mode):
  fixture['mode']=mode;private.write_text(json.dumps(fixture),encoding='utf-8');os.chmod(private,0o600)
  result=subprocess.run(['node','deploy/browser-outbound-transport.cjs'],capture_output=True,text=True)
  fixture.update(json.loads(private.read_text(encoding='utf-8')))
  if result.returncode:
   for line in result.stderr.splitlines():
    if line.startswith('Outbound form failed at stage: '):print(line)
   raise RuntimeError('Actual outbound UI failed; private inputs omitted')
  api.wait_task(fixture['task_id'])
 try:
  key=ec.generate_private_key(ec.SECP256R1());now=datetime.datetime.now(datetime.timezone.utc);name=x509.Name([x509.NameAttribute(NameOID.COMMON_NAME,'localhost')])
  cert=x509.CertificateBuilder().subject_name(name).issuer_name(name).public_key(key.public_key()).serial_number(x509.random_serial_number()).not_valid_before(now-datetime.timedelta(minutes=5)).not_valid_after(now+datetime.timedelta(days=1)).add_extension(x509.SubjectAlternativeName([x509.DNSName('localhost')]),False).add_extension(x509.BasicConstraints(ca=True,path_length=None),True).sign(key,hashes.SHA256())
  fixture['certificate']=cert.public_bytes(serialization.Encoding.PEM).decode()
  upload('cert.pem',fixture['certificate']);upload('key.pem',key.private_bytes(serialization.Encoding.PEM,serialization.PrivateFormat.PKCS8,serialization.NoEncryption()).decode())
  if os.getenv('MIGATE_OUTBOUND_HTTPS_ONLY')!='1':
   server('vmess-ws');browser('vmess-ws')
   route=task('routing.upsert',{'outbound_id':fixture['outbound_id'],'client_id':s['test_users'][0]['grant_id'],'inbound_id':1});route_id=route['resource_id']
   client.run_probe(s['test_users'][0],1)
   checks.append('actual UI VMess/WebSocket -> Agent receipt -> real client through loopback fixture -> Gate egress')
   shell('systemctl stop migate-outbound-fixture.service');link,_=client.get_link(s['test_users'][0]);client.run_probe(s['test_users'][0],1,False,link);client.run_probe(s['test_users'][1],2)
   checks.append('stopping fixture blocks only routed user; peer stays connected, proving outbound is used')
   server('vmess-grpc');browser('vmess-grpc');client.run_probe(s['test_users'][0],1)
   checks.append('reopen preserves WS, UI change to gRPC removes WS fields; actual gRPC client connects')
  server('https');browser('https')
  if route_id is None:
   route=task('routing.upsert',{'outbound_id':fixture['outbound_id'],'client_id':s['test_users'][0]['grant_id'],'inbound_id':1});route_id=route['resource_id']
  link,_=client.get_link(s['test_users'][0]);client.run_probe(s['test_users'][0],1,False,link);client.run_probe(s['test_users'][1],2)
  checks.append('HTTPS outbound defaults to verified TLS; owned self-signed certificate rejected while peer works')
  browser('https-trusted');client.run_probe(s['test_users'][0],1)
  checks.append('explicit trusted PEM reaches owned self-signed HTTPS fixture with TLS verification; password retained on edit')
  other_key=ec.generate_private_key(ec.SECP256R1())
  other_cert=x509.CertificateBuilder().subject_name(name).issuer_name(name).public_key(other_key.public_key()).serial_number(x509.random_serial_number()).not_valid_before(now-datetime.timedelta(minutes=5)).not_valid_after(now+datetime.timedelta(days=1)).add_extension(x509.SubjectAlternativeName([x509.DNSName('localhost')]),False).add_extension(x509.BasicConstraints(ca=True,path_length=None),True).sign(other_key,hashes.SHA256())
  task('outbound.upsert',{'id':fixture['outbound_id'],'tag':fixture['tag'],'protocol':'https','address':'127.0.0.1','port':24446,'username':'fixture','transport_options':{'security':'tls','sni':'localhost','trusted_certificate':other_cert.public_bytes(serialization.Encoding.PEM).decode()}})
  link,_=client.get_link(s['test_users'][0]);client.run_probe(s['test_users'][0],1,False,link);client.run_probe(s['test_users'][1],2)
  checks.append('wrong trusted certificate is rejected; peer unaffected')
  task('outbound.upsert',{'id':fixture['outbound_id'],'tag':fixture['tag'],'protocol':'https','address':'127.0.0.1','port':24446,'username':'fixture','transport_options':{'security':'tls','sni':'wrong.invalid','trusted_certificate':fixture['certificate']}})
  link,_=client.get_link(s['test_users'][0]);client.run_probe(s['test_users'][0],1,False,link);client.run_probe(s['test_users'][1],2)
  checks.append('trusted certificate still rejects wrong SNI; peer unaffected')
  peer_link,_=client.get_link(s['test_users'][1]);peer=urllib.parse.urlsplit(peer_link);q=urllib.parse.parse_qs(peer.query)
  fixture.update(uuid=peer.username,sni=q['sni'][0],pbk=q['pbk'][0],sid=q.get('sid',[''])[0],flow=q.get('flow',[''])[0])
  browser('vless-reality');client.run_probe(s['test_users'][0],1)
  checks.append('actual UI VLESS Reality public-key/SNI/short-ID/Flow connects through existing peer test credential; private keys never returned')
 except Exception:
  # Capture only fixed diagnostic classes, never raw logs/configs/credentials.
  _,gate_log,_=run(gate,"journalctl -u migate-gate-xray.service -u migate-outbound-fixture.service --since '4 minutes ago' --no-pager")
  classes=[label for label in ['certificate signed by unknown authority','certificate is not valid','certificate has expired','authentication failed','failed to read','failed to write','TLS handshake','HTTP/2','http2','bad certificate','remote error','EOF'] if label.lower() in gate_log.lower()]
  code,diagnostic,_=run(gate,"python3 - <<'PY'\nimport json,hashlib\nf=json.load(open('/etc/migate-gate/cores/xray.json'))\nfor o in f['outbounds']:\n if o.get('protocol')=='http':\n  tls=o.get('streamSettings',{}).get('tlsSettings',{})\n  print(json.dumps({'protocol':o['protocol'],'tls_keys':list(tls),'certificate_count':len(tls.get('certificates',[])),'server_name_is_localhost':tls.get('serverName')=='localhost','alpn':tls.get('alpn',[]),'password_matches_fixture':o.get('settings',{}).get('servers',[{}])[0].get('users',[{}])[0].get('pass')==json.load(open('/var/tmp/migate-outbound-fixture/config.json'))['inbounds'][0]['settings']['accounts'][0]['pass'],'certificate_matches_fixture': ('\\n'.join(tls.get('certificates',[{}])[0].get('certificate',[])).strip()==open('/var/tmp/migate-outbound-fixture/cert.pem').read().strip())}))\nPY")
  print(diagnostic)
  _,direct,_=run(gate,"python3 - <<'PY'\nimport json,ssl,socket,subprocess\nfrom pathlib import Path\nr=Path('/var/tmp/migate-outbound-fixture');f=json.load(open(r/'config.json'))\ntry:\n with ssl.create_default_context(cafile=str(r/'cert.pem')).wrap_socket(socket.socket(),server_hostname='localhost') as sock:sock.settimeout(5);sock.connect(('127.0.0.1',24446))\n print('Direct TLS verified')\nexcept Exception as e:print('Direct TLS failed:',type(e).__name__)\na=f['inbounds'][0]['settings']['accounts'][0]\np=subprocess.run(['curl','--max-time','12','--proxy','https://localhost:24446','--proxy-cacert',str(r/'cert.pem'),'--proxy-user',a['user']+':'+a['pass'],'https://www.cloudflare.com/cdn-cgi/trace'],capture_output=True,text=True)\nprint('Direct HTTPS proxy exit',p.returncode,'Gate egress','ip=154.9.26.146' in p.stdout)\nPY")
  print(direct)
  print('Owned fixture diagnostic classes: '+', '.join(classes))
  raise RuntimeError('Real outbound probe failed; private transport details omitted') from None
 finally:
  if route_id:task('routing.delete',{'id':route_id})
  if fixture.get('outbound_id'):task('outbound.delete',{'id':fixture['outbound_id']})
  run(gate,'systemctl stop migate-outbound-fixture.service')
  shell("python3 - <<'PY'\nfrom pathlib import Path\np=Path('/var/tmp/migate-outbound-fixture')\nfor name in ['config.json','cert.pem','key.pem']:\n (p/name).unlink(missing_ok=True)\np.rmdir()\nPY")
  private.unlink(missing_ok=True)
client.run_probe(s['test_users'][0],1)
Path('docs/evidence/'+('debug-https-only.json' if os.getenv('MIGATE_OUTBOUND_HTTPS_ONLY')=='1' else 'live-outbound-transport.json')).write_text(json.dumps({'version':api.request('GET','/api/v1/admin/overview')['version'],'date':datetime.datetime.now(datetime.timezone.utc).isoformat(),'checks':checks,'cleanup':'owned route/outbound/loopback service/fixture secrets removed; ordinary connectivity restored','visual':'screenshots captured; review pending'},indent=2),encoding='utf-8')
print('Real outbound VMess WS/gRPC, HTTPS verification, UI edit, peer isolation and cleanup passed')
