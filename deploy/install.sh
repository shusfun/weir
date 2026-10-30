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

base=/opt/weir
etc=/etc/weir
lib=/var/lib/weir
units_dir=/etc/systemd/system
# weir-update.service 不在这里：自更新时 install.sh 由它运行，停掉它会连带结束安装本身。
all_units="weir-center.service weir-dsh.service weir-gate.service weir-helper.service weir-local-pair.service weir-update.path"

stop_units() {
 for unit in $all_units; do
  systemctl disable --now "$unit" >/dev/null 2>&1 || true
 done
}

if [ "$action" = uninstall ]; then
 purge=${2:-}
 stop_units
 systemctl stop weir-update.service >/dev/null 2>&1 || true
 for unit in $all_units weir-update.service; do rm -f "$units_dir/$unit"; done
 systemctl daemon-reload
 rm -f /usr/local/bin/weir
 rm -rf "$base" /usr/local/lib/weir
 if [ "$purge" = --purge ]; then
  rm -rf "$etc" "$lib"
  echo 'Removed program, configuration and data. A PostgreSQL weir schema or Redis weir:* keys, if used, are not touched.'
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
exec 9>/run/lock/weir-install.lock
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

# —— 程序：releases/<版本>，current 指向当前，保留上一版本用于回退 ——
previous=''
if [ -L "$base/current" ]; then previous=$(basename "$(readlink "$base/current")"); fi
[ "$previous" != "$version" ] || previous=''
install -d -m 0755 "$base" "$base/releases"
if [ -e "$base/releases/$version" ]; then rm -rf "$base/releases/$version"; fi
install -d -m 0755 "$base/releases/$version"
# 保留整个发布包（含 install.sh），面板里「切回上一版本」直接用它重新安装。
for item in *; do cp -R "$item" "$base/releases/$version/"; done
chmod 0755 "$base/releases/$version/weir" "$base/releases/$version/install.sh"
stop_units
if [ -n "$previous" ]; then ln -sfn "releases/$previous" "$base/previous"; fi
ln -sfn "releases/$version" "$base/current"
ln -sfn "$base/current/weir" /usr/local/bin/weir
for old in "$base"/releases/*; do
 name=$(basename "$old")
 [ "$name" = "$version" ] || [ "$name" = "$previous" ] || rm -rf "$old"
done
install -d -m 0755 /usr/local/lib/weir
install -m 0755 "$package/install.sh" /usr/local/lib/weir/install.sh
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
 id weir-center >/dev/null 2>&1 || useradd --system --home-dir /nonexistent --shell /usr/sbin/nologin weir-center
 # dsh（DeepSeek Harness）的管理进程用独立用户：它能执行命令，所以读不到中心的配置、数据库与密钥。
 # 中心加入 weir-dsh 组，只为读取 dsh 写回的状态；dsh 只能读 control/，不能改中心下载的程序。
 id weir-dsh >/dev/null 2>&1 || useradd --system --home-dir /nonexistent --shell /usr/sbin/nologin weir-dsh
 usermod -a -G weir-dsh weir-center
 # /var/lib/weir 由 root 持有；中心与 Gate 各自只拥有自己的子目录，互不可写。
 install -d -m 0755 "$lib"
 install -d -m 0700 -o weir-center -g weir-center "$etc/center" "$lib/center"
 install -d -m 0755 "$lib/dsh"
 # 自更新：中心写请求、读进度（组 weir-center 可写）；root 的 weir-update 执行，解包目录只有 root 能进。
 install -d -m 2770 -o root -g weir-center "$lib/update"
 install -d -m 0700 "$lib/update-work"
 install -d -m 0755 -o weir-center -g weir-center "$lib/dsh/runtime"
 install -d -m 2750 -o weir-center -g weir-dsh "$lib/dsh/control" "$lib/dsh/control/jobs"
 install -d -m 2750 -o weir-dsh -g weir-dsh "$lib/dsh/status" "$lib/dsh/status/jobs"
 install -d -m 0700 -o weir-dsh -g weir-dsh "$lib/dsh/state"
 if [ ! -f "$etc/center/config.json" ]; then
  setpriv --reuid=weir-center --regid=weir-center --init-groups \
   "$base/current/weir" center --config "$etc/center/config.json" --listen "$listen" --init >/dev/null
 fi
 # 单机角色：本机 Gate 的 Agent 在中心进程里运行（内核仍由 root 辅助程序托管），
 # 中心加入 weir-gate 组以读取设备文件、访问辅助程序套接字，并拥有 Gate 的状态目录。
 local_groups='weir-dsh'; local_env=''; local_paths=''; local_after=''
 if [ "$role" = standalone ]; then
  local_groups='weir-dsh weir-gate'
  local_env='Environment=WEIR_LOCAL_GATE=/etc/weir/gate/agent.json'
  local_paths=' /var/lib/weir/gate /run/weir'
  local_after=' weir-helper.service'
 fi
 cat > "$units_dir/weir-center.service" <<UNIT
[Unit]
Description=Weir center
After=network-online.target$local_after
Wants=network-online.target$local_after
# 开机时存储容器可能比中心晚起来：不限次数，每 5 秒重试一次，不会因为「启动太频繁」停在 failed
StartLimitIntervalSec=0
[Service]
Type=simple
User=weir-center
Group=weir-center
SupplementaryGroups=$local_groups
Environment=WEIR_DSH_DIR=/var/lib/weir/dsh
Environment=WEIR_DSH_AGENT=systemd
$local_env
ExecStart=/opt/weir/current/weir center --config /etc/weir/center/config.json --serve
Restart=always
RestartSec=5
UMask=0077
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
ReadWritePaths=/etc/weir/center /var/lib/weir/center /var/lib/weir/dsh/runtime /var/lib/weir/dsh/control /var/lib/weir/update$local_paths
[Install]
WantedBy=multi-user.target
UNIT
 # dsh 管理进程：只能写自己的状态与家目录，中心与 Gate 的配置、数据目录对它不可见。
 cat > "$units_dir/weir-dsh.service" <<'UNIT'
[Unit]
Description=Weir dsh (DeepSeek Harness) agent
After=network-online.target weir-center.service
Wants=network-online.target
StartLimitIntervalSec=0
[Service]
Type=simple
User=weir-dsh
Group=weir-dsh
ExecStart=/opt/weir/current/weir dsh agent --dir /var/lib/weir/dsh
Restart=always
RestartSec=5
UMask=0077
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
ReadWritePaths=/var/lib/weir/dsh/status /var/lib/weir/dsh/state
InaccessiblePaths=-/etc/weir -/var/lib/weir/center -/var/lib/weir/gate
[Install]
WantedBy=multi-user.target
UNIT
 # 自更新执行端：中心写入 request.json 后由 path 单元触发，以 root 运行一次。
 cat > "$units_dir/weir-update.path" <<'UNIT'
[Unit]
Description=Weir self-update request watcher
[Path]
PathExists=/var/lib/weir/update/request.json
Unit=weir-update.service
[Install]
WantedBy=multi-user.target
UNIT
 cat > "$units_dir/weir-update.service" <<'UNIT'
[Unit]
Description=Weir self-update (runs as root, downloads and verifies the release itself)
[Service]
Type=oneshot
ExecStart=/opt/weir/current/weir self-update --apply
TimeoutStartSec=30min
UMask=0027
UNIT
 active="$active weir-center.service weir-dsh.service weir-update.path"
fi

# —— Gate ——
if [ "$has_gate" = true ]; then
 id weir-gate >/dev/null 2>&1 || useradd --system --home-dir /nonexistent --shell /usr/sbin/nologin weir-gate
 install -d -m 0750 -o root -g weir-gate "$etc/gate" "$etc/gate/cores" "$etc/gate/certs" "$base/cores"
 install -d -m 0755 "$lib"
 if [ "$role" = standalone ]; then
  install -d -m 0700 -o weir-center -g weir-center "$lib/gate"
  chown -R weir-center:weir-center "$lib/gate"
  usermod -a -G weir-gate weir-center
 else
  install -d -m 0700 -o weir-gate -g weir-gate "$lib/gate"
 fi
 cat > "$units_dir/weir-helper.service" <<'UNIT'
[Unit]
Description=Weir bounded core helper
After=network-online.target
StartLimitIntervalSec=0
[Service]
Group=weir-gate
RuntimeDirectory=weir
RuntimeDirectoryMode=0750
ExecStart=/opt/weir/current/weir gate helper
Restart=always
RestartSec=5
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
ReadWritePaths=/run/weir /etc/weir/gate /opt/weir/cores /opt/weir
[Install]
WantedBy=multi-user.target
UNIT
 if [ "$role" = standalone ]; then
  # 旧版单机的独立 Agent 服务：合并进中心进程后删除。
  systemctl disable --now weir-gate.service >/dev/null 2>&1 || true
  rm -f "$units_dir/weir-gate.service"
 else
 cat > "$units_dir/weir-gate.service" <<'UNIT'
[Unit]
Description=Weir Gate agent
After=network-online.target weir-helper.service
Requires=weir-helper.service
ConditionPathExists=/etc/weir/gate/agent.json
StartLimitIntervalSec=0
[Service]
User=weir-gate
Group=weir-gate
ExecStart=/opt/weir/current/weir gate run --config /etc/weir/gate/agent.json
Restart=always
RestartSec=5
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
ReadWritePaths=/var/lib/weir/gate /run/weir
[Install]
WantedBy=multi-user.target
UNIT
 fi
 # 内核由辅助程序作为子进程托管（降权为 weir-gate、崩溃自动重启），不再单独建 systemd 单元。
 rm -f "$units_dir/weir-xray.service" "$units_dir/weir-sing-box.service"
 active="$active weir-helper.service"
fi

# —— 单机：本机 Gate 用一次性凭证自动配对；中心只存摘要，配对成功后两边都删除 ——
if [ "$role" = standalone ] && [ ! -f "$etc/gate/agent.json" ]; then
 token=$(od -An -N32 -tx1 /dev/urandom | tr -d ' \n')
 printf '%s' "$token" > "$etc/gate/local-pair.token"
 chmod 0600 "$etc/gate/local-pair.token"
 printf '%s' "$token" | sha256sum | cut -d' ' -f1 > "$etc/center/local-pair.sha256"
 unset token
 chown weir-center:weir-center "$etc/center/local-pair.sha256"
 chmod 0600 "$etc/center/local-pair.sha256"
 cat > "$units_dir/weir-local-pair.service" <<UNIT
[Unit]
Description=Weir standalone local Gate pairing
After=network-online.target weir-center.service weir-helper.service
ConditionPathExists=/etc/weir/gate/local-pair.token
ConditionPathExists=!/etc/weir/gate/agent.json
[Service]
Type=oneshot
TimeoutStartSec=infinity
ExecStart=/opt/weir/current/weir gate local-pair --config /etc/weir/gate/agent.json --token-file /etc/weir/gate/local-pair.token --center http://127.0.0.1:$port
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
  if "$base/current/weir" center --config "$etc/center/config.json" --health-check >/dev/null 2>&1; then healthy=true; break; fi
  sleep 1
 done
 [ "$healthy" = true ] || rollback
fi
if [ "$has_gate" = true ]; then
 ready=false
 for attempt in 1 2 3 4 5; do
  if "$base/current/weir" gate helper-health >/dev/null 2>&1; then ready=true; break; fi
  sleep 1
 done
 [ "$ready" = true ] || rollback
 if [ "$role" = gate ] && [ -f "$etc/gate/agent.json" ]; then systemctl enable --now weir-gate.service >/dev/null; fi
 if [ -f "$units_dir/weir-local-pair.service" ] && [ ! -f "$etc/gate/agent.json" ]; then
  systemctl enable weir-local-pair.service >/dev/null
  systemctl start --no-block weir-local-pair.service
 fi
fi

echo "Installed Weir $version ($role)."
if [ "$has_center" = true ]; then
 if grep -Eq '"installed"[[:space:]]*:[[:space:]]*true' "$etc/center/config.json"; then
  echo 'Center is initialized; configuration and data were kept.'
 else
  echo 'Center is not initialized yet. Open https://<your domain> and enter this token in the setup wizard:'
  setpriv --reuid=weir-center --regid=weir-center --init-groups \
   "$base/current/weir" center --config "$etc/center/config.json" --token || true
  echo '  Expired? Run weir and choose 6; it shows a fresh token. Terminal setup: weir center --setup'
 fi
fi
if [ "$role" = standalone ]; then echo 'The local Gate pairs itself after the center is initialized.'; fi
if [ "$role" = gate ] && [ ! -f "$etc/gate/agent.json" ]; then echo 'Pair this Gate from the center (Add Gate) to finish.'; fi
