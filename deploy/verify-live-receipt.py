"""Re-lease one applied test task to reproduce a lost center acknowledgement.

Only the explicitly created core.restart task row is changed. Existing device
SQLite receipt must prevent a second core restart and resource duplication.
"""
import json,shlex,time
from pathlib import Path
from live_api import API,state
from remote import connect,run

a=API().admin();s=state();did=s['device_id']
tid=a.request('POST',f'/api/v1/admin/devices/{did}/tasks',{'operation':'core.restart','payload':{},'confirm':True})['id'];a.wait_task(tid)
with connect('gate') as gate,connect('control') as center:
 def marker():
  code,out,err=run(gate,'systemctl show migate-gate-xray -p MainPID -p ExecMainStartTimestampMonotonic')
  if code:raise RuntimeError(err)
  return out
 before=marker()
 script='''import json,subprocess,os
c=json.load(open('/etc/migate-control/config.json'))['database']
env=os.environ.copy();env['PGPASSWORD']=c['password']
query="UPDATE migate.tasks SET status='running',leased_until=to_timestamp(0) WHERE id=%d AND device_id=%d AND operation='core.restart' AND status='applied' RETURNING id;"
r=subprocess.run(['docker','exec','-i','-e','PGPASSWORD','1Panel-postgresql-EcCX','psql','-U',c['user'],'-d',c['database'],'-At','-v','ON_ERROR_STOP=1'],input=query,text=True,capture_output=True,env=env)
if r.returncode or str(%d) not in r.stdout.splitlines():raise RuntimeError('Could not re-lease scoped acceptance task')
print('Scoped task receipt-loss injected')
'''%(tid,did,tid)
 code,out,err=run(center,'python3 -',script)
 if code:raise RuntimeError('Receipt injection failed: '+err)
 result=a.wait_task(tid)
 assert result['status']=='applied'
 assert marker()==before,'Repeated task restarted the core'
 q="import sqlite3,json;d=sqlite3.connect('/var/lib/migate-gate/agent.db');s=json.loads(d.execute('select data from state where id=1').fetchone()[0]);print(json.dumps({'receipt_count':d.execute('select count(*) from receipts where id=?',("+str(tid)+",)).fetchone()[0],'grant_ids':[g['grant_id'] for g in s['grants']],'inbound_ids':[g['id'] for g in s['inbounds']]}))"
 code,out,err=run(gate,'python3 -c '+shlex.quote(q))
 if code:raise RuntimeError(err)
 local=json.loads(out);assert local['receipt_count']==1
 assert len(local['grant_ids'])==len(set(local['grant_ids'])) and len(local['inbound_ids'])==len(set(local['inbound_ids']))
Path('docs/evidence/live-receipt.json').write_text(json.dumps({'date':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),'task_id':tid,'checks':['expired lease redelivered','persisted receipt returned without core restart','single receipt and no duplicate grants/inbounds'],'passed':True,'injection':'re-leased only own completed acceptance task; equivalent lost center acknowledgement'},indent=2))
print('Real task redelivery and persistent receipt idempotence passed')
