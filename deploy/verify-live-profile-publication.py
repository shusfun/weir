"""Publish an explicitly owned inventory snapshot of an existing test grant; no new grant."""
import base64,hashlib,hmac,json,struct,time,urllib.parse
from pathlib import Path
from live_api import API,state
from importlib.util import spec_from_file_location,module_from_spec
spec=spec_from_file_location('client','deploy/verify-live-client.py');client=module_from_spec(spec);spec.loader.exec_module(client)
api=API().admin();s=state();user=s['test_users'][0];link,_=client.get_link(user);created=[]
def code():
 # Wait for a fresh tick: prior browser acceptance consumed the current one.
 tick=int(time.time())//30
 while int(time.time())//30<=tick:time.sleep(1)
 key=base64.b32decode(s['totp_secret']+'='*((8-len(s['totp_secret'])%8)%8));h=hmac.new(key,struct.pack('>Q',int(time.time())//30),hashlib.sha1).digest();o=h[-1]&15
 return str((struct.unpack('>I',h[o:o+4])[0]&0x7fffffff)%1000000).zfill(6)
def create(kind,data):
 r=api.request('POST','/api/v1/admin/resources/'+kind,{'data':data});created.append((kind,r['id']));return r
try:
 node=create('nodes',{'name':'owned-publication-client','url':link})
 profile=create('profiles',{'name':'owned-publication-client','enabled':True,'node_ids':[node['id']],'source_ids':[]})
 endpoint='/api/v1/admin/profiles/'+str(profile['id'])+'/publication'
 body={'action':'publish','target':'links','days':1,'revision':api.request('GET',endpoint)['revision'],'confirm':True,'password':s['admin_password'],'code':code()}
 published=api.request('PUT',endpoint,body);path=urllib.parse.urlsplit(published['url']).path
 anon=API();output=anon.request('GET',path,raw=True).decode();probe=client.run_probe(user,8,link_override=output)
 body.update(action='revoke',revision=api.request('GET',endpoint)['revision'],code=code());api.request('PUT',endpoint,body);anon.request('GET',path,expected=(404,));still=client.run_probe(user,8,link_override=output)
 # Revocation stops future download, not an already copied underlying credential.
 report={'date':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),'version':api.request('GET','/api/v1/admin/overview')['version'],'snapshot_client':probe,'revoked_download':404,'downloaded_credential_still_valid':still,'boundary':'sharing creates no grants; revoked download does not revoke an existing user grant; fixture uses only owned acceptance credentials','cleanup':'profile/publication/node deleted; existing grants untouched'}
 Path('docs/evidence/live-profile-publication-client.json').write_text(json.dumps(report,indent=2))
 print('Published anonymous snapshot imported into real Xray; direct Gate egress; revoked download blocked; existing grant preserved')
finally:
 for kind,id in reversed(created):api.request('DELETE',f'/api/v1/admin/resources/{kind}/{id}')
