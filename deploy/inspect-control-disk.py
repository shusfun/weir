"""Read-only disk diagnosis; run through production-run.py with scope none."""
import argparse
from remote import connect, run

parser = argparse.ArgumentParser()
parser.add_argument('--detail', action='store_true')
parser.add_argument('--inventory', action='store_true')
args = parser.parse_args()
checks = {
    'filesystem': 'df -hT / /var /opt; df -i /',
    'top directories': 'du -x -h --max-depth=1 / 2>/dev/null | sort -h',
    'application and backup directories': 'du -x -h --max-depth=2 /opt/migate-control /var/backups /var/tmp /tmp 2>/dev/null | sort -h | tail -45',
    'logs': 'journalctl --disk-usage; du -x -h --max-depth=1 /var/log 2>/dev/null | sort -h',
}
if args.detail:
    checks = {
        'var and opt breakdown': 'du -x -h --max-depth=2 /var /opt 2>/dev/null | sort -h | tail -55',
        'project artifact totals': "python3 - <<'PY'\nfrom pathlib import Path\nfrom collections import defaultdict\nfor root in ['/var/backups/migate','/var/tmp','/tmp','/opt/migate-control/releases']:\n p=Path(root); groups=defaultdict(lambda:[0,0]); largest=[]\n if not p.exists(): continue\n for f in p.rglob('*'):\n  if not f.is_file() or f.is_symlink(): continue\n  try: s=f.stat().st_size\n  except OSError: continue\n  key='snapshot' if f.name.endswith('.snapshot.json') else ('archive' if f.name.endswith(('.tar.gz','.tgz','.zip')) else ('database' if f.name.endswith(('.pgdump','.sql','.dump')) else 'other'))\n  groups[key][0]+=1;groups[key][1]+=s\n  largest.append((s,str(f)))\n print(root,dict(groups))\n for s,n in sorted(largest,reverse=True)[:12]: print(s,n)\nPY",
        'docker usage': 'docker system df',
        'deleted open files': "if command -v lsof >/dev/null; then lsof -nP +L1 2>/dev/null | head -25; fi",
    }
if args.inventory:
    checks = {'cleanup inventory': """python3 - <<'PY'
import json,os
from pathlib import Path
for name in ['/etc/migate-control/install-manifest.json','/opt/migate-control/current','/opt/migate-control/previous']:
 p=Path(name)
 print('PATH',name,'exists',p.exists(),'link',os.readlink(p) if p.is_symlink() else None)
 if p.is_file() and p.name.endswith('.json'):
  d=json.loads(p.read_text());print(json.dumps({k:d.get(k) for k in ['role','current','previous','releases']},ensure_ascii=False))
items=list(Path('/var/tmp').iterdir()); print('TOP_LEVEL',len(items))
for p in items:
 if not p.name.startswith('migate-control-'):print('OTHER',p.name)
dirs=[p for p in items if p.name.startswith('migate-control-') and p.is_dir()]
print('PROJECT_DIR_COUNT',len(dirs))
for p in sorted(dirs)[-6:]:
 print('DIR',p.name,sorted(x.name for x in p.iterdir()))
 v=p/'VERSION';print('VERSION',v.read_text().strip() if v.is_file() else 'missing')
for p in items:
 if p.name.startswith('migate-install-') and p.is_dir():
  print('INSTALL_SNAPSHOT',p.name,[(f.name,f.stat().st_size) for f in p.iterdir()])
  f=p/'install-manifest.json'
  if f.is_file():
   d=json.loads(f.read_text());print('SNAPSHOT_ROLE_VERSION',d.get('role'),d.get('current'))
print('MANIFEST_CANDIDATES')
for root in ['/etc/migate-control','/var/backups/migate']:
 for p in Path(root).glob('*manifest*'):print(str(p))
PY
systemctl is-active migate-control; df -h /"""}
with connect('control') as client:
    for label, command in checks.items():
        print('\n' + label, flush=True)
        code, output, error = run(client, command, timeout=120)
        print(output, end='', flush=True)
        if code:
            print('Exit:', code, error[:500], flush=True)
