"""Recover only this run's outbound pools, dependent routes and fixed fixture.

No pre-existing inventory, grants or users may be edited. Unknown tasks or
resources fail closed; the runner keeps the recovery journals for inspection.
"""
import json
import re
import time

KINDS = ['sources', 'device-outbound', 'device-routing']


def service_baseline():
    from remote import connect,run
    # Read only, fixed commands. Compare listeners including process ownership;
    # never stop or recreate an original service to make a check pass.
    with connect('gate') as client:
        code,active,_=run(client,'systemctl is-active migate-gate migate-gate-helper migate-gate-xray migate-gate-tunnel relay154-x-ui')
        if code:raise RuntimeError('Gate baseline contains an inactive service')
        code,listeners,_=run(client,"ss -H -lnptu '( sport = :80 or sport = :443 or sport = :8443 or sport = :24443 or sport = :24444 )'")
        if code:raise RuntimeError('Cannot inspect protected Gate listeners')
    return {'active':active.strip().splitlines(),'protected_listeners':sorted(listeners.strip().splitlines())}


def resources(api, kind):
    # Standalone implementation keeps this module usable by recovery/tests.
    items, page = [], 1
    while page <= 100:
        data = api.request('GET', f'/api/v1/admin/resources/{kind}?page={page}&page_size=500')
        items.extend(data['items'])
        if len(items) == data['total']:
            return items
        if not data['items']:
            break
        page += 1
    raise RuntimeError('Pool recovery inventory is incomplete')


def pools(api, device):
    return api.request('GET', f'/api/v1/admin/devices/{device}/pools')['items']


def tasks(api, device):
    items, page = [], 1
    while page <= 100:
        data = api.request('GET', f'/api/v1/admin/tasks?device_id={device}&page={page}&page_size=200')
        items.extend(data['items'])
        if len(items) == data['total']:
            return items
        if not data['items']:
            break
        page += 1
    raise RuntimeError('Pool recovery tasks are incomplete')


def source_configuration(row):
    # Existing sources continue scheduled observation while the fixture runs.
    # Compare their administrator-owned configuration, never overwrite caches.
    observations={'links','node_count','last_update','last_attempt','last_error',
                  'next_refresh','upstream_usage','last_rejection','status','stale_cache'}
    return {key:value for key,value in row['data'].items() if key not in observations}


def snapshot(api):
    from live_api import state
    s = state()
    device = s['device_id']
    active = tasks(api, device)
    if any(t['status'] in ['pending','running'] for t in active):
        raise RuntimeError('Device has unconfirmed tasks; pool acceptance must wait')
    catalog=[]
    for protocol in ['http','https','socks']:
        try:
            result=api.request('GET',f'/api/v1/admin/proxy-catalog/{protocol}?page=1')
            if result['items']:
                candidate=result['items'][0]
                catalog.append({'protocol':protocol,'key':candidate['key'],
                                'tag':'public-'+protocol+'-'+candidate['key'][:16]})
        except Exception:
            # A fixed external feed may be unavailable; do not invent a sample.
            pass
    return {'handler':'outbound-pool','device_id':device,'catalog_candidates':catalog,
            'test_grants':[u['grant_id'] for u in s['test_users']],
            'baseline':{k:resources(api,k) for k in KINDS},
            'pools':pools(api,device),'task_ids':[t['id'] for t in active],
            'services':service_baseline(),
            'fixture':{'root':'/var/lib/migate-acceptance',
                       'files':['owner.json','fixture.py','fixture.json','core.json'],
                       'ports':[24447,24448]}}


def owned_pools(api, record):
    scope = record['cleanup']
    baseline = {p['id']:p for p in scope['pools']}
    current = pools(api,scope['device_id'])
    for item in current:
        if item['id'] in baseline:
            if item != baseline[item['id']]:
                raise RuntimeError('Existing outbound pool changed')
        elif not item['name'].startswith(record['fixture_prefix']):
            raise RuntimeError('Unowned outbound pool found')
    return [p for p in current if p['id'] not in baseline]


def owned_members(api, record):
    ids = {p['id'] for p in owned_pools(api,record)}
    device = record['cleanup']['device_id']
    return [r for r in resources(api,'device-outbound') if r['owner_id']==device and
            r['data'].get('source')=='subscription' and r['data'].get('subscription_id') in ids]


