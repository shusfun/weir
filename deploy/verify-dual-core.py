"""Real dual-core/certificate checks in the explicitly labeled disposable Gate.

No production device, center data, host systemd or external TLS bypass involved.
"""
import argparse, copy, hashlib, json, subprocess, time
from pathlib import Path

p=argparse.ArgumentParser();p.add_argument('--version',required=True);args=p.parse_args()
container='migate-quota-acceptance';records=[]
def docker(*argv,input=None,check=True):
 r=subprocess.run(['docker',*argv],input=input,text=True,encoding='utf-8',errors='replace',capture_output=True)
 if check and r.returncode:raise RuntimeError(r.stderr[-1500:]+r.stdout[-1500:])
 return r
def sh(command):return docker('exec',container,'sh','-eu','-c',command).stdout
def record(name):records.append({'check':name,'passed':True});print(name,flush=True)
assert json.loads(docker('inspect',container,'--format','{{json .Config.Labels}}').stdout).get('migate.acceptance')=='quota'
docker('start',container)
try:
 sh('systemctl stop migate-gate')
 docker('cp',f'dist/migate-{args.version}-linux-amd64/.',container+':/release')
 installed=sh('readlink /opt/migate-gate/current').strip().rsplit('/',1)[-1]
 if installed!=args.version:sh('cd /release; sh install.sh gate >/var/tmp/migate-dual-install.log; systemctl stop migate-gate')
 else:sh('systemctl start migate-gate-helper')
 docker('cp','dist/cores/sing-box-1.14.3-stats/sing-box',container+':/opt/migate-gate/cores/sing-box')
 docker('cp','dist/dual-agent.test',container+':/var/tmp/dual-agent.test')
 docker('cp','dist/dual-xray.test',container+':/var/tmp/dual-xray.test')
 sh('chmod 0755 /opt/migate-gate/cores/sing-box /var/tmp/dual-agent.test /var/tmp/dual-xray.test; /opt/migate-gate/cores/sing-box version')
 code=r'''
import copy,hashlib,http.client,json,os,socket,subprocess,time,threading,http.server
from pathlib import Path
class Target(http.server.BaseHTTPRequestHandler):
 def do_GET(self):
  body=b'x'*1048576;self.send_response(200);self.send_header('Content-Length',str(len(body)));self.end_headers();self.wfile.write(body)
 def log_message(self,*args):pass
target=http.server.ThreadingHTTPServer(('127.0.0.1',19816),Target)
threading.Thread(target=target.serve_forever,daemon=True).start()
subprocess.run(['openssl','req','-x509','-newkey','ec','-pkeyopt','ec_paramgen_curve:P-256','-nodes','-days','1','-subj','/CN=gate-test.invalid','-addext','subjectAltName=DNS:gate-test.invalid','-keyout','/var/tmp/dual.key','-out','/var/tmp/dual.pem'],check=True,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
cert=Path('/var/tmp/dual.pem').read_text();key=Path('/var/tmp/dual.key').read_text()
pub=subprocess.check_output(['openssl','x509','-in','/var/tmp/dual.pem','-pubkey','-noout'])
der=subprocess.check_output(['openssl','pkey','-pubin','-outform','DER'],input=pub)
import base64
pin=base64.b64encode(hashlib.sha256(der).digest()).decode()
state={'versions':{},'inbounds':[],'grants':[],'certificates':[{'id':90,'certificate_pem':cert,'key_pem':key}]}
def apply(value,expected=200):
 c=http.client.HTTPConnection('localhost',timeout=75);c.sock=socket.socket(socket.AF_UNIX);c.sock.connect('/run/migate/helper.sock')
 c.request('POST','/apply',json.dumps(value),{'Content-Type':'application/json'});r=c.getresponse();r.read()
 if r.status!=expected:
  Path('/var/tmp/dual-failed-state.json').write_text(json.dumps(value))
  subprocess.run(['/var/tmp/dual-agent.test','-test.run','^TestLiveApplyDiagnostic$','-test.v'],env={**os.environ,'MIGATE_DUAL_LIVE':'1'})
 assert r.status==expected,('apply',r.status);c.close()
def probe(protocol,pinvalue=pin,success=True):
 out={'type':protocol,'tag':'test','server':'127.0.0.1','server_port':24446,'tls':{'enabled':True,'server_name':'gate-test.invalid','certificate_public_key_sha256':[pinvalue]}}
 if protocol=='hysteria2':out['password']='isolated-test-password'
 else:out.update({'uuid':'11111111-1111-4111-8111-111111111111','password':'isolated-test-password','congestion_control':'bbr'})
 cfg={'log':{'level':'error'},'inbounds':[{'type':'http','listen':'127.0.0.1','listen_port':20992}],'outbounds':[out]}
 path=Path('/var/tmp/dual-client.json');path.write_text(json.dumps(cfg))
 proc=subprocess.Popen(['/opt/migate-gate/cores/sing-box','run','-c',str(path)],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
 try:
  time.sleep(.8)
  c=http.client.HTTPConnection('127.0.0.1',20992,timeout=7);c.request('GET','http://127.0.0.1:19816/test');r=c.getresponse();data=r.read();ok=r.status==200 and len(data)==1048576
 except (OSError,http.client.HTTPException):ok=False
 finally:proc.terminate();proc.wait(timeout=5)
 assert ok==success,('probe',protocol,success)
checks=[]
for protocol in ['hysteria2','tuic']:
 state['inbounds']=[{'id':90,'protocol':protocol,'core':'sing-box','port':24446,'enabled':True,'certificate_id':90,'tls_sni':'gate-test.invalid','tuic_congestion_control':'bbr'}]
 state['grants']=[{'grant_id':90,'inbound_id':90,'credential':{'uuid':'11111111-1111-4111-8111-111111111111','password':'isolated-test-password'},'allocated':33554432,'used':0,'enabled':True,'desired_enabled':True}]
 apply(state);probe(protocol);checks.append(protocol+' real client downloaded 1MiB with SPKI pin')
 Path('/var/tmp/dual-state.json').write_text(json.dumps(state))
 subprocess.run(['/var/tmp/dual-xray.test','-test.run','^TestLiveSingboxRaw$','-test.v'],env={**os.environ,'MIGATE_DUAL_LIVE':'1'},check=True)
 subprocess.run(['/var/tmp/dual-agent.test','-test.run','^TestLiveSingboxMeter$'],env={**os.environ,'MIGATE_DUAL_LIVE':'1'},check=True)
 checks.append(protocol+' actual gRPC per-user counters charged once by Agent collector')
 probe(protocol,base64.b64encode(bytes(32)).decode(),False);checks.append(protocol+' rejects mismatched SPKI without insecure mode')
 bad=copy.deepcopy(state);bad['inbounds'][0]['tls_sni']='wrong.invalid';apply(bad,422);probe(protocol);checks.append(protocol+' bad certificate candidate preserves running config')
 state['grants'][0]['enabled']=False;apply(state);probe(protocol,success=False);checks.append(protocol+' disabled credential denied')
 state['grants'][0]['enabled']=True;state['grants'][0]['expires']=int(time.time())-1;apply(state);probe(protocol,success=False);checks.append(protocol+' expired credential denied by helper')
 state['grants'][0].pop('expires');state['grants'][0]['used']=33554432;apply(state);probe(protocol,success=False);checks.append(protocol+' exhausted credential denied by helper')
Path('/var/tmp/dual-results.json').write_text(json.dumps(checks))
for file in ['/var/tmp/dual.key','/var/tmp/dual.pem','/var/tmp/dual-client.json']:Path(file).unlink(missing_ok=True)
'''
 docker('exec','-i',container,'python3','-',input=code)
 for check in json.loads(sh('cat /var/tmp/dual-results.json')):record(check)
except Exception as e:
 records.append({'check':'dual core acceptance','passed':False,'error':str(e)});raise
finally:
 sh('systemctl stop migate-gate migate-gate-sing-box migate-gate-xray')
 docker('stop',container)
 report=json.dumps({'version':args.version,'scope':'isolated actual systemd and kernels; helper enforcement, not end-to-end Agent meter','records':records},indent=2)
 Path('docs/evidence/dual-core.json').write_text(report,encoding='utf-8')
 Path(f'docs/evidence/dual-core-{args.version}.json').write_text(report,encoding='utf-8')
