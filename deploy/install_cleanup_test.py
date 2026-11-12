"""Exercise installation retention on a real Linux filesystem with isolated services."""
import hashlib
import os
import pathlib
import subprocess
import tempfile
import unittest

if os.name == "posix":
    import fcntl


@unittest.skipUnless(os.name == "posix" and os.geteuid() == 0, "Linux root container required")
class InstallCleanupTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="weir-release-test-")
        self.addCleanup(self.temp.cleanup)
        self.root = pathlib.Path(self.temp.name)
        self.base = self.root / "opt/weir"
        self.script = pathlib.Path(__file__).with_name("install.sh").read_text(encoding="utf-8")
        for path in ("/usr/local/lib/weir", "/usr/local/bin/weir", "/etc/systemd/system", "/var/lib/weir", "/etc/weir", "/opt/weir", "/run/lock"):
            self.script = self.script.replace(path, str(self.root / path.lstrip("/")))
        for p in (self.root / "etc/systemd/system", self.root / "usr/local/bin", self.root / "bin"):
            p.mkdir(parents=True, exist_ok=True)
        systemctl = self.root / "bin/systemctl"
        systemctl.write_text('#!/bin/sh\ncase "$*" in *restart*) [ "${FAIL_SERVICE:-0}" != 1 ] || exit 1;; esac\nexit 0\n', encoding="utf-8")
        systemctl.chmod(0o755)
        self.env = dict(os.environ, PATH=str(systemctl.parent) + ":" + os.environ["PATH"], WEIR_UPDATE_ID="install-cleanup-test")

    def package(self, version, extra=None):
        p = self.root / ("package-" + version)
        p.mkdir(exist_ok=True)
        files = {
            "weir": '#!/bin/sh\n[ "${FAIL_HEALTH:-0}" != 1 ]\n',
            "install.sh": self.script,
            "VERSION": version + "\n",
            "source.tar.gz": "source",
        }
        files.update(extra or {})
        sums = []
        for name, body in files.items():
            target = p / name
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_text(body, encoding="utf-8")
            target.chmod(0o755)
            sums.append(hashlib.sha256(target.read_bytes()).hexdigest() + "  " + name)
        (p / "SHA256SUMS").write_text("\n".join(sums) + "\n", encoding="utf-8")
        (p / ("weir-" + version + "-linux-amd64.tar.gz")).write_text("outer download must not be installed", encoding="utf-8")
        return p

    def install(self, version, ok=True, **env):
        p = self.package(version)
        result = subprocess.run(["sh", str(p / "install.sh"), "gate"], env=dict(self.env, **env), text=True, capture_output=True)
        if (result.returncode == 0) != ok:
            self.fail(f"install {version}: {result.returncode}\n{result.stdout}\n{result.stderr}")
        return result

    def pointers(self):
        return tuple(os.readlink(self.base / name) if (self.base / name).is_symlink() else None for name in ("current", "previous"))

    def test_upgrade_reinstall_and_rollback(self):
        for version in ("1.0.0", "1.1.0", "1.2.0", "1.2.0"):
            result = self.install(version)
            self.assertIn("release cleanup update=install-cleanup-test", result.stdout)
        self.assertEqual(self.pointers(), ("releases/1.2.0", "releases/1.1.0"))
        self.assertEqual(sorted(p.name for p in (self.base / "releases").iterdir()), ["1.1.0", "1.2.0"])
        self.assertFalse(list((self.base / "releases").glob("*/weir-*-linux-*.tar.gz")))
        for version in ("1.1.0", "1.2.0"):
            target = self.base / "releases" / version
            self.assertTrue((target / "install.sh").is_file())
            self.assertTrue((target / "source.tar.gz").is_file())
            check = subprocess.run(["sha256sum", "-c", "SHA256SUMS"], cwd=target, capture_output=True)
            self.assertEqual(check.returncode, 0, check.stderr)
        self.install("1.1.0")
        self.assertEqual(self.pointers(), ("releases/1.1.0", "releases/1.2.0"))

    def test_failed_install_preserves_both_original_versions(self):
        self.install("1.0.0")
        self.install("1.1.0")
        self.install("1.2.0", ok=False, FAIL_SERVICE="1")
        self.assertEqual(self.pointers(), ("releases/1.1.0", "releases/1.0.0"))
        self.assertTrue((self.base / "releases/1.0.0/weir").is_file())
        self.install("1.3.0")
        self.assertEqual(sorted(p.name for p in (self.base / "releases").iterdir()), ["1.1.0", "1.3.0"])

    def test_in_use_release_is_preserved_until_next_install(self):
        self.install("1.0.0")
        process = subprocess.Popen(["sleep", "60"], cwd=self.base / "releases/1.0.0")
        try:
            self.install("1.1.0")
            self.install("1.2.0")
            self.assertTrue((self.base / "releases/1.0.0").is_dir())
        finally:
            process.terminate()
            process.wait(timeout=10)
        self.install("1.2.0")
        self.assertFalse((self.base / "releases/1.0.0").exists())

    def test_symlink_release_and_manifest_escape_are_rejected(self):
        self.install("1.0.0")
        outside = self.root / "outside"
        outside.mkdir()
        (outside / "keep").write_text("keep", encoding="utf-8")
        (self.base / "releases/1.1.0").symlink_to(outside, target_is_directory=True)
        self.install("1.1.0", ok=False)
        self.assertEqual((outside / "keep").read_text(), "keep")
        self.assertFalse((outside / "weir").exists())
        p = self.package("1.2.0", {"../escape": "bad"})
        result = subprocess.run(["sh", str(p / "install.sh"), "gate"], env=self.env, text=True, capture_output=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Invalid package manifest path", result.stderr)
        self.assertFalse((self.base / "releases/escape").exists())

    def test_concurrent_installer_is_rejected(self):
        self.install("1.0.0")
        with (self.root / "run/lock/weir-install.lock").open("w") as lock:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            result = self.install("1.1.0", ok=False)
            self.assertIn("Another installation is running", result.stderr)
        self.assertEqual(self.pointers(), ("releases/1.0.0", None))


if __name__ == "__main__":
    unittest.main()
