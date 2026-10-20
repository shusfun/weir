"""Real leased pool/browser/core acceptance; runner owns recovery on every exit."""
import importlib.util
import json
import os
from pathlib import Path
import secrets
import subprocess
import time
from urllib.parse import quote

from live_api import API,state,OPS
from pool_fixture import prepare_fixture,update_fixture
from production_guard import check
from production_pool_scope import resources,pools
from remote import connect,run

check()
record=json.loads((OPS/'production-pending.json').read_text(encoding='utf-8'))
if record['cleanup'].get('handler')!='outbound-pool':raise RuntimeError('Registered pool recovery scope required')
prefix=record['fixture_prefix'];s=state();device=record['cleanup']['device_id'];api=API().admin();base=f'/api/v1/admin/devices/{device}/pools'
spec=importlib.util.spec_from_file_location('proxy_clients',Path(__file__).with_name('verify-live-client.py'));clients=importlib.util.module_from_spec(spec);spec.loader.exec_module(clients)
spec=importlib.util.spec_from_file_location('singbox_clients',Path(__file__).with_name('verify-live-templates.py'));singbox=importlib.util.module_from_spec(spec);spec.loader.exec_module(singbox)

def probe_singbox(first=True):
    for index,user in enumerate(s['test_users']):
        singbox.probe(singbox.config_for(user),first if index==0 else True)

checks=[]
password=secrets.token_hex(16)
link=f'http://acceptance:{password}@127.0.0.1:24448#Loopback%20acceptance'
fixture={'path':prefix+secrets.token_hex(16),'links':[link,link.replace('#Loopback%20acceptance','#duplicate'),'snell://synthetic@example.com:443?version=4#Snell%20尚不支持托管']}
prepare_fixture(record,fixture,password)
source=api.request('POST','/api/v1/admin/resources/sources',{'data':{'name':prefix+'source','url':'http://154.9.26.146:24447/'+fixture['path'],'enabled':True,'refresh_seconds':86400}})
api.request('POST',f'/api/v1/admin/sources/{source["id"]}/refresh',{})
browser={'prefix':prefix,'device':device,'source_name':prefix+'source'}
file=OPS/'pool-fixture.json';file.write_text(json.dumps(browser),encoding='utf-8');os.chmod(file,0o600)
subprocess.run(['node','deploy/browser-outbound-pools-live.cjs'],check=True)
checks.append('Domain complete pool UI and actual Agent receipts; separate browser evidence records injected UI failure distinctly')
# Browser work may cross the existing operator session's expiry. Explicitly
# validate/renew before starting a new phase, without replaying a failed write.
api=API().admin()
# Full-order tests refuse to mutate any pre-existing collection. The runner
# captures this before granting the operation, and removes only new inventory.
if any(r['owner_id']==device and r['data'].get('source')!='subscription' for r in record['cleanup']['baseline']['device-outbound']):
    raise RuntimeError('Manual order needs an isolated device with no original manual inventory')
def task(operation,payload):
    queued=api.request('POST',f'/api/v1/admin/devices/{device}/tasks',{'operation':operation,'payload':payload,'confirm':True})
    api.wait_task(queued['id'])
    return next(x for x in api.request('GET',f'/api/v1/admin/tasks?device_id={device}&page_size=200')['items'] if x['id']==queued['id'])
for index,address in enumerate(['1.1.1.1','8.8.8.8']):
    task('outbound.upsert',{'tag':prefix+'tcp-'+str(index),'remark':prefix+'TCP fixture '+str(index),'protocol':'http','address':address,'port':443,'transport_options':{}})
subprocess.run(['node','deploy/browser-outbound-tools-live.cjs'],check=True)
checks.append('Domain new tool workflows use real Agent receipts and only pre-registered catalog candidates; separate tool browser evidence')
api=API().admin()
pool=api.request('POST',base,{'name':prefix+'network-pool','source_id':source['id'],'auto_refresh':True,'prepend':True})
def current():return next(p for p in pools(api,device) if p['id']==pool['id'])
def apply():
    p=current();preview=api.request('POST',base+f'/{p["id"]}/preview',{'version':p['version'],'refresh':True})
    selected=[n['identity'] for n in preview['items'] if n['supported'] and n['name'].startswith('Loopback')]
    if len(selected)!=1:raise RuntimeError('Expected one actual managed endpoint')
    task=api.request('POST',base+f'/{p["id"]}/apply',{'version':p['version'],'preview_token':preview['preview_token'],'selected':selected,'enabled':True,'confirm':True})
    api.wait_task(task['id']);return task['id']
apply()
# Presentation priority changes cannot modify routing or original pools.
api.request('POST',base,{'name':prefix+'ordering-pool','source_id':source['id']})
collection=pools(api,device)
api.request('POST',base+'/order',{'items':[{'id':p['id'],'version':p['version']} for p in reversed(collection)]})
report=api.request('POST',base+'/refresh',{})
if {x['result'] for x in report['items']}!={'sync_checked','manual_preview_required'}:raise RuntimeError('Full refresh omitted per-pool automatic/manual results')
checks.append('Complete owned pool order with two pools persists presentation priorities; real full refresh reports automatic and manual pools separately')
member=next(r for r in resources(api,'device-outbound') if r['owner_id']==device and r['data'].get('subscription_id')==pool['id'])
route=api.request('POST',f'/api/v1/admin/devices/{device}/tasks',{'operation':'routing.upsert','payload':{'outbound_id':member['data']['id'],'inbound_id':1,'client_id':s['test_users'][0]['grant_id']},'confirm':True});api.wait_task(route['id'])
results=[clients.run_probe(user,index+1) for index,user in enumerate(s['test_users'])]
probe_singbox()
if results[0]['credential_digest']==results[1]['credential_digest']:raise RuntimeError('Independent user credential boundary failed')
checks.append('Two actual Xray subscriptions share inbound with separate credentials; routed user traverses the authenticated loopback HTTP proxy; other user remains direct; both egress Gate')
# Disable the fixture proxy only: first user's route must fail, second remains
# connected. This distinguishes actual pool use from an accidental direct path.
with connect('gate') as gate:
    code,_,_=run(gate,'systemctl stop '+prefix+'proxy.service')
    if code:raise RuntimeError('Cannot stop owned test proxy')
