#!/bin/sh
# 从 GitHub Release 安装，或按本机清单卸载。目标机器不编译源码。
set -eu
umask 077

repo=${MIGATE_GITHUB_REPO:-shusfun/MiSub}
token=${MIGATE_GITHUB_TOKEN:-}
curl_bin=${MIGATE_CURL:-curl}
tty_device=${MIGATE_DEV_TTY:-/dev/tty}
allow_non_root=${MIGATE_ALLOW_NON_ROOT:-}
role=""
listen=""
public_url=""
version=""
command=install
yes_flag=0
with_cores=0
purge=0

die() { printf '%s\n' "$1" >&2; exit 1; }

interactive() {
  [ "${MIGATE_NONINTERACTIVE:-}" = 1 ] && return 1
  [ -r "$tty_device" ] && [ -w "$tty_device" ]
}

if [ "$tty_device" != /dev/tty ] && [ -r "$tty_device" ]; then
  exec 3< "$tty_device"
fi
ask() {
  printf '%s' "$1" >&2
  if [ "$tty_device" = /dev/tty ]; then
    printf '%s' "$1" > /dev/tty
    IFS= read -r reply < /dev/tty || reply=""
  else
    IFS= read -r reply <&3 || reply=""
  fi
  reply=$(printf '%s' "$reply" | tr -d '\r')
  printf '%s' "$reply"
}

valid_repo() {
  printf '%s' "$1" | grep -Eq '^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$'
}

valid_version() {
  printf '%s' "$1" | grep -Eq '^[0-9]\.(0|[1-9][0-9]?)\.(0|[1-9][0-9]{0,2})$'
}

allowed_host() {
  case "$1" in
    api.github.com|github.com|release-assets.githubusercontent.com|objects.githubusercontent.com|github-releases.githubusercontent.com) return 0 ;;
    *) return 1 ;;
  esac
}

check_url() {
  case "$1" in
    https://*) ;;
    *) return 1 ;;
  esac
  case "$1" in
    *\?*|*'@'*|*' '*|*'#'*) return 1 ;;
  esac
  host=$(printf '%s' "$1" | sed -n 's#https://\([^/]*\).*#\1#p')
  allowed_host "$host"
}

require_root() {
  if [ "$(id -u)" != 0 ] && [ -z "$allow_non_root" ]; then
    die "需要 root"
  fi
}

while [ $# -gt 0 ]; do
  case "$1" in
    install) command=install ;;
    uninstall) command=uninstall ;;
    control|gate) role=$1 ;;
    -y|--yes) yes_flag=1 ;;
    --with-cores) with_cores=1 ;;
    --purge) purge=1 ;;
    --version)
      shift
      version=${1:-}
      ;;
    --version=*) version=${1#--version=} ;;
    *)
      if [ -z "$listen" ]; then listen=$1
      elif [ -z "$public_url" ]; then public_url=$1
      else die "无法识别的参数"
      fi
      ;;
  esac
  shift
done

valid_repo "$repo" || die "仓库名称无效"
if [ -n "$version" ]; then
  valid_version "$version" || die "版本号无效"
fi
if [ "$with_cores" = 1 ] && [ "$purge" = 1 ]; then
  die "不能同时清除数据和只移除内核"
fi
case "$token" in
  *[!A-Za-z0-9_.:/=+-]*) die "令牌包含不支持的字符" ;;
esac

if interactive && [ -z "$role" ]; then
  choice=$(ask "安装角色 [1] 中心 [2] Gate，直接回车选择中心: ")
  case "$choice" in
    ""|1) role=control ;;
    2) role=gate ;;
    *) die "角色无效" ;;
  esac
