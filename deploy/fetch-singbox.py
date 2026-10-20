"""Pinned official sing-box 1.14.3 assets; SHA256 from GitHub release metadata."""
from pathlib import Path
import hashlib,tarfile,urllib.request,zipfile
VERSION='1.14.3'
ARTIFACTS={
 f'sing-box-{VERSION}-linux-amd64.tar.gz':'e2bdf179c15a3652955dc44867e33ca0965c9c9b91b34267dcc5a5d639a5feee',
 f'sing-box-{VERSION}-windows-amd64.zip':'d8b86ef58936dba9230ec6a57ffa4243933f3c14ef4809828b6c553440ed1518',
}
root=Path('dist/cores')/('sing-box-'+VERSION);root.mkdir(parents=True,exist_ok=True)
for name,expected in ARTIFACTS.items():
 path=root/name
 if not path.exists():
  req=urllib.request.Request(f'https://github.com/SagerNet/sing-box/releases/download/v{VERSION}/{name}',headers={'User-Agent':'MiGate-release'})
  with urllib.request.urlopen(req,timeout=60) as response:path.write_bytes(response.read())
 if hashlib.sha256(path.read_bytes()).hexdigest()!=expected:raise RuntimeError('Official asset checksum mismatch: '+name)
 binary='sing-box.exe' if 'windows' in name else 'sing-box'
 member=name.removesuffix('.tar.gz').removesuffix('.zip')+'/'+binary
 if name.endswith('.zip'):
  with zipfile.ZipFile(path) as archive:data=archive.read(member)
 else:
  with tarfile.open(path) as archive:data=archive.extractfile(member).read()
 (root/binary).write_bytes(data)
 print(name,'official SHA-256 verified',flush=True)
