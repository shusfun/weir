"""Upload a locally built release, verify SHA-256 and install owned services."""
import argparse
import hashlib
from pathlib import Path
import re
import shlex
import time
import paramiko
import stat
from remote import connect, run

p=argparse.ArgumentParser()
p.add_argument('target',choices=['control','gate'])
p.add_argument('version')
a=p.parse_args()
if not re.fullmatch(r'[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}',a.version): raise ValueError('invalid version')
archive=Path(f'dist/migate-{a.version}-linux-amd64.tar.gz')
from release_gate import verify
verify(archive)
digest=hashlib.sha256(archive.read_bytes()).hexdigest()
remote=f'/var/tmp/migate-{a.target}-{a.version}.tar.gz'
directory=f'/var/tmp/migate-{a.target}-{a.version}'
# Only resume a prefix verified against this exact local archive. A dropped
# upload never reaches extraction/installation; the full SHA-256 still gates it.
for attempt in range(3):
 try:
  with connect(a.target) as c:
   with c.open_sftp() as ftp:
    try:
     info=ftp.lstat(remote)
     if not stat.S_ISREG(info.st_mode) or info.st_uid != 0:
      raise RuntimeError('Refusing upload path that is not an owned root regular file')
     offset=info.st_size
    except FileNotFoundError:
     with ftp.open(remote,'wx'): pass
     ftp.chmod(remote,0o600)
     offset=0
    if offset > archive.stat().st_size: offset=0
    if offset:
     prefix=hashlib.sha256()
     with archive.open('rb') as local:
      remaining=offset
      while remaining:
       chunk=local.read(min(1024*1024,remaining))
       if not chunk: raise RuntimeError('Local archive changed during upload')
       prefix.update(chunk);remaining-=len(chunk)
     code,out,_=run(c,f'head -c {offset} {shlex.quote(remote)} | sha256sum')
     if code or not out.split() or out.split()[0] != prefix.hexdigest(): offset=0
    with ftp.open(remote,'r+' if offset else 'w') as destination:
     ftp.chmod(remote,0o600)
     destination.set_pipelined(True)
     destination.seek(offset)
     with archive.open('rb') as local:
      local.seek(offset)
      while chunk:=local.read(1024*1024): destination.write(chunk)
   print(f'{a.target}: archive upload complete (verified resume prefix {offset} bytes)',flush=True)
  break
 except (OSError, EOFError, paramiko.SSHException):
  if attempt==2: raise
  print(f'{a.target}: upload interrupted; reconnecting with prefix verification',flush=True)
  time.sleep(2)
with connect(a.target) as c:
 code,out,err=run(c,f"""printf '%s  %s\\n' {digest} {shlex.quote(remote)} | sha256sum -c - || exit 1
if [ -e {directory} ] || [ -L {directory} ]; then
 [ ! -L {directory} ] && [ -d {directory} ] && [ "$(stat -c %u {directory})" = 0 ] || exit 1
else
 mkdir -m 700 {directory} || exit 1
fi
chmod 700 {directory} && tar -xzf {remote} -C {directory}""")
 if code: raise RuntimeError(out+err)
 print(out,end='')
 args='control 127.0.0.1:19810 https://sub.caowo.baby /etc/migate-control/pg-dump' if a.target=='control' else 'gate'
 code,out,err=run(c,f'sh {directory}/install.sh {args}',timeout=180)
 print(out,end='');print(err,end='')
 if code: raise SystemExit(code)
 if a.target=='control':
  with c.open_sftp() as ftp:
   for utility in ['pg_dump','pg_restore']:
    path='/etc/migate-control/'+utility.replace('_','-')
    with ftp.open(path,'w') as f:
     f.write('#!/bin/sh\nexec docker exec -i -e PGPASSWORD 1Panel-postgresql-EcCX '+utility+' "$@"\n')
    ftp.chmod(path,0o700)
 print(a.target,a.version,'installed; acceptance still required')
