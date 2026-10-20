"""Isolated initialized center, real systemd + PG18/Redis8 lifecycle acceptance.

Never uses deployment.json, remote hosts, host mounts or production credentials.
Requires the dedicated test compose stack and a fresh named test container.
"""
import argparse,json,subprocess,time
from pathlib import Path

p=argparse.ArgumentParser();p.add_argument('--version',required=True);args=p.parse_args()
name='migate-initialized-acceptance';results=[]
def cmd(*argv,check=True,input=None):
 r=subprocess.run(argv,text=True,encoding='utf-8',errors='replace',capture_output=True,input=input)
 if check and r.returncode:raise RuntimeError(' '.join(argv[:4])+' failed: '+r.stderr[-1800:]+r.stdout[-1800:])
 return r
def shell(script,check=True):return cmd('docker','exec',name,'sh','-eu','-c',script,check=check)
def python(script):return cmd('docker','exec','-i',name,'python3','-',input=script)
def record(check):results.append({'check':check,'passed':True});print(check,flush=True)

if cmd('docker','container','inspect',name,check=False).returncode==0:raise RuntimeError('Existing named container: inspect and explicitly remove only the previous labeled test container first')
if cmd('docker','ps','-q','--filter','label=com.docker.compose.project=migate-local-test').stdout.strip():raise RuntimeError('Dedicated database stack is already in use; do not reset another active verification')
cmd('docker','compose','-f','deploy/test.compose.yml','up','-d','--wait')
try:
 cmd('docker','run','-d','--name',name,'--label','migate.acceptance=initialized-lifecycle','--network','migate-local-test_default','--privileged','--cgroupns=private','--tmpfs','/run','--tmpfs','/run/lock','--tmpfs','/tmp','migate-systemd-data-test:pg18')
 deadline=time.monotonic()+30
 while time.monotonic()<deadline:
  if shell('systemctl is-system-running',check=False).returncode==0:break
  time.sleep(1)
 else:raise RuntimeError('actual systemd did not become ready')
 cmd('docker','cp',f'dist/migate-{args.version}-linux-amd64/.',name+':/release')
 shell('cd /release; sh install.sh control 127.0.0.1:19810 http://localhost:19810')
 api='''import json,urllib.request,urllib.error,http.cookiejar
from pathlib import Path
jar=http.cookiejar.CookieJar();opener=urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))
def request(method,path,data=None,token=None,expected=200):
 headers={'Content-Type':'application/json','Origin':'http://localhost:19810'}
 if token:headers['Authorization']='Bearer '+token
 req=urllib.request.Request('http://localhost:19810'+path,data=None if data is None else json.dumps(data).encode(),headers=headers,method=method)
 try:r=opener.open(req,timeout=15)
 except urllib.error.HTTPError as e:r=e
 assert r.status==expected,(path,r.status)
 return json.loads(r.read())
def login():request('POST','/api/v1/auth/login',{'email':'admin@acceptance.test','password':'isolated-lifecycle-test-password'})
'''
 python(api+'''
token=Path('/etc/migate-control/setup-token').read_text().strip()
db={'host':'postgres','port':5432,'user':'gate','password':'local-test-only','database':'gate','ssl_mode':'disable'}
redis={'address':'redis:6379','username':'migate','password':'local-test-only'}
request('POST','/api/v1/setup/test-db',db,token)
request('POST','/api/v1/setup/test-redis',redis,token)
request('POST','/api/v1/setup/install',{'database':db,'redis':redis,'email':'admin@acceptance.test','password':'isolated-lifecycle-test-password'},token,201)
login()
request('POST','/api/v1/admin/users',{'email':'retained@acceptance.test','password':'isolated-user-test-password','quota':1073741824,'expires':0},expected=201)
''')
 record('real HTTP initialization and administrator user creation on actual systemd')
 # Sentinels belong to different applications in the isolated database services.
 cmd('docker','compose','-f','deploy/test.compose.yml','exec','-T','postgres','psql','-U','gate','-d','gate','-c',"CREATE TABLE public.lifecycle_sentinel(value text); INSERT INTO public.lifecycle_sentinel VALUES ('preserved');")
 cmd('docker','compose','-f','deploy/test.compose.yml','exec','-T','redis','redis-cli','SET','other-app:lifecycle','preserved')
 cmd('docker','compose','-f','deploy/test.compose.yml','exec','-T','redis','redis-cli','SET','migate:acceptance-business','retained')
 base='/release/migate-manage';manifest='/etc/migate-control/install-manifest.json'
 uninstall=f'{base} uninstall --manifest {manifest}'
 dump=' --pg-dump /usr/local/bin/pg_dump'
 shell(f'{base} backup --manifest {manifest} --backup-dir /var/backups/acceptance{dump} --confirm control:backup')
 snapshot=shell('find /var/backups/acceptance -name "*.snapshot.json" | sort | tail -1').stdout.strip()
 assert snapshot.startswith('/var/backups/acceptance/')
 shell('systemctl is-active migate-control; /opt/migate-control/current/migate-control --health-check')
 record('initialized backup captures configuration PG18 and Redis and resumes actual service')
 # Intentionally wrong dump helper: neither upgrade nor purge may continue.
 shell("printf '#!/bin/sh\\nexit 1\\n' > /failed-pg-dump; chmod 755 /failed-pg-dump")
 failed=shell(uninstall+' --purge --backup-dir /var/backups/failure --pg-dump /failed-pg-dump --confirm control:uninstall:purge',check=False)
 assert failed.returncode!=0
 shell('systemctl is-active migate-control; /opt/migate-control/current/migate-control --health-check')
 python(api+"login();assert any(u['email']=='retained@acceptance.test' for u in request('GET','/api/v1/admin/users')['items'])")
 record('backup failure stops initialized purge and resumes intact actual service')
 dry=json.loads(shell(uninstall+' --dry-run').stdout)
 assert not any('migate schema' in str(x) for x in dry)
 shell(uninstall+' --confirm control:uninstall')
 shell('test -f /etc/migate-control/config.json; test ! -e /opt/migate-control/current; ! systemctl is-active --quiet migate-control')
 shell('cd /release; sh install.sh control 127.0.0.1:19810 http://localhost:19810 /usr/local/bin/pg_dump')
 python(api+"login();assert len(request('GET','/api/v1/admin/users')['items'])==2;request('POST','/api/v1/setup/install',{},expected=409)")
 record('ordinary uninstall and reinstall preserve initialized state accounts and installation lock')
 shell(uninstall+f' --purge --backup-dir /var/backups/purge{dump} --confirm control:uninstall:purge')
 shell('test ! -e /etc/migate-control; test ! -e /opt/migate-control; test ! -e /var/lib/migate-control')
 count=cmd('docker','compose','-f','deploy/test.compose.yml','exec','-T','postgres','psql','-U','gate','-d','gate','-Atc',"SELECT count(*) FROM pg_namespace WHERE nspname='migate'").stdout.strip()
 assert count=='0'
 assert cmd('docker','compose','-f','deploy/test.compose.yml','exec','-T','postgres','psql','-U','gate','-d','gate','-Atc','SELECT value FROM public.lifecycle_sentinel').stdout.strip()=='preserved'
 assert cmd('docker','compose','-f','deploy/test.compose.yml','exec','-T','redis','redis-cli','GET','other-app:lifecycle').stdout.strip()=='preserved'
 assert cmd('docker','compose','-f','deploy/test.compose.yml','exec','-T','redis','redis-cli','GET','migate:acceptance-business').stdout.strip()==''
 record('initialized purge deletes owned roots schema and Redis namespace; foreign data survives')
 shell('cd /release; sh install.sh control 127.0.0.1:19810 http://localhost:19810')
 shell(f'{base} restore --manifest {manifest} --snapshot {snapshot} --backup-dir /var/backups/pre-restore{dump} --pg-restore /usr/local/bin/pg_restore --confirm control:restore')
 # restore intentionally requires an explicit start after replacing the stores.
 shell('systemctl start migate-control; i=0; until /opt/migate-control/current/migate-control --health-check; do i=$((i+1)); test "$i" -lt 20; sleep 1; done; systemctl is-active migate-control')
 python(api+"login();assert any(u['email']=='retained@acceptance.test' for u in request('GET','/api/v1/admin/users')['items']);request('POST','/api/v1/setup/install',{},expected=409)")
 assert cmd('docker','compose','-f','deploy/test.compose.yml','exec','-T','redis','redis-cli','GET','migate:acceptance-business').stdout.strip()=='retained'
 record('purge reinstall checksummed restore restores actual healthy initialized center and business data')
 report={'date':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),'version':args.version,'status':'passed','environment':'dedicated Docker, actual systemd PID1, PG18/Redis8 and compiled application','limitations':'local isolation; no production uninstall; paired Gate recovery tracked separately','snapshot':snapshot,'records':results}
 Path(f'docs/evidence/initialized-lifecycle-{args.version}.json').write_text(json.dumps(report,indent=2))
 Path('docs/evidence/initialized-lifecycle.json').write_text(json.dumps(report,indent=2))
except Exception as error:
 Path(f'docs/evidence/initialized-lifecycle-{args.version}.json').write_text(json.dumps({'version':args.version,'status':'failed','date':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),'records':results,'error':str(error)},indent=2))
 raise
finally:
 cmd('docker','stop',name,check=False)
 cmd('docker','compose','-f','deploy/test.compose.yml','down',check=False)
