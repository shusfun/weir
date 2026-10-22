import hashlib
import json
import sys
import io
import os
import pathlib
import shutil
import subprocess
import tarfile
import tempfile
import unittest

import release_fetch


def shell_program():
    if os.name != "nt":
        found = shutil.which("sh")
        if not found:
            raise unittest.SkipTest("sh is not installed")
        return found
    candidate = pathlib.Path(r"C:\Program Files\Git\bin\sh.exe")
    if candidate.exists():
        return str(candidate)
    found = shutil.which("sh")
    if found and "system32" not in found.lower():
        return found
    raise unittest.SkipTest("sh is not installed")


def archive_for(version, binary=b"migate-binary"):
    sums = []
    payload = io.BytesIO()
    with tarfile.open(fileobj=payload, mode="w:gz") as bundle:
        for name, data in (("migate", binary), ("install.sh", b"#!/bin/sh\nexit 0\n")):
            info = tarfile.TarInfo(name)
            info.size = len(data)
            bundle.addfile(info, io.BytesIO(data))
            sums.append(hashlib.sha256(data).hexdigest() + "  " + name)
        text = ("\n".join(sums) + "\n").encode()
        info = tarfile.TarInfo("SHA256SUMS")
        info.size = len(text)
        bundle.addfile(info, io.BytesIO(text))
    archive = payload.getvalue()
    name = release_fetch.archive_name(version)
    checksum = hashlib.sha256(archive).hexdigest() + "  " + name + "\n"
    return archive, checksum


class ReleaseFetchTests(unittest.TestCase):
    def test_url_policy(self):
        good = "https://github.com/shusfun/MiSub/releases/download/v0.2.0/migate-0.2.0-linux-amd64.tar.gz"
        release_fetch.check_url(good)
        release_fetch.check_url(
            "https://release-assets.githubusercontent.com/github-production-release-asset/file",
            allow_query=True,
        )
        release_fetch.check_url(
            "https://objects.githubusercontent.com/github-production-release-asset/file?name=migate.tar.gz",
            allow_query=True,
        )
        for bad in (
            "http://github.com/shusfun/MiSub/releases/download/v0.2.0/migate-0.2.0-linux-amd64.tar.gz",
            "https://evil.test/migate-0.2.0-linux-amd64.tar.gz",
            good + "?token=secret",
            "https://user:secret@github.com/shusfun/MiSub/releases/download/v0.2.0/file",
        ):
            with self.assertRaises(ValueError):
                release_fetch.check_url(bad)

    def test_bad_checksum_writes_nothing(self):
        archive, checksum = archive_for("0.2.0")
        root = pathlib.Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, root)
        checksum_path = root / "checksums.txt"
        archive_path = root / "archive.tar.gz"
        checksum_path.write_text(checksum.replace(hashlib.sha256(archive).hexdigest(), "0" * 64), encoding="utf-8")
        archive_path.write_bytes(archive)
        output = root / "gate-release"
        completed = subprocess.run(
            ["python", "release_fetch.py", "verify-local", "0.2.0", str(checksum_path), str(archive_path), str(output)],
            cwd=pathlib.Path(__file__).resolve().parent,
            text=True,
            capture_output=True,
        )
        self.assertNotEqual(completed.returncode, 0)
        self.assertFalse((output / "manifest.json").exists())

    def test_matching_checksum_writes_manifest(self):
        archive, checksum = archive_for("0.2.0")
        root = pathlib.Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, root)
        checksum_path = root / "checksums.txt"
        archive_path = root / "archive.tar.gz"
        checksum_path.write_text(checksum, encoding="utf-8")
        archive_path.write_bytes(archive)
        output = root / "gate-release"
        subprocess.run(
            ["python", "release_fetch.py", "verify-local", "0.2.0", str(checksum_path), str(archive_path), str(output)],
            cwd=pathlib.Path(__file__).resolve().parent,
            check=True,
        )
        self.assertTrue((output / "manifest.json").exists())
        self.assertTrue((output / "migate").exists())
        manifest = json.loads((output / "manifest.json").read_text(encoding="utf-8"))
        self.assertEqual(set(manifest), {"version", "sha256"})


