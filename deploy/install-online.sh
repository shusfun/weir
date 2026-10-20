#!/bin/bash
# MiGate 安装脚本，用法对齐 Sub2API：
#   curl -sSL https://raw.githubusercontent.com/shusfun/MiSub/main/deploy/install-online.sh | bash
set -euo pipefail

if [[ -z ${BASH_VERSION:-} ]]; then
  echo "Error: This installer must be run with Bash 4.0 or later." >&2
  exit 1
fi
if [[ ${BASH_VERSION%%.*} -lt 4 ]]; then
  echo "Error: Bash 4.0 or later is required. Current version: $BASH_VERSION" >&2
  exit 1
fi

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

GITHUB_REPO=${MIGATE_GITHUB_REPO:-shusfun/MiSub}
CURL_BIN=${MIGATE_CURL:-curl}
TTY_DEVICE=${MIGATE_DEV_TTY:-/dev/tty}
LANG_CHOICE=zh
LISTEN_HOST=127.0.0.1
LISTEN_PORT=19810
PUBLIC_URL=http://127.0.0.1:19810
ROLE=""
TARGET_VERSION=""
FORCE_YES=false
PURGE=false
WITH_CORES=false
WORKDIR=""
trap '[[ -n ${WORKDIR:-} && -d ${WORKDIR:-} ]] && rm -rf "$WORKDIR"' EXIT

declare -A MSG=(
  [zh.info]="信息" [zh.success]="成功" [zh.warning]="警告" [zh.error]="错误"
  [zh.select_lang]="请选择语言 / Select language"
  [zh.lang_zh]="中文" [zh.lang_en]="English" [zh.enter_choice]="请输入选项"
  [zh.title]="MiGate 安装"
  [zh.role_title]="选择安装角色"
  [zh.role_control]="中心" [zh.role_gate]="Gate"
  [zh.host_prompt]="监听地址" [zh.port_prompt]="监听端口" [zh.invalid_port]="端口必须是 1-65535"
  [zh.public_prompt]="公共 URL" [zh.public_https]="非本机监听必须填写 HTTPS 公共 URL"
  [zh.summary]="监听地址"
  [zh.downloading]="正在下载" [zh.verifying]="正在校验安装包" [zh.installing]="正在安装"
  [zh.done]="安装完成" [zh.visit]="访问地址"
  [zh.not_installed]="尚未安装" [zh.upgrading]="正在升级" [zh.current]="当前版本"
  [zh.rollback]="正在回退" [zh.same]="已经是这个版本"
  [zh.uninstall_confirm]="这将移除 MiGate 程序和服务。"
  [zh.are_you_sure]="确认继续？[y/N] "
  [zh.cancelled]="卸载已取消"
  [zh.purge_prompt]="同时清除配置和数据？默认保留 [y/N] "
  [zh.kept]="已保留配置和数据"
  [zh.purged]="已备份并清除本项目数据"
  [zh.need_yes]="没有终端。卸载请追加 -y"
  [zh.usage]="用法"
  [zh.cmd_install]="安装最新正式版"
  [zh.cmd_upgrade]="升级到最新正式版"
  [zh.cmd_rollback]="回退到指定版本"
  [zh.cmd_versions]="列出 GitHub Release"
  [zh.cmd_uninstall]="卸载程序，默认保留数据"
  [en.info]="info" [en.success]="success" [en.warning]="warning" [en.error]="error"
  [en.select_lang]="请选择语言 / Select language"
  [en.lang_zh]="中文" [en.lang_en]="English" [en.enter_choice]="Enter choice"
  [en.title]="MiGate installer"
  [en.role_title]="Choose role"
  [en.role_control]="Control" [en.role_gate]="Gate"
  [en.host_prompt]="Listen address" [en.port_prompt]="Listen port" [en.invalid_port]="Port must be 1-65535"
  [en.public_prompt]="Public URL" [en.public_https]="A non-local listener requires an HTTPS public URL"
  [en.summary]="Listen address"
  [en.downloading]="Downloading" [en.verifying]="Verifying archive" [en.installing]="Installing"
  [en.done]="Installation complete" [en.visit]="Open"
  [en.not_installed]="Not installed" [en.upgrading]="Upgrading" [en.current]="Current version"
  [en.rollback]="Rolling back" [en.same]="Already at this version"
  [en.uninstall_confirm]="This removes the MiGate program and service."
  [en.are_you_sure]="Continue? [y/N] "
  [en.cancelled]="Uninstall cancelled"
  [en.purge_prompt]="Also remove configuration and data? Default is keep [y/N] "
  [en.kept]="Configuration and data kept"
  [en.purged]="Project data backed up and removed"
  [en.need_yes]="Non-interactive mode detected. Add -y to confirm uninstall."
  [en.usage]="Usage"
  [en.cmd_install]="Install the latest stable release"
  [en.cmd_upgrade]="Upgrade to the latest stable release"
  [en.cmd_rollback]="Roll back to a version"
  [en.cmd_versions]="List GitHub releases"
  [en.cmd_uninstall]="Uninstall the program and keep data by default"
)

