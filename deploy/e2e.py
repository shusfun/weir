"""端到端测试：交叉编译 e2e 测试程序，在 debian 容器中用真实内核运行。

用法：python deploy/e2e.py [-run 正则]
需要 Docker 与 dist/cores 下的 Linux 版 Xray、mihomo 和带统计接口的 sing-box。
sing-box 夹具目录名 sing-box-1.14.3-stats 只是存放路径，不能当成二进制版本；测试从二进制的 version 输出登记缓存。
"""
import os
import ipaddress
import pathlib
import re
import shutil
import subprocess
import sys
from urllib.parse import urlsplit
from scenario_run import ScenarioRun, matched_subtest

ROOT = pathlib.Path(__file__).resolve().parent.parent
OUT = ROOT / "dist" / "e2e"
XRAY = ROOT / "dist" / "cores" / "v26.3.27" / "xray"
SINGBOX = ROOT / "dist" / "cores" / "sing-box-1.14.3-stats" / "sing-box"  # 带 with_v2ray_api 的构建
# 实测与 Clash 订阅检查共用 mihomo，必须经过官方摘要校验。
MIHOMO = ROOT / "dist" / "cores" / "fetched" / "mihomo" / "1.19.33" / "amd64" / "mihomo"
GEOIP = pathlib.Path(os.environ.get("WEIR_E2E_GEOIP", str(ROOT / "dist" / "cores" / "Country.mmdb")))
IMAGE = os.environ.get("WEIR_E2E_IMAGE", "debian:bookworm-slim")
def ca_bundle():
    raw = os.environ.get("WEIR_E2E_CA_BUNDLE")
    if raw:
        return pathlib.Path(raw)
    for candidate in ("/etc/ssl/cert.pem", r"C:\Program Files\Git\mingw64\etc\ssl\certs\ca-bundle.crt"):
        path = pathlib.Path(candidate)
        if path.is_file():
            return path
    return pathlib.Path("/etc/ssl/cert.pem")


CA_BUNDLE = ca_bundle()


def test_timeout(name):
    default = "80m" if name == "TestCertRenewalRealKernels" else "15m"
    if name == "TestNodeTestBrowserFixture" and os.environ.get("WEIR_E2E_NODE_BROWSER") == "1":
        seconds = int(os.environ.get("WEIR_E2E_BROWSER_SECONDS", "900"))
        if not 1 <= seconds <= 3600:
            raise ValueError("WEIR_E2E_BROWSER_SECONDS must be between 1 and 3600")
        # Leave time to set up and clean up around the interactive interval.
        default = str(seconds + 300) + "s"
    return os.environ.get("WEIR_E2E_TIMEOUT", default)


def browser_tls_mount():
    value = os.environ.get("WEIR_E2E_BROWSER_TLS_DIR")
    if not value:
        return []
    directory = pathlib.Path(value).resolve(strict=True)
    if not directory.is_dir():
        raise ValueError("WEIR_E2E_BROWSER_TLS_DIR must be a directory")
    # Mount only these files: a reused CA's signing key must stay on the host.
    mounts = []
    for name in ("ca.pem", "server.pem", "server.key"):
        path = directory / name
        if not path.is_file():
            raise ValueError("Browser TLS fixture missing " + name)
        mounts += ["-v", f"{path}:/browser-tls/{name}:ro"]
    return [*mounts, "-e", "WEIR_E2E_BROWSER_TLS_DIR=/browser-tls"]


