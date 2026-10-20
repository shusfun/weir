"""Create the app's restricted ACL without restarting the shared Redis."""
import hashlib
import json
import os
from pathlib import Path
import secrets
import sys
from remote import credentials, connect, run

ops = Path(os.environ['LOCALAPPDATA']) / 'MiGate/operations/deployment.json'
state = json.loads(ops.read_text()) if ops.exists() else {}
if 'redis_password' not in state:
    state['redis_password'] = secrets.token_urlsafe(36)
    state['admin_email'] = 'admin@sub.caowo.baby'
    state['admin_password'] = secrets.token_urlsafe(30)
    ops.write_text(json.dumps(state), encoding='utf-8')
password_hash = hashlib.sha256(state['redis_password'].encode()).hexdigest()
# The remote script reads existing administrative auth in memory. Neither the
# current password nor the generated application password is printed or in argv.
remote_script = r'''
import pathlib,shlex,subprocess,time,shutil,json
conf=pathlib.Path('/opt/1panel/apps/redis/redis/conf/redis.conf')
content=conf.read_text()
admin_password=''
for line in content.splitlines():
 parts=shlex.split(line,comments=True)
 if parts and parts[0]=='requirepass': admin_password=parts[1]
cmd=json.loads(subprocess.check_output(['docker','inspect','1Panel-redis-1BZN']))[0]['Config']['Cmd']
if '--requirepass' in cmd: admin_password=cmd[cmd.index('--requirepass')+1]
def resp(items):
 raw=[str(v).encode() for v in items]
 return b'*'+str(len(raw)).encode()+b'\r\n'+b''.join(b'$'+str(len(v)).encode()+b'\r\n'+v+b'\r\n' for v in raw)
def cli(command):
 wire=(resp(['AUTH',admin_password]) if admin_password else b'')+resp(command)
 p=subprocess.run(['docker','exec','-i','1Panel-redis-1BZN','redis-cli','--pipe'],input=wire,capture_output=True)
 if p.returncode or b'errors: 0' not in p.stdout:
  diagnostic=(p.stdout+p.stderr).decode(errors='replace')
  if admin_password: diagnostic=diagnostic.replace(admin_password,'[redacted]')
  diagnostic=diagnostic.replace('#HASH','[redacted]')
  raise RuntimeError('Redis administrative operation failed: '+diagnostic)
acl=['on','resetpass','#HASH','~migate:*','&migate:*','-@all','+ping','+hello','+get','+set','+del','+getdel','+incr','+expire','+scan','+eval','+evalsha','+script|load','+pttl','+dump','+restore','+unlink','+client|setinfo']
entry='user migate '+' '.join(acl)
matching=[line for line in content.splitlines() if line.strip().startswith('user migate ')]
if matching and matching!=[entry]: raise RuntimeError('Existing different migate ACL; refusing to overwrite')
backup=pathlib.Path('/var/backups/migate')
backup.mkdir(mode=0o700,parents=True,exist_ok=True)
if not matching:
 shutil.copy2(conf,backup/('redis-config-before-migate-'+time.strftime('%Y%m%dT%H%M%S')))
 # Persist only the restricted ACL, leaving all existing Redis settings intact.
 with conf.open('a') as f: f.write('\n'+entry+'\n');f.flush()
cli(['ACL','SETUSER','migate']+acl)
print('Dedicated Redis ACL applied and persisted; shared instance not restarted')
'''.replace('#HASH', '#' + password_hash)
with connect('control') as client:
    status, out, err = run(client, 'python3 -', remote_script)
    print(out, end='')
    print(err, end='', file=sys.stderr)
    sys.exit(status)
