"""Fixed, disposable Gate fixture owned by a persistent per-run intent.

Its HTTP source carries synthetic credentials only; the proxy binds loopback.
No existing service/config is written. Paths and units are derived from the
runner's random prefix, and cleanup checks ownership and every registered file.
"""
import base64
import json
import os
from pathlib import Path
import re
import shlex

from remote import connect,run

SERVER = r'''
from http.server import BaseHTTPRequestHandler,ThreadingHTTPServer
import json
from pathlib import Path
ROOT=Path(__file__).resolve().parent
class Handler(BaseHTTPRequestHandler):
 def do_GET(self):
  data=json.loads((ROOT/'fixture.json').read_text())
  if self.path!='/'+data['path']:
   self.send_error(404);return
  if data.get('fail'):
   self.send_error(503);return
  content='\n'.join(data['links']).encode()
  self.send_response(200);self.send_header('Content-Type','text/plain');self.send_header('Content-Length',str(len(content)));self.end_headers();self.wfile.write(content)
 def log_message(self,*_):pass
ThreadingHTTPServer(('0.0.0.0',24447),Handler).serve_forever()
'''

REMOTE = r'''
import json,os,stat,socket,subprocess,sys
from pathlib import Path
data=json.load(sys.stdin)
os.umask(0o077)
prefix=data['fixture_prefix'];run_id=data['run_id'];action=data['action']
import re
assert re.fullmatch(r'migate-acceptance-[a-f0-9]{12}-',prefix)
assert re.fullmatch(r'[a-f0-9]{32}',run_id)
parent=Path(data['root']);root=parent/prefix
assert str(parent) in ('/var/lib/migate-acceptance','/var/lib/migate-gate/acceptance')
intent=parent/(prefix+'intent.json')
files=['owner.json','fixture.py','fixture.json','core.json']
units=[prefix+'source.service',prefix+'proxy.service']
def regular(path):
 s=path.lstat();assert stat.S_ISREG(s.st_mode) and s.st_uid==0
def checked_parent():
 for p in [Path('/var'),Path('/var/lib'),parent]:
  if p.exists():
   s=p.lstat();assert stat.S_ISDIR(s.st_mode) and s.st_uid==0
if str(parent)=='/var/lib/migate-gate/acceptance':
 # The first fixture failed before its intent: the project data directory is
 # service-owned. This compatibility path only verifies absence; it never
 # deletes, follows, creates or adopts anything in that writable namespace.
 assert action=='cleanup' and not root.exists() and not root.is_symlink() and not intent.exists() and not intent.is_symlink()
 print('Legacy pre-intent fixture absent; no resources modified');sys.exit(0)
checked_parent()
if action=='prepare':
 parent.mkdir(mode=0o700,exist_ok=True)
 assert not root.exists() and not root.is_symlink() and not intent.exists() and not intent.is_symlink()
 for unit in units:
  state=subprocess.run(['systemctl','show',unit,'-p','LoadState','--value'],text=True,capture_output=True)
  assert state.returncode in (0,1) and state.stdout.strip()=='not-found'
 sockets=[]
 for port in [24447,24448]:
  for proto in [socket.SOCK_STREAM,socket.SOCK_DGRAM]:
   s=socket.socket(socket.AF_INET,proto);s.bind(('0.0.0.0',port));sockets.append(s)
 with intent.open('x') as f:json.dump({'run_id':run_id,'files':files,'units':units},f)
 intent.chmod(0o600)
 root.mkdir(mode=0o700)
 content=data['contents'];assert set(content)==set(files)-{'owner.json'}
 (root/'owner.json').write_text(json.dumps({'run_id':run_id}))
 for name,text in content.items():(root/name).write_text(text)
 for p in root.iterdir():p.chmod(0o600)
 for s in sockets:s.close()
 commands=[['/usr/bin/python3',str(root/'fixture.py')],['/opt/migate-gate/cores/xray','run','-config',str(root/'core.json')]]
 for unit,command in zip(units,commands):
  subprocess.run(['systemd-run','--quiet','--collect','--unit='+unit,'--property=NoNewPrivileges=yes','--property=ProtectSystem=strict','--property=ProtectHome=yes','--property=PrivateTmp=yes',*command],check=True)
 print('Owned Gate fixture started')
elif action=='update':
 regular(intent);assert json.loads(intent.read_text())['run_id']==run_id
 regular(root/'owner.json');assert json.loads((root/'owner.json').read_text())['run_id']==run_id
 regular(root/'fixture.json');(root/'fixture.json').write_text(data['content'])
 print('Owned source fixture updated')
elif action=='cleanup':
 if not intent.exists():
  assert not root.exists() and not root.is_symlink();sys.exit(0)
 regular(intent);expected=json.loads(intent.read_text());assert expected=={'run_id':run_id,'files':files,'units':units}
 if root.exists():
  s=root.lstat();assert stat.S_ISDIR(s.st_mode) and s.st_uid==0
  assert all(p.name in files for p in root.iterdir())
  for p in root.iterdir():regular(p)
  if (root/'owner.json').exists():assert json.loads((root/'owner.json').read_text())['run_id']==run_id
 for unit in units:
  # Units are transient names reserved before intent was written. Never touch
  # a persistent unit which happens to have appeared under the same name.
  state=subprocess.run(['systemctl','show',unit,'-p','LoadState','-p','FragmentPath'],text=True,capture_output=True)
  properties=dict(line.split('=',1) for line in state.stdout.splitlines() if '=' in line)
  assert state.returncode in (0,1) and properties.get('LoadState') in ('loaded','not-found')
  fragment=properties.get('FragmentPath')
  assert fragment==('/run/systemd/transient/'+unit) or properties['LoadState']=='not-found' and fragment==''
  subprocess.run(['systemctl','stop',unit],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
 if root.exists():
  for name in files:
   path=root/name
   if path.exists():path.unlink()
  root.rmdir()
 intent.unlink()
 print('Owned Gate fixture removed and paths verified')
else:raise ValueError('unknown fixture operation')
'''


def operation(record,action,**extra):
    if record['cleanup'].get('handler')!='outbound-pool':raise RuntimeError('Fixture scope missing')
    payload={k:record[k] for k in ['fixture_prefix','run_id']}
    payload['root']=record['cleanup']['fixture']['root']
    payload.update(action=action,**extra)
    # Program text is fixed; input, credentials and file contents stay on stdin.
    program=base64.b64encode(REMOTE.encode()).decode()
    command='python3 -c '+shlex.quote('import base64;exec(base64.b64decode('+repr(program)+'))')
    with connect('gate') as client:
        code,out,error=run(client,command,stdin=json.dumps(payload),timeout=120)
    if code:
        from live_api import OPS
        diagnostic=OPS/('pool-fixture-'+action+'-diagnostic.log')
        diagnostic.write_text(out+'\n'+error,encoding='utf-8');os.chmod(diagnostic,0o600)
        raise RuntimeError('Owned Gate fixture '+action+' failed; restricted diagnostics saved; keep recovery journal')


def prepare_fixture(record,fixture,password):
    core={'log':{'loglevel':'warning'},'inbounds':[{'listen':'127.0.0.1','port':24448,'protocol':'http',
            'settings':{'accounts':[{'user':'acceptance','pass':password}]}}],
          'outbounds':[{'protocol':'freedom'}]}
    operation(record,'prepare',contents={'fixture.py':SERVER,'fixture.json':json.dumps(fixture),
                                        'core.json':json.dumps(core)})


def update_fixture(record,fixture):operation(record,'update',content=json.dumps(fixture))
def cleanup_fixture(record):operation(record,'cleanup')
