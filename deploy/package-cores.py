"""Ship reviewed core packages, source/license and an immutable checksum catalog."""
import hashlib,json,shutil,sys
from pathlib import Path

destination=Path(sys.argv[1])/'core-packages'
destination.mkdir(exist_ok=True)
items=json.loads(Path('deploy/core-packages.json').read_text(encoding='utf-8'))
catalog=[]
for item in items:
 core,version,source,checksum=(item[k] for k in ['core','version','file','sha256'])
 data=Path(source).read_bytes()
 if hashlib.sha256(data).hexdigest()!=checksum:raise RuntimeError('Pinned core binary checksum mismatch: '+core+' '+version)
 (destination/(core+'-'+version)).write_bytes(data)
 catalog.append({'core':core,'version':version,'sha256':checksum,'compatibility':item.get('compatibility',''),'recommended':item.get('recommended',False)})
(destination/'catalog.json').write_text(json.dumps(catalog,indent=2),encoding='utf-8')
notices=destination/'notices';notices.mkdir(exist_ok=True)
(notices/'core-provenance.json').write_text(json.dumps(items,indent=2),encoding='utf-8')
for file in ['LICENSE','source-and-build.tar.gz','provenance.json','build-singbox.ps1','go.mod','go.sum']:
 shutil.copyfile(Path('dist/cores/sing-box-1.14.3-stats')/file,notices/('sing-box-'+file))
for version in ['v26.3.27','v26.10.17']:
 license=Path('dist/cores')/version/'LICENSE'
 if license.exists():shutil.copyfile(license,notices/(version+'-LICENSE'))
print('Pinned core packages and source notices included; helper never downloads arbitrary binaries')
