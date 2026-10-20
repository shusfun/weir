"""Real catalog UI changes with reauthentication, exact restoration and user isolation."""
import base64,hashlib,hmac,io,json,os,struct,subprocess,time
from PIL import Image
from pathlib import Path
from live_api import API,OPS,state

a=API().admin();s=state();u=API().acceptance_user(s['test_users'][0])
original=a.request('GET','/api/v1/admin/clients');before=u.request('GET','/api/v1/subscription')
version=a.request('GET','/api/v1/admin/overview')['version'];checks=[]
name='Owned catalog acceptance — Windows sing-box with a deliberately long display name'
private=OPS/'catalog-fixture.json';fixture={'name':name,'url':'https://example.com/migate-owned-client','user_cookie':u.cookie,'subscription_url':before['url']}
icon=io.BytesIO();Image.new('RGB',(32,32),(49,92,221)).save(icon,format='JPEG')
fixture['icon_base64']=base64.b64encode(icon.getvalue()+b'private-icon-metadata-fixture').decode()
laststep=int(time.time())//30
def reauth():
 global laststep
 # Respect anti-replay; do not lower MFA policy to accelerate acceptance.
 while int(time.time())//30<=laststep or time.time()%30>25:time.sleep(1)
 laststep=int(time.time())//30
 secret=base64.b32decode(s['totp_secret']+'='*((8-len(s['totp_secret'])%8)%8))
 d=hmac.new(secret,struct.pack('>Q',laststep),hashlib.sha1).digest();o=d[-1]&15
 return {'password':s['admin_password'],'code':str((struct.unpack('>I',d[o:o+4])[0]&0x7fffffff)%1000000).zfill(6)}
def ui(mode):
 global laststep
 if mode!='user':fixture.update(reauth())
 private.write_text(json.dumps(fixture),encoding='utf-8');os.chmod(private,0o600)
 r=subprocess.run(['node','deploy/browser-client-catalog.cjs',mode],capture_output=True,text=True)
 laststep=int(time.time())//30
 if r.returncode:raise RuntimeError('Actual catalog UI failed; see sanitized stage evidence')
try:
 API().request('GET','/api/v1/clients',expected=(401,));u.request('GET','/api/v1/admin/clients',expected=(403,));u.request('PUT','/api/v1/admin/clients',{},expected=(403,))
 ui('add');saved=a.request('GET','/api/v1/admin/clients');item=saved['clients'][0]
 assert item['name']==name and item['format']=='singbox' and item['enabled'] and item['platforms']==['windows']
 assert saved['clients'][1:]==original['clients']
 assert item['icon_data'].startswith('data:image/png;base64,')
 raw_icon=base64.b64decode(item['icon_data'].split(',',1)[1]);assert b'private-icon-metadata-fixture' not in raw_icon
 assert Image.open(io.BytesIO(raw_icon)).size==(32,32)
 visible=u.request('GET','/api/v1/clients');assert visible['clients'][0]['icon_data']==item['icon_data']
 checks.append('UI rejects SVG, JPEG upload normalized to static 32px PNG, trailing metadata removed, user receives same bounded icon')
 checks.append('UI adds long-name desktop client, selects format, moves to first, administrator MFA publishes; original order retained')
 ui('user');checks.append('ordinary UI selects sing-box own URL and QR, platform filtering and metadata-only download; no token sent to download')
 # Concurrent stale snapshot cannot overwrite a published edit.
 a.request('PUT','/api/v1/admin/clients',dict(clients=original['clients'],revision=original['revision'],**reauth()),expected=(409,))
 assert a.request('GET','/api/v1/admin/clients')['revision']==saved['revision']
 checks.append('stale revision rejected after reauthentication without overwrite')
 ui('disable');removed=a.request('GET','/api/v1/admin/clients')['clients'][0];assert not removed.get('icon_data');checks.append('UI reopens persisted icon then removes uploaded image and restores component fallback');user=u.request('GET','/api/v1/clients');assert not any(v['name']==name for v in user['clients']) and user['revision']==''
 checks.append('UI edit restores enabled state; disabling hides item from ordinary endpoint')
 ui('delete');assert a.request('GET','/api/v1/admin/clients')['clients']==original['clients']
 checks.append('UI deletion restores original catalog and order; no grants or subscription token changed')
finally:
 current=a.request('GET','/api/v1/admin/clients')
 if current['clients']!=original['clients']:
  a.request('PUT','/api/v1/admin/clients',dict(clients=original['clients'],revision=current['revision'],**reauth()))
 private.unlink(missing_ok=True)
after=u.request('GET','/api/v1/subscription');assert after['url']==before['url'] and after['quota']==before['quota'] and after['node_count']==before['node_count']
Path('docs/evidence/live-client-catalog.json').write_text(json.dumps({'version':version,'date':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),'checks':checks,'cleanup':'original catalog, order and user grants restored; private handoff deleted','visual':'four widths captured; manual review pending','remaining':'automatic release-version retrieval remains a migration gap; commercial clients not tested'},indent=2),encoding='utf-8')
print('Real catalog UI, MFA publishing, order, user filtering, own format, conflict and exact restoration passed')
