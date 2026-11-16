import hashlib
import io
import pathlib
import tarfile
import tempfile
import unittest
import zipfile
from unittest.mock import patch

import prepare_protocols as fixtures


class ProtocolFixtureTests(unittest.TestCase):
    def archive(self, member=None):
        body = io.BytesIO()
        with tarfile.open(fileobj=body, mode='w:gz') as archive:
            root = tarfile.TarInfo('shadowsocksr-' + fixtures.SSR_COMMIT)
            root.type = tarfile.DIRTYPE
            archive.addfile(root)
            if member is None:
                member = tarfile.TarInfo(root.name + '/shadowsocks/server.py')
                member.size = 3
            archive.addfile(member, io.BytesIO(b'abc') if member.isfile() else None)
        return body.getvalue()

    def download(self, ssr):
        body = io.BytesIO()
        with zipfile.ZipFile(body, 'w') as archive:
            archive.writestr('snell-server', b'fixture')
        return lambda url, digest: body.getvalue() if url == fixtures.SNELL_URL else ssr

    def test_root_directory_and_source_are_extracted(self):
        with tempfile.TemporaryDirectory() as destination:
            fixtures.prepare(destination, self.download(self.archive()))
            self.assertEqual((pathlib.Path(destination) / 'ssr/shadowsocks/server.py').read_bytes(), b'abc')

    def test_untrusted_archive_paths_and_links_are_rejected(self):
        prefix = 'shadowsocksr-' + fixtures.SSR_COMMIT + '/'
        for name, kind in [(prefix + '../escape', tarfile.REGTYPE),
                           (prefix + '/escape', tarfile.REGTYPE),
                           (prefix + '..\\escape', tarfile.REGTYPE),
                           (prefix + 'C:/escape', tarfile.REGTYPE),
                           ('another-root/file', tarfile.REGTYPE),
                           (prefix + 'link', tarfile.SYMTYPE),
                           (prefix + 'hardlink', tarfile.LNKTYPE),
                           (prefix + 'device', tarfile.CHRTYPE)]:
            with self.subTest(name=name, kind=kind), tempfile.TemporaryDirectory() as destination:
                member = tarfile.TarInfo(name)
                member.type = kind
                member.linkname = '/tmp/escape'
                with self.assertRaises(ValueError):
                    fixtures.prepare(destination, self.download(self.archive(member)))

    def test_download_digest_and_size_are_checked(self):
        for body, digest in [(b'bad', '0' * 64), (b'x' * (8 * 1024 * 1024 + 1), '')]:
            with patch.object(fixtures.urllib.request, 'urlopen', return_value=io.BytesIO(body)):
                with self.assertRaises(ValueError):
                    fixtures.fetch('https://fixture.test/server', digest)
        with patch.object(fixtures.urllib.request, 'urlopen', return_value=io.BytesIO(b'ok')):
            self.assertEqual(fixtures.fetch('https://fixture.test/server', hashlib.sha256(b'ok').hexdigest()), b'ok')


if __name__ == '__main__':
    unittest.main()