def main(argv, run):
    network = os.environ.get("WEIR_E2E_NETWORK", "")
    owned_network = ""
    if os.environ.get("WEIR_E2E_NODE_BROWSER") == "1" and not network:
        subnet = ipaddress.ip_network(os.environ.get("WEIR_E2E_BROWSER_SUBNET", "192.0.2.0/24"))
        documentation = [ipaddress.ip_network(value) for value in ("192.0.2.0/24", "198.51.100.0/24", "203.0.113.0/24")]
        if not isinstance(subnet, ipaddress.IPv4Network) or subnet.prefixlen > 28 or not any(subnet.subnet_of(block) for block in documentation):
            raise ValueError("WEIR_E2E_BROWSER_SUBNET must be a TEST-NET IPv4 subnet with at least 16 addresses")
        network = "weir-browser-" + run.id
        run.run("browser-network-prepare", ["docker", "network", "create", "--label", "weir.scenario=" + run.id,
                                           "--subnet", str(subnet), network], check=True,
                recovery="Check Docker network overlap; choose a free TEST-NET subnet with WEIR_E2E_BROWSER_SUBNET or explicitly supply WEIR_E2E_NETWORK. See docs/implementation.html#certificate")
        owned_network = network
    try:
        return run_scenarios(argv, run, network)
    finally:
        if owned_network:
            run.run("browser-network-cleanup", ["docker", "network", "rm", owned_network], check=True,
                    recovery="Stop only this run's weir-e2e container if it survived interruption, then remove the network named in this step")


