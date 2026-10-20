"""Sequential real lock/recovery exercise; no service restart or user changes."""
import json
from pathlib import Path
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parent.parent
def run(*args):
    return subprocess.run([sys.executable, '-X', 'utf8', 'deploy/production-run.py', *args], cwd=ROOT)

stopped = run('--label', 'hard-stop-recovery-probe', '--scope', 'guestbook', '--',
              sys.executable, '-X', 'utf8', 'deploy/verify-production-interruption.py')
if stopped.returncode == 0:
    raise RuntimeError('Runner was not interrupted')
blocked = run('--label', 'must-refuse-pending-cleanup', '--', sys.executable, 'deploy/verify-production-lock.py')
if blocked.returncode == 0:
    raise RuntimeError('Pending cleanup failed to block new production work')
restored = run('--label', 'recover-hard-stop', '--recover')
if restored.returncode:
    raise RuntimeError('Recovery incomplete; retain the private journal for operator review')
released = run('--label', 'verify-post-recovery-lock', '--', sys.executable, 'deploy/verify-production-lock.py')
if released.returncode:
    raise RuntimeError('Lock not usable after recovery')
Path('docs/evidence/production-interruption.json').write_text(json.dumps({
    'date': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()), 'result': 'passed',
    'checks': ['hard-stopped only this runner after a disabled owned page was persisted',
               'new production operation refused while remote marker remained',
               'recovery reacquired shared lock, removed owned page and verified original baseline',
               'later production run acquired lock; independent second acquisition refused'],
    'boundary': 'No service restart, user/grant/kernel or original page modification'}, indent=2), encoding='utf-8')
print('Hard interruption, pending-run refusal and baseline recovery passed')
