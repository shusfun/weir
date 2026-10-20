"""Restricted outbound control tunnel for a network that resets direct TLS."""
import json
import shlex
from remote import connect,run

with connect('control') as center,connect('gate') as gate:
 code,out,err=run(gate,"install -d -m 700 -o migate-gate -g migate-gate /var/lib/migate-gate/tunnel; test -f /var/lib/migate-gate/tunnel/id_ed25519 || runuser -u migate-gate -- ssh-keygen -q -t ed25519 -N '' -f /var/lib/migate-gate/tunnel/id_ed25519; cat /var/lib/migate-gate/tunnel/id_ed25519.pub")
 if code:raise RuntimeError(err)
 pub=out.strip()
 code,out,err=run(center,"id migate-tunnel >/dev/null 2>&1 || useradd --system --home-dir /var/lib/migate-tunnel --shell /usr/sbin/nologin migate-tunnel; install -d -m 700 -o migate-tunnel -g migate-tunnel /var/lib/migate-tunnel/.ssh; cat /etc/ssh/ssh_host_ed25519_key.pub")
 if code:raise RuntimeError(err)
 hostkey=out.strip()
 authorized='restrict,port-forwarding,permitopen="127.0.0.1:443",command="/usr/sbin/nologin" '+pub+'\n'
 with center.open_sftp() as ftp:
  target='/var/lib/migate-tunnel/.ssh/authorized_keys'
  try:
   with ftp.open(target) as f: old=f.read().decode()
  except FileNotFoundError: old=''
  if old and old!=authorized:raise RuntimeError('Different tunnel key already registered')
  with ftp.open(target,'w') as f:f.write(authorized)
  ftp.chmod(target,0o600)
  with ftp.open('/etc/migate-control/tunnel-owned','w') as f:f.write('Dedicated MiGate transport; authorize Gate public key again after reinstall.\n')
  ftp.chmod('/etc/migate-control/tunnel-owned',0o600)
  manifest_path='/etc/migate-control/install-manifest.json'
  with ftp.open(manifest_path) as f:manifest=json.load(f)
  resource={'path':target,'kind':'transport-key'}
  if resource not in manifest['resources']:manifest['resources'].append(resource)
  with ftp.open(manifest_path,'w') as f:f.write(json.dumps(manifest))
 run(center,'chown migate-tunnel:migate-tunnel /var/lib/migate-tunnel/.ssh/authorized_keys')
 unit='''[Unit]
Description=MiGate restricted outbound control transport
After=network-online.target
Wants=network-online.target
[Service]
User=migate-gate
Group=migate-gate
ExecStart=/usr/bin/ssh -NT -o BatchMode=yes -o StrictHostKeyChecking=yes -o ExitOnForwardFailure=yes -o ServerAliveInterval=15 -o ServerAliveCountMax=3 -o UserKnownHostsFile=/var/lib/migate-gate/tunnel/known_hosts -i /var/lib/migate-gate/tunnel/id_ed25519 -L 127.0.0.1:19814:127.0.0.1:443 migate-tunnel@106.55.5.233
Restart=always
RestartSec=5
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
[Install]
WantedBy=multi-user.target
'''
 with gate.open_sftp() as ftp:
  with ftp.open('/var/lib/migate-gate/tunnel/known_hosts','w') as f:f.write('106.55.5.233 '+hostkey+'\n')
  ftp.chmod('/var/lib/migate-gate/tunnel/known_hosts',0o644)
  with ftp.open('/etc/systemd/system/migate-gate-tunnel.service','w') as f:f.write(unit)
  ftp.chmod('/etc/systemd/system/migate-gate-tunnel.service',0o644)
  manifest_path='/etc/migate-gate/install-manifest.json'
  with ftp.open(manifest_path) as f:manifest=json.load(f)
  resource={'path':'/etc/systemd/system/migate-gate-tunnel.service','kind':'service'}
  if resource not in manifest['resources']:manifest['resources'].append(resource)
  with ftp.open(manifest_path,'w') as f:f.write(json.dumps(manifest))
 code,out,err=run(gate,'systemctl daemon-reload; systemctl enable --now migate-gate-tunnel.service; systemctl is-active migate-gate-tunnel.service')
 if code:raise RuntimeError(err)
 print('Dedicated control tunnel enabled; SSH host key pinned; only center loopback HTTPS forwarding permitted')
