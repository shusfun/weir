"""Run bounded commands and retain correlated, independently reviewable results."""
import datetime
import hashlib
import json
import pathlib
import re
import subprocess
import time
import uuid


def go_test_results(output):
    states = {}
    for line in output.splitlines():
        native = re.match(r"\s*(?:=== (RUN)\s+|--- (PASS|FAIL|SKIP): )([^\s]+)", line)
        if native:
            states[("", native[3])] = (native[1] or native[2]).lower()
            continue
        try:
            event = json.loads(line)
        except (ValueError, TypeError):
            continue
        if not isinstance(event, dict) or not event.get("Test"):
            continue
        action = event.get("Action")
        if action in ("run", "pass", "fail", "skip"):
            states[(event.get("Package", ""), event["Test"])] = action
    return {status: [f"{package}/{test}" for (package, test), action in states.items() if action == value]
            for status, value in (("passed", "pass"), ("failed", "fail"), ("skipped", "skip"), ("unfinished", "run"))}


def matched_subtest(name, selection, passed):
    """A parent PASS alone never proves a selected descendant ran."""
    patterns = selection.split('/')
    for test in passed:
        # Native results start with '/', JSON results start with a package.
        parts = test[test.rfind('/' + name) + 1:].split('/')
        if len(parts) > len(patterns) and parts[0] == name and all(
                re.search(pattern, part) for pattern, part in zip(patterns, parts[1:])):
            return True
    return False


def skipped_test_details(output, skipped):
    """Keep test output near a skip so its missing prerequisite is actionable."""
    messages = {}
    native = []
    for line in output.splitlines():
        try:
            event = json.loads(line)
        except (ValueError, TypeError):
            native.append(line)
            match = re.search(r'--- SKIP: (\S+)', line)
            if match:
                messages['/' + match[1]] = '\n'.join(native[-8:])[-2000:]
            continue
        if isinstance(event, dict) and event.get("Test") and event.get("Action") == "output":
            key = event.get("Package", "") + "/" + event["Test"]
            messages[key] = (messages.get(key, "") + event.get("Output", ""))[-2000:]
    return {name: messages.get(name, "See the step log for the missing prerequisite") for name in skipped}


