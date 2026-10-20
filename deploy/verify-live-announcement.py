"""Publish/withdraw through real UI with explicit audiences and exact restoration."""
import base64,hashlib,hmac,json,os,struct,subprocess,time
from pathlib import Path
from live_api import API,OPS,state

a=API().admin();s=state();u=API().acceptance_user(s['test_users'][0]);anonymous=API()
rich='--rich' in __import__('sys').argv
original=a.request('GET','/api/v1/admin/announcement');version=a.request('GET','/api/v1/admin/overview')['version']
private=OPS/'announcement-fixture.json';laststep=int(time.time())//30;page=None;checks=[]
fixture={'original':original,'user_cookie':u.cookie,'slug':'announcement-'+str(int(time.time())),'announcement':{'enabled':True,'authenticated':True,'public':False,'title':'服务维护提醒 · 长标题在手机也应清晰显示','content':'这是本次验收专用公告。个人订阅仍只向本人提供。\n<script>window.__globalXSS=1</script>','type':'warning','dismissible':True}}
if rich:fixture['announcement'].update(content_format='html',content='<p><strong>维护窗口</strong>：请阅读 <a href="https://example.com/help" target="_blank" rel="opener">查看指南</a>。</p><ul><li>授权与订阅保持隔离</li><li><em>更新后请重新连接</em></li></ul><pre><code>client config</code></pre>')
def reauth():
 global laststep
 while int(time.time())//30<=laststep or time.time()%30>23:time.sleep(1)
 laststep=int(time.time())//30;secret=base64.b32decode(s['totp_secret']+'='*((8-len(s['totp_secret'])%8)%8));d=hmac.new(secret,struct.pack('>Q',laststep),hashlib.sha1).digest();offset=d[-1]&15
 return {'password':s['admin_password'],'code':str((struct.unpack('>I',d[offset:offset+4])[0]&0x7fffffff)%1000000).zfill(6)}
def ui(mode):
 global laststep
 if mode=='save':fixture.update(reauth())
 private.write_text(json.dumps(fixture),encoding='utf-8');os.chmod(private,0o600)
 result=subprocess.run(['node','deploy/browser-announcement.cjs',mode],capture_output=True,text=True)
 laststep=int(time.time())//30
 if result.returncode:raise RuntimeError('Actual announcement browser failed: '+mode+' (sanitized stage: '+result.stderr.strip()+')')
try:
 u.request('GET','/api/v1/admin/announcement',expected=(403,));u.request('PUT','/api/v1/admin/announcement',{},expected=(403,));anonymous.request('GET','/api/v1/announcements',expected=(401,))
 before=u.request('GET','/api/v1/subscription')
 page=a.request('POST','/api/v1/admin/resources/public-page',{'data':{'name':'Owned announcement acceptance','slug':fixture['slug'],'title':'服务指南与公告','description':'公开说明与登录用户通知分开发布。','enabled':True}})
 fixture['page_id']=page['id']
 ui('save');saved=a.request('GET','/api/v1/admin/announcement');assert saved['announcement']==fixture['announcement']
 assert anonymous.request('GET','/api/v1/announcements/public')=={'announcement':None}
 published=u.request('GET','/api/v1/announcements')['announcement'];assert len(published)==(6 if rich else 5) and published['notice']==fixture['announcement']['content']
 ui('user');checks.append('Real UI authenticated-only publish with MFA; anonymous projection null; ordinary admin writes forbidden; user literal text, keyboard dismissal/reload, four widths and failed-fetch unknown/retry')
 # Stale revisions cannot overwrite a newer publish, even with valid reauthentication.
 a.request('PUT','/api/v1/admin/announcement',dict(announcement=fixture['announcement'],revision=original['revision'],**reauth()),expected=(409,))
 fixture['announcement']['public']=True;ui('save')
 assert anonymous.request('GET','/api/v1/announcements/public')['announcement']==published
 ui('public');checks.append('Explicit public audience through UI; anonymous exact five-field allowlist and same visible revision; four widths, dismissal, failure/retry; no subscription disclosure')
 fixture['announcement']['enabled']=False;ui('save')
 assert anonymous.request('GET','/api/v1/announcements/public')=={'announcement':None}
 assert u.request('GET','/api/v1/announcements')=={'announcement':None}
 assert a.request('GET','/api/v1/admin/announcement')['announcement']['content']==fixture['announcement']['content']
 after=u.request('GET','/api/v1/subscription');assert all(before[k]==after[k] for k in ['url','quota','node_count'])
 checks.append('UI withdrawal hides draft title/content from both audiences; admin retains draft; stale revision conflict; user subscription and authorizations unchanged')
finally:
 current=a.request('GET','/api/v1/admin/announcement')
 if current['announcement']!=original['announcement']:a.request('PUT','/api/v1/admin/announcement',dict(announcement=original['announcement'],revision=current['revision'],**reauth()))
 if page:a.request('DELETE','/api/v1/admin/resources/public-page/'+str(page['id']))
 private.unlink(missing_ok=True)
Path('docs/evidence/live-announcement-rich.json' if rich else 'docs/evidence/live-announcement.json').write_text(json.dumps({'version':version,'date':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),'checks':checks,'visual':'screenshots captured; manual review pending','cleanup':'original announcement restored; owned published page removed; secret handoff deleted','remaining':'Public-page per-page notices retain plain text; global rich content uses explicit tag/attribute allowlist and HTTPS links' if rich else 'restricted rich text not migrated; plaintext adaptation only'},ensure_ascii=False,indent=2),encoding='utf-8')
print('Actual announcement UI, MFA, explicit audiences, withdrawal, conflict and exact restoration passed')
