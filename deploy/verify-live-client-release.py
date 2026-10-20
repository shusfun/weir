"""Actual GitHub metadata, shared cache and browser presentation; no catalog edits."""
import json,os,subprocess,time
from pathlib import Path
from live_api import API,OPS,state
s=state();a=API().admin();u=API().acceptance_user(s['test_users'][0]);original=a.request('GET','/api/v1/admin/clients');before=u.request('GET','/api/v1/subscription')
version=a.request('GET','/api/v1/admin/overview')['version'];checks=[]
API().request('GET','/api/v1/clients/nonexistent/release',expected=(401,))
u.request('GET','/api/v1/clients/nonexistent/release',expected=(404,))
item=next(v for v in original['clients'][:6] if v['enabled'] and v['repo'])
endpoint='/api/v1/clients/'+item['id']+'/release'
r=u.request('GET',endpoint)
for _ in range(3):
 if r['status']!='pending':break
 time.sleep(16);r=u.request('GET',endpoint)
assert r['status']=='current','Actual GitHub release unavailable; do not count fetch success'
assert r['source']=='github-releases' and r['version'] and r['url'].startswith('https://github.com/'+item['repo']+'/releases/tag/')
assert set(r)<=set(['status','source','observed_at','version','url','published_at'])
assert a.request('GET',endpoint)==r
assert u.request('GET',endpoint+'?repo=untrusted/override')==r
checks.append('real HTTPS GitHub release from catalog mapping; authenticated only, unknown ID rejected, request repository override ignored; exact field projection')
checks.append('second admin/user read shares Redis cached release and original observation timestamp')
private=OPS/'release-fixture.json'
try:
 private.write_text(json.dumps({'cookie':u.cookie,'name':item['name'],'version':r['version'],'url':r['url']}),encoding='utf-8');os.chmod(private,0o600)
 result=subprocess.run(['node','deploy/browser-client-release.cjs'],capture_output=True,text=True)
 if result.returncode:raise RuntimeError('Release real-browser check failed; no secrets logged')
 checks.append('user card displays actual version and official link with observed/published times; four widths and actual failed-request unknown without breaking own subscription')
finally:private.unlink(missing_ok=True)
renamed=next((v for v in original['clients'] if v['id']=='hiddify' and v['enabled']),None)
if renamed:
 moved=u.request('GET','/api/v1/clients/'+renamed['id']+'/release')
 for _ in range(3):
  if moved['status']!='pending':break
  time.sleep(16);moved=u.request('GET','/api/v1/clients/'+renamed['id']+'/release')
 assert moved['status']=='current' and moved['url'].startswith('https://github.com/hiddify/hiddify-app/releases/tag/'),'Renamed official repository not resolved'
 checks.append('actual Hiddify old repository resolves through bounded official GitHub API redirect to canonical hiddify-app release; no browser override')
assert a.request('GET','/api/v1/admin/clients')==original
assert u.request('GET','/api/v1/subscription')['url']==before['url']
Path('docs/evidence/live-client-release.json').write_text(json.dumps({'version':version,'date':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),'repo':item['repo'],'release':r,'checks':checks,'visual':'four widths and failure captured; manual review pending','cleanup':'no catalog, grant or subscription edits; private handoff deleted','limits':'GitHub metadata does not certify client binary compatibility or commercial-client acceptance'},indent=2),encoding='utf-8')
print('Real GitHub release, shared cache, user isolation and browser failure handling passed')
