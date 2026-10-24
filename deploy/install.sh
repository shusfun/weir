#!/bin/sh
# 安装本机已上传并解压的发布包（目标机器不编译源码）。
#   sh install.sh standalone|center [监听地址]   单机 / 管理端（默认 127.0.0.1:19810）
#   sh install.sh gate                           只装 Gate 执行端
#   sh install.sh uninstall [--purge]            卸载；--purge 同时删除配置与数据目录
# 新版直接覆盖旧版：发现旧版服务单元与程序目录时停止并删除；旧数据目录只列出，不自动删除。
set -eu
umask 077

usage() { echo 'usage: sh install.sh standalone|center [listen] | gate | uninstall [--purge]' >&2; exit 2; }
[ "$(id -u)" = 0 ] || { echo 'root required' >&2; exit 1; }
action=${1:-}
case "$action" in standalone|center|gate|uninstall) ;; *) usage;; esac

base=/opt/migate
etc=/etc/migate
lib=/var/lib/migate
units_dir=/etc/systemd/system
all_units="migate-center.service migate-dsh.service migate-gate.service migate-helper.service migate-local-pair.service"

stop_units() {
 for unit in $all_units; do
  systemctl disable --now "$unit" >/dev/null 2>&1 || true
 done
}

if [ "$action" = uninstall ]; then
 purge=${2:-}
 stop_units
 for unit in $all_units; do rm -f "$units_dir/$unit"; done
 systemctl daemon-reload
 rm -f /usr/local/bin/migate
 rm -rf "$base" /usr/local/lib/migate
 if [ "$purge" = --purge ]; then
  rm -rf "$etc" "$lib"
  echo 'Removed program, configuration and data. A PostgreSQL migate schema or Redis migate:* keys, if used, are not touched.'
 else
  echo "Removed program and services. Kept configuration and data: $etc $lib"
 fi
 exit 0
fi

role=$action
package=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
cd "$package"
sha256sum -c SHA256SUMS >/dev/null
version=$(cat VERSION)
case "$version" in ''|.|..|*[!a-zA-Z0-9._-]*) echo 'invalid version' >&2; exit 1;; esac
install -d -m 0755 /run/lock
exec 9>/run/lock/migate-install.lock
flock -n 9 || { echo 'Another installation is running' >&2; exit 1; }

# 角色只能扩展到单机，不能静默缩小：已装单机时再装 gate 视为升级同一单机。
if [ -f "$etc/role" ]; then
 installed=$(cat "$etc/role")
 if [ "$installed" = standalone ]; then role=standalone; fi
 if [ "$installed" != "$role" ] && [ "$role" != standalone ]; then
  echo "This machine is installed as $installed; uninstall first or install standalone" >&2; exit 1
 fi
fi
has_center=false; has_gate=false
case "$role" in standalone) has_center=true; has_gate=true;; center) has_center=true;; gate) has_gate=true;; esac
# 中心拉取内核 / 订阅源、Gate 实测 REALITY 目标都要校验 HTTPS 证书：最小化系统可能没有 CA 证书库，尽量补装。
if [ ! -s /etc/ssl/certs/ca-certificates.crt ] && [ ! -s /etc/pki/tls/certs/ca-bundle.crt ] && [ ! -s /etc/ssl/cert.pem ]; then
 if command -v apt-get >/dev/null 2>&1; then apt-get install -y ca-certificates >/dev/null 2>&1 || true
 elif command -v dnf >/dev/null 2>&1; then dnf install -y ca-certificates >/dev/null 2>&1 || true
 elif command -v apk >/dev/null 2>&1; then apk add --no-cache ca-certificates >/dev/null 2>&1 || true
 fi
 [ -s /etc/ssl/certs/ca-certificates.crt ] || [ -s /etc/pki/tls/certs/ca-bundle.crt ] || [ -s /etc/ssl/cert.pem ] ||
  echo 'warning: no system CA bundle; install ca-certificates (kernel downloads, subscription sources and REALITY probing need it)' >&2
