"""Coordination-only probe: a silent/disconnected holder expires within 20 s."""
import importlib.util
import json
from pathlib import Path
import time

spec = importlib.util.spec_from_file_location('production_runner', Path(__file__).with_name('production-run.py'))
runner = importlib.util.module_from_spec(spec)
spec.loader.exec_module(runner)
lease = runner.RemoteLease().__enter__()
try:
    if lease.marker():
        raise RuntimeError('Recovery pending; do not run a lease expiry probe')
    # No foreground alive() calls for longer than the remote 20-second timeout.
    # This reproduces slow synchronous snapshot/recovery/capture phases.
    time.sleep(24)
    try:
        with runner.RemoteLease():
            raise AssertionError('Background heartbeat failed; second holder acquired the lock')
    except RuntimeError as e:
        if 'Another production operation' not in str(e):
            raise
    lease.require_alive()
    started = time.monotonic()
    # Intentionally skip graceful shutdown_write to reproduce the old failure.
    lease.channel.close()
    lease.client.close()
    time.sleep(22)
    with runner.RemoteLease():
        pass
    elapsed = time.monotonic() - started
finally:
    lease.__exit__(None, None, None)
Path('docs/evidence/production-lease-expiry.json').write_text(json.dumps({
    'date': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()), 'result': 'passed',
    'elapsed_seconds': round(elapsed, 2),
    'checks': ['background heartbeat retained exclusive ownership across 24 s without foreground checks',
               'silent/disconnected holder released before a new independent acquisition at 22 s',
               'no application service, inventory, user, grant or kernel changed'],
    'contract': '20-second heartbeat timeout; pending recovery marker remains a separate gate'}, indent=2), encoding='utf-8')
print('Silent/disconnected real SSH lease expired; independent reacquisition passed')
