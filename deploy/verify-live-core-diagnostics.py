"""UI diagnostics must preserve runtime PIDs/configs and ordinary-user isolation."""
import importlib.util,json,subprocess,time
from pathlib import Path
from live_api import API,state
from remote import connect,run

a=API().admin();s=state();did=s['device_id']
def snapshot():
 with connect('gate') as ssh:
  command="systemctl show migate-gate-xray.service migate-gate-sing-box.service relay154-x-ui.service --property=Id,MainPID,ActiveState,ExecMainStartTimestampMonotonic; sha256sum /etc/migate-gate/cores/xray.json /etc/migate-gate/cores/sing-box.json"
  status,out,_=run(ssh,command)
  if status:raise RuntimeError('Cannot verify owned runtime snapshot')
  return out
before=snapshot()
result=subprocess.run(['node','deploy/browser-core-diagnostics.cjs'],capture_output=True,text=True)
if result.returncode:
 for line in result.stderr.splitlines():
  if line.startswith('Core diagnostics browser failed at '):print(line)
 raise RuntimeError('Diagnostics browser acceptance failed; private context omitted')
ids=json.loads(result.stdout.strip().splitlines()[-1])['task_ids'];observations=[]
for tid in ids:
 task=a.wait_task(tid);r=task['result'];c=r.get('config',{});j=r.get('journal',{})
 if c.get('validity')!='fresh' or c.get('validation')!='passed' or j.get('validity')!='fresh' or r.get('read_only') is not True:raise RuntimeError('Real diagnostic returned incomplete observation')
 if set(r)-{'core','observed_at','source','validity','read_only','config','journal'} or set(c)-{'sha256','bytes','inbounds','outbounds','rules','validity','modified_at','validation'}:raise RuntimeError('Unexpected diagnostic fields')
 observations.append({'task_id':tid,'core':r['core'],'configuration_validation':c['validation'],'journal_validity':j['validity'],'observed_at':r['observed_at']})
if before!=snapshot():raise RuntimeError('Read-only diagnostic restarted service or changed config')
ordinary=API().acceptance_user(s['test_users'][0]);ordinary.request('POST',f'/api/v1/admin/devices/{did}/tasks',{'operation':'core.inspect','payload':{'core':'xray'},'confirm':True},expected=(403,))
for payload in [{'core':'x-ui'},{'core':'xray','path':'/etc/passwd'},{'core':'xray','command':'restart'}]:
 a.request('POST',f'/api/v1/admin/devices/{did}/tasks',{'operation':'core.inspect','payload':payload,'confirm':True},expected=(400,))
spec=importlib.util.spec_from_file_location('client',Path(__file__).with_name('verify-live-client.py'));client=importlib.util.module_from_spec(spec);spec.loader.exec_module(client)
for i,u in enumerate(s['test_users'],1):client.run_probe(u,i)
Path('docs/evidence/live-core-diagnostics.json').write_text(json.dumps({'version':a.request('GET','/api/v1/admin/overview')['version'],'date':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),'observations':observations,'checks':['UI submit and actual helper validation of both core configurations','both journals classified locally; config projection allowlist','owned core PIDs/start times/config hashes and x-ui unchanged','ordinary user forbidden; arbitrary core/path/command rejected','two independent users still connect directly to Gate'],'visual':'four screenshots captured; human review pending'},indent=2),encoding='utf-8')
print('Real UI diagnostics, both core validators, nonmutation, isolation and direct clients passed')
