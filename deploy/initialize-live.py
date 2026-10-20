"""Initialize the authorized center through its public HTTPS origin."""
import json
import os
from pathlib import Path
from remote import credentials, connect, run

state=json.loads((Path(os.environ['LOCALAPPDATA'])/'MiGate/operations/deployment.json').read_text())
payload={'database':{'host':'127.0.0.1','port':5432,'user':'gate','database':'gate','password':credentials()['database_password'],'ssl_mode':'disable'},
         'redis':{'address':'127.0.0.1:6379','username':'migate','password':state['redis_password'],'tls':False},
         'email':state['admin_email'],'password':state['admin_password']}
script=r'''
import json,urllib.request,urllib.error,pathlib,os
base='https://sub.caowo.baby'
def req(path,data=None,token=None):
 headers={'Content-Type':'application/json','Origin':base}
 if token: headers['Authorization']='Bearer '+token
 r=urllib.request.Request(base+'/api/v1'+path,None if data is None else json.dumps(data).encode(),headers=headers)
 with urllib.request.urlopen(r,timeout=20) as response: return response.status,json.load(response)
status,current=req('/setup/status')
if current['installed']:
 print('Center already initialized; configuration preserved')
else:
 token=pathlib.Path('/etc/migate-control/setup-token').read_text().strip()
 try: req('/setup/test-db',payload['database'],'invalid')
 except urllib.error.HTTPError as e:
  assert e.code==401
 else: raise RuntimeError('Unauthenticated initialization accepted')
 assert req('/setup/test-db',payload['database'],token)[1]['ok']
 assert req('/setup/test-redis',payload['redis'],token)[1]['ok']
 assert req('/setup/install',payload,token)[0]==201
 access=pathlib.Path('/root/migate-admin-access.json')
 fd=os.open(access,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
 with os.fdopen(fd,'w') as f: json.dump({'url':base,'email':payload['email'],'password':payload['password']},f)
 print('HTTPS initialization passed; PG/Redis connected; protected administrator handoff saved')
'''
with connect('control') as c:
 code,out,err=run(c,'python3 -','payload='+repr(payload)+'\n'+script,timeout=90)
 print(out,end='')
 if code: raise RuntimeError('Initialization failed: '+err)
