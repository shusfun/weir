"""Upload locally cross-built pinned metering core; retain previous owned binary.

The Agent applies configuration only through its bounded local helper. This
installer never starts a listener or changes x-ui/system-wide services.
"""
import hashlib,json,subprocess
from pathlib import Path
from remote import connect,run

directory=Path('dist/cores/sing-box-1.14.3-stats')
provenance=json.loads((directory/'provenance.json').read_text(encoding='utf-8-sig'))
assert provenance['version']=='v1.14.3' and provenance['sum']=='h1:cBg8TP5SPWoMAlWqTqbSNdHGKPUciZisjQbLtqFlMZA='
checks={line.split(maxsplit=1)[1]:line.split()[0] for line in (directory/'SHA256SUMS').read_text(encoding='utf-8-sig').splitlines()}
for name,digest in checks.items():
 assert '/' not in name and '\\' not in name
 assert hashlib.sha256((directory/name).read_bytes()).hexdigest()==digest
package=directory/'source-and-build.tar.gz'
sources=[name for name in checks if name not in ['sing-box','sing-box.exe']]
subprocess.run(['tar','-czf',str(package),'-C',str(directory),*sources],check=True)
binary_digest=checks['sing-box'];source_digest=hashlib.sha256(package.read_bytes()).hexdigest()
with connect('gate') as c:
 code,out,err=run(c,'test -f /etc/migate-gate/install-manifest.json && test -d /opt/migate-gate/cores && test ! -L /opt/migate-gate/cores && ! systemctl is-active --quiet migate-gate-sing-box')
 if code:raise RuntimeError('Owned installation required; existing running sing-box requires explicit core upgrade flow')
 with c.open_sftp() as ftp:
  ftp.put(str(directory/'sing-box'),'/opt/migate-gate/cores/sing-box.new');ftp.chmod('/opt/migate-gate/cores/sing-box.new',0o755)
  ftp.put(str(package),'/opt/migate-gate/cores/sing-box-source.tar.gz');ftp.chmod('/opt/migate-gate/cores/sing-box-source.tar.gz',0o644)
 code,out,err=run(c,f"printf '%s  %s\\n' {binary_digest} /opt/migate-gate/cores/sing-box.new {source_digest} /opt/migate-gate/cores/sing-box-source.tar.gz | sha256sum -c - && if test -f /opt/migate-gate/cores/sing-box; then cp -p /opt/migate-gate/cores/sing-box /opt/migate-gate/cores/sing-box.previous; fi && mv /opt/migate-gate/cores/sing-box.new /opt/migate-gate/cores/sing-box && /opt/migate-gate/cores/sing-box version")
 if code:raise RuntimeError('Verified core installation failed')
 print('Pinned sing-box metering core uploaded and verified; no inbound started')
 Path('docs/evidence/live-singbox-install.json').write_text(json.dumps({'version':'1.14.3-migate.stats1','sha256':binary_digest,'source_archive_sha256':source_digest,'verified_upload':True,'started_listener':False},indent=2))
