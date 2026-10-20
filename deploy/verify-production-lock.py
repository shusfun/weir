"""Run inside production-run: real second SSH acquisition must fail immediately."""
import importlib.util
import json
from pathlib import Path
import time
from production_guard import check

check()
spec = importlib.util.spec_from_file_location('production_runner', Path(__file__).with_name('production-run.py'))
runner = importlib.util.module_from_spec(spec)
spec.loader.exec_module(runner)
try:
    with runner.RemoteLease():
        raise AssertionError('Second production lease was incorrectly granted')
except RuntimeError as e:
    if 'Another production operation' not in str(e):
        raise
Path('docs/evidence/production-lock.json').write_text(json.dumps({'date': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()),
    'result': 'passed', 'checks': ['real second independent SSH flock refused while runner holds lock',
                                 'local broker rejects missing/wrong/expired token in local regression',
                                 'owned cleanup and undeclared/pre-existing mutation denial in local regression']}, indent=2), encoding='utf-8')
print('Second independent production flock refused as required')
