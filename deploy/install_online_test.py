import hashlib
import io
import os
import pathlib
import shutil
import stat
import subprocess
import tarfile
import tempfile
import textwrap
import unittest


def shell_program():
    if os.name != "nt":
        found = shutil.which("bash")
        if not found:
            raise unittest.SkipTest("bash is not installed")
        return found
    candidate = pathlib.Path(r"C:\Program Files\Git\bin\bash.exe")
    if candidate.exists():
        return str(candidate)
    found = shutil.which("bash")
    if found and "system32" not in found.lower():
        return found
    raise unittest.SkipTest("bash is not installed")


def archive(version):
    payload = io.BytesIO()
    members = {
        "install.sh": "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$INSTALL_LOG\"\n",
        "migate-control": "control",
        "migate-gate": "gate",
        "migate-manage": "manage",
        "VERSION": version + "\n",
    }
    sums = []
    with tarfile.open(fileobj=payload, mode="w:gz") as bundle:
        encoded = {name: data.encode() for name, data in members.items()}
        for name, data in encoded.items():
            sums.append(hashlib.sha256(data).hexdigest() + "  " + name)
        text = ("\n".join(sums) + "\n").encode()
        encoded["SHA256SUMS"] = text
        for name, data in encoded.items():
            info = tarfile.TarInfo(name)
            info.size = len(data)
            info.mode = 0o755
            bundle.addfile(info, io.BytesIO(data))
    body = payload.getvalue()
    name = f"migate-{version}-linux-amd64.tar.gz"
    checksum = hashlib.sha256(body).hexdigest() + "  " + name + "\n"
    return body, checksum, name