fi
listen=${2:-127.0.0.1:19810}
port=${listen##*:}
case "$port" in ''|*[!0-9]*) echo 'invalid listen address' >&2; exit 2;; esac

# —— 覆盖旧版：只删除旧服务单元与旧程序目录，旧配置 / 数据目录保留并提示 ——
legacy=false
for unit in migate-control.service migate-control-update.service migate-control-update.path migate-gate.service migate-gate-helper.service migate-gate-xray.service migate-gate-sing-box.service migate-gate-tunnel.service migate-gate-local-pair.service; do
 if [ -f "$units_dir/$unit" ]; then
  [ "$unit" = migate-gate.service ] && [ -f "$etc/role" ] && continue
  systemctl disable --now "$unit" >/dev/null 2>&1 || true
  rm -f "$units_dir/$unit"
  legacy=true
 fi
done
for dir in /opt/migate-control /opt/migate-gate; do
 if [ -d "$dir" ]; then rm -rf "$dir"; legacy=true; fi
done
for link in /usr/local/bin/migate-control /usr/local/bin/migate-gate /usr/local/bin/migate-manage /usr/local/bin/migate-install; do rm -f "$link"; done
if [ -L /usr/local/bin/migate ] && [ "$(readlink /usr/local/bin/migate)" != "$base/current/migate" ]; then rm -f /usr/local/bin/migate; fi

# —— 程序：releases/<版本>，current 指向当前，保留上一版本用于回退 ——
previous=''
if [ -L "$base/current" ]; then previous=$(basename "$(readlink "$base/current")"); fi
[ "$previous" != "$version" ] || previous=''
install -d -m 0755 "$base" "$base/releases"
if [ -e "$base/releases/$version" ]; then rm -rf "$base/releases/$version"; fi
install -d -m 0755 "$base/releases/$version"
install -m 0755 migate "$base/releases/$version/migate"
cp SHA256SUMS VERSION "$base/releases/$version/"
for item in sources.json source.tar.gz third_party; do
 if [ -e "$item" ]; then cp -R "$item" "$base/releases/$version/"; fi
