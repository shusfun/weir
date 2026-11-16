import json
import pathlib
import subprocess
import tempfile
import unittest

from scenario_run import ScenarioRun, go_test_results, matched_subtest


class ScenarioRunTests(unittest.TestCase):
    def test_selected_descendant_requires_its_own_pass(self):
        self.assertFalse(matched_subtest('TestNative', 'udp', ['/TestNative']))
        self.assertFalse(matched_subtest('TestNative', 'udp', ['/TestNative/tcp']))
        self.assertFalse(matched_subtest('TestNative', 'udp/missing', ['/TestNative/udp']))
        self.assertTrue(matched_subtest('TestNative', 'udp', ['/TestNative/udp']))
        self.assertTrue(matched_subtest('TestNative', '(tcp|udp)/ok', ['github.com/org/pkg/TestNative/udp/ok']))

    def test_mandatory_case_cannot_be_replaced_by_another_success(self):
        with tempfile.TemporaryDirectory() as d:
            execute = lambda command, **kwargs: subprocess.CompletedProcess(command, 0, b'--- PASS: TestOther (0.01s)\n', b'')
            with ScenarioRun(d, pathlib.Path(d) / 'runs', execute, snapshot=lambda: {}) as run:
                run.run('native', ['fixture'], required_tests=['TestNative/udp'], recovery='Run the UDP fixture')
            self.assertEqual(run.exit_code, 2)
            self.assertEqual(run.report['steps'][0]['missing_tests'], ['TestNative/udp'])
            self.assertEqual(run.report['recovery'], ['Run the UDP fixture'])
            with ScenarioRun(d, pathlib.Path(d) / 'runs', execute, snapshot=lambda: {}) as retry:
                self.assertEqual(retry.resume_from(run.path / 'report.json'), ['native'])
                retry.run('native', ['fixture'], required_tests=['TestOther'])
            self.assertEqual(retry.exit_code, 0)

    def test_artifact_digest_is_bound_to_report(self):
        with tempfile.TemporaryDirectory() as d:
            binary = pathlib.Path(d) / 'kernel'
            binary.write_bytes(b'abc')
            execute = lambda command, **kwargs: subprocess.CompletedProcess(command, 0, b'1.0', b'')
            with ScenarioRun(d, pathlib.Path(d) / 'runs', execute, snapshot=lambda: {}) as run:
                run.run('kernel-version', ['kernel', 'version'])
                run.artifact('kernel', binary, 'kernel-version')
            artifact = run.report['artifacts'][0]
            self.assertEqual(artifact['sha256'], 'ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad')
            self.assertEqual(artifact['version_step'], 'kernel-version')

    def test_native_test_binary_reports_skipped_subtest(self):
        result = go_test_results("=== RUN   TestNative\n=== RUN   TestNative/client\n    --- SKIP: TestNative/client (0.00s)\n--- PASS: TestNative (0.01s)\nPASS")
        self.assertEqual(result["skipped"], ["/TestNative/client"])
        self.assertEqual(result["passed"], ["/TestNative"])
        self.assertEqual(result["unfinished"], [])

    def test_go_results_keep_skips_and_unfinished_distinct(self):
        events = [("run", "A"), ("pass", "A"), ("run", "B"), ("skip", "B"), ("run", "C"), ("run", "D"), ("fail", "D")]
        output = "log\n" + "\n".join(json.dumps({"Package": "pkg", "Action": action, "Test": name}) for action, name in events)
        self.assertEqual(go_test_results(output), {"passed": ["pkg/A"], "skipped": ["pkg/B"], "unfinished": ["pkg/C"], "failed": ["pkg/D"]})

    def test_successful_command_with_skips_is_incomplete(self):
        def execute(command, **kwargs):
            return subprocess.CompletedProcess(command, 0, b'{"Package":"pkg","Action":"skip","Test":"A"}\n', b"")
        with tempfile.TemporaryDirectory() as d:
            with ScenarioRun(d, pathlib.Path(d) / "runs", execute, snapshot=lambda: {}) as run:
                run.run("test", ["go", "test", "-json"])
            self.assertEqual(json.loads((run.path / "report.json").read_text())["status"], "incomplete")
            self.assertEqual(run.exit_code, 2)

    def test_skip_reason_and_recovery_are_retained(self):
        output = '\n'.join(json.dumps(event) for event in [
            {'Package': 'pkg', 'Test': 'A', 'Action': 'output', 'Output': 'requires Linux and sh\n'},
            {'Package': 'pkg', 'Test': 'A', 'Action': 'skip'},
        ])
        with tempfile.TemporaryDirectory() as d:
            execute = lambda command, **kwargs: subprocess.CompletedProcess(command, 0, output, '')
            with ScenarioRun(d, pathlib.Path(d) / 'runs', execute, snapshot=lambda: {}) as run:
                run.run('tests', ['go', 'test', '-json'], recovery='Run deploy/check.py --linux')
            self.assertEqual(run.exit_code, 2)
            self.assertIn('requires Linux and sh', run.report['steps'][0]['skip_reasons']['pkg/A'])
            self.assertEqual(run.report['recovery'], ['Run deploy/check.py --linux'])
            self.assertIn('requires Linux and sh', (run.path / 'report.md').read_text())

    def test_requirements_cannot_be_replaced_by_fixture_success(self):
        with tempfile.TemporaryDirectory() as d:
            execute = lambda command, **kwargs: subprocess.CompletedProcess(command, 0, b"ready", b"")
            with ScenarioRun(d, pathlib.Path(d) / "runs", execute, snapshot=lambda: {}) as run:
                run.run("fixture", ["fixture"])
                run.require("browser-flow", False, "Run the browser flow against the fixture", kind="interaction")
            self.assertEqual(run.exit_code, 2)
            self.assertIn("browser-flow", (run.path / "report.md").read_text())

    def test_missing_environment_is_incomplete_and_includes_recovery(self):
        with tempfile.TemporaryDirectory() as d:
            with ScenarioRun(d, pathlib.Path(d) / "runs", snapshot=lambda: {}) as run:
                run.require("signed kernel fixtures", False, "Fetch the pinned artifacts and verify their SHA-256")
            report = json.loads((run.path / "report.json").read_text())
            self.assertEqual(report["status"], "incomplete")
            self.assertIn("Fetch the pinned artifacts", report["recovery"][0])
            self.assertEqual(run.exit_code, 2)

    def test_timeout_retains_failed_step(self):
        def execute(command, **kwargs):
            raise subprocess.TimeoutExpired(command, 1, output=b"started")
        with tempfile.TemporaryDirectory() as d:
            with self.assertRaises(subprocess.TimeoutExpired):
                with ScenarioRun(d, pathlib.Path(d) / "runs", execute, snapshot=lambda: {}) as run:
                    run.run("timed", ["slow"], timeout=1)
            self.assertEqual(run.exit_code, 1)
            self.assertEqual(run.report["steps"][0]["exit_code"], 124)
            self.assertIn("started", (run.path / run.report["steps"][0]["log"]).read_text())
            self.assertTrue(run.report["recovery"])

    def test_failed_preparation_preserves_recovery_before_exception(self):
        with tempfile.TemporaryDirectory() as d:
            execute = lambda command, **kwargs: subprocess.CompletedProcess(command, 100, b"APT DNS failed", b"")
            with self.assertRaises(subprocess.CalledProcessError):
                with ScenarioRun(d, pathlib.Path(d) / "runs", execute, snapshot=lambda: {}) as run:
                    run.run("firewall-image", ["docker", "build"], check=True,
                            recovery="Use the documented signed APT mirror and rerun the firewall scenario")
            self.assertEqual(run.exit_code, 1)
            self.assertEqual(run.report["recovery"], ["Use the documented signed APT mirror and rerun the firewall scenario"])
            self.assertIn("Failed firewall-image. Recovery:", (run.path / "report.md").read_text())

    def test_unmatched_tests_and_failed_test_with_zero_exit_are_not_success(self):
        with tempfile.TemporaryDirectory() as d:
            for output, code in ((b"", 2), (b'{"Test":"A","Action":"fail"}', 1)):
                execute = lambda command, **kwargs: subprocess.CompletedProcess(command, 0, output, b"")
                with ScenarioRun(d, pathlib.Path(d) / "runs", execute, snapshot=lambda: {}) as run:
                    run.run("tests", ["go", "test", "-json"])
                self.assertEqual(run.exit_code, code)

    def test_resume_selects_failures_and_rejects_changed_source(self):
        with tempfile.TemporaryDirectory() as d:
            execute = lambda command, **kwargs: subprocess.CompletedProcess(command, int(command[0] == "fail"), b"", b"")
            snapshot = lambda: {"commit": "a", "worktree_sha256": "b"}
            with ScenarioRun(d, pathlib.Path(d) / "runs", execute, snapshot=snapshot) as first:
                first.run("bad", ["fail"])
                first.run("good", ["ok"])
            with ScenarioRun(d, pathlib.Path(d) / "runs", execute, snapshot=snapshot) as retry:
                self.assertEqual(retry.resume_from(first.path / "report.json"), ["bad"])
                retry.run("bad", ["ok"])
            self.assertEqual([s["name"] for s in retry.report["steps"]], ["bad"])
            with self.assertRaisesRegex(ValueError, "different source"):
                with ScenarioRun(d, pathlib.Path(d) / "runs", execute, snapshot=lambda: {}) as changed:
                    changed.resume_from(first.path / "report.json")

    def test_source_snapshot_supports_non_git_project_and_detects_changes(self):
        with tempfile.TemporaryDirectory() as d:
            state = {"source_sha256": "before"}
            def snapshot():
                return dict(state)
            def execute(command, **kwargs):
                self.assertNotEqual(command[0], "git")
                return subprocess.CompletedProcess(command, 0, b"OK", b"")
            with ScenarioRun(d, pathlib.Path(d) / "runs", execute, snapshot=snapshot, end_marker="SOURCE_END") as run:
                run.run("scan", ["scan"])
                state["source_sha256"] = "after"
            report = json.loads((run.path / "report.json").read_text())
            self.assertEqual(report["status"], "failed")
            self.assertNotEqual(report["before"], report["after"])

    def test_failure_does_not_hide_later_steps_and_report_is_correlated(self):
        def fake(command, **kwargs):
            if command[:2] == ["git", "rev-parse"]:
                return subprocess.CompletedProcess(command, 0, b"commit\n", b"")
            if command[0] == "git":
                return subprocess.CompletedProcess(command, 0, b"", b"")
            return subprocess.CompletedProcess(command, 1 if command[0] == "fail" else 0, b"trace=123456789012345678901234567890ab\n", b"")
        with tempfile.TemporaryDirectory() as d:
            with ScenarioRun(d, pathlib.Path(d) / "runs", fake) as run:
                run.run("broken", ["fail"])
                run.run("following", ["ok"])
            report = json.loads((run.path / "report.json").read_text())
            self.assertEqual(report["status"], "failed")
            self.assertEqual([s["exit_code"] for s in report["steps"]], [1, 0])
            self.assertTrue(all(s["run_id"] == run.id and len(s["trace_ids"]) == 1 for s in report["steps"]))
            self.assertTrue((run.path / "report.md").exists())

    def test_exception_still_finishes_report(self):
        def fake(command, **kwargs):
            return subprocess.CompletedProcess(command, 0, b"", b"")
        with tempfile.TemporaryDirectory() as d:
            with self.assertRaisesRegex(RuntimeError, "interrupted"):
                with ScenarioRun(d, pathlib.Path(d) / "runs", fake) as run:
                    raise RuntimeError("interrupted")
            report = json.loads((run.path / "report.json").read_text())
            self.assertEqual(report["status"], "failed")
            self.assertEqual(report["error"], "interrupted")


if __name__ == "__main__":
    unittest.main()