def run_scenarios(argv, run, network):
    for f in (XRAY, SINGBOX, MIHOMO):
        run.require(f.name + " fixture", f.is_file(), "Install the checksum-verified Linux binary at the path declared in deploy/e2e.py")
        if not f.is_file():
            print(f"E2E fixture missing: {f}; see docs/implementation.html#environment", flush=True)
            return
    binaries = run.path / "bin"
    binaries.mkdir()
    image = IMAGE
    firewall = os.environ.get("WEIR_E2E_FIREWALL", "")
    if firewall:
        if firewall not in ("ufw", "firewalld", "iptables"):
            raise ValueError("WEIR_E2E_FIREWALL must be ufw, firewalld or iptables")
        image = "weir-firewall-e2e:" + firewall
        build = ["docker", "build"]
        if os.environ.get("WEIR_E2E_BUILDER"):
            build += ["--builder", os.environ["WEIR_E2E_BUILDER"], "--load"]
        for arg in ("DEBIAN_MIRROR", "DEBIAN_SECURITY_MIRROR"):
            value = os.environ.get("WEIR_E2E_" + arg)
            if value:
                parsed = urlsplit(value)
                if parsed.scheme not in ("http", "https") or not parsed.hostname or parsed.username or parsed.password or any(c in value for c in "\n\r|\"$"):
                    raise ValueError("Invalid " + arg)
                build += ["--build-arg", arg + "=" + value]
        run.run("firewall-image", [*build, "--build-arg", "FIREWALL_TOOL=" + firewall,
                                  "-f", str(ROOT / "deploy/firewall.Dockerfile"), "-t", image, str(ROOT / "deploy")], check=True,
                timeout=900, recovery="Follow docs/implementation.html#recovery: check base-image access separately from APT DNS, set the documented WEIR_E2E_DEBIAN_* mirror variables and a working WEIR_E2E_BUILDER, then rerun the same firewall scenario with signature verification enabled")
    env = dict(os.environ, GOOS="linux", GOARCH="amd64", CGO_ENABLED="0")
    flags = "-X github.com/shusfun/weir/internal/buildinfo.Revision=" + run.before["commit"] + "-" + run.before["worktree_sha256"][:12]
    run.run("build-tests", ["go", "test", "-c", "-tags", "e2e", "-ldflags", flags, "-o", str(binaries / "e2e.test"), "./internal/e2e"], cwd=ROOT, env=env, check=True)
    # 单机合并为一个进程的验收要用真实的 weir 程序（中心 + 本机 Gate + 辅助程序）。
    run.run("build-program", ["go", "build", "-ldflags", flags, "-o", str(binaries / "weir"), "./apps/weir"], cwd=ROOT, env=env, check=True)
    # 测试程序只认 -test.run；文档与习惯写的 -run 在这里换过去
    args = ["-test.v", *("-test.run" if a == "-run" else a for a in argv)]
    pattern = "."
    for i, arg in enumerate(args):
        if arg == "-test.run" and i + 1 < len(args):
            pattern = args[i + 1]
        elif arg.startswith("-test.run="):
            pattern = arg.split("=", 1)[1]
    top, separator, children = pattern.partition("/")
    # 宿主机若把外网域名解析成假 IP，容器会继承它，REALITY 目标在内核里解析失败。测试用公网 DNS。
    cmd = ["docker", "run", "--rm", "--name", "weir-e2e-" + run.id, "--label", "weir.scenario=" + run.id, "--cap-add", "NET_ADMIN", "--cap-add", "NET_RAW",
           "-v", f"{binaries}:/t:ro", "-v", f"{XRAY}:/cores/xray:ro", "-v", f"{SINGBOX}:/cores/sing-box:ro",
           "-e", "WEIR_E2E_XRAY=/cores/xray", "-e", "WEIR_E2E_SINGBOX=/cores/sing-box", "-e", "WEIR_E2E_BINARY=/t/weir"]
    for dns in os.environ.get("WEIR_E2E_DNS", "1.1.1.1,8.8.8.8").split(","):
        cmd += ["--dns", dns.strip()]
    if os.environ.get("WEIR_E2E_NODE_BROWSER") == "1":
        cmd += browser_tls_mount()
        browser_port = int(os.environ.get("WEIR_E2E_BROWSER_PORT", "22342"))
        if not 1024 <= browser_port <= 65535 or browser_port in (22340, 22341, 22443, 22444):
            raise ValueError("WEIR_E2E_BROWSER_PORT must be an isolated port (default 22342)")
        browser_dir = OUT / ("browser-" + run.id)
        browser_dir.mkdir()
        console = run.path / "console"
        shutil.copytree(ROOT / "apps/weir/static", console)
        cmd += ["-p", f"127.0.0.1:{browser_port}:19810", "-v", f"{console}:/console:ro",
                "-p", "127.0.0.1:22443:22443/tcp", "-p", "127.0.0.1:22443:22443/udp", "-p", "127.0.0.1:22444:22444/udp",
                "-v", f"{browser_dir}:/browser", "-e", "WEIR_E2E_BROWSER_DIR=/browser", "-e", "WEIR_E2E_RUN_ID=" + run.id,
                "-e", "WEIR_E2E_NODE_BROWSER=1", "-e", f"WEIR_E2E_BROWSER_ORIGIN=http://127.0.0.1:{browser_port}", "-e", "WEIR_E2E_CONSOLE=/console",
                "-e", "WEIR_E2E_BROWSER_SECONDS=" + os.environ.get("WEIR_E2E_BROWSER_SECONDS", "900")]
    if firewall:
        cmd += ["-e", "WEIR_E2E_FIREWALL=" + firewall]
    if network:
        cmd += ["--network", network, "-e", "WEIR_E2E_PUBLIC_NETWORK=1"]
    if os.environ.get("WEIR_E2E_SUBSCRIPTION_DIR"):
        fixture = pathlib.Path(os.environ["WEIR_E2E_SUBSCRIPTION_DIR"]).resolve(strict=True)
        cmd += ["-v", f"{fixture}:/subscription", "-e", "WEIR_E2E_SUBSCRIPTION_DIR=/subscription",
                "-e", "WEIR_E2E_PROBE_URL=" + os.environ.get("WEIR_E2E_PROBE_URL", "")]
    if os.environ.get("WEIR_E2E_PROTOCOL_FIXTURES"):
        fixture = pathlib.Path(os.environ["WEIR_E2E_PROTOCOL_FIXTURES"]).resolve(strict=True)
        cmd += ["-v", f"{fixture}:/protocols:ro", "-e", "WEIR_E2E_PROTOCOL_FIXTURES=/protocols"]
        for path in sorted(fixture.rglob('*')):
            if path.is_file():
                run.artifact('protocols/' + path.relative_to(fixture).as_posix(), path)
    if MIHOMO.is_file():
        cmd += ["-v", f"{MIHOMO}:/cores/mihomo:ro", "-e", "WEIR_E2E_MIHOMO=/cores/mihomo"]
    if GEOIP.is_file():
        cmd += ["-v", f"{GEOIP}:/cores/Country.mmdb:ro", "-e", "WEIR_E2E_GEOIP=/cores/Country.mmdb"]
    # 精简镜像没有 CA 证书库（安装器会补装）；Gate 实测 REALITY 目标等访问外网的测试借用本机的证书库，只读。
    if CA_BUNDLE.is_file():
        cmd += ["-v", f"{CA_BUNDLE}:/etc/ssl/certs/ca-certificates.crt:ro", "-e", "SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt"]
    cmd += [image, "/t/e2e.test"]
    for name, path, mounted, arg in (("xray", XRAY, "/cores/xray", "version"),
                                     ("sing-box", SINGBOX, "/cores/sing-box", "version"),
                                     ("mihomo", MIHOMO, "/cores/mihomo", "-v")):
        step = name + "-version"
        run.run(step, [*cmd[:-1], mounted, arg], check=True)
        run.artifact(name, path, step)
    run.artifact("weir", binaries / "weir", "build-program")
    run.artifact("e2e.test", binaries / "e2e.test", "build-tests")
    # 系统根证书池会在进程内缓存；各用例的临时 CA、DNS 与网络状态必须隔离。
    listed = run.run("list", [*cmd, "-test.list", top], text=True, encoding="utf-8", check=True)
    names = [line.strip() for line in listed.stdout.splitlines() if line.startswith("Test")]
    if not names:
        print(f"no tests matched {pattern}", file=sys.stderr)
        raise ValueError(f"no tests matched {pattern}")
    failed = []
    for name in names:
        selected = "^" + name + "$" + ("/" + children if separator else "")
        # Xray 会在一小时周期内重载证书；其他流程保持较短的独立超时。
        timeout = test_timeout(name)
        print(f"\n=== isolated {name} (timeout {timeout}) ===", flush=True)
        result = run.run(name, [*cmd, "-test.timeout", timeout, *args, "-test.run", selected], required_tests=[name],
                         recovery="Resolve the failure in this step's log and rerun deploy/e2e.py -run " + selected +
                         "; preserve the declared fixture environment. Snell/SSR requires the protocol runtime documented at docs/implementation.html#protocol-fixtures")
        if separator:
            matched = matched_subtest(name, children, run.steps[-1]['tests']['passed'])
            run.require(name + ' subtest selection', matched,
                        'Correct the subtest selector or prepare its fixture and rerun: ' + selected, kind='test')
        if result.returncode:
            failed.append(name)
    if os.environ.get("WEIR_E2E_NODE_BROWSER") == "1":
        import json
        receipt = browser_dir / "result.json"
        result = json.loads(receipt.read_text(encoding="utf-8")) if receipt.is_file() else {}
        required = ["https", "webrtc", "webtransport", "source-grant", "chain-subscription", "restore", "settings", "unauthorized"]
        checks = result.get("checks", {})
        valid_receipt = result.get("run_id") == run.id and isinstance(checks, dict)
        for name in required:
            run.require("browser interaction: " + name, valid_receipt and checks.get(name) == "passed",
                        "Run the " + name + " browser interaction against this fixture and retain assertions with its run ID; restart the fixture if expired. See docs/implementation.html#certificate", kind="interaction")
    if failed:
        print("failed: " + ", ".join(failed), file=sys.stderr)
    return int(bool(failed))


if __name__ == "__main__":
    with ScenarioRun(ROOT, OUT / "runs") as run:
        result = main(sys.argv[1:], run)
    sys.exit(result or run.exit_code)