def owned_manual(api, record):
    scope=record['cleanup'];baseline={r['id'] for r in scope['baseline']['device-outbound']}
    tags={x['tag'] for x in scope.get('catalog_candidates',[])}
    result=[]
    for r in resources(api,'device-outbound'):
        if r['id'] in baseline or r['owner_id']!=scope['device_id']:continue
        data=r['data']
        if data.get('source')=='subscription':continue
        if not (data.get('tag','').startswith(record['fixture_prefix']) or data.get('tag') in tags):
            raise RuntimeError('Unregistered new manual outbound')
        result.append(r)
    return result


def authorize(api,record,method,path,data):
    scope,prefix=record['cleanup'],record['fixture_prefix']
    device=scope['device_id']
    base=f'/api/v1/admin/devices/{device}/pools'
    if method=='POST' and path in [base+'/order',base+'/refresh']:
        # Full-collection operations are allowed only when the entire device
        # collection belongs to this run; never rewrite original priorities.
        if scope['pools']:raise RuntimeError('Full pool operation would affect original pools')
        current=owned_pools(api,record)
        if path.endswith('/order') and {x['id'] for x in (data or {}).get('items',[])}!={x['id'] for x in current}:
            raise RuntimeError('Incomplete fixture pool order')
        return
    catalog_base=f'/api/v1/admin/devices/{device}/proxy-catalog/'
    if method=='POST' and path in [catalog_base+'probe',catalog_base+'import']:
        if any(x['protocol']==(data or {}).get('protocol') and x['key']==(data or {}).get('key') for x in scope.get('catalog_candidates',[])):
            return
        raise RuntimeError('Catalog candidate was not registered before acceptance')
    if path==base and method=='POST':
        if not (data or {}).get('name','').startswith(prefix):
            raise RuntimeError('Pool requires the current fixture prefix')
        if (data or {}).get('id') and data['id'] not in {p['id'] for p in owned_pools(api,record)}:
            raise RuntimeError('Cannot edit an existing pool')
        source=next((x for x in resources(api,'sources') if x['id']==data.get('source_id')),None)
        if not source or not source['data'].get('name','').startswith(prefix):
            raise RuntimeError('Pool source must belong to this fixture')
        return
    match=re.fullmatch(re.escape(base)+r'/(\d+)/(preview|apply)',path)
    if method=='POST' and match:
        if int(match[1]) not in {p['id'] for p in owned_pools(api,record)}:
            raise RuntimeError('Pool outside the current recovery scope')
        return
    match=re.fullmatch(r'/api/v1/admin/sources/(\d+)/refresh',path)
    if method=='POST' and match:
        source=next((x for x in resources(api,'sources') if x['id']==int(match[1])),None)
        if source and source['data'].get('name','').startswith(prefix):return
    if method=='POST' and path==f'/api/v1/admin/devices/{device}/tasks':
        payload=(data or {}).get('payload',{})
        op=(data or {}).get('operation')
        manual=owned_manual(api,record)
        manual_ids={r['data']['id'] for r in manual}
        if op=='outbound.upsert':
            if payload.get('id') and payload['id'] not in manual_ids:raise RuntimeError('Cannot edit original outbound')
            if not payload.get('tag','').startswith(prefix):raise RuntimeError('Manual outbound needs fixture prefix')
            return
        if op=='outbound.delete' and payload.get('id') in manual_ids:return
        if op=='outbound.probe':
            ids=payload.get('ids',[])
            if ids and set(ids)<={r['data']['id'] for r in owned_members(api,record)}|manual_ids and set(payload)=={'ids'}:return
        if op=='outbound.reorder':
            baseline_manual=[r for r in scope['baseline']['device-outbound'] if r['owner_id']==device and r['data'].get('source')!='subscription']
            if not baseline_manual and {x['id'] for x in payload.get('items',[])}==manual_ids:return
            raise RuntimeError('Full manual order would affect original inventory')
        if op=='routing.upsert' and not payload.get('id'):
            if payload.get('outbound_id') not in {r['data']['id'] for r in owned_members(api,record)}:
                raise RuntimeError('Route must reference a fixture pool member')
            if payload.get('client_id') not in scope['test_grants']:
                raise RuntimeError('Route must target an owned acceptance grant')
            return
        if op=='routing.delete':
            members={r['data']['id'] for r in owned_members(api,record)}
            baseline={r['id'] for r in scope['baseline']['device-routing']}
            found=next((r for r in resources(api,'device-routing') if r['owner_id']==device and
                        r['data']['id']==payload.get('id') and r['id'] not in baseline),None)
            if found and found['data'].get('outbound_id') in members and found['data'].get('client_id') in scope['test_grants']:
                return
    # Sources are handled by the runner's existing prefix/resource contract.
    raise RuntimeError('Pool operation has no recovery contract')


