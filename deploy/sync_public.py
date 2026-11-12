"""Synchronize a release snapshot and confirm its public build; never build locally."""
import argparse
import json
import os
import pathlib
import re
import time

from export_public import OPERATION_ID, event, git, manifest, snapshot, stage, tree
from release_public import gh, validate_notes, verify_remote
from version import format_version, next_version, parse


def validate_source(source, tag):
    if not re.fullmatch(r"v[0-9]+\.[0-9]+\.[0-9]+", tag):
        raise ValueError("invalid release tag")
    commit = git(source, "rev-parse", "--verify", f"refs/tags/{tag}^{{commit}}").decode().strip()
    git(source, "merge-base", "--is-ancestor", commit, "refs/remotes/origin/main")
    version = format_version(parse(git(source, "show", f"{commit}:VERSION").decode()))
    if tag != "v" + version:
        raise ValueError("tag does not match VERSION")
    validate_notes(git(source, "show", f"{commit}:.github/release-notes/{tag}.md").decode())
    return commit, version


def synchronize(source, public, tag, dry_run=False):
    source, public = pathlib.Path(source).resolve(), pathlib.Path(public).resolve()
    if source == public or not (public / ".git").is_dir():
        raise ValueError("public synchronization requires a separate clone")
    with stage("validate-tag", tag=tag):
        if dry_run and tag == "main":
            source_commit = git(source, "rev-parse", "HEAD").decode().strip()
            git(source, "merge-base", "--is-ancestor", source_commit, "refs/remotes/origin/main")
            version = format_version(parse(git(source, "show", f"{source_commit}:VERSION").decode()))
            tag = "v" + version
            event("preview-main", source_commit=source_commit, version=version)
        else:
            source_commit, version = validate_source(source, tag)
    event("source-selected", source_commit=source_commit, tag=tag, version=version)
    _, files = snapshot(source, source_commit)
    exported = manifest(files)
    with stage("prepare-public", tag=tag):
        if git(public, "status", "--porcelain", "--untracked-files=all").strip():
            raise ValueError("public clone must be clean")
        git(public, "fetch", "origin", "main", "--tags")
        existing = git(public, "ls-remote", "--refs", "origin", f"refs/tags/{tag}").strip()
        if existing:
            git(public, "fetch", "origin", f"refs/tags/{tag}")
            public_commit, current = tree(public, "FETCH_HEAD")
            if current != files:
                raise ValueError("existing public tag does not match the exported source")
            event("reuse-public-tag", tag=tag, public_commit=public_commit)
        else:
            git(public, "checkout", "--detach", "refs/remotes/origin/main")
            _, current = tree(public, "HEAD")
            if "VERSION" in current and parse(current["VERSION"][1].decode()) > parse(version):
                raise ValueError("refusing to synchronize an older public version")
            public_commit = None
        changed = sorted(p for p in set(current) | set(files) if current.get(p) != files.get(p))
        event("public-diff", changed_files=len(changed), digest=exported["digest"], dry_run=dry_run)
        if not dry_run and public_commit is None:
            for name in current.keys() - files.keys():
                (public / name).unlink()
            for name, (mode, data) in files.items():
                destination = public / name
                # A public contributor's symlink must never redirect the export.
                if destination.is_symlink() or any(p.is_symlink() for p in destination.parents if p != public and p.is_relative_to(public)):
                    raise ValueError("symlink in public checkout")
                destination.parent.mkdir(parents=True, exist_ok=True)
                destination.write_bytes(data)
                destination.chmod(0o755 if mode == "100755" else 0o644)
            git(public, "config", "user.name", "github-actions[bot]")
            git(public, "config", "user.email", "41898282+github-actions[bot]@users.noreply.github.com")
            git(public, "add", "--all")
            git(public, "add", "--force", "--pathspec-from-file=-", "--pathspec-file-nul", input=b"\0".join(p.encode() for p in files) + b"\0")
            # Git on Windows may ignore executable bits in the working tree.
            for mode, flag in (("100755", "+x"), ("100644", "-x")):
                names = [p.encode() for p, (m, _) in files.items() if m == mode]
                if names:
                    git(public, "update-index", "--chmod=" + flag, "-z", "--stdin", input=b"\0".join(names) + b"\0")
            if git(public, "diff", "--cached", "--name-only").strip():
                git(public, "commit", "-m", "chore: release " + tag)
            public_commit = git(public, "rev-parse", "HEAD").decode().strip()
            if tree(public, public_commit)[1] != files:
                raise ValueError("public commit differs from the export")
            git(public, "tag", tag, public_commit)
            with stage("push-public", tag=tag, public_commit=public_commit):
                git(public, "push", "--atomic", "origin", "HEAD:refs/heads/main", f"refs/tags/{tag}:refs/tags/{tag}")
    report = {"operation_id": OPERATION_ID, "source_commit": source_commit, "public_commit": public_commit,
              "tag": tag, "version": version, "digest": exported["digest"], "changed_files": changed, "dry_run": dry_run}
    event("synchronize-result", **{k: v for k, v in report.items() if k not in {"operation_id", "changed_files"}})
    return report


