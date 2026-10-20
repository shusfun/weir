"""Actual settled traffic and control-link interruption, without fake samples."""
import importlib.util,json,secrets,shlex,time
from pathlib import Path
from live_api import API,state
from remote import connect,run
spec=importlib.util.spec_from_file_location('client',Path(__file__).with_name('verify-live-client.py'));client=importlib.util.module_from_spec(spec);spec.loader.exec_module(client)
api=API().admin();s=state();did=s['device_id'];checks=[];started=time.time()
endpoint=f'/api/v1/admin/devices/{did}/traffic-history'

def wait_for(predicate,label,timeout=210):
 deadline=time.monotonic()+timeout;last_notice=0
 while time.monotonic()<deadline:
  history=api.request('GET',endpoint)
  if predicate(history['items']):print(label+' passed',flush=True);return history
  if time.monotonic()-last_notice>30:print('Waiting for actual '+label,flush=True);last_notice=time.monotonic()
  time.sleep(2)
 raise RuntimeError('Timed out waiting for actual '+label)

fixture_path='/history-'+secrets.token_hex(12)
fixture='''from http.server import BaseHTTPRequestHandler,HTTPServer
class H(BaseHTTPRequestHandler):
 def do_GET(self):
  if self.path != PATH:self.send_response(404);self.end_headers();return
  data=b'x'*(2<<20);self.send_response(200);self.send_header('Content-Length',str(len(data)));self.end_headers();self.wfile.write(data)
 def log_message(self,*args):pass
HTTPServer(('127.0.0.1',19816),H).serve_forever()
'''
link_down=False;fixture_started=False
with connect('gate') as gate:
 try:
  ready=wait_for(lambda rows: bool(rows) and rows[-1]['quality']=='fresh','settlement baseline')
  before=ready['items'][-1]['at']
  code,out,_=run(gate,"ss -H -lnt 'sport = :19816'")
  if code or out.strip():raise RuntimeError('Private fixture port is occupied')
  code,_,_=run(gate,'systemd-run --unit=migate-acceptance-history --collect --property=User=nobody /usr/bin/python3 -c '+shlex.quote('PATH='+repr(fixture_path)+'\n'+fixture))
  if code:raise RuntimeError('History download fixture failed to start')
  fixture_started=True;time.sleep(1)
  result=client.run_probe(s['test_users'][0],1,url='http://127.0.0.1:19816'+fixture_path)
  if result['download_bytes'] != 2<<20:raise RuntimeError('Actual download size mismatch')
  positive=wait_for(lambda rows:any(r['at']>before and r['quality']=='fresh' and int(r['delta_bytes'] or '0')>=2<<20 for r in rows),'real download interval')
  checks.append({'check':'2 MiB actual Xray download via Gate loopback fixture reflected in accepted-settlement sample','bytes':2<<20,'passed':True})
  disconnect=time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime())
  link_down=True
  code,_,_=run(gate,'systemctl stop migate-gate-tunnel.service')
  if code:raise RuntimeError('Cannot pause owned control tunnel')
  # User traffic still travels via Gate while management is disconnected.
  client.run_probe(s['test_users'][1],2)
  unknown=wait_for(lambda rows:any(r['at']>disconnect and r['quality']=='unknown' and r.get('reason') in ['device_offline','meter_unknown'] and r['delta_bytes'] is None for r in rows),'disconnected unknown sample')
  checks.append({'check':'owned control tunnel stopped; real user proxy continues; elapsed heartbeat/report produces unknown interval, never zero','passed':True})
 finally:
  if link_down:
   code,_,_=run(gate,'systemctl start migate-gate-tunnel.service')
   if code:raise RuntimeError('Owned control tunnel restoration failed')
  if fixture_started:
   code,_,_=run(gate,'systemctl stop migate-acceptance-history.service')
   if code:raise RuntimeError('Owned history fixture cleanup failed')
recovered_at=time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime())
recovered=wait_for(lambda rows:any(r['at']>recovered_at and r['quality']=='fresh' and r['delta_bytes'] is not None for r in rows),'restored valid interval')
client.run_probe(s['test_users'][0],1);client.run_probe(s['test_users'][1],2)
checks.append({'check':'tunnel restored, new valid baseline/interval recorded, both clients connect; no invented history','passed':True})
Path('docs/evidence/live-traffic-history.json').write_text(json.dumps({'date':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),'version':api.request('GET','/api/v1/admin/overview')['version'],'duration_seconds':round(time.time()-started),'checks':checks,'samples':recovered['items'],'source':recovered['source']},indent=2),encoding='utf-8')
print('Actual traffic sample, control disconnect unknown gap and restored valid interval passed',flush=True)
