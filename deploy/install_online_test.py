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
            : > "$dump"
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
        self.assertIn("control 127.0.0.1:19810 https://example.test", self.install_log.read_text(encoding="utf-8"))
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
        self.assertIn("control 127.0.0.1:19810", self.install_log.read_text(encoding="utf-8"))

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
