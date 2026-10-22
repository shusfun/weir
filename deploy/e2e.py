"""端到端测试：交叉编译 e2e 测试程序，在 debian 容器中用真实内核运行。

用法：python deploy/e2e.py [-run 正则]
需要 Docker 与 dist/cores 下的 Linux 版 Xray（26.3.27）。
"""
import os
import pathlib
import subprocess
import sys

ROOT = pathlib.Path(__file__).resolve().parent.parent
OUT = ROOT / "dist" / "e2e"
XRAY = ROOT / "dist" / "cores" / "v26.3.27" / "xray"
IMAGE = os.environ.get("MIGATE_E2E_IMAGE", "debian:bookworm-slim")


def main(argv):
    if not XRAY.is_file():
        raise SystemExit(f"missing {XRAY}")
    OUT.mkdir(parents=True, exist_ok=True)
    env = dict(os.environ, GOOS="linux", GOARCH="amd64", CGO_ENABLED="0")
    subprocess.run(["go", "test", "-c", "-tags", "e2e", "-o", str(OUT / "e2e.test"), "./internal/e2e"], cwd=ROOT, env=env, check=True)
    args = ["-test.v", "-test.timeout", "15m", *argv]
    cmd = ["docker", "run", "--rm",
           "-v", f"{OUT}:/t", "-v", f"{XRAY}:/cores/xray:ro",
           "-e", "MIGATE_E2E_XRAY=/cores/xray", IMAGE, "/t/e2e.test", *args]
    return subprocess.run(cmd).returncode


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
