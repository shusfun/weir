"""Fetch pinned native protocol servers for isolated Linux e2e scenarios."""
import argparse
import hashlib
import io
import json
import pathlib
import tarfile
import urllib.request
import zipfile

SNELL_URL = 'https://dl.nssurge.com/snell/snell-server-v4.1.1-linux-amd64.zip'
SNELL_SHA256 = 'cc2271b79c7506888b34e651e8741b3aa7fc7d5f60aa65ef8bb096f3313a193b'
SSR_COMMIT = 'fd723a92c488d202b407323f0512987346944136'
SSR_URL = 'https://codeload.github.com/shadowsocksrr/shadowsocksr/tar.gz/' + SSR_COMMIT
SSR_SHA256 = '932fa17657ff21221fe78872229c2bbc66cac07290125613bd638eafab3e758f'


def fetch(url, digest):
    with urllib.request.urlopen(url, timeout=90) as response:
        body = response.read(8 * 1024 * 1024 + 1)
    if len(body) > 8 * 1024 * 1024 or hashlib.sha256(body).hexdigest() != digest:
        raise ValueError('Protocol fixture checksum mismatch: ' + url)
    return body


def prepare(destination, download=fetch):
    destination = pathlib.Path(destination)
    destination.mkdir(parents=True, exist_ok=True)
    with zipfile.ZipFile(io.BytesIO(download(SNELL_URL, SNELL_SHA256))) as archive:
        binary = destination / 'snell-server'
        binary.write_bytes(archive.read('snell-server'))
        binary.chmod(0o755)
    with tarfile.open(fileobj=io.BytesIO(download(SSR_URL, SSR_SHA256)), mode='r:gz') as archive:
        root = 'shadowsocksr-' + SSR_COMMIT
        prefix = root + '/'
        for member in archive.getmembers():
            if member.name.rstrip('/') == root and member.isdir():
                continue
            if not member.name.startswith(prefix):
                raise ValueError('Unexpected SSR archive root')
            relative = pathlib.PurePosixPath(member.name[len(prefix):])
            if relative.is_absolute() or '..' in relative.parts or any(c in member.name for c in ('\\', ':')) or not (member.isfile() or member.isdir()):
                raise ValueError('Unsafe SSR archive member')
            if member.isfile():
                target = destination / 'ssr' / relative
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_bytes(archive.extractfile(member).read())
    manifest = {'snell': {'version': '4.1.1', 'url': SNELL_URL, 'archive_sha256': SNELL_SHA256},
                'ssr': {'commit': SSR_COMMIT, 'url': SSR_URL, 'archive_sha256': SSR_SHA256}}
    (destination / 'sources.json').write_text(json.dumps(manifest, indent=2) + '\n', encoding='utf-8')
    print('Protocol fixtures ready: Snell 4.1.1; SSR ' + SSR_COMMIT)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--out', type=pathlib.Path, default=pathlib.Path(__file__).resolve().parent.parent / 'dist/cores/protocols')
    prepare(parser.parse_args().out)
