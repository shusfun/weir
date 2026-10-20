"""Assigned automatic groups preserve authorization and real Gate egress."""
import importlib.util,json,time
from pathlib import Path
from live_api import API,state
spec=importlib.util.spec_from_file_location('auto_template_client',Path(__file__).with_name('verify-live-templates.py'))
client=importlib.util.module_from_spec(spec);spec.loader.exec_module(client)
api=API().admin();users=state()['test_users'];owned=[];checks=[]
version=api.request('GET','/api/v1/admin/overview')['version']
uid=next(u['id'] for u in api.request('GET','/api/v1/admin/users')['items'] if u['email']==users[0]['email'])
before=client.config_for(users[0]);peer=client.config_for(users[1])
try:
 for mode in ['region','protocol']:
  template={'name':'auto-'+mode+'-'+str(int(time.time())),'target':'singbox','auto_groups':mode,'groups':[{'name':'AutoChoice','type':'select','members':['$auto']}],'rules':[{'type':'FINAL','policy':'AutoChoice'}],'dns':['1.1.1.1']}
  item=api.request('POST','/api/v1/admin/resources/templates',{'data':template});owned.append(('templates',item['id']))
  profile=api.request('POST','/api/v1/admin/resources/profiles',{'data':{'name':'auto-'+mode,'template_ids':[item['id']],'user_ids':[uid]}});owned.append(('profiles',profile['id']))
  cfg=client.config_for(users[0]);nodes=[p for p in cfg['outbounds'] if 'server' in p];original=[p for p in before['outbounds'] if 'server' in p]
  assert {(p.get('uuid'),p.get('password')) for p in nodes}=={(p.get('uuid'),p.get('password')) for p in original}
  groups=[p for p in cfg['outbounds'] if p['type']=='urltest'];assert groups and sorted(n for p in groups for n in p['outbounds'])==sorted(p['tag'] for p in nodes)
  assert client.config_for(users[1])==peer
  client.probe(cfg,True);checks.append(mode+' groups contain only authorized nodes, peer isolated and actual sing-box reaches destination through Gate')
  api.request('DELETE',f"/api/v1/admin/resources/profiles/{profile['id']}");owned.remove(('profiles',profile['id']))
finally:
 for kind,id in reversed(owned):api.request('DELETE',f'/api/v1/admin/resources/{kind}/{id}')
 assert client.config_for(users[0])==before
Path('docs/evidence/live-auto-groups.json').write_text(json.dumps({'version':version,'date':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),'checks':checks,'cleanup':'fresh records deleted; original subscription restored','limitations':'sing-box CLI; region labels are keyword hints, not geolocation'},indent=2),encoding='utf-8')
print('Actual authorized automatic groups, user isolation and Gate egress passed')
