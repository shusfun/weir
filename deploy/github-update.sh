#!/bin/sh
# 中心机器以 root 拉取已校验的 GitHub Release。校验失败不安装，也不留下 Gate 清单。
set -eu
umask 077

repo=${MIGATE_GITHUB_REPO:-shusfun/MiSub}
token_file=${MIGATE_GITHUB_TOKEN_FILE:-/etc/migate-control/github-update.token}
release_root=${MIGATE_GATE_RELEASE_DIR:-/var/lib/migate-control/gate-release}
status_file=${MIGATE_RELEASE_STATUS:-/var/lib/migate-control/release-status.json}
request_file=${MIGATE_RELEASE_REQUEST:-/var/lib/migate-control/release-request.json}
fetcher=${MIGATE_RELEASE_FETCH:-/usr/local/lib/migate/release_fetch.py}
python_bin=${MIGATE_PYTHON:-python3}
tag=${1:-}

case "$repo" in
  */*) ;;
  *) echo 'repository refused' >&2; exit 1;;
esac
case "$repo" in
  *[!A-Za-z0-9/_.-]*) echo 'repository refused' >&2; exit 1;;
esac

mkdir -p "$(dirname "$status_file")" "$(dirname "$release_root")"

if [ "$tag" = "--apply-request" ]; then
  tag=$($python_bin -c 'import json,sys; print(json.load(open(sys.argv[1],encoding="utf-8")).get("tag",""))' "$request_file")
  rm -f "$request_file"
fi

set +e
$python_bin "$fetcher" fetch "$repo" "$tag" "$token_file" "$release_root" "$status_file"
fetched=$?
set -e
if [ "$fetched" -ne 0 ]; then
  rm -rf "${release_root}.partial"
  exit "$fetched"
fi

if [ -n "${MIGATE_INSTALL_RECORD:-}" ]; then
  printf '%s\n' control >> "$MIGATE_INSTALL_RECORD"
  exit 0
fi

archive_dir=$(mktemp -d /var/tmp/migate-release-XXXXXX)
trap 'rm -rf "$archive_dir"' EXIT
tar -xzf "$release_root/package.tar.gz" -C "$archive_dir"
sh "$archive_dir/install.sh" control