def wait_terminal(api,device,old_ids):
    deadline=time.monotonic()+100
    while True:
        active=[t for t in tasks(api,device) if t['id'] not in old_ids and t['status'] in ['pending','running']]
        if not active:return
        if time.monotonic()>=deadline:
            raise RuntimeError('Unconfirmed pool acceptance task; retain recovery journal')
        time.sleep(1)


def cleanup(api,record):
    scope=record['cleanup'];device=scope['device_id']
    wait_terminal(api,device,set(scope['task_ids']))
    # Disarm owned periodic pools before deleting their resources. A queued
    # sync must finish first; saving uses the same device lock as the worker.
    for pool in owned_pools(api,record):
        if pool.get('auto_refresh'):
            api.request('POST',f'/api/v1/admin/devices/{device}/pools',
                        {'id':pool['id'],'version':pool['version'],'name':pool['name'],
                         'source_id':pool['source_id'],'auto_refresh':False,'prepend':pool.get('prepend',False)})
    wait_terminal(api,device,set(scope['task_ids']))
    members={r['data']['id'] for r in owned_members(api,record)}
    baseline={r['id']:r for r in scope['baseline']['device-routing']}
    for row in resources(api,'device-routing'):
        if row['id'] in baseline:
            if row!=baseline[row['id']]:raise RuntimeError('Existing routing changed')
            continue
        if row['owner_id']!=device or row['data'].get('outbound_id') not in members or row['data'].get('client_id') not in scope['test_grants']:
            raise RuntimeError('Unowned routing found; refusing removal')
        result=api.request('POST',f'/api/v1/admin/devices/{device}/tasks',
                           {'operation':'routing.delete','payload':{'id':row['data']['id']},'confirm':True})
        api.wait_task(result['id'])
    for pool in owned_pools(api,record):
        result=api.request('POST',f'/api/v1/admin/devices/{device}/pools/{pool["id"]}/apply',
                           {'version':pool['version'],'enabled':False,'delete':True,'confirm':True})
        api.wait_task(result['id'])
    if pools(api,device)!=scope['pools']:raise RuntimeError('Pool baseline was not restored')
    for row in owned_manual(api,record):
        result=api.request('POST',f'/api/v1/admin/devices/{device}/tasks',
                           {'operation':'outbound.delete','payload':{'id':row['data']['id']},'confirm':True})
        api.wait_task(result['id'])
    for kind in ['device-routing','device-outbound']:
        if {r['id']:r for r in resources(api,kind)}!={r['id']:r for r in scope['baseline'][kind]}:
            raise RuntimeError('Device resource baseline was not restored')
    baseline={r['id']:r for r in scope['baseline']['sources']}
    for row in resources(api,'sources'):
        if row['id'] in baseline:
            if source_configuration(row)!=source_configuration(baseline[row['id']]):
                raise RuntimeError('Existing source configuration changed')
        elif row['data'].get('name','').startswith(record['fixture_prefix']):
            api.request('DELETE',f'/api/v1/admin/resources/sources/{row["id"]}')
        else:raise RuntimeError('Unowned source found')
    if {r['id'] for r in resources(api,'sources')}!=set(baseline):raise RuntimeError('Source baseline was not restored')
    from pool_fixture import cleanup_fixture
    cleanup_fixture(record)
    if service_baseline()!=scope['services']:
        raise RuntimeError('Original Gate services or protected listeners changed; retain recovery journal')