class UpdateScriptTests(unittest.TestCase):
    def test_failed_fetch_does_not_install_or_keep_gate_files(self):
        root = pathlib.Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, root)
        fetcher = root / "fetch.py"
        fetcher.write_text("import sys\nsys.exit(1)\n", encoding="utf-8")
        token = root / "token"
        token.write_text("sentinel-token-value", encoding="utf-8")
        record = root / "install.log"
        release = root / "gate-release"
        release.mkdir()
        (release / "old").write_text("stale", encoding="utf-8")
        env = os.environ.copy()
        env.update({
            "MIGATE_RELEASE_FETCH": fetcher.as_posix(),
            "MIGATE_GITHUB_TOKEN_FILE": token.as_posix(),
            "MIGATE_GATE_RELEASE_DIR": release.as_posix(),
            "MIGATE_RELEASE_STATUS": (root / "status.json").as_posix(),
            "MIGATE_INSTALL_RECORD": record.as_posix(),
            "MIGATE_PYTHON": sys.executable,
        })
        completed = subprocess.run([shell_program(), str(pathlib.Path(__file__).resolve().parent / "github-update.sh"), "v0.2.0"], text=True, capture_output=True, env=env)
        self.assertNotEqual(completed.returncode, 0)
        self.assertFalse(record.exists())
        self.assertTrue((release / "old").exists())
        self.assertNotIn("sentinel-token-value", completed.stdout + completed.stderr)
        status = json.loads((root / "status.json").read_text(encoding="utf-8"))
        self.assertEqual(status["status"], "failed")
        self.assertNotIn("/", status["message"])

    def test_verified_fetch_records_control_install(self):
        root = pathlib.Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, root)
        fetcher = root / "fetch.py"
        fetcher.write_text("import sys\nsys.exit(0)\n", encoding="utf-8")
        record = root / "install.log"
        env = os.environ.copy()
        env.update({
            "MIGATE_RELEASE_FETCH": fetcher.as_posix(),
            "MIGATE_GATE_RELEASE_DIR": (root / "gate-release").as_posix(),
            "MIGATE_RELEASE_STATUS": (root / "status.json").as_posix(),
            "MIGATE_INSTALL_RECORD": record.as_posix(),
            "MIGATE_GITHUB_TOKEN_FILE": (root / "missing-token").as_posix(),
            "MIGATE_PYTHON": sys.executable,
        })
        subprocess.run([shell_program(), str(pathlib.Path(__file__).resolve().parent / "github-update.sh"), "v0.2.0"], check=True, env=env, text=True, capture_output=True)
        self.assertEqual(record.read_text(encoding="utf-8").strip(), "control")
        status = json.loads((root / "status.json").read_text(encoding="utf-8"))
        self.assertEqual(status["status"], "installing")

    def test_apply_request_uses_saved_tag_and_removes_file(self):
        root = pathlib.Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, root)
        fetcher = root / "fetch.py"
        fetcher.write_text(
            "import pathlib, sys\n"
            "root = pathlib.Path(sys.argv[5])\n"
            "root.mkdir(parents=True, exist_ok=True)\n"
            "(root / 'seen-tag').write_text(sys.argv[3], encoding='utf-8')\n",
            encoding="utf-8",
        )
        request = root / "request.json"
        request.write_text('{"tag":"v0.2.0"}', encoding="utf-8")
        env = os.environ.copy()
        env.update({
            "MIGATE_RELEASE_FETCH": fetcher.as_posix(),
            "MIGATE_RELEASE_REQUEST": request.as_posix(),
            "MIGATE_GATE_RELEASE_DIR": (root / "gate-release").as_posix(),
            "MIGATE_RELEASE_STATUS": (root / "status.json").as_posix(),
            "MIGATE_INSTALL_RECORD": (root / "install.log").as_posix(),
            "MIGATE_GITHUB_TOKEN_FILE": (root / "missing-token").as_posix(),
            "MIGATE_PYTHON": sys.executable,
        })
        subprocess.run([shell_program(), str(pathlib.Path(__file__).resolve().parent / "github-update.sh"), "--apply-request"], check=True, env=env, text=True, capture_output=True)
        self.assertFalse(request.exists())
        self.assertEqual((root / "gate-release" / "seen-tag").read_text(encoding="utf-8"), "v0.2.0")


if __name__ == "__main__":
    unittest.main()
