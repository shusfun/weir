"""Pinned official Xray artifacts for deployment and real-client acceptance."""
from pathlib import Path
import hashlib
import urllib.request
import zipfile

VERSION = 'v26.3.27'
ARTIFACTS = {
 'Xray-linux-64.zip': '23cd9af937744d97776ee35ecad4972cf4b2109d1e0fe6be9930467608f7c8ae',
 'Xray-windows-64.zip': 'd004c39288ce9ada487c6f398c7c545f7d749e44bdfdd59dbc9f865afba4e1ad',
}
root = Path('dist/cores') / VERSION
root.mkdir(parents=True, exist_ok=True)
for name, expected in ARTIFACTS.items():
 path = root / name
 if not path.exists():
  req = urllib.request.Request(f'https://github.com/XTLS/Xray-core/releases/download/{VERSION}/{name}',headers={'User-Agent':'MiGate-release'})
  with urllib.request.urlopen(req, timeout=60) as response:
   path.write_bytes(response.read())
 if hashlib.sha256(path.read_bytes()).hexdigest()!=expected:
  raise RuntimeError(f'Checksum mismatch: {name}')
 with zipfile.ZipFile(path) as archive:
  binary='xray.exe' if 'windows' in name else 'xray'
  (root/binary).write_bytes(archive.read(binary))
 print(VERSION,name,'official SHA-256 verified')
