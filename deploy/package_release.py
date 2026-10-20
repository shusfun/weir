"""Assemble a verified linux/amd64 release directory into checksums and an archive."""
import hashlib
import json
import os
import pathlib
import sys
import tarfile
import time


def package(version):
    root = pathlib.Path(f"dist/migate-{version}-linux-amd64")
    if not (root / "migate-control").is_file():
        raise SystemExit("release binaries are missing")
    (root / "BUILD.json").write_text(json.dumps({
        "version": version,
        "built_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    }), encoding="utf-8")
    source = root / "source.tar.gz"
    with tarfile.open(source, "w:gz") as bundle:
        for name in ("apps", "internal", "ent", "packages", "services", "deploy", "docs", "third_party", "go.mod", "go.sum", "go.work", "go.work.sum", "package.json", "pnpm-lock.yaml", "pnpm-workspace.yaml", "AGENTS.md", "VERSION", ".gitignore", ".gitattributes"):
            path = pathlib.Path(name)
            if not path.exists():
                continue
            for file in ([path] if path.is_file() else path.rglob("*")):
                if not file.is_file():
                    continue
                relative = file.as_posix()
                parts = set(file.parts)
                if "node_modules" in parts or "__pycache__" in parts or relative.startswith("docs/evidence") or relative.startswith("dist/"):
                    continue
                bundle.add(file, arcname=relative)
    lines = []
    for path in sorted(item for item in root.rglob("*") if item.is_file() and item.name != "SHA256SUMS"):
        digest = hashlib.sha256(path.read_bytes()).hexdigest()
        lines.append(f"{digest}  {path.relative_to(root).as_posix()}")
    (root / "SHA256SUMS").write_text("\n".join(lines) + "\n", encoding="utf-8")
    archive = pathlib.Path(f"dist/migate-{version}-linux-amd64.tar.gz")
    with tarfile.open(archive, "w:gz") as bundle:
        for path in sorted(item for item in root.rglob("*") if item.is_file()):
            bundle.add(path, arcname=path.relative_to(root).as_posix())
    digest = hashlib.sha256(archive.read_bytes()).hexdigest()
    pathlib.Path("dist/checksums.txt").write_text(f"{digest}  {archive.name}\n", encoding="utf-8")


if __name__ == "__main__":
    package(os.environ.get("VERSION") or sys.argv[1])
