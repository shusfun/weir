"""Actual CPU/history, Agent-only outage, baseline reset and service isolation."""
import json,subprocess,time
from pathlib import Path
from live_api import API,state
from remote import connect,run

a=API().admin();s=state();version=a.request('GET','/api/v1/admin/overview')['version'];checks=[]
def device():return next(d for d in a.request('GET','/api/v1/admin/devices')['items'] if d['id']==s['device_id'])
def host():return device()['metadata'].get('host_status',{})
def history():return a.request('GET',f"/api/v1/admin/devices/{s['device_id']}/host-history")
def wait(predicate,message,timeout=150):
 deadline=time.monotonic()+timeout
 while time.monotonic()<deadline:
  value=predicate()
  if value:return value
  time.sleep(2)
 raise RuntimeError(message)
def ui(mode):
 r=subprocess.run(['node','deploy/browser-host-history.cjs',mode],capture_output=True,text=True)
 if r.returncode:raise RuntimeError('Host history browser '+mode+' failed')
 print('Host history UI '+mode+' passed',flush=True)
fresh=wait(lambda:host() if host().get('cpu_percent') is not None else None,'Actual CPU unavailable')
assert 0<=fresh['cpu_percent']<=100 and 1<=fresh['cpu_interval_seconds']<=60
wait(lambda:any(p['cpu_percent'] is not None for p in history()['items']),'No actual minute history')
API().acceptance_user(s['test_users'][0]).request('GET',f"/api/v1/admin/devices/{s['device_id']}/host-history",expected=(403,))
API().request('GET',f"/api/v1/admin/devices/{s['device_id']}/host-history",expected=(401,))
ui('fresh');checks.append('Actual host CPU interval, accepted minute history, metric switching/details/error retry and ordinary/anonymous rejection')
with connect('gate') as ssh:
 def shell(cmd):
  code,out,_=run(ssh,cmd,timeout=50)
  if code:raise RuntimeError('Owned host history operation failed')
  return out
 # Independent real Linux counter delta. The interval is separately timed,
 # so record both values instead of claiming byte/time-identical windows.
 actual=json.loads(shell("""python3 - <<'PY'
import json,time
def read():return [int(x) for x in open('/proc/stat').readline().split()[1:9]]
a=read();at=time.monotonic();time.sleep(15);b=read();elapsed=time.monotonic()-at;d=[y-x for x,y in zip(a,b)];print(json.dumps({'cpu_percent':100*(sum(d)-d[3]-d[4])/sum(d),'interval_seconds':elapsed,'source':'independent fixed /proc/stat delta; distinct sample window'}))
PY"""))
 checks.append('Independent fixed /proc/stat delta sampled; distinct windows explicitly recorded, not asserted identical')
 try:
  shell('systemctl stop migate-gate.service');stopped=time.time()
  shell('systemctl is-active --quiet migate-gate-xray.service');shell('systemctl is-active --quiet relay154-x-ui.service')
  print('Owned Agent stopped; awaiting actual offline and persisted unknown history; cores/x-ui active',flush=True)
  wait(lambda:device()['status']=='offline','Offline state missing',90)
  unknown=wait(lambda:next((p for p in history()['items'] if p['quality']=='unknown' and __import__('datetime').datetime.fromisoformat(p['at'].replace('Z','+00:00')).timestamp()>stopped+60),None),'Offline minute not unknown',120)
  assert unknown['cpu_percent'] is None and unknown['memory_percent'] is None and unknown['disk_used_percent'] is None
  ui('stale');checks.append('Actual Agent outage records unknown CPU/memory/disk minute; stale current metrics unknown; proxy core and x-ui stay active')
 finally:shell('systemctl start migate-gate.service')
 first=wait(lambda:host() if device()['status']=='approved' else None,'Agent restart failed',45)
 # First sample must be captured during the first heartbeat window.
 assert first.get('cpu_percent') is None and first.get('cpu_interval_seconds') is None
 recovered=wait(lambda:host() if host().get('cpu_percent') is not None else None,'CPU baseline did not recover',60)
 wait(lambda:next((p for p in history()['items'] if p['cpu_percent'] is not None and p['observed_at']>=recovered['observed_at']),None),'Recovered minute history missing',100)
 ui('recovered');shell('systemctl is-active --quiet migate-gate.service migate-gate-xray.service relay154-x-ui.service')
 checks.append('Agent restart first CPU unknown; next interval restores CPU; real later minute persists, offline history remains; existing x-ui/core active')
Path('docs/evidence/live-host-history.json').write_text(json.dumps({'date':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),'version':version,'checks':checks,'fresh':fresh,'independent_counter_delta':actual,'first_after_restart':first,'recovered':recovered,'unknown_minute':unknown,'limits':'CPU is interval average; once/minute snapshots, not continuous high-frequency host monitoring; no user billing. History acquired only since this release.','cleanup':'owned Agent active; no grant/config/core changes','visual':'four widths captured; manual review pending'},indent=2),encoding='utf-8')
print('Real CPU/history, persisted outage, first baseline and restart recovery passed')
