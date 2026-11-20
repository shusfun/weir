"""Run backend regression checks with a source fingerprint and retained logs."""
import argparse
import os
import pathlib
import re

from scenario_run import ScenarioRun


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--postgres", action="store_true")
    parser.add_argument("--run", default=".")
    parser.add_argument("--smoke-only", action="store_true")
    parser.add_argument("--center-only", action="store_true", help="Rerun center batches without repeating other packages")
    parser.add_argument("--linux", action="store_true", help="Cross-compile and run center tests in an isolated Linux container")
    parser.add_argument("--image", default="debian:bookworm-slim", help="Linux test runtime image")
    parser.add_argument("--upstream", action="store_true", help="Include the Node v24 subscription reference tests")
    parser.add_argument("--storage-scale", action="store_true", help="Include bounded storage measurements and query plans")
    parser.add_argument("--resume", type=pathlib.Path, help="Rerun unfinished steps from an unchanged source state")
    parser.add_argument("--batch-size", type=int, default=30)
    args = parser.parse_args()
    if args.batch_size < 1:
        parser.error("--batch-size must be positive")
    if args.linux and args.upstream:
        parser.error("Run --upstream on the host with Node v24; --linux runs center tests only")
    root = pathlib.Path(__file__).resolve().parent.parent
    with ScenarioRun(root, root / "dist" / "checks") as run:
        execute_checks(run, args, root)
    return run.exit_code


def execute_checks(run, args, root):
    flags = "-X github.com/shusfun/weir/internal/buildinfo.Revision=" + run.before["commit"] + "-" + run.before["worktree_sha256"][:12]
    env = dict(os.environ)
    if args.postgres:
        env["WEIR_CENTER_STORE"] = "postgres"
        env["WEIR_INTEGRATION"] = "1"
    if args.storage_scale:
        env["WEIR_STORAGE_SCALE"] = "1"
    if args.upstream:
        version = run.run("node-version", ["node", "--version"], env=env, check=True)
        if not version.stdout.decode("utf-8").strip().startswith("v24."):
            raise ValueError("Subscription reference tests require Node v24 on PATH")
        env["WEIR_UPSTREAM_TESTS"] = "1"
    pending = set(run.resume_from(args.resume)) if args.resume else None
    center = CenterTests(run, args, root, env, flags)
    center.test("trace-smoke", "^TestADRTraceSmoke$", ["TestADRTraceSmoke"], check=True)
    if not args.postgres and not args.linux:
        run.run("browser-protocol-smoke", ["go", "test", "./internal/browsertest", "-run", "^TestNetworkProtocolsAndSessionRevocation$", "-count=1", "-json", "-ldflags", flags], env=env, check=True)
    if args.smoke_only:
        return
    listed = center.list(args.run)
    names = [line for line in listed.stdout.decode("utf-8").splitlines() if re.fullmatch(r"Test\w+", line)]
    if not names:
        raise RuntimeError("No center tests matched --run")
    for start in range(0, len(names), args.batch_size):
        selected = names[start:start + args.batch_size]
        pattern = "^(" + "|".join(selected) + ")$"
        name = f"{'postgres' if args.postgres else 'center'}-{start // args.batch_size + 1:02d}"
        if pending is None or name in pending:
            center.test(name, pattern, selected)
            if pending is not None:
                pending.discard(name)
    if not args.center_only and not args.linux:
        packages = (["./internal/store"] if args.postgres else
                    ["./internal/gate", "./internal/browsertest/...", "./internal/nodeprobe", "./internal/hoststats", "./internal/sub", "./internal/subgroup", "./internal/subscription", "./internal/subout", "./internal/kernelconf", "./internal/kernelconf/model", "./internal/kernels", "./internal/store", "./packages/contracts"])
        if pending is None or "backend" in pending:
            # Center and store integration tests share an isolated Redis fixture;
            # finish the center batches before opening store test databases.
            run.run("backend", ["go", "test", *packages, "-run", args.run, "-count=1", "-timeout=30m", "-json", "-ldflags", flags], env=env)
            if pending is not None:
                pending.discard("backend")
    if not args.postgres and not args.linux:
        run.run("build", ["go", "build", "./..."], env=env)
        run.run("vet", ["go", "vet", "./..."], env=env)
    if pending is not None:
        pending.difference_update({"trace-smoke", "browser-protocol-smoke", "center-list", "build", "vet", "node-version", "center-build"})
        if pending:
            raise ValueError("Resume options do not select previous unfinished steps: " + ", ".join(sorted(pending)))
    if any(step["exit_code"] for step in run.steps):
        raise RuntimeError("One or more checks failed; see individual logs")


class CenterTests:
    def __init__(self, run, args, root, env, flags):
        self.run, self.args, self.env, self.flags = run, args, env, flags
        self.container = None
        if args.linux:
            binary = run.path / "center.test"
            compiled = dict(env, GOOS="linux", GOARCH="amd64", CGO_ENABLED="0")
            run.run("center-build", ["go", "test", "-c", "-o", str(binary), "-ldflags", flags, "./internal/center"], env=compiled, check=True)
            run.artifact("center.test", binary, "center-build")
            self.container = ["docker", "run", "--rm", "--name", "weir-check-" + run.id,
                              "--label", "weir.scenario=" + run.id,
                              "-v", f"{binary}:/t/center.test:ro", "-v", f"{root}:/workspace:ro",
                              "-w", "/workspace/internal/center"]
            if args.postgres:
                self.container += ["--add-host", "host.docker.internal:host-gateway",
                                   "-e", "WEIR_CENTER_STORE=postgres", "-e", "WEIR_TEST_HOST=host.docker.internal"]
            if args.storage_scale:
                self.container += ["-e", "WEIR_STORAGE_SCALE=1"]
            self.container += [args.image, "/t/center.test"]

    def list(self, selection):
        command = ([*self.container, "-test.list", selection] if self.container else
                   ["go", "test", "./internal/center", "-list", selection])
        return self.run.run("center-list", command, env=self.env, check=True)

    def test(self, name, pattern, required, **kwargs):
        timeout = "30m" if self.args.storage_scale else "10m"
        command = (["go", "tool", "test2json", "-p", "github.com/shusfun/weir/internal/center",
                    *self.container, "-test.v=test2json", "-test.run", pattern, "-test.timeout", timeout] if self.container else
                   ["go", "test", "./internal/center", "-run", pattern, "-count=1", "-timeout=" + timeout, "-json", "-ldflags", self.flags])
        return self.run.run(name, command, required_tests=required, env=self.env,
                            recovery="Resolve the reported skip or failure, then rerun deploy/check.py with the same platform, store, selection and batch size", **kwargs)


if __name__ == "__main__":
    raise SystemExit(main())
