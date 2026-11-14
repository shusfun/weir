"""端到端测试：交叉编译 e2e 测试程序，在 debian 容器中用真实内核运行。

用法：python deploy/e2e.py [-run 正则]
需要 Docker 与 dist/cores 下的 Linux 版 Xray、mihomo 和带统计接口的 sing-box。
sing-box 夹具目录名 sing-box-1.14.3-stats 只是存放路径，不能当成二进制版本；测试从二进制的 version 输出登记缓存。
"""
import os
import pathlib
import subprocess
import sys

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


def main(argv):
    for f in (XRAY, SINGBOX, MIHOMO):
        if not f.is_file():
            raise SystemExit(f"missing {f}")
    OUT.mkdir(parents=True, exist_ok=True)
    env = dict(os.environ, GOOS="linux", GOARCH="amd64", CGO_ENABLED="0")
    subprocess.run(["go", "test", "-c", "-tags", "e2e", "-o", str(OUT / "e2e.test"), "./internal/e2e"], cwd=ROOT, env=env, check=True)
    # 单机合并为一个进程的验收要用真实的 weir 程序（中心 + 本机 Gate + 辅助程序）。
    subprocess.run(["go", "build", "-o", str(OUT / "weir"), "./apps/weir"], cwd=ROOT, env=env, check=True)
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
    cmd = ["docker", "run", "--rm", "--cap-add", "NET_ADMIN", "--cap-add", "NET_RAW", "--dns", "1.1.1.1", "--dns", "8.8.8.8",
           "-v", f"{OUT}:/t", "-v", f"{XRAY}:/cores/xray:ro", "-v", f"{SINGBOX}:/cores/sing-box:ro",
           "-e", "WEIR_E2E_XRAY=/cores/xray", "-e", "WEIR_E2E_SINGBOX=/cores/sing-box", "-e", "WEIR_E2E_BINARY=/t/weir"]
    if os.environ.get("WEIR_E2E_NODE_BROWSER") == "1":
        cmd += ["-p", "127.0.0.1:22341:19810", "-v", f"{ROOT / 'apps/weir/static'}:/console:ro",
                "-e", "WEIR_E2E_NODE_BROWSER=1", "-e", "WEIR_E2E_CONSOLE=/console",
                "-e", "WEIR_E2E_BROWSER_SECONDS=" + os.environ.get("WEIR_E2E_BROWSER_SECONDS", "900")]
    if os.environ.get("WEIR_E2E_NETWORK"):
        cmd += ["--network", os.environ["WEIR_E2E_NETWORK"], "-e", "WEIR_E2E_PUBLIC_NETWORK=1"]
    if os.environ.get("WEIR_E2E_SUBSCRIPTION_DIR"):
        fixture = pathlib.Path(os.environ["WEIR_E2E_SUBSCRIPTION_DIR"]).resolve(strict=True)
        cmd += ["-v", f"{fixture}:/subscription", "-e", "WEIR_E2E_SUBSCRIPTION_DIR=/subscription",
                "-e", "WEIR_E2E_PROBE_URL=" + os.environ.get("WEIR_E2E_PROBE_URL", "")]
    if MIHOMO.is_file():
        cmd += ["-v", f"{MIHOMO}:/cores/mihomo:ro", "-e", "WEIR_E2E_MIHOMO=/cores/mihomo"]
    if GEOIP.is_file():
        cmd += ["-v", f"{GEOIP}:/cores/Country.mmdb:ro", "-e", "WEIR_E2E_GEOIP=/cores/Country.mmdb"]
    # 精简镜像没有 CA 证书库（安装器会补装）；Gate 实测 REALITY 目标等访问外网的测试借用本机的证书库，只读。
    if CA_BUNDLE.is_file():
        cmd += ["-v", f"{CA_BUNDLE}:/etc/ssl/certs/ca-certificates.crt:ro", "-e", "SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt"]
    cmd += [IMAGE, "/t/e2e.test"]
    # 系统根证书池会在进程内缓存；各用例的临时 CA、DNS 与网络状态必须隔离。
    listed = subprocess.run([*cmd, "-test.list", top], capture_output=True, text=True, encoding="utf-8", check=True)
    names = [line.strip() for line in listed.stdout.splitlines() if line.startswith("Test")]
    if not names:
        print(f"no tests matched {pattern}", file=sys.stderr)
        return 1
    failed = []
    for name in names:
        selected = "^" + name + "$" + ("/" + children if separator else "")
        # Xray 会在一小时周期内重载证书；其他流程保持较短的独立超时。
        timeout = os.environ.get("WEIR_E2E_TIMEOUT", "80m" if name == "TestCertRenewalRealKernels" else "15m")
        print(f"\n=== isolated {name} (timeout {timeout}) ===", flush=True)
        result = subprocess.run([*cmd, "-test.timeout", timeout, *args, "-test.run", selected])
        if result.returncode:
            failed.append(name)
    if failed:
        print("failed: " + ", ".join(failed), file=sys.stderr)
    return int(bool(failed))


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