fi
if [ "$command" = install ] && interactive; then
  if [ "$role" = control ]; then
    if [ -z "$listen" ]; then
      listen=$(ask "中心监听地址 [127.0.0.1:19810]: ")
      listen=${listen:-127.0.0.1:19810}
    fi
    if [ -z "$public_url" ]; then
      public_url=$(ask "公共 URL [http://127.0.0.1:19810]: ")
      public_url=${public_url:-http://127.0.0.1:19810}
    fi
  fi
fi
role=${role:-control}

if [ "$command" = uninstall ]; then
  if ! interactive && [ "$yes_flag" != 1 ]; then
    die "没有终端。卸载请追加 -y"
  fi
  if interactive && [ "$yes_flag" != 1 ]; then
    answer=$(ask "确认卸载 ${role}？[y/N]: ")
    case "$answer" in
      y|Y) ;;
      *) die "卸载已取消" ;;
    esac
    if [ "$with_cores" != 1 ] && [ "$purge" != 1 ]; then
      scope=$(ask "卸载范围 [1] 保留数据和内核 [2] 移除托管内核 [3] 备份后清除本项目数据，直接回车保留: ")
      case "$scope" in
        ""|1) ;;
        2) with_cores=1 ;;
        3) purge=1 ;;
        *) die "卸载范围无效" ;;
      esac
    fi
  fi
  require_root
  manifest=${MIGATE_MANIFEST:-/etc/migate-$role/install-manifest.json}
  manager=${MIGATE_MANAGE:-/opt/migate-$role/migate-manage}
  [ -f "$manifest" ] || die "没有安装清单"
  [ -x "$manager" ] || die "没有本机卸载程序"
  confirm="$role:uninstall"
  set -- uninstall --manifest "$manifest" --confirm "$confirm"
  if [ "$with_cores" = 1 ]; then
    confirm="$role:uninstall:with-cores"
    set -- uninstall --manifest "$manifest" --confirm "$confirm" --with-cores
  fi
  if [ "$purge" = 1 ]; then
    confirm="$role:uninstall:purge"
    if [ "$role" = control ]; then
      command -v pg_dump >/dev/null 2>&1 || die "清除中心数据需要 pg_dump"
      set -- uninstall --manifest "$manifest" --confirm "$confirm" --purge --backup-dir /var/backups/migate --pg-dump pg_dump
    else
      set -- uninstall --manifest "$manifest" --confirm "$confirm" --purge --backup-dir /var/backups/migate
    fi
  fi
  exec "$manager" "$@"
fi

require_root
workdir=$(mktemp -d "${TMPDIR:-/tmp}/migate-install-XXXXXX")
trap 'rm -rf "$workdir"' EXIT
token_config=$workdir/curl-config
if [ -n "$token" ]; then
  printf 'header = "Authorization: Bearer %s"\n' "$token" > "$token_config"
  chmod 0600 "$token_config"
fi

fetch_url() {
  dest=$1
  url=$2
  hops=0
  while [ "$hops" -lt 4 ]; do
    check_url "$url" || die "下载地址被拒绝"
    header=$workdir/headers
    if [ -n "$token" ]; then
      code=$("$curl_bin" -sS --proto '=https' --config "$token_config" --max-redirs 0 -D "$header" -o "$dest" -w '%{http_code}' "$url") || die "下载失败"
    else
      code=$("$curl_bin" -sS --proto '=https' --max-redirs 0 -D "$header" -o "$dest" -w '%{http_code}' "$url") || die "下载失败"
    fi
    case "$code" in
      200) return 0 ;;
      301|302|303|307|308)
        url=$(grep -i '^location:' "$header" | tail -n 1 | awk '{ print $2 }' | tr -d '\r')
        ;;
      *) die "下载失败" ;;
    esac
    hops=$((hops + 1))
  done
  die "下载重定向过多"
}

if [ -z "$version" ]; then
  fetch_url "$workdir/release.json" "https://api.github.com/repos/$repo/releases/latest"
  tag=$(sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$workdir/release.json" | head -n 1)
  version=${tag#v}
  valid_version "$version" || die "发布版本无效"
else
  tag=v$version
fi

archive_name=migate-$version-linux-amd64.tar.gz
base=https://github.com/$repo/releases/download/$tag
fetch_url "$workdir/checksums.txt" "$base/checksums.txt"
fetch_url "$workdir/$archive_name" "$base/$archive_name"
expected=$(awk -v name="$archive_name" '$2==name { print $1 }' "$workdir/checksums.txt")
actual=$(sha256sum "$workdir/$archive_name" | awk '{ print $1 }')
[ -n "$expected" ] && [ "$expected" = "$actual" ] || die "压缩包校验失败"
tar -xzf "$workdir/$archive_name" -C "$workdir"
(
  cd "$workdir"
  sha256sum -c SHA256SUMS >/dev/null
)
if [ "$role" = control ]; then
  listen=${listen:-127.0.0.1:19810}
  public_url=${public_url:-http://127.0.0.1:19810}
  sh "$workdir/install.sh" control "$listen" "$public_url"
else
  sh "$workdir/install.sh" gate
fi