class OnlineInstallTests(unittest.TestCase):
    def setUp(self):
        self.root = pathlib.Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, self.root, True)
        self.body, self.checksum, self.name = archive("0.2.0")
        (self.root / self.name).write_bytes(self.body)
        (self.root / "checksums.txt").write_text(self.checksum, encoding="utf-8")
        (self.root / "latest.json").write_text('{"tag_name":"v0.2.0"}', encoding="utf-8")
        curl = self.root / "curl"
        curl.write_text(textwrap.dedent("""\
            #!/bin/sh
            out=""
            dump=""
            prev=""
            url=""
            for arg in "$@"; do
              if [ "$prev" = "-o" ]; then out=$arg; fi
              if [ "$prev" = "-D" ]; then dump=$arg; fi
              case "$arg" in
                https://*) url=$arg ;;
              esac
              prev=$arg
            done
            printf '%s\\n' "$url" >> "$CURL_LOG"
            printf '%s\\n' "$*" >> "$CURL_ARGV"
            if [ -n "$dump" ]; then : > "$dump"; fi
            # 模拟加速镜像：https://<mirror>/https://github.com/...
            case "$url" in
              https://*/https://github.com/*/migate-*-linux-amd64.tar.gz)
                mirror=${url#https://}; mirror=${mirror%%/*}
                case ",${DEAD_MIRRORS:-}," in *",$mirror,"*) exit 7 ;; esac
                case ",${BAD_MIRRORS:-}," in *",$mirror,"*) printf 'tampered' > "$out"; printf '200'; exit 0 ;; esac
                cp "$CURL_FIXTURE"/migate-*-linux-amd64.tar.gz "$out"
                printf '200'
                exit 0 ;;
            esac
            # 模拟直连附件过慢：首次直连压缩包按速度下限中止（curl 退出码 28）
            case "$url" in
              https://github.com/*/releases/download/*/migate-*-linux-amd64.tar.gz)
                if [ -n "${DIRECT_SLOW:-}" ] && [ ! -e "$DIRECT_SLOW" ]; then : > "$DIRECT_SLOW"; exit 28; fi ;;
            esac
            # 模拟下载挂起，供中断清理测试使用
            case "$url" in
              https://release-assets.githubusercontent.com/asset/migate-*)
                if [ -n "${HANG_MARK:-}" ]; then echo $$ > "$HANG_MARK.tmp"; mv "$HANG_MARK.tmp" "$HANG_MARK"; exec sleep 30; fi ;;
            esac
            # 模拟 GitHub 附件 302 到带签名查询串的存储地址
            case "$url" in
              https://github.com/*/releases/download/*)
                file=${url##*/}
                printf 'HTTP/1.1 302 Found\\r\\nLocation: https://release-assets.githubusercontent.com/asset/%s?sp=r&sig=abc%%3D\\r\\n\\r\\n' "$file" > "$dump"
                printf '302'
                exit 0 ;;
            esac
            case "$url" in
              */releases/latest) cp "$CURL_FIXTURE/latest.json" "$out" ;;
              *'/releases?per_page=20') cp "$CURL_FIXTURE/latest.json" "$out" ;;
              */asset/checksums.txt\\?*) cp "$CURL_FIXTURE/checksums.txt" "$out" ;;
              */asset/migate-*-linux-amd64.tar.gz\\?*) cp "$CURL_FIXTURE"/migate-*-linux-amd64.tar.gz "$out" ;;
              *) printf 'unexpected\\n' >> "$CURL_LOG"; exit 1 ;;
            esac
            printf '200'
            """), encoding="utf-8")
        curl.chmod(curl.stat().st_mode | stat.S_IEXEC)
        self.install_log = self.root / "install.log"
        self.env = os.environ.copy()
        self.env.update({
            "PATH": self.root.as_posix() + ":" + self.env.get("PATH", ""),
            "CURL_LOG": (self.root / "curl.log").as_posix(),
            "CURL_ARGV": (self.root / "curl-argv.log").as_posix(),
            "CURL_FIXTURE": self.root.as_posix(),
            "INSTALL_LOG": self.install_log.as_posix(),
            "MIGATE_NONINTERACTIVE": "1",
            "MIGATE_ALLOW_NON_ROOT": "1",
            "MIGATE_GITHUB_TOKEN": "sentinel-token-value",
            "MIGATE_CURL": (self.root / "curl").as_posix(),
            "TMPDIR": self.root.as_posix(),
        })

    def run_script(self, *args, tty=None):
        env = self.env.copy()
        if tty is not None:
            path = self.root / "tty"
            path.write_text(tty, encoding="utf-8", newline="\n")
            env["MIGATE_DEV_TTY"] = path.as_posix()
            env.pop("MIGATE_NONINTERACTIVE", None)
        return subprocess.run([shell_program(), str(pathlib.Path(__file__).resolve().parent / "install-online.sh"), *args], text=True, capture_output=True, env=env)

    def test_checksum_match_runs_package_installer(self):
        completed = self.run_script("control", "127.0.0.1:19810", "https://example.test")
        self.assertEqual(completed.returncode, 0, completed.stderr)
        recorded = self.install_log.read_text(encoding="utf-8")
        # 旧版公共 URL 参数被忽略，只传监听地址
        self.assertIn("control 127.0.0.1:19810", recorded)
        self.assertNotIn("example.test", recorded)
        self.assertNotIn("sentinel-token-value", completed.stdout + completed.stderr)
        self.assertNotIn("sentinel-token-value", (self.root / "curl-argv.log").read_text(encoding="utf-8"))

    def test_release_asset_redirect_to_signed_url(self):
        completed = self.run_script("gate")
        self.assertEqual(completed.returncode, 0, completed.stderr)
        urls = (self.root / "curl.log").read_text(encoding="utf-8")
        self.assertIn("https://release-assets.githubusercontent.com/asset/checksums.txt?sp=r&sig=abc%3D", urls)
        self.assertNotIn("sig=abc", completed.stdout + completed.stderr)
        # 令牌配置不得转发到签名存储主机
        for line in (self.root / "curl-argv.log").read_text(encoding="utf-8").splitlines():
            # 每次下载都必须带卡住检测，避免无限等待
            self.assertIn("--speed-limit", line)
            self.assertIn("--speed-time", line)
            if "release-assets.githubusercontent.com" in line:
                self.assertNotIn("--config", line)
            elif "api.github.com" in line:
                self.assertIn("--config", line)

    def test_query_rejected_outside_signed_hosts(self):
        (self.root / "latest.json").write_text('{"tag_name":"v0.2.0"}', encoding="utf-8")
        curl = self.root / "curl"
        curl.write_text("#!/bin/sh\nfor a in \"$@\"; do case \"$a\" in -D) d=1 ;; *) [ -n \"$d\" ] && { printf 'Location: https://github.com/x?y=1\\r\\n' > \"$a\"; d=; } ;; esac; done\nprintf '302'\n", encoding="utf-8")
        completed = self.run_script("gate")
        self.assertNotEqual(completed.returncode, 0)
        self.assertIn("下载地址被拒绝", completed.stderr)
        self.assertFalse(self.install_log.exists())

    def test_list_versions_allows_fixed_page_query(self):
        completed = self.run_script("list-versions")
        self.assertEqual(completed.returncode, 0, completed.stderr)
        self.assertIn("v0.2.0", completed.stdout)

    def test_interactive_prompts_reach_terminal(self):
        completed = self.run_script(tty="1\n1\n\n\n")
        self.assertEqual(completed.returncode, 0, completed.stderr)
        self.assertIn("请输入选项 [1]: ", completed.stderr)
        self.assertIn("监听地址 [127.0.0.1]: ", completed.stderr)
        argv = (self.root / "curl-argv.log").read_text(encoding="utf-8").splitlines()
        self.assertTrue(any("--progress-bar" in line and "release-assets" in line for line in argv))
        self.assertFalse(any("--progress-bar" in line and "api.github.com" in line for line in argv))
        self.assertIn("control 127.0.0.1:19810", self.install_log.read_text(encoding="utf-8"))

    def test_slow_direct_switches_to_mirror(self):
        self.env["DIRECT_SLOW"] = (self.root / "direct-slow").as_posix()
        self.env["DEAD_MIRRORS"] = "gh-proxy.com"
        self.env["BAD_MIRRORS"] = "ghfast.top"
        completed = self.run_script("gate")
        self.assertEqual(completed.returncode, 0, completed.stderr)
        self.assertTrue(self.install_log.exists())
        urls = (self.root / "curl.log").read_text(encoding="utf-8")
        # 不可用与校验不一致的镜像依次跳过，第三个镜像成功后不再尝试后续镜像
        self.assertIn("https://gh-proxy.com/https://github.com/", urls)
        self.assertIn("https://ghfast.top/https://github.com/", urls)
        self.assertIn("https://ghproxy.net/https://github.com/", urls)
        self.assertNotIn("gh.llkk.cc", urls)
        self.assertIn("校验不一致", completed.stdout)
        # 校验和只从 GitHub 获取，令牌不发给镜像
        for line in (self.root / "curl-argv.log").read_text(encoding="utf-8").splitlines():
            if "https://ghproxy.net/" in line or "https://ghfast.top/" in line:
                self.assertNotIn("--config", line)
                self.assertNotIn("checksums.txt", line)

    def test_all_mirrors_fail_falls_back_to_direct(self):
        self.env["DIRECT_SLOW"] = (self.root / "direct-slow").as_posix()
        self.env["DEAD_MIRRORS"] = "gh-proxy.com,ghfast.top,ghproxy.net,gh.llkk.cc"
        completed = self.run_script("gate")
        self.assertEqual(completed.returncode, 0, completed.stderr)
        self.assertIn("继续直连", completed.stdout)
        self.assertTrue(self.install_log.exists())

    def test_tampered_archive_from_every_source_does_not_install(self):
        self.env["DIRECT_SLOW"] = (self.root / "direct-slow").as_posix()
        self.env["BAD_MIRRORS"] = "gh-proxy.com,ghfast.top,ghproxy.net,gh.llkk.cc"
        (self.root / self.name).write_bytes(b"tampered")
        completed = self.run_script("gate")
        self.assertNotEqual(completed.returncode, 0)
        self.assertFalse(self.install_log.exists())

    def test_mirrors_off_skips_mirrors(self):
        self.env["DIRECT_SLOW"] = (self.root / "direct-slow").as_posix()
        self.env["MIGATE_GITHUB_MIRRORS"] = "off"
        completed = self.run_script("gate")
        self.assertEqual(completed.returncode, 0, completed.stderr)
        urls = (self.root / "curl.log").read_text(encoding="utf-8")
        self.assertNotIn("/https://github.com/", urls)

    def test_invalid_mirror_rejected_before_download(self):
        self.env["MIGATE_GITHUB_MIRRORS"] = "evil.example/path?x=1"
        completed = self.run_script("gate")
        self.assertNotEqual(completed.returncode, 0)
        self.assertIn("MIGATE_GITHUB_MIRRORS", completed.stderr)
        self.assertFalse((self.root / "curl.log").exists())

    def test_terminate_during_download_removes_workdir(self):
        mark = (self.root / "hang.pid").as_posix()
        script = (pathlib.Path(__file__).resolve().parent / "install-online.sh").as_posix()
        wrapper = textwrap.dedent(f"""\
            HANG_MARK='{mark}' "$BASH" '{script}' gate >/dev/null 2>&1 &
            pid=$!
            for i in $(seq 1 100); do [ -s '{mark}' ] && break; sleep 0.1; done
            kill -TERM "$pid"
            kill -TERM "$(cat '{mark}')" 2>/dev/null
            wait "$pid"
            echo "status=$?"
            """)
        completed = subprocess.run([shell_program(), "-c", wrapper], text=True, capture_output=True, env=self.env, timeout=60)
        self.assertIn("status=143", completed.stdout, completed.stderr)
        self.assertEqual(list(self.root.glob("migate-install-*")), [])
        self.assertFalse(self.install_log.exists())

    def test_non_local_listen_warns_plain_http(self):
        completed = self.run_script("control", "0.0.0.0:19810")
        self.assertEqual(completed.returncode, 0, completed.stderr)
        self.assertIn("明文 HTTP", completed.stdout)
        self.assertIn("control 0.0.0.0:19810", self.install_log.read_text(encoding="utf-8"))

    def test_bad_checksum_does_not_install(self):
        (self.root / "checksums.txt").write_text("0" * 64 + "  " + self.name + "\n", encoding="utf-8")
        completed = self.run_script("gate")
        self.assertNotEqual(completed.returncode, 0)
        self.assertFalse(self.install_log.exists())

    def test_bad_version_does_not_download(self):
        completed = self.run_script("--version", "0.2.0-rc1", "control")
        self.assertNotEqual(completed.returncode, 0)
        self.assertFalse((self.root / "curl.log").exists())

    def test_uninstall_without_confirmation_does_not_run(self):
        completed = self.run_script("uninstall", "control")
        self.assertNotEqual(completed.returncode, 0)
        self.assertIn("没有终端", completed.stderr)

    def test_interactive_cancel_does_not_uninstall(self):
        completed = self.run_script("uninstall", "control", tty="1\nn\n")
        self.assertEqual(completed.returncode, 0, completed.stderr)
        self.assertIn("卸载已取消", completed.stdout + completed.stderr)

    def test_interactive_retain_omits_destructive_flags(self):
        manifest = self.root / "manifest.json"
        manifest.write_text("{}", encoding="utf-8")
        manager = self.root / "migate-manage"
        manager.write_text("#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$INSTALL_LOG\"\n", encoding="utf-8")
        manager.chmod(manager.stat().st_mode | stat.S_IEXEC)
        self.env["MIGATE_MANIFEST"] = manifest.as_posix()
        self.env["MIGATE_MANAGE"] = manager.as_posix()
        completed = self.run_script("uninstall", "gate", tty="1\ny\nn\n")
        self.assertEqual(completed.returncode, 0, completed.stderr)
        recorded = self.install_log.read_text(encoding="utf-8")
        self.assertIn("--confirm gate:uninstall", recorded)
        self.assertNotIn("--purge", recorded)
        self.assertNotIn("--with-cores", recorded)


if __name__ == "__main__":
    unittest.main()
