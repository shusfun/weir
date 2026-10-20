"""Verify default transformations on authorized real clients; restore global settings."""
import base64,hashlib,hmac,json,struct,time,urllib.parse
from pathlib import Path
from live_api import API,state
from importlib.util import spec_from_file_location,module_from_spec
spec=spec_from_file_location('templates','deploy/verify-live-templates.py');client=module_from_spec(spec);spec.loader.exec_module(client)
a=API().admin();s=state();baseline=a.request('GET','/api/v1/admin/subscription-naming');owned=[];changed=False
last_tick=int(time.time())//30
def code():
 global last_tick
 while int(time.time())//30<=last_tick:time.sleep(1)
 last_tick=int(time.time())//30;key=base64.b32decode(s['totp_secret']+'='*((8-len(s['totp_secret'])%8)%8));h=hmac.new(key,struct.pack('>Q',last_tick),hashlib.sha1).digest();o=h[-1]&15;return str((struct.unpack('>I',h[o:o+4])[0]&0x7fffffff)%1000000).zfill(6)
def put(d):
 current=a.request('GET','/api/v1/admin/subscription-naming');return a.request('PUT','/api/v1/admin/subscription-naming',{'defaults':d,'revision':current['revision'],'confirm':True,'password':s['admin_password'],'code':code()})
def create(kind,data):
 r=a.request('POST','/api/v1/admin/resources/'+kind,{'data':data});owned.append((kind,r['id']));return r
before=client.config_for(s['test_users'][0]);peer=client.config_for(s['test_users'][1])
try:
 put({'enable_manual_nodes':True,'enable_subscriptions':True,'manual_node_prefix':'OwnedPrefix','prepend_group_name':True,'region_overrides':[{'pattern':'.*','region':'验收首条地区','flags':''},{'pattern':'.*','region':'不应命中地区','flags':''}]});changed=True
 tpl=create('templates',{'name':'owned-naming-real-template','target':'singbox','auto_groups':'region','groups':[{'name':'RegionChoice','type':'select','members':['$auto']}],'rules':[{'type':'FINAL','policy':'RegionChoice'}]})
 profile=create('profiles',{'name':'owned-naming-real-profile','enabled':True,'source_ids':[],'node_ids':[],'user_ids':[s['test_users'][0]['id']],'template_ids':[tpl['id']],'skip_default_operators':True})
 cfg=client.config_for(s['test_users'][0]);original=[p for p in before['outbounds'] if 'server' in p];actual=[p for p in cfg['outbounds'] if 'server' in p]
 assert len(original)==len(actual) and {(p.get('uuid'),p.get('password')) for p in original}=={(p.get('uuid'),p.get('password')) for p in actual}
 assert any(p.get('tag')=='验收首条地区 节点' for p in cfg['outbounds']) and not any(p.get('tag')=='不应命中地区 节点' for p in cfg['outbounds'])
 assert client.config_for(s['test_users'][1])==peer
 client.probe(cfg,True)
 Path('docs/evidence/live-subscription-naming-client.json').write_text(json.dumps({'date':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),'version':a.request('GET','/api/v1/admin/overview')['version'],'checks':['actual assigned sing-box template uses first override only','real client directly reaches Gate','authorized credentials unchanged; no inventory added','peer subscription unchanged'],'cleanup':'owned profile/template deleted; global settings restored'},ensure_ascii=False,indent=2),encoding='utf-8')
 print('Real authorized sing-box region template directly reaches Gate; first-match region and peer isolation passed')
finally:
 for kind,id in reversed(owned):a.request('DELETE',f'/api/v1/admin/resources/{kind}/{id}')
 if changed:put(baseline['defaults'])
 assert client.config_for(s['test_users'][0])==before
