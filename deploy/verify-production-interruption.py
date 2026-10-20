"""Controlled hard stop: leave only a disabled, prefixed page for recovery."""
import os
import signal
from live_api import API

api = API().admin()
prefix = os.environ['MIGATE_FIXTURE_PREFIX']
api.request('POST', '/api/v1/admin/resources/public-page', {'data': {
    'name': prefix + 'hard-stop', 'title': 'Disposable recovery probe',
    'slug': prefix + 'hard-stop', 'enabled': False}})
print('Owned disabled fixture persisted; simulating runner termination', flush=True)
# The runner injects this PID; never enumerate or kill unrelated operator jobs.
parent = int(os.environ['MIGATE_RUNNER_PID'])
if parent != os.getppid():
    raise RuntimeError('Recovery probe is not a direct runner child')
os.kill(parent, signal.SIGTERM)
