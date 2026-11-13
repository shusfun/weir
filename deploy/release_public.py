"""Publish immutable public release assets and verify their GitHub digests."""
import argparse
import hashlib
import json
import pathlib
import re
import subprocess
import tempfile

from export_public import event, git, snapshot, stage
from version import format_version, parse


HEADINGS = ("## 概览", "## 主要变化", "## 修复", "## 升级与兼容", "## 验证")


def gh(*args, binary=False, missing_ok=False):
    result = subprocess.run(["gh", *args], capture_output=True)
    if result.returncode:
        if missing_ok and b"HTTP 404" in result.stderr:
            return None
        raise ValueError("GitHub operation failed: " + result.stderr.decode("utf-8", "replace").strip())
    if binary:
        return result.stdout
    return json.loads(result.stdout) if result.stdout.strip() else None


def validate_notes(text):
    if not all(heading in text.splitlines() for heading in HEADINGS):
        raise ValueError("release notes are missing required headings")


def asset_names(tag):
    version = format_version(parse(tag.removeprefix("v")))
    if tag != "v" + version:
        raise ValueError("invalid release tag")
    return [f"weir-{version}-linux-{arch}.tar.gz" for arch in ("amd64", "arm64")] + ["checksums.txt"]


def marker(commit):
    return f"<!-- weir-source: {commit} -->"


def find_release(repository, tag):
    release = gh("api", f"repos/{repository}/releases/tags/{tag}", missing_ok=True)
    if release is not None:
        return release
    # GitHub's tag endpoint can omit unpublished drafts. Authenticated release
    # listing includes them; retain their identity instead of creating a second.
    page = 1
    while True:
        releases = gh("api", f"repos/{repository}/releases?per_page=100&page={page}")
        matches = [r for r in releases if r["tag_name"] == tag]
        if len(matches) > 1:
            raise ValueError("multiple releases match the requested tag")
        if matches:
            return matches[0]
        if len(releases) < 100:
            return None
        page += 1


def verify_tag(repository, tag, commit):
    target = gh("api", f"repos/{repository}/git/ref/tags/{tag}")["object"]
    for _ in range(4):
        if target["type"] == "commit":
            if target["sha"] != commit:
                raise ValueError("remote public tag differs from the expected source")
            return
        if target["type"] != "tag":
            break
        target = gh("api", f"repos/{repository}/git/tags/{target['sha']}")["object"]
    raise ValueError("unsupported public tag target")


def create_draft(repository, tag, commit, notes):
    # Use the creation response: GitHub's tag/list indexes may lag a successful POST.
    with tempfile.TemporaryDirectory() as temp:
        payload = pathlib.Path(temp) / "release.json"
        payload.write_text(json.dumps({"tag_name": tag, "target_commitish": commit, "name": tag,
                                      "body": notes.rstrip() + "\n\n" + marker(commit) + "\n",
                                      "draft": True, "prerelease": False}), encoding="utf-8")
        release = gh("api", f"repos/{repository}/releases", "--method", "POST", "--input", str(payload))
    verify_metadata(release, tag, commit, allow_draft=True)
    if not release.get("id") or not release["draft"]:
        raise ValueError("release creation did not return a draft")
    return release


def verify_metadata(release, tag, commit, allow_draft=False):
    if release["tag_name"] != tag or release["prerelease"] or (release["draft"] and not allow_draft):
        raise ValueError("release is not the expected stable public release")
    if marker(commit) not in (release.get("body") or ""):
        raise ValueError("release does not belong to the expected public commit")


def remote_assets(release):
    assets = {}
    for asset in release["assets"]:
        name = asset["name"]
        digest = asset.get("digest") or ""
        if name in assets or asset.get("state") != "uploaded" or asset["size"] <= 0 or not re.fullmatch(r"sha256:[0-9a-f]{64}", digest):
            raise ValueError("missing or invalid GitHub asset digest: " + name)
        assets[name] = digest.removeprefix("sha256:")
    return assets


