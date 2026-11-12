"""Export a reviewed Git tree without sharing its objects or working directory."""
import argparse
import contextlib
import fnmatch
import hashlib
import io
import json
import os
import pathlib
import posixpath
import re
import subprocess
import sys
import time
import urllib.parse
import uuid


OPERATION_ID = os.environ.get("WEIR_OPERATION_ID") or os.environ.get("GITHUB_RUN_ID") or uuid.uuid4().hex
START = "<!-- private-only:start -->"
END = "<!-- private-only:end -->"


def event(stage, **fields):
    print(json.dumps({"operation_id": OPERATION_ID, "stage": stage, **fields}, ensure_ascii=False), file=sys.stderr, flush=True)


@contextlib.contextmanager
def stage(name, **fields):
    started = time.monotonic()
    event(name, status="started", **fields)
    try:
        yield
    except BaseException as exc:
        event(name, status="failed", elapsed_ms=round((time.monotonic() - started) * 1000), error=str(exc), **fields)
        raise
    else:
        event(name, status="succeeded", elapsed_ms=round((time.monotonic() - started) * 1000), **fields)


def git(repo, *args, input=None):
    result = subprocess.run(["git", "-C", str(repo), *args], input=input, capture_output=True, check=False)
    if result.returncode:
        # Remote URLs can contain authentication material; do not echo arguments.
        raise ValueError("git operation failed: " + result.stderr.decode("utf-8", "replace").strip())
    return result.stdout


def policy():
    return json.loads(pathlib.Path(__file__).with_name("public-export.json").read_text(encoding="utf-8"))


def public_path(name, rules):
    path = pathlib.PurePosixPath(name)
    if path.is_absolute() or ".." in path.parts or "\\" in name or any(ord(c) < 32 for c in name):
        raise ValueError("unsafe source path")
    lowered = [part.casefold() for part in path.parts]
    if any(part in {p.casefold() for p in rules["private_directories"]} for part in lowered):
        return False
    if path.name.casefold() in {p.casefold() for p in rules["private_names"]}:
        return False
    if (path.name.startswith(".env") and path.name != ".env.example") or path.suffix.lower() in {".pem", ".key", ".p12", ".pyc"}:
        return False
    if any(name == p or name.startswith(p + "/") for p in rules["private_paths"]):
        return False
    if path.parts[0] == ".github":
        if any(fnmatch.fnmatchcase(name, p) for p in rules["github"]):
            return True
    elif len(path.parts) > 1 and path.parts[0] in rules["directories"]:
        return True
    elif len(path.parts) == 1 and any(fnmatch.fnmatchcase(name, p) for p in rules["files"]):
        return True
    raise ValueError("source path needs an explicit export rule: " + name)


def strip_private(data):
    text = data.decode("utf-8")
    result = []
    inside = False
    for line in text.splitlines(keepends=True):
        marker = line.strip()
        if marker == START:
            if inside:
                raise ValueError("nested private README block")
            inside = True
        elif marker == END:
            if not inside:
                raise ValueError("unmatched private README end")
            inside = False
        elif "private-only:" in line:
            raise ValueError("invalid private README marker")
        elif not inside:
            result.append(line)
    if inside:
        raise ValueError("unclosed private README block")
    return "".join(result).encode("utf-8")


def tree(repo, ref):
    commit = git(repo, "rev-parse", "--verify", ref + "^{commit}").decode().strip()
    entries = []
    for record in git(repo, "ls-tree", "-rz", "--full-tree", commit).split(b"\0"):
        if not record:
            continue
        metadata, name = record.split(b"\t", 1)
        mode, kind, oid = metadata.decode().split()
        entries.append((name.decode("utf-8"), mode, kind, oid))
    objects = io.BytesIO(git(repo, "cat-file", "--batch", input="".join(e[3] + "\n" for e in entries).encode()))
    files = {}
    for name, mode, kind, oid in entries:
        header = objects.readline().decode().split()
        if len(header) != 3 or header[0] != oid or header[1] != "blob":
            raise ValueError("submodules and non-blob source entries are unsupported")
        data = objects.read(int(header[2]))
        if objects.read(1) != b"\n":
            raise ValueError("invalid Git object stream")
        files[name] = (mode, data)
    return commit, files


def check_links(files):
    for name, (_, data) in files.items():
        if pathlib.PurePosixPath(name).suffix.lower() not in {".md", ".html"}:
            continue
        text = data.decode("utf-8")
        links = re.findall(r"\]\(<?([^\s)>]+)>?(?:\s+[^)]*)?\)", text)
        links += re.findall(r'''(?:href|src)\s*=\s*["']([^"']+)["']''', text, re.I)
        for link in links:
            parsed = urllib.parse.urlsplit(link)
            if parsed.scheme or parsed.netloc or not parsed.path or parsed.path.startswith("/"):
                continue
            target = posixpath.normpath(posixpath.join(posixpath.dirname(name), urllib.parse.unquote(parsed.path)))
            if target not in files and not any(p.startswith(target.rstrip("/") + "/") for p in files):
                raise ValueError(f"broken public document link in {name}: {link}")


def snapshot(repo, ref="HEAD", strict=False):
    with stage("read-source"):
        commit, original = tree(repo, ref)
        rules = policy()
        files = {}
        for name, (mode, data) in original.items():
            if not public_path(name, rules):
                if strict:
                    raise ValueError("private file in public source: " + name)
                continue
            if mode not in {"100644", "100755"}:
                raise ValueError("source symlinks are unsupported: " + name)
            if name == "README.md":
                transformed = strip_private(data)
                if strict and transformed != data:
                    raise ValueError("private README content in public source")
                data = transformed
            files[name] = (mode, data)
    with stage("check-links"):
        check_links(files)
    return commit, files


def manifest(files):
    records = [{"path": name, "mode": mode, "sha256": hashlib.sha256(data).hexdigest()}
               for name, (mode, data) in sorted(files.items())]
    digest = hashlib.sha256(json.dumps(records, sort_keys=True, separators=(",", ":")).encode()).hexdigest()
    return {"digest": digest, "files": records}


def write_tree(files, destination):
    destination = pathlib.Path(destination)
    if destination.exists() and any(destination.iterdir()):
        raise ValueError("export destination must be empty")
    destination.mkdir(parents=True, exist_ok=True)
    for name, (mode, data) in files.items():
        path = destination / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(data)
        path.chmod(0o755 if mode == "100755" else 0o644)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repo", default=".")
    parser.add_argument("--ref", default="HEAD")
    parser.add_argument("--output")
    parser.add_argument("--manifest")
    parser.add_argument("--check", action="store_true", help="Reject private files instead of filtering them")
    args = parser.parse_args()
    with stage("export"):
        commit, files = snapshot(args.repo, args.ref, args.check)
        report = {"source_commit": commit, "operation_id": OPERATION_ID, **manifest(files)}
        if args.manifest:
            report_path = pathlib.Path(args.manifest).resolve()
            if args.output and report_path.is_relative_to(pathlib.Path(args.output).resolve()):
                raise ValueError("private manifest must be outside the public export")
            report_path.write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
        if args.output:
            write_tree(files, args.output)
        event("export-result", source_commit=commit, digest=report["digest"], file_count=len(files))


if __name__ == "__main__":
    main()
