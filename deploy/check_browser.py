"""Measure real loopback browser protocols with a trusted short-lived certificate."""
import argparse
import json
import os
import pathlib
import subprocess
import time

from scenario_run import ScenarioRun


def run_browser(run, args, root, launch=subprocess.Popen):
    binary = run.path / ('browser-fixture.exe' if os.name == 'nt' else 'browser-fixture')
    revision = run.before['commit'] + '-' + run.before['worktree_sha256'][:12]
    run.run('fixture-build', ['go', 'build', '-ldflags', '-X github.com/shusfun/weir/internal/buildinfo.Revision=' + revision,
                             '-o', str(binary), './internal/browsertest/cmd/localfixture'], check=True)
    run.artifact('browser-fixture', binary, 'fixture-build')
    ready = run.path / 'listener.json'
    with (run.path / 'fixture.log').open('w', encoding='utf-8') as log:
        process = launch([str(binary), '-cert', str(args.cert), '-key', str(args.key), '-origin', args.base,
                          '-lifetime', '2m', '-stdin-close', '-ready', str(ready)], cwd=root,
                         stdin=subprocess.PIPE, stdout=log, stderr=log)
        try:
            deadline = time.monotonic() + 20
            while not ready.exists():
                if process.poll() is not None:
                    raise RuntimeError('Fixture exited before readiness; inspect fixture.log')
                if time.monotonic() >= deadline:
                    raise TimeoutError('Fixture did not publish listener metadata in 20 seconds')
                time.sleep(0.1)
            metadata = json.loads(ready.read_text(encoding='utf-8'))
            run.require('fixture revision', metadata['build_revision'] == revision,
                        'Rebuild the fixture from the current source state')
            config = {'control': metadata['control_url'], 'base': args.base, 'output': str(run.path / 'browser.json'), 'name': 'weir-browser-' + run.id}
            if args.space is not None:
                config['space'] = args.space
            script = run.path / 'browser.mjs'
            script.write_text('const {measure} = await import(' + json.dumps((root / 'deploy/check_browser.mjs').as_uri()) + ');\nawait measure(' + json.dumps(config) + ');\n', encoding='utf-8')
            run.run('browser-protocols', [args.ego, 'nodejs', '-e', 'await import(' + json.dumps(script.as_uri()) + ')'],
                    timeout=90, check=True, recovery='Check certificate SAN/trust, short-lived P-256 leaf, console and fixture logs, then rerun check_browser.py')
        finally:
            # The child was created here and exits cooperatively on stdin EOF.
            process.stdin.close()
            process.wait(timeout=15)
    run.artifact('fixture-log', run.path / 'fixture.log')
    run.artifact('browser-results', run.path / 'browser.json')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--cert', type=pathlib.Path, required=True)
    parser.add_argument('--key', type=pathlib.Path, required=True)
    parser.add_argument('--base', default='http://127.0.0.1:22340')
    parser.add_argument('--ego', default='ego-browser')
    parser.add_argument('--space', type=int, help='Existing task-owned ego space; leave it open for subsequent work')
    args = parser.parse_args()
    args.cert, args.key = args.cert.resolve(), args.key.resolve()
    root = pathlib.Path(__file__).resolve().parent.parent
    with ScenarioRun(root, root / 'dist/checks') as run:
        run_browser(run, args, root)
    return run.exit_code


if __name__ == '__main__':
    raise SystemExit(main())
