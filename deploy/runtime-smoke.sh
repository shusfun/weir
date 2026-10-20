#!/bin/sh
# Disposable container only. No database, public listener or installation.
set -eu
if command -v node >/dev/null 2>&1; then
 echo 'Runtime test requires an image without Node.js' >&2
 exit 1
fi
cd /release
sha256sum -c SHA256SUMS
echo 'Release checksums verified; Node.js is absent'
./migate-gate version
./migate-manage uninstall --help >/dev/null 2>&1
test_dir=$(mktemp -d)
trap 'kill "${control_pid:-}" 2>/dev/null || true; rm -rf "$test_dir"' EXIT
# Suppress the disposable initialization token in evidence output.
./migate-control --config "$test_dir/config.json" --init >/dev/null
test -s "$test_dir/setup-token"
./migate-control --config "$test_dir/config.json" >"$test_dir/control.log" 2>&1 &
control_pid=$!
ready=0
for attempt in 1 2 3 4 5; do
 if ./migate-control --config "$test_dir/config.json" --health-check >"$test_dir/health" 2>/dev/null; then
  ready=1
  break
 fi
 sleep 1
done
if [ "$ready" != 1 ]; then
 cat "$test_dir/control.log" >&2
 exit 1
fi
cat "$test_dir/health"
echo 'Standalone Linux control startup and health check passed (uninstalled mode)'