try:
    saved,_=clients.get_link(s['test_users'][0]);clients.run_probe(s['test_users'][0],1,False,saved)
    clients.run_probe(s['test_users'][1],2)
    probe_singbox(False)
finally:
    # Transient --collect units are removed after stopping. Restart via the
    # exact previously registered binary/config, never change production units.
    root=record['cleanup']['fixture']['root']+'/'+prefix
    with connect('gate') as gate:
        command=f'systemd-run --quiet --collect --unit={prefix}proxy.service --property=NoNewPrivileges=yes --property=ProtectSystem=strict --property=ProtectHome=yes --property=PrivateTmp=yes /opt/migate-gate/cores/xray run -config {root}/core.json'
        code,_,_=run(gate,command)
        if code:raise RuntimeError('Cannot restore owned test proxy')
clients.run_probe(s['test_users'][0],1)
probe_singbox()
checks.append('Stopping only the registered pool proxy blocks its routed user; independent shared-inbound user remains connected; restoring owned proxy recovers')
fixture['links']=[link.replace('#Loopback%20acceptance','#Loopback%20renamed'),fixture['links'][2]]
old_task=current()['task_id']
update_fixture(record,fixture)
api.request('POST',f'/api/v1/admin/sources/{source["id"]}/refresh',{})
def wait_sync(predicate,reason):
    deadline=time.monotonic()+130
    while time.monotonic()<deadline:
        p=current()
        if predicate(p):return p
        time.sleep(2)
    raise RuntimeError(reason)
wait_sync(lambda p:p.get('task_id')!=old_task and p['status']=='applied','Automatic source rename did not receive an applied task')
after=next(r for r in resources(api,'device-outbound') if r['owner_id']==device and r['data'].get('subscription_id')==pool['id'])
if after['data']['id']!=member['data']['id'] or after['data']['tag']!=member['data']['tag']:raise RuntimeError('Source rename changed routed identity')
clients.run_probe(s['test_users'][0],1)
fixture['fail']=True;update_fixture(record,fixture)
api.request('POST',base+f'/{pool["id"]}/preview',{'version':current()['version'],'refresh':True},expected=(502,))
clients.run_probe(s['test_users'][0],1)
checks.append('Source rename/reorder retains stable endpoint ID/tag and route; real upstream HTTP 503 preserves applied forwarding')
# A changed source must materialize new supported nodes automatically; removal
# of a routed member must preserve the previous applied collection.
fixture['fail']=False
fixture['links'].append(link.replace('acceptance:', 'unused-fixture:').replace('#Loopback%20acceptance','#Additional'))
update_fixture(record,fixture);api.request('POST',f'/api/v1/admin/sources/{source["id"]}/refresh',{})
wait_sync(lambda p:p['status']=='applied' and p['member_count']==2,'Automatic materialization did not add the new node')
before_ids={r['data']['id'] for r in resources(api,'device-outbound') if r['owner_id']==device and r['data'].get('subscription_id')==pool['id']}
fixture['links']=fixture['links'][1:]
update_fixture(record,fixture);api.request('POST',f'/api/v1/admin/sources/{source["id"]}/refresh',{})
wait_sync(lambda p:p.get('sync_error')=='pool_member_has_routes','Automatic routed member removal was not rejected')
after_ids={r['data']['id'] for r in resources(api,'device-outbound') if r['owner_id']==device and r['data'].get('subscription_id')==pool['id']}
if before_ids!=after_ids:raise RuntimeError('Failed auto-sync changed applied pool membership')
clients.run_probe(s['test_users'][0],1);probe_singbox()
checks.append('Real timed auto-sync adds a source member; routed endpoint removal records explicit failure and preserves inventory, Xray and sing-box forwarding')
api.request('POST',base+f'/{pool["id"]}/apply',{'version':current()['version'],'enabled':False,'delete':True,'confirm':True},expected=(409,))
checks.append('Deleting a referenced pool is refused without removing route or changing established configuration')
with connect('gate') as gate:
    code,_,_=run(gate,'systemctl restart migate-gate')
    if code:raise RuntimeError('Owned Agent restart failed')
time.sleep(3)
clients.run_probe(s['test_users'][0],1);clients.run_probe(s['test_users'][1],2)
probe_singbox()
with connect('gate') as gate:
    code,_,_=run(gate,'systemctl is-active migate-gate migate-gate-helper migate-gate-xray migate-gate-tunnel relay154-x-ui')
    if code:raise RuntimeError('Gate or original x-ui readiness failed')
checks.append('Actual Agent restart retains pool/routing and both user connections; owned services and existing x-ui remain active')
version=api.request('GET','/api/v1/admin/overview')['version']
Path('docs/evidence/live-outbound-pools.json').write_text(json.dumps({'date':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),'version':version,'result':'passed','checks':checks,'cleanup':'Runner must restore and verify baseline before releasing the production lock','limits':'No managed kernel package changed; commercial Snell client and production WireGuard are not claimed'},indent=2),encoding='utf-8')
print('Real outbound pool, scoped two-user forwarding, source failure and restart passed; runner recovery follows')
