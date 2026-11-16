import argparse
import pathlib
import subprocess
import tempfile
import unittest
from unittest.mock import patch

import check
from scenario_run import ScenarioRun


class CheckRunnerTests(unittest.TestCase):
    def execute_checks(self, **options):
        commands = []
        def execute(command, **kwargs):
            commands.append((command, kwargs))
            output = b''
            if '-c' in command:
                pathlib.Path(command[command.index('-o') + 1]).write_bytes(b'binary')
            elif '-list' in command or '-test.list' in command:
                output = b'TestStorage\n'
            elif '-json' in command or '-test.run' in command:
                pattern = command[command.index('-run') + 1] if '-run' in command else command[command.index('-test.run') + 1]
                name = 'TestADRTraceSmoke' if 'TestADRTraceSmoke' in pattern else 'TestStorage'
                output = ('{"Package":"pkg","Test":"' + name + '","Action":"pass"}\n').encode()
            return subprocess.CompletedProcess(command, 0, output, b'')
        args = argparse.Namespace(postgres=False, run='.', smoke_only=False, center_only=False,
                                  batch_size=30, linux=False, image='debian:bookworm-slim',
                                  upstream=False, storage_scale=False, resume=None)
        vars(args).update(options)
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            with ScenarioRun(root, root / 'runs', execute, snapshot=lambda: {'commit': 'a', 'worktree_sha256': 'b'}) as run:
                check.execute_checks(run, args, root)
            self.assertEqual(run.exit_code, 0)
        return commands

    def test_postgres_also_executes_store_integration_after_center(self):
        commands = self.execute_checks(postgres=True)
        backend = next((cmd, kwargs) for cmd, kwargs in commands if './internal/store' in cmd)
        self.assertEqual(backend[1]['env']['WEIR_INTEGRATION'], '1')
        self.assertEqual(backend[1]['env']['WEIR_CENTER_STORE'], 'postgres')
        self.assertFalse(any('./internal/gate' in cmd for cmd, _ in commands))
        center = next(i for i, (cmd, _) in enumerate(commands) if '-run' in cmd and 'TestStorage' in cmd[cmd.index('-run') + 1])
        self.assertLess(center, commands.index(backend))

    def test_linux_cross_compiles_and_executes_real_test_binary(self):
        commands = self.execute_checks(linux=True, postgres=True)
        compile_cmd, compile_opts = next((cmd, opts) for cmd, opts in commands if '-c' in cmd)
        self.assertEqual(compile_opts['env']['GOOS'], 'linux')
        self.assertEqual(compile_opts['env']['CGO_ENABLED'], '0')
        test = next(cmd for cmd, _ in commands if '-test.run' in cmd)
        self.assertIn('test2json', test)
        self.assertIn('WEIR_TEST_HOST=host.docker.internal', test)
        self.assertIn('/t/center.test', test)
        self.assertTrue(any(value.endswith(':/t/center.test:ro') for value in test))
        self.assertFalse(any('./internal/store' in cmd for cmd, _ in commands))

    def test_default_backend_includes_original_subscription_package(self):
        commands = self.execute_checks()
        self.assertTrue(any('./internal/sub' in cmd for cmd, _ in commands))
        self.assertTrue(any('./internal/browsertest/...' in cmd for cmd, _ in commands))


if __name__ == '__main__':
    unittest.main()
