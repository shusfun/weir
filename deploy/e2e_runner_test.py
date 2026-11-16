import pathlib
import subprocess
import tempfile
import unittest
from unittest.mock import patch

import e2e
from scenario_run import ScenarioRun


class E2ERunnerTests(unittest.TestCase):
    def test_browser_tls_exposes_only_leaf_key_and_public_chain(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            for name in ('ca.pem', 'server.pem', 'server.key', 'ca.key'):
                (root / name).write_text('fixture')
            with patch.dict(e2e.os.environ, {'WEIR_E2E_BROWSER_TLS_DIR': directory}, clear=True):
                mounts = e2e.browser_tls_mount()
                self.assertEqual(mounts[-2:], ['-e', 'WEIR_E2E_BROWSER_TLS_DIR=/browser-tls'])
                self.assertEqual([mounts[i + 1] for i, arg in enumerate(mounts) if arg == '-v'],
                                 [f'{root / name}:/browser-tls/{name}:ro' for name in ('ca.pem', 'server.pem', 'server.key')])
                (root / 'server.key').unlink()
                with self.assertRaisesRegex(ValueError, 'server.key'):
                    e2e.browser_tls_mount()

    def test_browser_network_is_owned_and_cleaned_after_failure(self):
        commands = []
        def execute(command, **kwargs):
            commands.append(command)
            return subprocess.CompletedProcess(command, 0, b"network-id\n", b"")
        with tempfile.TemporaryDirectory() as directory:
            with patch.dict(e2e.os.environ, {'WEIR_E2E_NODE_BROWSER': '1'}, clear=True), patch.object(e2e, 'run_scenarios', side_effect=RuntimeError('fixture failed')) as scenarios:
                with ScenarioRun(directory, pathlib.Path(directory) / 'runs', execute, snapshot=lambda: {}) as run:
                    with self.assertRaisesRegex(RuntimeError, 'fixture failed'):
                        e2e.main([], run)
                network = 'weir-browser-' + run.id
                scenarios.assert_called_once_with([], run, network)
                self.assertEqual(commands[0][-1], network)
                self.assertIn('192.0.2.0/24', commands[0])
                self.assertEqual(commands[-1], ['docker', 'network', 'rm', network])

    def test_explicit_network_is_reused_without_cleanup(self):
        with patch.dict(e2e.os.environ, {'WEIR_E2E_NODE_BROWSER': '1', 'WEIR_E2E_NETWORK': 'explicit-fixture'}, clear=True), patch.object(e2e, 'run_scenarios', return_value=0) as scenarios:
            self.assertEqual(e2e.main([], None), 0)
            scenarios.assert_called_once_with([], None, 'explicit-fixture')

    def test_automatic_network_rejects_real_public_or_private_subnet(self):
        for subnet in ('10.0.0.0/24', '8.8.8.0/24', '::/64', '192.0.2.0/30'):
            with self.subTest(subnet=subnet), patch.dict(e2e.os.environ, {'WEIR_E2E_NODE_BROWSER': '1', 'WEIR_E2E_BROWSER_SUBNET': subnet}, clear=True):
                with self.assertRaises(ValueError):
                    e2e.main([], None)

    def test_browser_timeout_includes_setup_and_cleanup(self):
        with patch.dict(e2e.os.environ, {'WEIR_E2E_NODE_BROWSER': '1', 'WEIR_E2E_BROWSER_SECONDS': '3600'}, clear=True):
            self.assertEqual(e2e.test_timeout('TestNodeTestBrowserFixture'), '3900s')
            self.assertEqual(e2e.test_timeout('TestNative'), '15m')
            e2e.os.environ['WEIR_E2E_BROWSER_SECONDS'] = '0'
            with self.assertRaises(ValueError):
                e2e.test_timeout('TestNodeTestBrowserFixture')

    def test_runs_keep_separate_read_only_binaries(self):
        commands = []

        def execute(command, **kwargs):
            commands.append(command)
            output = b"fixture version 1\n"
            if command[0] == "go":
                pathlib.Path(command[command.index("-o") + 1]).write_bytes(b"binary")
            elif "-test.list" in command:
                output = b"TestNative\n"
            elif "-test.run" in command:
                output = b"=== RUN   TestNative\n--- PASS: TestNative (0.01s)\nPASS\n"
            if kwargs.get("text"):
                return subprocess.CompletedProcess(command, 0, output.decode(), "")
            return subprocess.CompletedProcess(command, 0, output, b"")

        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            core = root / "kernel"
            core.write_bytes(b"fixed-kernel")
            with patch.multiple(e2e, ROOT=root, OUT=root / "e2e", XRAY=core, SINGBOX=core, MIHOMO=core), patch.dict(e2e.os.environ, {}, clear=True):
                runs = []
                for _ in range(2):
                    with ScenarioRun(root, root / "runs", execute, snapshot=lambda: {"commit": "a", "worktree_sha256": "b"}) as run:
                        self.assertEqual(e2e.main(["-run", "TestNative"], run), 0)
                    self.assertEqual(run.exit_code, 0)
                    runs.append(run)
                mounts = [arg for command in commands for arg in command if isinstance(arg, str) and arg.endswith(":/t:ro")]
                self.assertEqual(set(mounts), {f"{run.path / 'bin'}:/t:ro" for run in runs})
                self.assertTrue(all((run.path / "bin/e2e.test").is_file() for run in runs))
                self.assertTrue(all((run.path / "bin/weir").is_file() for run in runs))


if __name__ == "__main__":
    unittest.main()
