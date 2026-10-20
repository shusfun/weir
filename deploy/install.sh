#!/bin/sh
# Install a locally built and uploaded release; never builds on the target.
set -eu
umask 077
role=${1:-}
case "$role" in control|gate) ;; *) echo 'usage: sh install.sh control|gate [listen-address] [public-HTTPS-URL]' >&2; exit 2;; esac
[ "$(id -u)" = 0 ] || { echo 'root required' >&2; exit 1; }
package=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
cd "$package"
sha256sum -c SHA256SUMS
chmod 0755 migate-control migate-gate migate-manage
version=$(cat VERSION)
case "$version" in ''|.|..|*[!a-zA-Z0-9._-]*) echo 'invalid version' >&2; exit 1;; esac
base=/opt/migate-$role
config=/etc/migate-$role
data=/var/lib/migate-$role
exec 9>"/run/lock/migate-$role-install.lock"
flock -n 9 || { echo 'Another installation is running' >&2; exit 1; }
for p in "$base" "$base/releases" "$config" "$config/cores" "$data" /opt /etc /var /var/lib; do
 [ ! -L "$p" ] || { echo "symlink path refused: $p" >&2; exit 1; }
done
if [ -e "$base" ] && [ ! -f "$config/install-manifest.json" ]; then
 echo 'Existing unmanaged directory; manual review required' >&2; exit 1
fi
if [ -e "$base/releases/$version" ]; then echo 'Version already exists; use a new release version' >&2; exit 1; fi
previous=''
releases=''
if [ -f "$config/install-manifest.json" ] && [ -x "$base/migate-manage" ]; then
 releases=$("$package/migate-manage" releases --manifest "$config/install-manifest.json")
fi
if [ -L "$base/current" ]; then previous=$(basename "$(readlink "$base/current")"); fi
# Keep exact pre-update metadata and service files outside the install tree.
rollback_dir=$(mktemp -d /var/tmp/migate-install-XXXXXX)
active_units=''
units="migate-$role.service"
if [ "$role" = gate ]; then units="$units migate-gate-helper.service migate-gate-xray.service migate-gate-sing-box.service"; fi
if [ "$role" = gate ] && [ -f /etc/systemd/system/migate-gate-tunnel.service ]; then units="$units migate-gate-tunnel.service"; fi
for unit in $units; do
 if systemctl is-active --quiet "$unit"; then active_units="$active_units $unit"; fi
 if [ -f "/etc/systemd/system/$unit" ]; then cp -p "/etc/systemd/system/$unit" "$rollback_dir/$unit"; fi
done
for item in install-manifest.json; do
 if [ -f "$config/$item" ]; then cp -p "$config/$item" "$rollback_dir/$item"; fi
done
if [ -f "$base/migate-manage" ]; then cp -p "$base/migate-manage" "$rollback_dir/migate-manage"; fi
old_previous=''
if [ -L "$base/previous" ]; then old_previous=$(readlink "$base/previous"); fi
committed=false
rollback() {
 result=$?
 trap - EXIT HUP INT TERM
 if [ "$committed" != true ]; then
  for unit in $units; do systemctl stop "$unit" >/dev/null 2>&1 || true; done
  if [ -n "$previous" ]; then ln -sfn "releases/$previous" "$base/current"; fi
  if [ -n "$old_previous" ]; then ln -sfn "$old_previous" "$base/previous"; elif [ -L "$base/previous" ]; then rm "$base/previous"; fi
  for unit in $units; do
   if [ -f "$rollback_dir/$unit" ]; then cp -p "$rollback_dir/$unit" "/etc/systemd/system/$unit"; fi
  done
  if [ -f "$rollback_dir/install-manifest.json" ]; then cp -p "$rollback_dir/install-manifest.json" "$config/install-manifest.json"; fi
  if [ -f "$rollback_dir/migate-manage" ]; then cp -p "$rollback_dir/migate-manage" "$base/migate-manage"; fi
  systemctl daemon-reload
  recovery_ready=true
  for unit in $active_units; do
   systemctl reset-failed "$unit" >/dev/null 2>&1 || true
   if ! systemctl start "$unit"; then recovery_ready=false; fi
  done
  # Type=simple only confirms exec, not that the previous application is ready.
  if [ -n "$previous" ]; then
   case " $active_units " in
    *" migate-$role.service "*)
     application_ready=false
     for attempt in 1 2 3 4 5 6 7 8 9 10; do
      if [ "$role" = control ]; then
       if "$base/current/migate-control" --config "$config/config.json" --health-check >/dev/null 2>&1; then application_ready=true; break; fi
      elif systemctl is-active --quiet migate-gate.service && "$base/current/migate-gate" helper-health >/dev/null 2>&1; then
       application_ready=true; break
      fi
      sleep 1
     done
     [ "$application_ready" = true ] || recovery_ready=false
     ;;
   esac
  fi
  if [ "$recovery_ready" = true ]; then
   echo "Installation failed; previous runtime and metadata restored; readiness verified. Diagnostic snapshot: $rollback_dir" >&2
  else
   echo "Installation failed; previous files restored but service recovery incomplete. Review diagnostics: $rollback_dir" >&2
  fi
  [ "$result" != 0 ] || result=1
 else
  rm -r "$rollback_dir"
 fi
 exit "$result"
}
trap rollback EXIT
trap 'exit 130' HUP INT TERM
if [ -n "$previous" ]; then
 "$package/migate-manage" backup --manifest "$config/install-manifest.json" \
  --backup-dir /var/backups/migate --pg-dump "${4:-pg_dump}" --confirm "$role:backup"
