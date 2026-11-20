import hashlib
import io
import pathlib
import sqlite3
import tarfile
import tempfile
import unittest

from check_upgrade import CenterFixture, extract_binary, free_fixture_subnet


class UpgradeTests(unittest.TestCase):
    def test_upgrade_subnet_does_not_overlap_existing_test_networks(self):
        network = free_fixture_subnet(["192.0.2.0/24", "198.51.100.0/25", "2001:db8::/32"])
        self.assertEqual(str(network), "198.51.100.128/27")

    def test_checks_archive_before_extracting_release(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            archive, checksums, binary = root / "release.tar.gz", root / "checksums.txt", root / "weir"
            content = b"test executable"
            with tarfile.open(archive, "w:gz") as package:
                entry = tarfile.TarInfo("./weir")
                entry.size = len(content)
                package.addfile(entry, io.BytesIO(content))
            checksums.write_text("invalid  release.tar.gz\n")
            with self.assertRaisesRegex(ValueError, "checksum mismatch"):
                extract_binary(archive, checksums, binary)
            self.assertFalse(binary.exists())
            digest = hashlib.sha256(archive.read_bytes()).hexdigest()
            checksums.write_text(f"{digest}  release.tar.gz\n")
            extract_binary(archive, checksums, binary)
            self.assertEqual(binary.read_bytes(), content)

    def test_sqlite_backup_preserves_committed_wal_data(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            source, backup = root / "source.db", root / "backup.db"
            db = sqlite3.connect(source)
            try:
                db.execute("PRAGMA journal_mode=WAL")
                db.execute("CREATE TABLE fixture (id INTEGER PRIMARY KEY, label TEXT)")
                db.execute("INSERT INTO fixture VALUES (?, ?)", (42, "retained"))
                db.commit()
                CenterFixture(root, "sqlite", "seed").sqlite_backup(source, backup)
                restored = sqlite3.connect(backup)
                try:
                    self.assertEqual(restored.execute("SELECT * FROM fixture").fetchall(), [(42, "retained")])
                finally:
                    restored.close()
            finally:
                db.close()