msg() { printf '%s' "${MSG[$LANG_CHOICE.$1]}"; }
print_info() { echo -e "${BLUE}[$(msg info)]${NC} $1"; }
print_success() { echo -e "${GREEN}[$(msg success)]${NC} $1"; }
print_warning() { echo -e "${YELLOW}[$(msg warning)]${NC} $1"; }
print_error() { echo -e "${RED}[$(msg error)]${NC} $1" >&2; }

if [[ -n ${MIGATE_DEV_TTY:-} && -r $TTY_DEVICE ]]; then
  exec 3<"$TTY_DEVICE"
fi

is_interactive() {
  [[ ${MIGATE_NONINTERACTIVE:-} == 1 ]] && return 1
  [[ -e $TTY_DEVICE && -r $TTY_DEVICE && -w $TTY_DEVICE ]]
}

read_tty() {
  if [[ -n ${MIGATE_DEV_TTY:-} ]]; then
    IFS= read -r REPLY <&3 || REPLY=""
  else
    IFS= read -r REPLY < /dev/tty || REPLY=""
  fi
  REPLY=${REPLY//$'\r'/}
}

prompt_tty() {
  if [[ -n ${MIGATE_DEV_TTY:-} ]]; then
    printf '%s' "$1" >&2
  else
    printf '%s' "$1" >/dev/tty
  fi
  read_tty
}

select_language() {
  if ! is_interactive; then
    LANG_CHOICE=zh
    return
  fi
  echo ""
  echo -e "${CYAN}=============================================="
  echo "  $(msg select_lang)"
  echo -e "==============================================${NC}"
  echo "  1) $(msg lang_zh) (默认/default)"
  echo "  2) $(msg lang_en)"
  echo ""
  prompt_tty "$(msg enter_choice) [1]: "
  case "$REPLY" in
    2|en|EN|english|English) LANG_CHOICE=en ;;
    *) LANG_CHOICE=zh ;;
  esac
}

