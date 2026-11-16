import argparse
import io
import json
import pathlib
import subprocess
import tempfile
import unittest

from check_browser import run_browser
from scenario_run import ScenarioRun


class BrowserRunnerTests(unittest.TestCase):
    def scenario(self, browser_exit=0, startup_exit=None):
        commands = []
        closed = []
        class Input(io.BytesIO):
            def close(self):
                closed.append(True)
                super().close()
        class Child:
            stdin = Input()
            def poll(self):
                return startup_exit
            def wait(self, timeout):
                if not self.stdin.closed:
                    raise AssertionError('Fixture did not receive stdin EOF')
                return 0
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            def execute(command, **kwargs):
                commands.append(command)
                if '-o' in command:
                    pathlib.Path(command[command.index('-o') + 1]).write_bytes(b'binary')
                else:
                    (run.path / 'browser.json').write_text('{}', encoding='utf-8')
                return subprocess.CompletedProcess(command, browser_exit if 'nodejs' in command else 0, b'', b'')
            def launch(command, **kwargs):
                self.assertIn('-stdin-close', command)
                self.assertIn('-lifetime', command)
                if startup_exit is None:
                    pathlib.Path(command[command.index('-ready') + 1]).write_text(json.dumps({
                        'build_revision': 'commit-fingerprint', 'control_url': 'http://127.0.0.1:9999'}), encoding='utf-8')
                return Child()
            args = argparse.Namespace(cert=root/'server.pem', key=root/'server.key', base='http://127.0.0.1:22340', ego='ego-browser', space=69)
            with ScenarioRun(root, root/'runs', execute, snapshot=lambda: {'commit': 'commit', 'worktree_sha256': 'fingerprint'}) as run:
                if startup_exit is not None:
                    with self.assertRaisesRegex(RuntimeError, 'before readiness'):
                        run_browser(run, args, root, launch)
                    run.require('fixture started', False, 'Repair the certificate or listener before rerunning')
                elif browser_exit:
                    with self.assertRaises(subprocess.CalledProcessError):
                        run_browser(run, args, root, launch)
                else:
                    run_browser(run, args, root, launch)
                    script = (run.path/'browser.mjs').read_text(encoding='utf-8')
                    self.assertIn('"space": 69', script)
            self.assertTrue(closed)
            return run.report

    def test_browser_and_fixture_artifacts_are_bound_to_build(self):
        report = self.scenario()
        self.assertEqual(report['status'], 'passed')
        self.assertEqual({a['name'] for a in report['artifacts']}, {'browser-fixture', 'fixture-log', 'browser-results'})

    def test_browser_failure_still_closes_owned_fixture(self):
        self.assertEqual(self.scenario(browser_exit=1)['status'], 'failed')

    def test_startup_failure_is_not_browser_success(self):
        self.assertEqual(self.scenario(startup_exit=1)['status'], 'incomplete')


if __name__ == '__main__':
    unittest.main()
