"""Package binaries with the exact, verified public Git source tree."""
import datetime
import gzip
import hashlib
import io
import json
import os
import pathlib
import sys
import tarfile

from export_public import event, git, manifest, snapshot, stage


ARCHES = ("amd64", "arm64")


def archive(path, files, timestamp):
    with open(path, "wb") as output, gzip.GzipFile(filename="", fileobj=output, mode="wb", mtime=0) as compressed:
        with tarfile.open(fileobj=compressed, mode="w", format=tarfile.PAX_FORMAT) as bundle:
            for name, (mode, data) in sorted(files.items()):
                info = tarfile.TarInfo(name)
                info.mode = 0o755 if mode == "100755" else 0o644
                info.mtime = timestamp
                info.size = len(data)
                bundle.addfile(info, io.BytesIO(data))


def package(version, arch):
    if arch not in ARCHES:
        raise ValueError("unsupported release architecture")
    with stage("package", version=version, arch=arch):
        commit, files = snapshot(".", "HEAD", strict=True)
        if files["VERSION"][1].decode().strip() != version:
            raise ValueError("package version does not match source VERSION")
        root = pathlib.Path(f"dist/weir-{version}-linux-{arch}")
        required = {"weir", "install.sh", "sources.json", "version.py", "VERSION"}
        if not all((root / name).is_file() and not (root / name).is_symlink() for name in required):
            raise ValueError("release files are missing")
        allowed = required | {"BUILD.json", "source.tar.gz", "SHA256SUMS"}
        if any(not item.is_file() or item.is_symlink() or item.name not in allowed for item in root.iterdir()):
            raise ValueError("unexpected file in release staging directory")
        for name, original in (("install.sh", "deploy/install.sh"), ("sources.json", "deploy/sources.json"), ("version.py", "deploy/version.py"), ("VERSION", "VERSION")):
            if (root / name).read_bytes() != files[original][1]:
                raise ValueError("release file differs from public source: " + name)
        timestamp = int(git(".", "show", "-s", "--format=%ct", commit).decode())
        (root / "BUILD.json").write_text(json.dumps({
            "version": version, "arch": arch, "revision": commit,
            "source_digest": manifest(files)["digest"],
            "built_at": datetime.datetime.fromtimestamp(timestamp, datetime.timezone.utc).isoformat().replace("+00:00", "Z"),
        }, sort_keys=True) + "\n", encoding="utf-8", newline="\n")
        archive(root / "source.tar.gz", files, timestamp)
        release_files = {name: ("100755" if name in {"weir", "install.sh"} else "100644", (root / name).read_bytes())
                         for name in sorted(allowed - {"SHA256SUMS"})}
        sums = "".join(f"{hashlib.sha256(data).hexdigest()}  {name}\n" for name, (_, data) in release_files.items())
        (root / "SHA256SUMS").write_text(sums, encoding="utf-8", newline="\n")
        release_files["SHA256SUMS"] = ("100644", sums.encode())
        result = pathlib.Path(f"dist/weir-{version}-linux-{arch}.tar.gz")
        archive(result, release_files, timestamp)
        event("package-result", public_commit=commit, arch=arch, sha256=hashlib.sha256(result.read_bytes()).hexdigest())
        return result


def checksums(archives):
    lines = [f"{hashlib.sha256(a.read_bytes()).hexdigest()}  {a.name}" for a in archives]
    pathlib.Path("dist/checksums.txt").write_text("\n".join(lines) + "\n", encoding="utf-8", newline="\n")


if __name__ == "__main__":
    version = os.environ.get("VERSION") or sys.argv[1]
    arches = sys.argv[2:] or [a for a in ARCHES if pathlib.Path(f"dist/weir-{version}-linux-{a}/weir").is_file()]
    if not arches:
        raise SystemExit("release binaries are missing")
    checksums([package(version, a) for a in arches])
