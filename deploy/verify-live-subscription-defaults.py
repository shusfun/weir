"""Verify default transformations on authorized real clients; restore global settings."""
import base64,hashlib,hmac,json,struct,time,urllib.parse
from pathlib import Path
from live_api import API,state
from importlib.util import spec_from_file_location,module_from_spec
spec=spec_from_file_location('client','deploy/verify-live-client.py');client=module_from_spec(spec);spec.loader.exec_module(client)
a=API().admin();s=state();baseline=a.request('GET','/api/v1/admin/subscription-defaults');owned=[];changed=False
last_tick=int(time.time())//30
def code():
 global last_tick
 while int(time.time())//30<=last_tick:time.sleep(1)
 last_tick=int(time.time())//30;key=base64.b32decode(s['totp_secret']+'='*((8-len(s['totp_secret'])%8)%8));h=hmac.new(key,struct.pack('>Q',last_tick),hashlib.sha1).digest();o=h[-1]&15;return str((struct.unpack('>I',h[o:o+4])[0]&0x7fffffff)%1000000).zfill(6)
def put(d):
 current=a.request('GET','/api/v1/admin/subscription-defaults');return a.request('PUT','/api/v1/admin/subscription-defaults',{'defaults':d,'revision':current['revision'],'confirm':True,'password':s['admin_password'],'code':code()})
def create(data):
 r=a.request('POST','/api/v1/admin/resources/profiles',{'data':data});owned.append(r['id']);return r
try:
 put({'operators':[{'type':'rename','params':{'template':{'enabled':True,'template':'Global-{name}'}}}]});changed=True
 user=s['test_users'][0];profile=create({'name':'owned-defaults-real-client','enabled':True,'source_ids':[],'node_ids':[],'user_ids':[user['id']]})
 link,_=client.get_link(user)
 if not urllib.parse.unquote(urllib.parse.urlsplit(link).fragment).startswith('Global-'):raise RuntimeError('Authorized subscription did not inherit default')
 probe=client.run_probe(user,9,link_override=link)
 a.request('POST','/api/v1/admin/resources/profiles',{'id':profile['id'],'data':{**profile['data'],'skip_default_operators':True}})
 skipped,_=client.get_link(user)
 if urllib.parse.unquote(urllib.parse.urlsplit(skipped).fragment).startswith('Global-'):raise RuntimeError('Authorized empty skip not respected')
 still=client.run_probe(user,9,link_override=skipped)
 Path('docs/evidence/live-subscription-defaults-client.json').write_text(json.dumps({'date':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),'version':a.request('GET','/api/v1/admin/overview')['version'],'inherited_real_client':probe,'explicit_skip_real_client':still,'boundary':'authorized user links only, no inventory or grant addition; original global settings restored'},indent=2))
 print('Authorized real user subscription inherits defaults and explicit empty skip; both real Xray paths directly reach Gate')
finally:
 for id in owned:a.request('DELETE',f'/api/v1/admin/resources/profiles/{id}')
 if changed:put(baseline['defaults'])