class ScenarioRun:
    def __init__(self, root, output, execute=subprocess.run, snapshot=None, end_marker="WEIR_SCENARIO_END"):
        self.root = pathlib.Path(root)
        self.execute = execute
        self.snapshot_provider = snapshot
        self.end_marker = end_marker
        self.id = datetime.datetime.now(datetime.timezone.utc).strftime("%Y%m%dT%H%M%SZ-") + uuid.uuid4().hex[:12]
        self.path = pathlib.Path(output) / self.id
        self.path.mkdir(parents=True, exist_ok=False)
        self.steps = []
        self.error = None
        self.requirements = []
        self.artifacts = []
        self.report = None
        self.before = self.snapshot()

    @property
    def exit_code(self):
        if self.report is None:
            raise RuntimeError("Scenario has not finished")
        return {"passed": 0, "failed": 1, "incomplete": 2}[self.report["status"]]

    def require(self, name, satisfied, recovery, kind="environment"):
        if type(satisfied) is not bool or not name or not recovery:
            raise ValueError("A requirement needs a name, boolean result and recovery instructions")
        self.requirements.append({"name": name, "satisfied": satisfied, "recovery": recovery, "kind": kind})

    def resume_from(self, path):
        previous = json.loads(pathlib.Path(path).read_text(encoding="utf-8"))
        if previous.get("before") != self.before or previous.get("after") != self.before:
            raise ValueError("Cannot resume results from a different source fingerprint")
        # Only select work to rerun. A previous success is never copied into this run.
        return [s["name"] for s in previous["steps"] if s["exit_code"] or s.get("missing_tests") or
                any(s.get("tests", {}).get(k) for k in ("failed", "skipped", "unfinished"))]

    def artifact(self, name, path, version_step=None):
        path = pathlib.Path(path)
        with path.open("rb") as stream:
            digest = hashlib.file_digest(stream, "sha256").hexdigest()
        self.artifacts.append({"name": name, "sha256": digest, "bytes": path.stat().st_size,
                               "version_step": version_step})

    def snapshot(self):
        if self.snapshot_provider is not None:
            return self.snapshot_provider()
        head = self.execute(["git", "rev-parse", "HEAD"], cwd=self.root, capture_output=True, check=True).stdout
        diff = self.execute(["git", "diff", "--binary", "HEAD"], cwd=self.root, capture_output=True, check=True).stdout
        files = self.execute(["git", "ls-files", "--others", "--exclude-standard", "-z"], cwd=self.root, capture_output=True, check=True).stdout
        digest = hashlib.sha256(diff)
        for raw in sorted(files.split(b"\0")):
            if raw:
                digest.update(raw)
                digest.update((self.root / raw.decode("utf-8")).read_bytes())
        return {"commit": head.decode().strip(), "worktree_sha256": digest.hexdigest()}

    def run(self, name, command, *, required_tests=(), recovery=None, **kwargs):
        if isinstance(required_tests, str) or any(not isinstance(test, str) or not test for test in required_tests):
            raise ValueError("required_tests must contain explicit test names")
        started = time.monotonic()
        kwargs.pop("capture_output", None)
        check = kwargs.pop("check", False)
        interruption = None
        try:
            result = self.execute(command, cwd=kwargs.pop("cwd", self.root), capture_output=True, **kwargs)
        except (subprocess.TimeoutExpired, OSError, KeyboardInterrupt) as error:
            interruption = error
            result = subprocess.CompletedProcess(command, 124 if isinstance(error, subprocess.TimeoutExpired) else 130 if isinstance(error, KeyboardInterrupt) else 127,
                                                 getattr(error, "stdout", None) or "", getattr(error, "stderr", None) or str(error))
        stdout = result.stdout or ""
        stderr = result.stderr or ""
        if isinstance(stdout, bytes):
            stdout = stdout.decode("utf-8", errors="replace")
        if isinstance(stderr, bytes):
            stderr = stderr.decode("utf-8", errors="replace")
        output = stdout + stderr
        filename = f"{len(self.steps) + 1:02d}-{re.sub(r'[^a-zA-Z0-9_.-]', '_', name)}.log"
        (self.path / filename).write_text(output, encoding="utf-8")
        self.steps.append({"name": name, "exit_code": result.returncode, "duration_seconds": round(time.monotonic() - started, 3), "run_id": self.id, "log": filename,
                           "trace_ids": sorted(set(re.findall(r'(?:trace|trace_id|traceId)[=\": ]+([a-f0-9]{32})', output)))})
        if result.returncode:
            self.steps[-1]["recovery"] = recovery or "Inspect this step's retained log, resolve the reported error and rerun the same scenario"
        if required_tests or (command[:2] == ["go", "test"] and "-json" in command) or ("/t/e2e.test" in command and "-test.list" not in command):
            self.steps[-1]["tests"] = go_test_results(output)
            observed = self.steps[-1]["tests"]
            self.steps[-1]["skip_reasons"] = skipped_test_details(output, observed["skipped"])
            self.steps[-1]["required_tests"] = list(required_tests)
            self.steps[-1]["missing_tests"] = [test for test in required_tests
                if not any(name == test or name.endswith("/" + test) for name in observed["passed"])]
            if self.steps[-1]["missing_tests"]:
                self.require(name + " mandatory tests", False,
                             recovery or "Rerun this step with the declared tests after resolving missing or skipped cases", kind="test")
            for test in observed["skipped"]:
                self.require(name + ": " + test, False,
                             recovery or "Resolve the prerequisite in skip_reasons and rerun this step; a skipped test is not a pass", kind="test")
            if observed["unfinished"] or not any(observed.values()):
                self.require(name + " completed test execution", False,
                             recovery or "Check the test selector and timeout, then rerun until the selected tests finish", kind="test")
        print(f"[{self.id}] {name}: exit={result.returncode}, {self.steps[-1]['duration_seconds']}s", flush=True)
        if interruption is not None:
            raise interruption
        if check and result.returncode:
            raise subprocess.CalledProcessError(result.returncode, command)
        return result

    def __enter__(self):
        return self

    def __exit__(self, kind, error, traceback):
        self.error = str(error) if error else None
        try:
            after = self.snapshot()
        except Exception as snapshot_error:
            after = None
            self.error = str(snapshot_error)
        command_failed = any(step["exit_code"] != 0 for step in self.steps)
        source_changed = after != self.before
        test_incomplete = any(s["tests"]["skipped"] or s["tests"]["unfinished"] or not s["tests"]["passed"] for s in self.steps if "tests" in s)
        test_failed = any(s["tests"]["failed"] for s in self.steps if "tests" in s)
        requirements_missing = any(not r["satisfied"] for r in self.requirements)
        interrupted = error is not None
        failed = interrupted or command_failed or test_failed or source_changed
        incomplete = test_incomplete or requirements_missing or not self.steps
        status = "failed" if failed else "incomplete" if incomplete else "passed"
        recovery = list(dict.fromkeys([s["recovery"] for s in self.steps if s.get("recovery")] +
                                     [r["recovery"] for r in self.requirements if not r["satisfied"]]))
        report = {"run_id": self.id, "status": status, "before": self.before, "after": after, "steps": self.steps, "requirements": self.requirements, "error": self.error,
                  "artifacts": self.artifacts, "recovery": recovery}
        self.report = report
        (self.path / "report.json").write_text(json.dumps(report, ensure_ascii=False, indent=2), encoding="utf-8")
        lines = [f"# {self.id}", "", f"Status: {report['status']}", f"Commit: {self.before.get('commit', 'not recorded')}", "", "| Step | Exit | Seconds | Evidence |", "|---|---:|---:|---|"]
        lines += [f"| {s['name']} | {s['exit_code']} | {s['duration_seconds']} | [{s['log']}]({s['log']}) |" for s in self.steps]
        if self.error:
            lines += ["", self.error]
        for step in self.steps:
            if step.get("recovery"):
                lines += ["", f"Failed {step['name']}. Recovery: {step['recovery']}"]
        for requirement in self.requirements:
            if not requirement["satisfied"]:
                lines += ["", f"Missing {requirement['kind']}: {requirement['name']}. Recovery: {requirement['recovery']}"]
        for step in self.steps:
            for test, reason in step.get("skip_reasons", {}).items():
                lines += ["", f"Skipped {test} ({step['name']}):", "", "```text", reason.rstrip(), "```"]
        if after != self.before:
            lines += ["", "The code changed during this run; results do not describe one code state."]
        (self.path / "report.md").write_text("\n".join(lines) + "\n", encoding="utf-8")
        print(f"{self.end_marker} run_id={self.id} status={report['status']} report={self.path / 'report.json'}", flush=True)
        return False