def wait_for_release(repository, tag, commit, timeout=3600, interval=20):
    deadline = time.monotonic() + timeout
    with stage("wait-public-release", public_commit=commit, tag=tag):
        while time.monotonic() < deadline:
            runs = gh("api", f"repos/{repository}/actions/workflows/release.yml/runs?head_sha={commit}&per_page=100")["workflow_runs"]
            runs = [r for r in runs if r["head_sha"] == commit and r["head_branch"] == tag]
            if runs:
                run = max(runs, key=lambda r: (r["id"], r.get("run_attempt", 1)))
                if run["status"] == "completed":
                    if run["conclusion"] != "success":
                        raise ValueError("public release workflow failed: " + run["html_url"])
                    verify_remote(repository, tag, commit)
                    event("public-release-confirmed", public_run_id=run["id"], public_commit=commit, tag=tag)
                    return
            time.sleep(min(interval, max(0, deadline - time.monotonic())))
        raise ValueError("timed out waiting for the matching public release; VERSION was not advanced")


def advance_version(source, version, source_commit):
    with stage("advance-private-version", source_commit=source_commit):
        if git(source, "status", "--porcelain").strip():
            raise ValueError("private checkout must be clean before advancing VERSION")
        git(source, "fetch", "origin", "main")
        git(source, "merge-base", "--is-ancestor", source_commit, "refs/remotes/origin/main")
        git(source, "checkout", "--detach", "refs/remotes/origin/main")
        path = pathlib.Path(source) / "VERSION"
        current = format_version(parse(path.read_text(encoding="utf-8")))
        if current != version:
            if parse(current) < parse(next_version(version)):
                raise ValueError("private VERSION is behind the published version")
            event("version-already-advanced", version=current)
            return
        following = next_version(version)
        path.write_text(following + "\n", encoding="utf-8", newline="\n")
        git(source, "config", "user.name", "github-actions[bot]")
        git(source, "config", "user.email", "41898282+github-actions[bot]@users.noreply.github.com")
        git(source, "add", "VERSION")
        git(source, "commit", "-m", f"chore: advance VERSION to {following} [skip ci]")
        git(source, "push", "origin", "HEAD:refs/heads/main")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="command", required=True)
    sync = sub.add_parser("sync")
    sync.add_argument("--source", required=True)
    sync.add_argument("--public", required=True)
    sync.add_argument("--tag", required=True)
    sync.add_argument("--report", required=True)
    sync.add_argument("--dry-run", action="store_true")
    wait = sub.add_parser("wait")
    wait.add_argument("--repository", required=True)
    wait.add_argument("--report", required=True)
    wait.add_argument("--timeout", type=int, default=3600)
    advance = sub.add_parser("advance")
    advance.add_argument("--source", required=True)
    advance.add_argument("--report", required=True)
    args = parser.parse_args()
    with stage(args.command):
        if args.command == "sync":
            report = synchronize(args.source, args.public, args.tag, args.dry_run)
            pathlib.Path(args.report).write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
            output = os.environ.get("GITHUB_OUTPUT")
            if output:
                with open(output, "a", encoding="utf-8") as stream:
                    stream.write(f"public_commit={report['public_commit'] or ''}\n")
        else:
            report = json.loads(pathlib.Path(args.report).read_text(encoding="utf-8"))
            if report["dry_run"] or not report["public_commit"]:
                raise ValueError("a preview cannot confirm a release or advance VERSION")
            if args.command == "wait":
                wait_for_release(args.repository, report["tag"], report["public_commit"], args.timeout)
            else:
                advance_version(args.source, report["version"], report["source_commit"])


if __name__ == "__main__":
    main()
