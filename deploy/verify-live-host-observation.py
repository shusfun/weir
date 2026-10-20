"""Compare actual unprivileged telemetry to the same Gate's fixed system files."""
import json,subprocess,time
from pathlib import Path
from live_api import API,state
from remote import connect,run

a=API().admin();s=state();version=a.request('GET','/api/v1/admin/overview')['version'];checks=[]
def device():return next(d for d in a.request('GET','/api/v1/admin/devices')['items'] if d['id']==s['device_id'])
def sample():return device()['metadata'].get('host_status',{})
def wait_sample():
 deadline=time.monotonic()+70
 while time.monotonic()<deadline:
  h=sample()
  if device()['status']=='approved' and h.get('validity')=='available':return h
  time.sleep(1)
 raise RuntimeError('Real host telemetry unavailable')
def ui(mode):
 r=subprocess.run(['node','deploy/browser-host-observation.cjs',mode],capture_output=True,text=True)
 if r.returncode:raise RuntimeError('Actual host observation browser failed')
h=wait_sample()
u=API().acceptance_user(s['test_users'][0]);u.request('GET','/api/v1/admin/devices',expected=(403,));API().request('GET','/api/v1/admin/devices',expected=(401,))
with connect('gate') as ssh:
 def shell(cmd):
  code,out,_=run(ssh,cmd)
  if code:raise RuntimeError('Owned host observation operation failed')
  return out
 # Fixed paths only; no server credentials/configuration or raw journal captured.
 script="""python3 - <<'PY'
import json,os
m={}
for line in open('/proc/meminfo'):
 f=line.split()
 if f[0] in ['MemTotal:','MemAvailable:']:m[f[0]]=int(f[1])*1024
r=t=0
for line in open('/proc/net/dev'):
 if ':' not in line:continue
 name,fields=line.split(':',1)
 if name.strip()=='lo':continue
 f=fields.split();r+=int(f[0]);t+=int(f[8])
v=os.statvfs('/var/lib/migate-gate')
print(json.dumps({'memory_total':m['MemTotal:'],'memory_available':m['MemAvailable:'],'disk_total':v.f_blocks*v.f_frsize,'disk_available':v.f_bavail*v.f_frsize,'network_rx':str(r),'network_tx':str(t),'uptime':float(open('/proc/uptime').read().split()[0]),'cpus':len(os.sched_getaffinity(0))}))
PY"""
 actual=json.loads(shell(script))
 assert h['memory_total']==actual['memory_total'] and h['disk_total']==actual['disk_total'] and h['logical_cpus']==actual['cpus']
 assert abs(h['memory_available']-actual['memory_available'])<128*1024*1024 and abs(h['disk_available']-actual['disk_available'])<128*1024*1024
 assert 0<=actual['uptime']-h['uptime_seconds']<60 and int(actual['network_rx'])>=int(h['network_rx']) and int(actual['network_tx'])>=int(h['network_tx'])
 checks.append('authenticated Agent heartbeat matches actual Gate procfs/statfs totals, CPU affinity, uptime and monotonic NIC counters; available memory/disk within 128 MiB sampling tolerance')
 ui('fresh')
 try:
  shell('systemctl stop migate-gate.service')
  # Existing proxy cores and x-ui continue; only the owned Agent is stopped.
  shell('systemctl is-active --quiet migate-gate-xray.service');shell('systemctl is-active --quiet relay154-x-ui.service')
  print('Owned Agent stopped; waiting real 65-second telemetry expiry, proxy core and x-ui active',flush=True)
  for _ in range(13):time.sleep(5)
  assert device()['status']=='offline';ui('stale')
  checks.append('actual Agent stop for 65 seconds marks every host metric unknown; owned proxy core and existing x-ui remain active')
 finally:shell('systemctl start migate-gate.service')
 h=wait_sample();ui('recovered');checks.append('Agent restart restores fresh real telemetry; ordinary/anonymous host administration rejected')
Path('docs/evidence/live-host-observation.json').write_text(json.dumps({'version':version,'date':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),'checks':checks,'sample':h,'limits':'load averages are not CPU percent; NIC counters include all non-loopback interfaces and may duplicate virtual traffic; no user accounting; no host historical curves implemented','cleanup':'Agent active; no resource/grant/config changes','visual':'four widths captured; manual review pending'},indent=2),encoding='utf-8')
print('Real host telemetry, system comparison, actual expiry and restart passed')
