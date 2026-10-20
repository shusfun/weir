"""Enroll administrator MFA and retain handoff only in protected operator storage."""
import base64
import hashlib
import hmac
import json
from pathlib import Path
import struct
import time
from live_api import API,state,save_state
from remote import connect,run

def totp(secret):
 key=base64.b32decode(secret+'='*((8-len(secret)%8)%8))
 value=hmac.new(key,struct.pack('>Q',int(time.time())//30),hashlib.sha1).digest()
 offset=value[-1]&15
 return str((struct.unpack('>I',value[offset:offset+4])[0]&0x7fffffff)%1000000).zfill(6)

s=state();a=API().admin();checks=[]
if not s.get('totp_secret'):
 settings=a.request('GET','/api/v1/admin/settings')
 settings['security'].update(totp_enabled=True,require_admin_mfa=True,registration_enabled=False)
 a.request('PUT','/api/v1/admin/settings',{**settings,'password':s['admin_password']})
 a.request('GET','/api/v1/admin/users',expected=(401,))
 a.login(s['admin_email'],s['admin_password'])
 a.request('GET','/api/v1/admin/users',expected=(403,))
 checks.append('password-only enrollment session cannot manage resources')
 enrollment=a.request('POST','/api/v1/security/totp/setup',{'password':s['admin_password']})
 s['totp_secret']=enrollment['secret'];save_state(s)
 enrolled=a.request('POST','/api/v1/security/totp/confirm',{'code':totp(s['totp_secret'])})
 s['recovery_codes']=enrolled['recovery_codes'];s.pop('admin_cookie',None);save_state(s)
 a.request('GET','/api/v1/me',expected=(401,))
 checks.append('enrollment revokes prior session')

# Deliver the new factor before further tests, so an interrupted test cannot
# strand the operator with only the password. Never emit these values.
handoff={'url':'https://sub.caowo.baby','email':s['admin_email'],'password':s['admin_password'],
         'totp_secret':s['totp_secret'],'recovery_codes':s['recovery_codes']}
with connect('control') as c:
 code,out,err=run(c,'python3 -',"import json,os\np="+repr(handoff)+"\nfd=os.open('/root/migate-admin-access.json',os.O_WRONLY|os.O_TRUNC|os.O_CREAT,0o600)\nos.fchmod(fd,0o600)\nwith os.fdopen(fd,'w') as f:json.dump(p,f)\n")
 if code:raise RuntimeError('Cannot update protected MFA handoff')

a=API();challenge=a.login(s['admin_email'],s['admin_password'])['challenge']
a.request('GET','/api/v1/admin/users',expected=(401,))
a.request('POST','/api/v1/auth/mfa',{'challenge':challenge,'code':'invalid'},expected=(401,))
# Enrollment consumed the current TOTP tick; use a fresh tick for a real login.
time.sleep(31-int(time.time())%30)
code=totp(s['totp_secret'])
a.request('POST','/api/v1/auth/mfa',{'challenge':challenge,'code':code})
a.request('GET','/api/v1/admin/users')
checks.extend(['password alone has no administrator session','invalid second factor rejected','fresh TOTP permits administrator access'])
b=API();challenge2=b.login(s['admin_email'],s['admin_password'])['challenge']
b.request('POST','/api/v1/auth/mfa',{'challenge':challenge2,'code':code},expected=(401,))
checks.append('replayed TOTP rejected')
a.request('POST','/api/v1/auth/logout')
a.request('GET','/api/v1/admin/users',expected=(401,))
checks.append('logout revokes session')
time.sleep(31-int(time.time())%30)
s=state();s.pop('admin_cookie',None);save_state(s)
API().admin()
Path('docs/evidence/live-mfa.json').write_text(json.dumps({'date':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),'version':'0.2.0-rc6','checks':checks,'passed':True},indent=2))
print('Live administrator TOTP, replay rejection, MFA policy and session revocation passed')