valid_version() {
  local raw=${1#v}
  [[ $raw =~ ^[0-9]\.(0|[1-9][0-9]?)\.(0|[1-9][0-9]{0,2})$ ]]
}

valid_port() { [[ $1 =~ ^[0-9]+$ ]] && (( 10#$1 >= 1 && 10#$1 <= 65535 )); }

check_root() {
  if [[ $(id -u) -ne 0 && -z ${MIGATE_ALLOW_NON_ROOT:-} ]]; then
    print_error "root required"
    exit 1
  fi
}

ask_role() {
  [[ -n $ROLE ]] && return
  if ! is_interactive; then
    ROLE=control
    return
  fi
  echo ""
  echo -e "${CYAN}  $(msg role_title)${NC}"
  echo "  1) $(msg role_control) (默认/default)"
  echo "  2) $(msg role_gate)"
  echo ""
  prompt_tty "$(msg enter_choice) [1]: "
  case "$REPLY" in
    2|gate|Gate) ROLE=gate ;;
    *) ROLE=control ;;
  esac
}

configure_control() {
  [[ $ROLE == gate ]] && return
  if ! is_interactive; then
    if [[ $LISTEN_HOST == 127.0.0.1 || $LISTEN_HOST == localhost ]]; then
      [[ $PUBLIC_URL == http://127.0.0.1:19810 ]] && PUBLIC_URL=http://127.0.0.1:${LISTEN_PORT}
    fi
    print_info "$(msg summary): ${LISTEN_HOST}:${LISTEN_PORT}"
    return
  fi
  echo ""
  prompt_tty "$(msg host_prompt) [${LISTEN_HOST}]: "
  [[ -n $REPLY ]] && LISTEN_HOST=$REPLY
  while true; do
    prompt_tty "$(msg port_prompt) [${LISTEN_PORT}]: "
    if [[ -z $REPLY ]]; then
      break
    elif valid_port "$REPLY"; then
      LISTEN_PORT=$REPLY
      break
    else
      print_error "$(msg invalid_port)"
    fi
  done
  if [[ $LISTEN_HOST != 127.0.0.1 && $LISTEN_HOST != localhost ]]; then
    while true; do
      prompt_tty "$(msg public_prompt): "
      if [[ $REPLY == https://* ]]; then
        PUBLIC_URL=$REPLY
        break
      fi
      print_error "$(msg public_https)"
    done
  elif [[ $PUBLIC_URL == http://127.0.0.1:19810 || $PUBLIC_URL == http://127.0.0.1:${LISTEN_PORT} ]]; then
    PUBLIC_URL=http://127.0.0.1:${LISTEN_PORT}
  fi
  print_info "$(msg summary): ${LISTEN_HOST}:${LISTEN_PORT}"
}

fetch_url() {
  local dest=$1 url=$2 hops=0 code header
  while (( hops < 4 )); do
    case "$url" in
      https://*) ;;
      *) print_error "下载地址被拒绝"; exit 1 ;;
    esac
    case "$url" in
      *\?*|*'@'*|*' '*|*'#'*) print_error "下载地址被拒绝"; exit 1 ;;
    esac
    local host=${url#https://}
    host=${host%%/*}
    case "$host" in
      api.github.com|github.com|release-assets.githubusercontent.com|objects.githubusercontent.com|github-releases.githubusercontent.com) ;;
      *) print_error "下载地址被拒绝"; exit 1 ;;
    esac
    print_info "$(msg downloading) $url"
    header=$WORKDIR/headers
    local curl_config=()
    if [[ -n ${MIGATE_GITHUB_TOKEN:-} ]]; then
      printf 'header = "Authorization: Bearer %s"\n' "$MIGATE_GITHUB_TOKEN" > "$WORKDIR/curl-config"
      chmod 600 "$WORKDIR/curl-config"
      curl_config=(--config "$WORKDIR/curl-config")
    fi
    code=$("$CURL_BIN" --silent --show-error --connect-timeout 20 --proto '=https' "${curl_config[@]}" -D "$header" -o "$dest" -w '%{http_code}' "$url") || { print_error "下载失败"; exit 1; }
    code=${code//$'\n'/}
    case "$code" in
      200) return 0 ;;
      301|302|303|307|308)
        url=$(grep -i '^location:' "$header" | tail -n 1 | awk '{print $2}' | tr -d '\r')
        [[ -n $url ]] || { print_error "下载失败"; exit 1; }
        ;;
      *) print_error "下载失败"; exit 1 ;;
    esac
    hops=$((hops + 1))
  done
  print_error "下载重定向过多"
  exit 1
}

resolve_version() {
  if [[ -n $TARGET_VERSION ]]; then
    valid_version "$TARGET_VERSION" || { print_error "版本号无效"; exit 1; }
    TARGET_VERSION=${TARGET_VERSION#v}
    return
  fi
  fetch_url "$WORKDIR/release.json" "https://api.github.com/repos/${GITHUB_REPO}/releases/latest"
  local tag
  tag=$(sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$WORKDIR/release.json" | head -n 1)
  TARGET_VERSION=${tag#v}
  valid_version "$TARGET_VERSION" || { print_error "发布版本无效"; exit 1; }
}

download_package() {
  WORKDIR=$(mktemp -d "${TMPDIR:-/tmp}/migate-install-XXXXXX")
  resolve_version
  local name=migate-${TARGET_VERSION}-linux-amd64.tar.gz
  local base=https://github.com/${GITHUB_REPO}/releases/download/v${TARGET_VERSION}
  fetch_url "$WORKDIR/checksums.txt" "$base/checksums.txt"
  fetch_url "$WORKDIR/$name" "$base/$name"
  print_info "$(msg verifying)"
  local expected actual
  expected=$(awk -v archive="$name" '$2==archive {print $1}' "$WORKDIR/checksums.txt")
  actual=$(sha256sum "$WORKDIR/$name" | awk '{print $1}')
  if [[ -z $expected || $expected != "$actual" ]]; then
    print_error "压缩包校验失败"
    exit 1
  fi
  tar -xzf "$WORKDIR/$name" -C "$WORKDIR"
  ( cd "$WORKDIR" && sha256sum -c SHA256SUMS >/dev/null )
}

install_package() {
  download_package
  print_info "$(msg installing) v${TARGET_VERSION}"
  if [[ -n ${MIGATE_INSTALL_RECORD:-} ]]; then
    printf '%s %s:%s %s\n' "$ROLE" "$LISTEN_HOST" "$LISTEN_PORT" "$PUBLIC_URL" >> "$MIGATE_INSTALL_RECORD"
    return
  fi
  local package=$WORKDIR
  if [[ $ROLE == control ]]; then
    sh "$package/install.sh" control "${LISTEN_HOST}:${LISTEN_PORT}" "$PUBLIC_URL"
  else
    sh "$package/install.sh" gate
  fi
  rm -rf "$WORKDIR"
  WORKDIR=""
  echo ""
  echo "=============================================="
  print_success "$(msg done)"
  echo "=============================================="
  if [[ $ROLE == control ]]; then
    echo "  $(msg visit): ${PUBLIC_URL}"
    echo ""
    echo "  migate-control --version"
    echo "  migate-control --setup"
    echo "  systemctl status migate-control"
    echo "  journalctl -u migate-control -f"
    echo "  systemctl restart migate-control"
  else
    echo "  migate-gate version"
    echo "  migate-gate pair --center <HTTPS 地址> --name <名称>"
    echo "  systemctl status migate-gate"
    echo "  journalctl -u migate-gate -f"
    echo "  systemctl restart migate-gate"
  fi
  echo ""
}

role_installed() {
  local manifest=${MIGATE_MANIFEST:-/etc/migate-$1/install-manifest.json}
  local manager=${MIGATE_MANAGE:-/opt/migate-$1/migate-manage}
  [[ -f $manifest && -x $manager ]]
}

ensure_role() {
  if [[ -n $ROLE ]]; then
    return
  fi
  if role_installed control && ! role_installed gate; then ROLE=control; return; fi
  if role_installed gate && ! role_installed control; then ROLE=gate; return; fi
  ask_role
}

fresh_install() {
  check_root
  ask_role
  configure_control
  install_package
}

change_version() {
  check_root
  ensure_role
  role_installed "$ROLE" || { print_error "$(msg not_installed)"; exit 1; }
  print_info "$(msg upgrading)"
  install_package
}

clear_install_lock() {
  local config=/etc/migate-$ROLE/config.json
  [[ -f $config ]] || return 0
  python3 - "$config" <<'PY'
import json, sys
path = sys.argv[1]
with open(path, encoding="utf-8") as handle:
    data = json.load(handle)
data["installed"] = False
data["setup_hash"] = ""
with open(path, "w", encoding="utf-8") as handle:
    json.dump(data, handle)
PY
}

run_uninstall() {
  check_root
  ensure_role
  print_warning "$(msg uninstall_confirm)"
  if ! is_interactive; then
    if [[ $FORCE_YES != true ]]; then
      print_error "$(msg need_yes)"
      exit 1
    fi
  else
    echo -n "$(msg are_you_sure)"
    read_tty
    echo ""
    [[ $REPLY =~ ^[Yy]$ ]] || { print_info "$(msg cancelled)"; exit 0; }
    if [[ $PURGE != true && $WITH_CORES != true ]]; then
      echo -n "$(msg purge_prompt)"
      read_tty
      echo ""
      [[ $REPLY =~ ^[Yy]$ ]] && PURGE=true
    fi
  fi
  local manifest=${MIGATE_MANIFEST:-/etc/migate-$ROLE/install-manifest.json}
  local manager=${MIGATE_MANAGE:-/opt/migate-$ROLE/migate-manage}
  [[ -f $manifest ]] || { print_error "$(msg not_installed)"; exit 1; }
  [[ -x $manager ]] || { print_error "$(msg not_installed)"; exit 1; }
  local confirm="$ROLE:uninstall"
  local -a args=(uninstall --manifest "$manifest" --confirm "$confirm")
  if [[ $WITH_CORES == true && $PURGE == true ]]; then
    print_error "不能同时清除数据和只移除内核"
    exit 1
  fi
  if [[ $WITH_CORES == true ]]; then
    confirm="$ROLE:uninstall:with-cores"
    args=(uninstall --manifest "$manifest" --confirm "$confirm" --with-cores)
  fi
  if [[ $PURGE == true ]]; then
    confirm="$ROLE:uninstall:purge"
    if [[ $ROLE == control ]]; then
      command -v pg_dump >/dev/null 2>&1 || { print_error "清除中心数据需要 pg_dump"; exit 1; }
      args=(uninstall --manifest "$manifest" --confirm "$confirm" --purge --backup-dir /var/backups/migate --pg-dump pg_dump)
    else
      args=(uninstall --manifest "$manifest" --confirm "$confirm" --purge --backup-dir /var/backups/migate)
    fi
    print_success "$(msg purged)"
  else
    print_warning "$(msg kept): /etc/migate-$ROLE /var/lib/migate-$ROLE"
  fi
  "$manager" "${args[@]}"
  if [[ $PURGE != true ]]; then
    clear_install_lock
  fi
}

list_versions() {
  WORKDIR=$(mktemp -d "${TMPDIR:-/tmp}/migate-install-XXXXXX")
  fetch_url "$WORKDIR/releases.json" "https://api.github.com/repos/${GITHUB_REPO}/releases?per_page=20"
  grep -o '"tag_name"[[:space:]]*:[[:space:]]*"[^"]*"' "$WORKDIR/releases.json" | sed -n 's/.*"\([^"]*\)"$/\1/p'
  rm -rf "$WORKDIR"
}

show_help() {
  echo "$(msg usage): $0 [command] [options]"
  echo ""
  echo "Commands:"
  echo "  install              $(msg cmd_install)"
  echo "  upgrade              $(msg cmd_upgrade)"
  echo "  rollback <version>   $(msg cmd_rollback)"
  echo "  list-versions        $(msg cmd_versions)"
  echo "  uninstall            $(msg cmd_uninstall)"
  echo ""
  echo "Options:"
  echo "  -v, --version <ver>  指定版本，例如 v0.1.1"
  echo "  -y, --yes            没有终端时确认卸载"
  echo "  --purge              卸载时清除本项目数据"
  echo "  --with-cores         卸载时移除托管内核"
  echo ""
  echo "Examples:"
  echo "  curl -sSL https://raw.githubusercontent.com/shusfun/MiSub/main/deploy/install-online.sh | bash"
  echo "  $0 install -v v0.1.1"
  echo "  $0 upgrade"
  echo "  $0 rollback v0.1.1"
  echo "  $0 uninstall"
}

main() {
  local positional=()
  while [[ $# -gt 0 ]]; do
    case "$1" in
      -y|--yes) FORCE_YES=true; shift ;;
      --purge) PURGE=true; shift ;;
      --with-cores) WITH_CORES=true; shift ;;
      -v|--version)
        TARGET_VERSION=${2:-}
        [[ -n $TARGET_VERSION ]] || { print_error "版本号无效"; exit 1; }
        shift 2 ;;
      --version=*) TARGET_VERSION=${1#*=}; shift ;;
      *) positional+=("$1"); shift ;;
    esac
  done
  set -- "${positional[@]}"
  select_language
  echo ""
  echo "=============================================="
  echo "       $(msg title)"
  echo "=============================================="
  echo ""
  local action=${1:-}
  local arg
  for arg in "$@"; do
    case "$arg" in
      control|gate) ROLE=$arg ;;
      install|upgrade|update|rollback|list-versions|versions|uninstall|remove|--help|-h|help) action=$arg ;;
      http://*|https://*) PUBLIC_URL=$arg ;;
      *:*) LISTEN_HOST=${arg%%:*}; LISTEN_PORT=${arg##*:} ;;
      v[0-9]*|[0-9]*) TARGET_VERSION=$arg ;;
    esac
  done
  [[ $action == control || $action == gate ]] && action=install
  case "$action" in
    upgrade|update)
      print_info "$(msg upgrading)"
      change_version
      ;;
    install)
      if [[ -n $TARGET_VERSION ]] && role_installed "${ROLE:-control}"; then
        ROLE=${ROLE:-control}
        change_version
      else
        fresh_install
      fi
      ;;
    rollback)
      TARGET_VERSION=${TARGET_VERSION:-${3:-}}
      [[ -n $TARGET_VERSION ]] || { print_error "版本号无效"; show_help; exit 1; }
      print_info "$(msg rollback)"
      change_version
      ;;
    list-versions|versions)
      list_versions
      ;;
    uninstall|remove)
      run_uninstall
      ;;
    --help|-h|help)
      show_help
      ;;
    "")
      if [[ -n $TARGET_VERSION ]]; then
        fresh_install
      else
        fresh_install
      fi
      ;;
    *)
      print_error "无法识别的参数"
      show_help
      exit 1
      ;;
  esac
}

main "$@"
