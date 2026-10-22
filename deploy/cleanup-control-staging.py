"""Explicit operator cleanup of verified control staging files, never installed releases."""
import argparse
import json
from pathlib import Path
from remote import connect, run

p = argparse.ArgumentParser()
p.add_argument('--apply', action='store_true')
p.add_argument('--diagnostics', action='store_true')
a = p.parse_args()
root = Path(__file__).resolve().parent.parent
versions = sorted({f.name.removeprefix('migate-').removesuffix('-linux-amd64.tar.gz')
                   for f in (root / 'dist').glob('migate-*-linux-amd64.tar.gz')})
rollbacks = set()
for f in (root / 'docs/evidence').glob('live-rollback*.json'):
    data = json.loads(f.read_text(encoding='utf-8'))
    for item in data.get('results', []):
        if item.get('role') == 'control' and item.get('injected_version'):
            rollbacks.add(item['injected_version'])
payload = {'versions': versions, 'rollbacks': sorted(rollbacks), 'apply': a.apply, 'diagnostics': a.diagnostics}
report_name = 'control-staging-diagnostics' if a.diagnostics else 'control-staging-cleanup'
if a.apply:
    prior = json.loads((root / 'docs/evidence' / (report_name+'-plan.json')).read_text(encoding='utf-8'))
    payload['verified'] = {item['path']: item['identity'] for item in prior['plan']}
