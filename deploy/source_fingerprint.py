"""Hash deployable source inputs, excluding build output and acceptance evidence."""
import hashlib
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent


def fingerprint():
    files = []
    for dirname in ['apps', 'internal', 'ent', 'packages', 'services']:
        files.extend(p for p in (ROOT / dirname).rglob('*') if p.is_file() and
                     not any(x in p.parts for x in ['node_modules', 'static', 'dist', '.vite']))
    files.extend(ROOT / x for x in ['go.mod', 'go.sum', 'go.work', 'go.work.sum',
                                   'package.json', 'pnpm-lock.yaml', 'pnpm-workspace.yaml', 'deploy/install.sh', 'deploy/core-packages.json'])
    digest = hashlib.sha256()
    for path in sorted(set(files)):
        name = path.relative_to(ROOT).as_posix().encode()
        digest.update(len(name).to_bytes(4, 'big') + name)
        content = path.read_bytes()
        digest.update(len(content).to_bytes(8, 'big') + content)
    return digest.hexdigest()


if __name__ == '__main__':
    print(fingerprint())
