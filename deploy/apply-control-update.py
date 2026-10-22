"""Download the published v0.1.8 archive through a checksummed mirror and install it."""
from pathlib import Path
from remote import connect, run

fetcher = Path(__file__).resolve().parent / "release_fetch.py"
remote_fetcher = "/var/tmp/release_fetch.new.py"
script = r"""
set -eu
python3 - <<'PY'
import os, signal, time
from pathlib import Path
for item in Path('/proc').iterdir():
    if not item.name.isdigit():
        continue
    try:
        command = (item / 'cmdline').read_bytes().split(b'\0')
    except OSError:
        continue
    if command and command[0].endswith(b'migate-install'):
        os.kill(int(item.name), signal.SIGTERM)
time.sleep(1)
PY
rm -rf /var/tmp/migate-manual-v018
mkdir -p /var/tmp/migate-manual-v018
cd /var/tmp/migate-manual-v018
name=migate-0.1.8-linux-amd64.tar.gz
path=shusfun/MiSub/releases/download/v0.1.8
ok=0
for prefix in \
  "https://ghfast.top/https://github.com/$path" \
  "https://ghproxy.net/https://github.com/$path" \
  "https://gh.llkk.cc/https://github.com/$path" \
  "https://github.com/$path"
 do
  rm -f checksums.txt "$name"
  if curl -fL --connect-timeout 20 --retry 2 --retry-delay 2 -o checksums.txt "$prefix/checksums.txt" \
    && curl -fL --connect-timeout 20 --retry 2 --retry-delay 2 -o "$name" "$prefix/$name"; then
    expected=$(awk -v n="$name" '$2==n {print $1}' checksums.txt)
    got=$(sha256sum "$name" | awk '{print $1}')
    if [ -n "$expected" ] && [ "$expected" = "$got" ]; then ok=1; break; fi
  fi
 done
test "$ok" = 1
rm -rf bundle
mkdir bundle
tar -xzf "$name" -C bundle
cd bundle
test -x /etc/migate-control/pg-dump
sh install.sh control 127.0.0.1:19810 https://sub.caowo.baby /etc/migate-control/pg-dump
/opt/migate-control/current/migate-control --version
systemctl is-active migate-control
"""
client = connect("control")
try:
    with client.open_sftp() as sftp:
        sftp.put(str(fetcher), remote_fetcher)
    status, output, error = run(client, script, timeout=600)
finally:
    client.close()
print(output, end="")
if error.strip():
    print(error, end="")
if status:
    raise SystemExit(status)