def parse_checksums(data, tag):
    result = {}
    for line in data.decode("utf-8").splitlines():
        match = re.fullmatch(r"([0-9a-f]{64})  ([^/\\\s]+)", line)
        if not match or match[2] in result:
            raise ValueError("invalid release checksums")
        result[match[2]] = match[1]
    if set(result) != set(asset_names(tag)[:-1]):
        raise ValueError("release checksums must cover both architectures")
    return result


def verify_remote(repository, tag, commit, expected=None, allow_draft=False):
    verify_tag(repository, tag, commit)
    release = find_release(repository, tag)
    if release is None:
        raise ValueError("release not found")
    verify_metadata(release, tag, commit, allow_draft)
    assets = remote_assets(release)
    if set(assets) != set(asset_names(tag)):
        raise ValueError("release assets are incomplete or unexpected")
    checksum_id = next(a["id"] for a in release["assets"] if a["name"] == "checksums.txt")
    checksums = gh("api", f"repos/{repository}/releases/assets/{checksum_id}", "-H", "Accept: application/octet-stream", binary=True)
    if hashlib.sha256(checksums).hexdigest() != assets["checksums.txt"]:
        raise ValueError("downloaded checksums do not match the GitHub asset digest")
    if any(assets[name] != digest for name, digest in parse_checksums(checksums, tag).items()):
        raise ValueError("GitHub assets do not match the release checksums")
    if expected is not None and assets != expected:
        raise ValueError("existing release assets differ; refusing to overwrite them")
    event("verify-public-assets", tag=tag, public_commit=commit, release_id=release["id"], assets=assets)
    return release


def publish(repository, tag, commit, directory):
    with stage("publish-public-release", tag=tag, public_commit=commit):
        if git(".", "rev-parse", f"refs/tags/{tag}^{{commit}}").decode().strip() != commit:
            raise ValueError("public tag no longer matches the built source")
        verify_tag(repository, tag, commit)
        notes = git(".", "show", f"{commit}:.github/release-notes/{tag}.md").decode()
        validate_notes(notes)
        paths = {name: pathlib.Path(directory) / name for name in asset_names(tag)}
        expected = {name: hashlib.sha256(path.read_bytes()).hexdigest() for name, path in paths.items()}
        if parse_checksums(paths["checksums.txt"].read_bytes(), tag) != {name: expected[name] for name in asset_names(tag)[:-1]}:
            raise ValueError("local archives do not match checksums.txt")
        release = find_release(repository, tag)
        if release is not None:
            verify_metadata(release, tag, commit, allow_draft=True)
            if not release["draft"]:
                verify_remote(repository, tag, commit, expected)
                return
        else:
            release = create_draft(repository, tag, commit, notes)
        uploaded = remote_assets(release)
        if set(uploaded) - set(expected):
            raise ValueError("unexpected assets in the release draft")
        for name, digest in uploaded.items():
            if digest != expected[name]:
                raise ValueError("draft asset differs; refusing to replace it: " + name)
        for name in paths.keys() - uploaded.keys():
            with stage("upload-asset", asset=name, public_commit=commit):
                gh("release", "upload", tag, str(paths[name]), "--repo", repository, binary=True)
        verify_remote(repository, tag, commit, expected, allow_draft=True)
        gh("release", "edit", tag, "--repo", repository, "--draft=false", "--latest", binary=True)
        verify_remote(repository, tag, commit, expected)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=["check", "publish"])
    parser.add_argument("--tag", required=True)
    parser.add_argument("--commit")
    parser.add_argument("--repository")
    parser.add_argument("--directory", default="dist")
    args = parser.parse_args()
    if args.command == "check":
        with stage("check-public-source", tag=args.tag):
            commit, files = snapshot(".", "HEAD", strict=True)
            version = format_version(parse(files["VERSION"][1].decode()))
            if args.tag != "v" + version:
                raise ValueError("tag does not match VERSION")
            validate_notes(files[f".github/release-notes/{args.tag}.md"][1].decode())
            event("public-source-verified", public_commit=commit, tag=args.tag)
    else:
        if not args.commit or not args.repository:
            parser.error("publish requires --commit and --repository")
        publish(args.repository, args.tag, args.commit, args.directory)


if __name__ == "__main__":
    main()