fi
for unit in $units; do systemctl stop "$unit" >/dev/null 2>&1 || true; done
install -d -m 0755 "$base/releases/$version"
install -d -m 0700 "$config" "$data"
install -m 0755 "migate-$role" "$base/releases/$version/"
install -m 0755 migate-manage "$base/migate-manage"
cp SHA256SUMS sources.json "$base/releases/$version/"
for item in source.tar.gz UIAble-LICENSE VERSION third_party; do
 if [ -e "$item" ]; then cp -R "$item" "$base/releases/$version/"; fi
done
if [ "$role" = gate ] && [ -f core-packages/catalog.json ]; then
 cp -R core-packages "$base/releases/$version/"
fi
if [ -n "$previous" ]; then ln -sfn "releases/$previous" "$base/previous"; fi
ln -sfn "releases/$version" "$base/current"
if [ "$role" = control ]; then
 listen=${2:-127.0.0.1:19810}
 public=${3:-http://localhost:19810}
 if [ ! -f "$config/config.json" ]; then
  "$base/current/migate-control" --config "$config/config.json" --listen "$listen" --public-url "$public" --init > "$config/initialization.txt"
  chmod 0600 "$config/initialization.txt"
 fi
 cat > /etc/systemd/system/migate-control.service <<'UNIT'
[Unit]
Description=MiGate Control
After=network-online.target
Wants=network-online.target
[Service]
Type=simple
DynamicUser=yes
StateDirectory=migate-control
ConfigurationDirectory=migate-control
ConfigurationDirectoryMode=0700
StateDirectoryMode=0700
ExecStart=/opt/migate-control/current/migate-control --config /etc/migate-control/config.json
Restart=on-failure
UMask=0077
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
ReadWritePaths=/etc/migate-control /var/lib/migate-control
[Install]
WantedBy=multi-user.target
UNIT
 # DynamicUser needs ownership of the existing credential directory. A static
 # service account keeps ownership deterministic across reinstall/recovery.
 id migate-control >/dev/null 2>&1 || useradd --system --home-dir /nonexistent --shell /usr/sbin/nologin migate-control
 sed -i 's/DynamicUser=yes/User=migate-control\nGroup=migate-control/' /etc/systemd/system/migate-control.service
 chown -R migate-control:migate-control "$config" "$data"
 if [ -f "$package/github-update.sh" ]; then install -m 0755 "$package/github-update.sh" /usr/local/bin/migate-install; fi
 if [ -f "$package/release_fetch.py" ]; then
  install -d -m 0755 /usr/local/lib/migate
  install -m 0644 "$package/release_fetch.py" "$package/version.py" /usr/local/lib/migate/
 fi
 cat > /etc/systemd/system/migate-control-update.service <<'UNIT'
[Unit]
Description=MiGate verified GitHub release update
[Service]
Type=oneshot
User=root
ExecStart=/usr/local/bin/migate-install --apply-request
UNIT
 cat > /etc/systemd/system/migate-control-update.path <<'UNIT'
[Unit]
Description=Watch MiGate release request
[Path]
PathExists=/var/lib/migate-control/release-request.json
Unit=migate-control-update.service
[Install]
WantedBy=multi-user.target
UNIT
 systemctl daemon-reload
 systemctl enable --now migate-control-update.path >/dev/null 2>&1 || true
else
 id migate-gate >/dev/null 2>&1 || useradd --system --home-dir /nonexistent --shell /usr/sbin/nologin migate-gate
 install -d -m 0750 "$config/cores" "$config/certs" "$base/cores"
 chown -R migate-gate:migate-gate "$data"
 cat > /etc/systemd/system/migate-gate.service <<'UNIT'
[Unit]
Description=MiGate outbound Agent
After=network-online.target migate-gate-helper.service
Requires=migate-gate-helper.service
ConditionPathExists=/etc/migate-gate/agent.json
[Service]
User=migate-gate
Group=migate-gate
ExecStart=/opt/migate-gate/current/migate-gate run
Restart=on-failure
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
ReadWritePaths=/var/lib/migate-gate /run/migate
[Install]
WantedBy=multi-user.target
UNIT
 cat > /etc/systemd/system/migate-gate-helper.service <<'UNIT'
[Unit]
Description=MiGate bounded core helper
After=network-online.target
[Service]
Group=migate-gate
RuntimeDirectory=migate
RuntimeDirectoryMode=0750
ExecStart=/opt/migate-gate/current/migate-gate helper
Restart=on-failure
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
NoNewPrivileges=yes
ReadWritePaths=/run/migate /etc/migate-gate/cores /etc/migate-gate/certs /opt/migate-gate/cores /opt/migate-gate
[Install]
WantedBy=multi-user.target
UNIT
 cat > /etc/systemd/system/migate-gate-xray.service <<'UNIT'
[Unit]
Description=MiGate isolated Xray core
StartLimitIntervalSec=10
StartLimitBurst=30
After=network-online.target
ConditionPathExists=/etc/migate-gate/cores/xray.json
[Service]
User=migate-gate
Group=migate-gate
ExecStart=/opt/migate-gate/cores/xray run -config /etc/migate-gate/cores/xray.json
Restart=on-failure
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
[Install]
WantedBy=multi-user.target
UNIT
 cat > /etc/systemd/system/migate-gate-sing-box.service <<'UNIT'
[Unit]
Description=MiGate isolated sing-box core
StartLimitIntervalSec=10
StartLimitBurst=30
After=network-online.target
ConditionPathExists=/etc/migate-gate/cores/sing-box.json
[Service]
User=migate-gate
Group=migate-gate
ExecStart=/opt/migate-gate/cores/sing-box run -c /etc/migate-gate/cores/sing-box.json
Restart=on-failure
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
[Install]
WantedBy=multi-user.target
UNIT
 chown root:migate-gate "$config" "$config/cores" "$config/certs" "$base/cores"
 chmod 0750 "$config"
fi
# Record only exact owned resources. The manifest survives non-purge uninstall.
cat > "$config/install-manifest.json" <<EOF
{"version":1,"role":"$role","current":"$version","previous":"$previous","releases":["$version"
EOF
for recorded in $releases; do
 case "$recorded" in ''|.|..|*[!a-zA-Z0-9._-]*) echo 'invalid recorded release' >&2; exit 1;; esac
 printf ',"%s"' "$recorded" >> "$config/install-manifest.json"
done
cat >> "$config/install-manifest.json" <<EOF
],"resources":[
 {"path":"$base/releases","kind":"program"},
 {"path":"$base/current","kind":"program"},
 {"path":"$base/previous","kind":"program"},
 {"path":"$base/migate-manage","kind":"program"},
 {"path":"$config","kind":"config"},
 {"path":"$data","kind":"data"},
 {"path":"/etc/systemd/system/migate-$role.service","kind":"service"}
EOF
if [ "$role" = gate ]; then cat >> "$config/install-manifest.json" <<'EOF'
 ,{"path":"/etc/systemd/system/migate-gate-helper.service","kind":"service"}
 ,{"path":"/etc/systemd/system/migate-gate-xray.service","kind":"core-service"}
 ,{"path":"/etc/systemd/system/migate-gate-sing-box.service","kind":"core-service"}
 ,{"path":"/opt/migate-gate/cores","kind":"core"}
 ,{"path":"/etc/migate-gate/cores","kind":"core"}
EOF
fi
if [ "$role" = gate ] && [ -f /etc/systemd/system/migate-gate-tunnel.service ]; then
 echo ',{"path":"/etc/systemd/system/migate-gate-tunnel.service","kind":"service"}' >> "$config/install-manifest.json"
fi
if [ "$role" = control ] && [ -f "$config/tunnel-owned" ]; then
 echo ',{"path":"/var/lib/migate-tunnel/.ssh/authorized_keys","kind":"transport-key"}' >> "$config/install-manifest.json"
fi
echo ']}' >> "$config/install-manifest.json"
chmod 0600 "$config/install-manifest.json"
systemctl daemon-reload
systemctl enable "migate-$role.service"
if [ "$role" = gate ]; then
 if [ -f /etc/systemd/system/migate-gate-tunnel.service ]; then systemctl start migate-gate-tunnel.service; fi
 systemctl enable --now migate-gate-helper.service
 helper_ready=false
 for attempt in 1 2 3 4 5; do
  if "$base/current/migate-gate" helper-health >/dev/null 2>&1 && systemctl is-active --quiet migate-gate-helper.service; then helper_ready=true; break; fi
  sleep 1
 done
 [ "$helper_ready" = true ] || { echo 'Gate helper health failed' >&2; exit 1; }
 if [ ! -f "$config/agent.json" ]; then
  echo 'Installed. Verify a separate Xray release/checksum, then pair using migate-gate pair --center HTTPS_URL --name NAME.'
  echo 'After pairing: chown root:migate-gate /etc/migate-gate/agent.json; chmod 0640 /etc/migate-gate/agent.json; systemctl start migate-gate'
  committed=true
  exit 0
 fi
fi
if ! systemctl restart "migate-$role.service"; then
 if [ -n "$previous" ]; then ln -sfn "releases/$previous" "$base/current"; systemctl restart "migate-$role.service"; fi
 echo 'Startup failed; previous binary restored when available. Data migration rollback requires verified backup.' >&2
 exit 1
fi
if [ "$role" = control ]; then
 healthy=false
 for attempt in 1 2 3 4 5 6 7 8 9 10; do
  if "$base/current/migate-control" --config "$config/config.json" --health-check >/dev/null 2>&1; then healthy=true; break; fi
  sleep 1
 done
 if [ "$healthy" != true ]; then
  if [ -n "$previous" ]; then ln -sfn "releases/$previous" "$base/current"; systemctl restart migate-control.service; fi
  echo 'Application health failed. Previous binary restored when present; review external backup before any data restore.' >&2; exit 1
 fi
else
 # Agent reconciles its persisted desired state, including intentionally
 # stopped cores. Starting cores here would bypass that state after upgrades.
 # Type=simple returning does not prove the process survived startup. Require
 # a stable Agent PID and an actual reply from the bounded helper.
 gate_pid=$(systemctl show -p MainPID --value migate-gate.service)
 [ "$gate_pid" -gt 0 ] || { echo 'Gate process absent' >&2; exit 1; }
 for attempt in 1 2 3; do
  sleep 1
  systemctl is-active --quiet migate-gate.service || { echo 'Gate startup failed' >&2; exit 1; }
  [ "$(systemctl show -p MainPID --value migate-gate.service)" = "$gate_pid" ] || { echo 'Gate restarted during startup' >&2; exit 1; }
  "$base/current/migate-gate" helper-health >/dev/null 2>&1 || { echo 'Gate helper unavailable' >&2; exit 1; }
 done
fi
if [ "$role" = control ]; then echo "Initialization credentials: $config/initialization.txt (root/service account only)"; fi
committed=true
echo 'Installed. Perform application health and real-client acceptance before marking passed.'
