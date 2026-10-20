"""Real assigned remote rule-set output and sing-box download/enforcement.

Fresh template/profile only; credentials remain in the restricted operator
directory. The peer subscription and original assigned output are restored.
"""
import hashlib,importlib.util,json,time,urllib.request
from pathlib import Path
from live_api import API,state

spec=importlib.util.spec_from_file_location('template_client',Path(__file__).with_name('verify-live-templates.py'))
client=importlib.util.module_from_spec(spec);spec.loader.exec_module(client)

api=API().admin();users=state()['test_users'];owned=[];checks=[]
version=api.request('GET','/api/v1/admin/overview')['version']
url='https://raw.githubusercontent.com/SagerNet/sing-geosite/rule-set/geosite-cloudflare.srs'
body=urllib.request.urlopen(url,timeout=25).read(1<<20)
assert body.startswith(b'SRS') and len(body)<1<<20
records=api.request('GET','/api/v1/admin/users')['items']
uid=next(u['id'] for u in records if u['email']==users[0]['email'])
before=client.config_for(users[0]);peer=client.config_for(users[1])
template={'name':'remote-rules-'+str(int(time.time())),'target':'singbox',
          'groups':[{'name':'RemoteChoice','type':'select','members':['$nodes']}],
          'rule_sets':[{'name':'cloudflare','url':url,'format':'binary','interval':86400}],
          'rules':[{'type':'RULE-SET','value':'cloudflare','policy':'RemoteChoice'},{'type':'FINAL','policy':'RemoteChoice'}],
          'dns':['1.1.1.1']}
try:
    item=api.request('POST','/api/v1/admin/resources/templates',{'data':template});owned.append(('templates',item['id']))
    profile=api.request('POST','/api/v1/admin/resources/profiles',{'data':{'name':'remote-rule-client','template_ids':[item['id']],'user_ids':[uid]}});owned.append(('profiles',profile['id']))
    cfg=client.config_for(users[0]);declaration=cfg['route']['rule_set'][0]
    assert declaration['url']==url and declaration['format']=='binary' and declaration['download_detour']=='direct'
    actual=[p for p in cfg['outbounds'] if 'server' in p];original=[p for p in before['outbounds'] if 'server' in p]
    assert {(p.get('uuid'),p.get('password')) for p in actual}=={(p.get('uuid'),p.get('password')) for p in original}
    assert client.config_for(users[1])==peer
    client.probe(cfg,True)
    template['rules'][0]['policy']='REJECT'
    api.request('POST','/api/v1/admin/resources/templates',{'id':item['id'],'data':template})
    client.probe(client.config_for(users[0]),False)
    checks=['actual assigned subscription emits remote rule-set declaration','authorized credentials unchanged and peer subscription isolated','real sing-box downloads public HTTPS binary rule set','Cloudflare request first proxies directly through Gate; changing rule-set policy rejects it']
finally:
    for kind,record_id in reversed(owned):api.request('DELETE',f'/api/v1/admin/resources/{kind}/{record_id}')
    assert client.config_for(users[0])==before
Path('docs/evidence/live-rule-sets.json').write_text(json.dumps({'version':version,'date':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),'checks':checks,'remote_fixture':{'url':url,'sha256':hashlib.sha256(body).hexdigest(),'bytes':len(body)},'limitations':'sing-box CLI, not a commercial client; remote branch content may change; Clash mapping is separately format-tested','cleanup':'fresh profile/template deleted; original subscription restored'},indent=2),encoding='utf-8')
print('Actual remote rule-set download, Gate egress, policy enforcement and user isolation passed')
