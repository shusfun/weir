import hashlib
import io
import json
import tarfile
import unittest

from export_public import manifest, snapshot, write_tree
from package_release import checksums, package
from public_test_support import GitFixture, commit, init, working_directory, write


class PackageTests(GitFixture):
    def public_source(self):
        self.export = self.root / "export"
        _, self.files = snapshot(self.source)
        write_tree(self.files, self.export)
        init(self.export)
        self.public_commit = commit(self.export)
        for arch in ("amd64", "arm64"):
            root = self.export / f"dist/weir-1.0.0-linux-{arch}"
            root.mkdir(parents=True)
            write(root, "weir", b"fixture binary " + arch.encode())
            for target, source in (("install.sh", "deploy/install.sh"), ("sources.json", "deploy/sources.json"), ("version.py", "deploy/version.py"), ("VERSION", "VERSION")):
                write(root, target, self.files[source][1])

    def test_both_archives_have_exact_public_source_and_valid_digests(self):
        self.public_source()
        write(self.export, "apps/untracked-secret.txt", "not committed")
        with working_directory(self.export):
            archives = [package("1.0.0", arch) for arch in ("amd64", "arm64")]
            checksums(archives)
            for path in archives:
                data = path.read_bytes()
                with tarfile.open(fileobj=io.BytesIO(data), mode="r:gz") as outer:
                    sums = outer.extractfile("SHA256SUMS").read().decode()
                    for line in sums.splitlines():
                        digest, name = line.split("  ", 1)
                        self.assertEqual(hashlib.sha256(outer.extractfile(name).read()).hexdigest(), digest)
                    info = json.loads(outer.extractfile("BUILD.json").read())
                    self.assertEqual(info["revision"], self.public_commit)
                    self.assertEqual(info["source_digest"], manifest(self.files)["digest"])
                    with tarfile.open(fileobj=io.BytesIO(outer.extractfile("source.tar.gz").read()), mode="r:gz") as source:
                        self.assertEqual(set(source.getnames()), set(self.files))
                        for name, (_, original) in self.files.items():
                            self.assertEqual(source.extractfile(name).read(), original)
                        self.assertIn("internal/center/dshskills/product/SKILL.md", source.getnames())
                arch = info["arch"]
                self.assertEqual(package("1.0.0", arch).read_bytes(), data)
            for line in (self.export / "dist/checksums.txt").read_text().splitlines():
                digest, name = line.split("  ", 1)
                self.assertEqual(hashlib.sha256((self.export / "dist" / name).read_bytes()).hexdigest(), digest)

    def test_packager_rejects_private_checkout(self):
        with working_directory(self.source), self.assertRaisesRegex(ValueError, "private file"):
            package("1.0.0", "amd64")

    def test_unexpected_staging_file_and_modified_install_script_rejected(self):
        self.public_source()
        staging = self.export / "dist/weir-1.0.0-linux-amd64"
        write(staging, "private.txt", "not allowed")
        with working_directory(self.export), self.assertRaisesRegex(ValueError, "unexpected file"):
            package("1.0.0", "amd64")
        (staging / "private.txt").unlink()
        write(staging, "install.sh", "different script")
        with working_directory(self.export), self.assertRaisesRegex(ValueError, "differs from public source"):
            package("1.0.0", "amd64")


if __name__ == "__main__":
    unittest.main()