done
stop_units
if [ -n "$previous" ]; then ln -sfn "releases/$previous" "$base/previous"; fi
ln -sfn "releases/$version" "$base/current"
ln -sfn "$base/current/migate" /usr/local/bin/migate
for old in "$base"/releases/*; do
 name=$(basename "$old")
 [ "$name" = "$version" ] || [ "$name" = "$previous" ] || rm -rf "$old"
done
install -d -m 0755 /usr/local/lib/migate
install -m 0755 "$package/install.sh" /usr/local/lib/migate/install.sh
install -d -m 0755 "$etc"

rollback() {
 if [ -n "$previous" ]; then
  ln -sfn "releases/$previous" "$base/current"
  systemctl daemon-reload
  for unit in $active; do systemctl restart "$unit" || true; done
  echo "Startup failed; switched back to $previous" >&2
 else
  echo 'Startup failed; no previous version to restore' >&2
 fi
 exit 1
}
active=''

# —— 中心 ——
if [ "$has_center" = true ]; then
 id migate-center >/dev/null 2>&1 || useradd --system --home-dir /nonexistent --shell /usr/sbin/nologin migate-center
 # dsh（DeepSeek Harness）的管理进程用独立用户：它能执行命令，所以读不到中心的配置、数据库与密钥。
 # 中心加入 migate-dsh 组，只为读取 dsh 写回的状态；dsh 只能读 control/，不能改中心下载的程序。
 id migate-dsh >/dev/null 2>&1 || useradd --system --home-dir /nonexistent --shell /usr/sbin/nologin migate-dsh
 usermod -a -G migate-dsh migate-center
 # /var/lib/migate 由 root 持有；中心与 Gate 各自只拥有自己的子目录，互不可写。
 install -d -m 0755 "$lib"
 install -d -m 0700 -o migate-center -g migate-center "$etc/center" "$lib/center"
 install -d -m 0755 "$lib/dsh"
 install -d -m 0755 -o migate-center -g migate-center "$lib/dsh/runtime"
 install -d -m 2750 -o migate-center -g migate-dsh "$lib/dsh/control" "$lib/dsh/control/jobs"
 install -d -m 2750 -o migate-dsh -g migate-dsh "$lib/dsh/status" "$lib/dsh/status/jobs"
 install -d -m 0700 -o migate-dsh -g migate-dsh "$lib/dsh/state"
 if [ ! -f "$etc/center/config.json" ]; then
  setpriv --reuid=migate-center --regid=migate-center --init-groups \
   "$base/current/migate" center --config "$etc/center/config.json" --listen "$listen" --init >/dev/null
 fi
 cat > "$units_dir/migate-center.service" <<'UNIT'
[Unit]
Description=MiGate center
After=network-online.target
Wants=network-online.target
[Service]
Type=simple
User=migate-center
Group=migate-center
SupplementaryGroups=migate-dsh
Environment=MIGATE_DSH_DIR=/var/lib/migate/dsh
Environment=MIGATE_DSH_AGENT=systemd
ExecStart=/opt/migate/current/migate center --config /etc/migate/center/config.json --serve
Restart=on-failure
UMask=0077
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
ReadWritePaths=/etc/migate/center /var/lib/migate/center /var/lib/migate/dsh/runtime /var/lib/migate/dsh/control
[Install]
WantedBy=multi-user.target
UNIT
 # dsh 管理进程：只能写自己的状态与家目录，中心与 Gate 的配置、数据目录对它不可见。
 cat > "$units_dir/migate-dsh.service" <<'UNIT'
[Unit]
Description=MiGate dsh (DeepSeek Harness) agent
After=network-online.target migate-center.service
Wants=network-online.target
[Service]
Type=simple
User=migate-dsh
Group=migate-dsh
ExecStart=/opt/migate/current/migate dsh agent --dir /var/lib/migate/dsh
Restart=always
RestartSec=5
UMask=0077
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
ReadWritePaths=/var/lib/migate/dsh/status /var/lib/migate/dsh/state
InaccessiblePaths=-/etc/migate -/var/lib/migate/center -/var/lib/migate/gate
[Install]
WantedBy=multi-user.target
UNIT
 active="$active migate-center.service migate-dsh.service"
fi

# —— Gate ——
if [ "$has_gate" = true ]; then
 id migate-gate >/dev/null 2>&1 || useradd --system --home-dir /nonexistent --shell /usr/sbin/nologin migate-gate
 install -d -m 0750 -o root -g migate-gate "$etc/gate" "$etc/gate/cores" "$etc/gate/certs" "$base/cores"
 install -d -m 0755 "$lib"
 install -d -m 0700 -o migate-gate -g migate-gate "$lib/gate"
 cat > "$units_dir/migate-helper.service" <<'UNIT'
[Unit]
Description=MiGate bounded core helper
After=network-online.target
[Service]
Group=migate-gate
RuntimeDirectory=migate
RuntimeDirectoryMode=0750
ExecStart=/opt/migate/current/migate gate helper
Restart=on-failure
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
ReadWritePaths=/run/migate /etc/migate/gate /opt/migate/cores /opt/migate
[Install]
WantedBy=multi-user.target
UNIT
 cat > "$units_dir/migate-gate.service" <<'UNIT'
[Unit]
Description=MiGate Gate agent
After=network-online.target migate-helper.service
Requires=migate-helper.service
ConditionPathExists=/etc/migate/gate/agent.json
[Service]
User=migate-gate
Group=migate-gate
ExecStart=/opt/migate/current/migate gate run --config /etc/migate/gate/agent.json
Restart=on-failure
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
ReadWritePaths=/var/lib/migate/gate /run/migate
[Install]
WantedBy=multi-user.target
UNIT
 # 内核由辅助程序作为子进程托管（降权为 migate-gate、崩溃自动重启），不再单独建 systemd 单元。
 rm -f "$units_dir/migate-xray.service" "$units_dir/migate-sing-box.service"
 active="$active migate-helper.service"
fi

# —— 单机：本机 Gate 用一次性凭证自动配对；中心只存摘要，配对成功后两边都删除 ——
if [ "$role" = standalone ] && [ ! -f "$etc/gate/agent.json" ]; then
 token=$(od -An -N32 -tx1 /dev/urandom | tr -d ' \n')
 printf '%s' "$token" > "$etc/gate/local-pair.token"
 chmod 0600 "$etc/gate/local-pair.token"
 printf '%s' "$token" | sha256sum | cut -d' ' -f1 > "$etc/center/local-pair.sha256"
 unset token
 chown migate-center:migate-center "$etc/center/local-pair.sha256"
 chmod 0600 "$etc/center/local-pair.sha256"
 cat > "$units_dir/migate-local-pair.service" <<UNIT
[Unit]
Description=MiGate standalone local Gate pairing
After=network-online.target migate-center.service migate-helper.service
ConditionPathExists=/etc/migate/gate/local-pair.token
ConditionPathExists=!/etc/migate/gate/agent.json
[Service]
Type=oneshot
TimeoutStartSec=infinity
ExecStart=/opt/migate/current/migate gate local-pair --config /etc/migate/gate/agent.json --token-file /etc/migate/gate/local-pair.token --center http://127.0.0.1:$port
ExecStartPost=/usr/bin/env systemctl start migate-gate.service
[Install]
WantedBy=multi-user.target
UNIT
fi

printf '%s\n' "$role" > "$etc/role"
chmod 0644 "$etc/role"
systemctl daemon-reload
for unit in $active; do
 systemctl enable "$unit" >/dev/null
 systemctl restart "$unit" || rollback
done
if [ "$has_center" = true ]; then
 healthy=false
 for attempt in 1 2 3 4 5 6 7 8 9 10; do
  if "$base/current/migate" center --config "$etc/center/config.json" --health-check >/dev/null 2>&1; then healthy=true; break; fi
  sleep 1
 done
 [ "$healthy" = true ] || rollback
fi
if [ "$has_gate" = true ]; then
 ready=false
 for attempt in 1 2 3 4 5; do
  if "$base/current/migate" gate helper-health >/dev/null 2>&1; then ready=true; break; fi
  sleep 1
 done
 [ "$ready" = true ] || rollback
 if [ -f "$etc/gate/agent.json" ]; then systemctl enable --now migate-gate.service >/dev/null; fi
 if [ -f "$units_dir/migate-local-pair.service" ] && [ ! -f "$etc/gate/agent.json" ]; then
  systemctl enable migate-local-pair.service >/dev/null
  systemctl start --no-block migate-local-pair.service
 fi
fi

echo "Installed MiGate $version ($role)."
if [ "$has_center" = true ]; then
 if grep -Eq '"installed"[[:space:]]*:[[:space:]]*true' "$etc/center/config.json"; then
  echo 'Center is initialized; configuration and data were kept.'
 else
  echo 'Center is not initialized yet. Run: migate'
  echo "  Web: read $etc/center/setup-token (root only, valid 30 minutes) and open the wizard."
  echo '  Terminal: migate center --setup'
 fi
fi
if [ "$role" = standalone ]; then echo 'The local Gate pairs itself after the center is initialized.'; fi
if [ "$role" = gate ] && [ ! -f "$etc/gate/agent.json" ]; then echo 'Pair this Gate from the center (Add Gate) to finish.'; fi
if [ "$legacy" = true ]; then
 echo 'Replaced a previous MiGate installation. Old data directories were kept and can be removed manually:'
 for dir in /etc/migate-control /etc/migate-gate /var/lib/migate-control /var/lib/migate-gate; do
  [ -e "$dir" ] && echo "  $dir"
 done
fi