code = r'''
import json,os,re,stat,hashlib,tarfile,shutil,subprocess,time,fcntl
from pathlib import Path
params=PARAMS
base=Path('/var/tmp')
assert base.resolve()==base
lock=open('/run/lock/migate-control-install.lock','a')
fcntl.flock(lock,fcntl.LOCK_EX|fcntl.LOCK_NB)
manifest=Path('/etc/migate-control/install-manifest.json')
m=json.loads(manifest.read_text()) if manifest.exists() else {}
keep={m.get('current'),m.get('previous')}-{None,''}
for name in ['current','previous']:
 p=Path('/opt/migate-control')/name
 if p.exists() or p.is_symlink():keep.add(p.resolve().name)
valid=re.compile(r'0\.2\.0-rc\d+')
available=[v for v in params['versions'] if valid.fullmatch(v) and (base/('migate-control-'+v)).is_dir()]
keep.update(sorted(available,key=lambda v:int(v.rsplit('rc',1)[1]))[-2:])
known=set(params['versions']); rb=set(params['rollbacks'])
reviewed_failures={'0.2.0-rc21-rollback-1792352224','0.2.0-rc7-rollback-1792345505','0.2.0-rc82-rollback-1792405245'} if params['diagnostics'] else set()
plan=[];skipped=[];diagnostics=[]
def fingerprint(p):
 s=p.lstat()
 assert s.st_uid==0 and not p.is_symlink()
 assert p.parent==base and p.resolve()==p
 assert stat.S_ISREG(s.st_mode) or stat.S_ISDIR(s.st_mode)
 return [s.st_dev,s.st_ino,s.st_size,s.st_mtime_ns]
def treecheck(p,strict_owner=True):
 total=0
 for folder,dirs,files in os.walk(p,followlinks=False):
  for name in dirs+files:
   q=Path(folder)/name;s=q.lstat()
   if q.is_symlink() or (strict_owner and s.st_uid!=0) or s.st_dev!=p.stat().st_dev or not (stat.S_ISREG(s.st_mode) or stat.S_ISDIR(s.st_mode)):
    raise ValueError('Unsafe nested file: '+str(q))
   if stat.S_ISREG(s.st_mode):total+=s.st_size
 return total
for p in sorted(base.iterdir()):
 name=p.name
 if not name.startswith('migate-control-'):continue
 if name.endswith('.log'):continue
 version=name.removeprefix('migate-control-').removesuffix('.tar.gz')
 if version in keep:continue
 if params['diagnostics'] and version not in reviewed_failures:continue
 if version in reviewed_failures:
  assert version.split('-rollback-')[0] in known
  assert (p/'migate-control').read_bytes()==b'#!/bin/sh\nexit 1\n'
  rb.add(version)
 if version not in known and version not in rb:
  skipped.append({'path':str(p),'reason':'not in local release or rollback evidence'});continue
 try:
  identity=fingerprint(p)
  if p.is_dir():
   assert (p/'VERSION').read_text().strip()==version
   for required in ['SHA256SUMS','install.sh','migate-control','migate-manage']:
    assert (p/required).is_file() and not (p/required).is_symlink()
   assert 'migate-control' in (p/'SHA256SUMS').read_text()
   size=treecheck(p)
   diagnostics.append(p)
  else:
   assert name.endswith('.tar.gz') and version in known
   if not params['apply'] or params.get('verified',{}).get(str(p))!=identity:
    with tarfile.open(p,'r:gz') as t:
     v=next(x for x in t if x.name in ['VERSION','./VERSION'])
     assert v.isfile() and v.size<128
     assert t.extractfile(v).read().decode().strip()==version
   size=p.stat().st_size
  plan.append({'path':str(p),'identity':identity,'bytes':size,'directory':p.is_dir()})
 except Exception as e:skipped.append({'path':str(p),'reason':str(e) or type(e).__name__})
if params['diagnostics']:
 for p in sorted(base.iterdir()):
  if not re.fullmatch(r'migate-install-[A-Za-z0-9]{6}',p.name):continue
  identity=fingerprint(p);assert p.is_dir()
  assert {x.name for x in p.iterdir()}=={'migate-manage','migate-control.service','install-manifest.json'}
  d=json.loads((p/'install-manifest.json').read_text())
  assert d.get('role')=='control' and d.get('current') in known and d.get('current') not in keep
  # Installer cp -p preserves the service account owner on old manifests.
  # The enclosing root-owned snapshot and exact three filenames are checked.
  plan.append({'path':str(p),'identity':identity,'bytes':treecheck(p,False),'directory':True,'backup_full':True})
# Archive diagnostic snapshots before removing their redundant loose copies.
before=shutil.disk_usage('/')
services=subprocess.run(['systemctl','list-units','--type=service','--state=running','--no-legend','--plain'],capture_output=True,text=True,check=True).stdout
report={'date':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),'scope':'control /var/tmp verified staging only','preserved_versions':sorted(keep),'plan':plan,'skipped':skipped,'planned_bytes':sum(x['bytes'] for x in plan),'disk_before':before._asdict(),'applied':False}
if params['apply']:
 out=Path('/var/backups/migate')/('staging-cleanup-'+time.strftime('%Y%m%dT%H%M%SZ',time.gmtime()))
 out.mkdir(mode=0o700,parents=True,exist_ok=False)
 (out/'plan.json').write_text(json.dumps(report,indent=2));os.chmod(out/'plan.json',0o600)
 with tarfile.open(out/'staging-metadata.tar.gz','w:gz') as tar:
  for d in diagnostics:
   for name in ['VERSION','SHA256SUMS','sources.json','BUILD.json','install.sh']:
    f=d/name
    if f.is_file():tar.add(f,arcname=d.name+'/'+name,recursive=False)
  for item in plan:
   if item.get('backup_full'):tar.add(item['path'],arcname=Path(item['path']).name,recursive=True)
 os.chmod(out/'staging-metadata.tar.gz',0o600)
 with tarfile.open(out/'staging-metadata.tar.gz','r:gz') as tar:
  for member in tar:
   if not member.isfile():continue
   archived=tar.extractfile(member)
   original=base/member.name
   digest=hashlib.sha256()
   while block:=archived.read(1024*1024):digest.update(block)
   with original.open('rb') as source:
    expected=hashlib.file_digest(source,'sha256').hexdigest()
   assert digest.hexdigest()==expected,'Diagnostic archive verification failed'
 completed=[]
 try:
  for item in plan:
   p=Path(item['path']);assert fingerprint(p)==item['identity']
   if item['directory']:shutil.rmtree(p)
   else:p.unlink()
   completed.append(str(p))
  report['applied']=True
 finally:
  report['removed']=completed
  report['disk_after']=shutil.disk_usage('/')._asdict()
  report['freed_bytes']=report['disk_after']['free']-report['disk_before']['free']
  report['receipt_directory']=str(out)
  after=subprocess.run(['systemctl','list-units','--type=service','--state=running','--no-legend','--plain'],capture_output=True,text=True,check=True).stdout
  report['running_service_set_unchanged']=sorted(x.split()[0] for x in services.splitlines())==sorted(x.split()[0] for x in after.splitlines())
  (out/'result.json').write_text(json.dumps(report,indent=2));os.chmod(out/'result.json',0o600)
print(json.dumps(report))
'''.replace('PARAMS', repr(payload))
with connect('control') as client:
    status, output, error = run(client, 'python3 -', stdin=code, timeout=600)
    if status:
        raise RuntimeError(error[-2500:])
    report = json.loads(output)
target = root / 'docs/evidence' / (report_name+('.json' if a.apply else '-plan.json'))
target.write_text(json.dumps(report, indent=2), encoding='utf-8')
print(json.dumps({k:v for k,v in report.items() if k not in ['plan','removed']}, indent=2))
print('Candidates:', len(report['plan']), 'Removed:', len(report.get('removed',[])), flush=True)
