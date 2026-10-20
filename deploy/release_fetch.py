"""从固定 GitHub Release 下载并校验安装包。失败时不写 Gate 清单。"""
import hashlib
import io
import json
import os
import pathlib
import sys
import tarfile
import urllib.error
import urllib.parse
import urllib.request

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))

ALLOWED_HOSTS = {
    "api.github.com",
    "github.com",
    "release-assets.githubusercontent.com",
    "objects.githubusercontent.com",
    "github-releases.githubusercontent.com",
}
TOKEN_HOSTS = ALLOWED_HOSTS
ARCHIVE_SUFFIX = "-linux-amd64.tar.gz"


def check_url(url):
    parts = urllib.parse.urlsplit(url)
    if parts.scheme != "https" or parts.username or parts.password or parts.query or parts.fragment:
        raise ValueError("release url refused")
    if (parts.hostname or "").lower() not in ALLOWED_HOSTS:
        raise ValueError("release host refused")
    return parts


def archive_name(version):
    return "migate-" + version + ARCHIVE_SUFFIX


def release_urls(repo, tag, version):
    if "/" not in repo or not all(part and all(ch.isalnum() or ch in "_.-" for ch in part) for part in repo.split("/")):
        raise ValueError("repository refused")
    if tag != "v" + version:
        raise ValueError("tag refused")
    base = "https://github.com/" + repo + "/releases/download/" + tag + "/"
    archive = base + archive_name(version)
    checksums = base + "checksums.txt"
    api = "https://api.github.com/repos/" + repo + "/releases/" + ("latest" if tag == "" else "tags/" + tag)
    for url in (archive, checksums):
        check_url(url)
    return archive, checksums, api


class GuardRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        check_url(newurl)
        redirected = super().redirect_request(req, fp, code, msg, headers, newurl)
        host = (urllib.parse.urlsplit(newurl).hostname or "").lower()
        if redirected is not None and host not in TOKEN_HOSTS:
            redirected.remove_header("Authorization")
        return redirected


def _request(url, token):
    check_url(url)
    request = urllib.request.Request(url, headers={"User-Agent": "MiGate-release", "Accept": "application/vnd.github+json"})
    host = (urllib.parse.urlsplit(url).hostname or "").lower()
    if token and host in TOKEN_HOSTS:
        request.add_header("Authorization", "Bearer " + token)
    opener = urllib.request.build_opener(GuardRedirect)
    try:
        response = opener.open(request, timeout=30)
    except urllib.error.HTTPError as exc:
        raise RuntimeError("release download failed") from exc
    check_url(response.geturl())
    return response


def read_token(path):
    if not path or not os.path.isfile(path):
        return ""
    token = pathlib.Path(path).read_text(encoding="utf-8").strip()
    if any(ch in token for ch in "\r\n\x00 "):
        raise ValueError("token file refused")
    return token


def verify_archive(checksum_text, archive, version):
    name = archive_name(version)
    expected = ""
    for line in checksum_text.splitlines():
        parts = line.split()
        if len(parts) == 2 and parts[1] == name:
            expected = parts[0].lower()
    digest = hashlib.sha256(archive).hexdigest()
    if not expected or expected != digest:
        raise ValueError("archive checksum mismatch")
    files = {}
    with tarfile.open(fileobj=io.BytesIO(archive), mode="r:gz") as bundle:
        checksums = bundle.extractfile("SHA256SUMS")
        if checksums is None:
            raise ValueError("package sums missing")
        wanted = {}
        for line in checksums.read().decode("utf-8").splitlines():
            parts = line.split()
            if len(parts) == 2 and parts[1] in {"migate-gate", "migate-manage"}:
                wanted[parts[1]] = parts[0].lower()
        if set(wanted) != {"migate-gate", "migate-manage"}:
            raise ValueError("gate binaries missing")
        for filename, file_sum in wanted.items():
            member = bundle.extractfile(filename)
            if member is None:
                raise ValueError("gate binary missing")
            data = member.read()
            if hashlib.sha256(data).hexdigest() != file_sum:
                raise ValueError("gate binary checksum mismatch")
            if len(data) > 32 * 1024 * 1024:
                raise ValueError("gate binary too large")
            files[filename] = data
    return files


def write_manifest(directory, version, files, archive=b""):
    root = pathlib.Path(directory)
    temporary = root.with_name(root.name + ".partial")
    if temporary.exists():
        for child in sorted(temporary.rglob("*"), reverse=True):
            if child.is_file():
                child.unlink()
        temporary.rmdir()
    temporary.mkdir(parents=True, exist_ok=True)
    hashes = {}
    for name, data in files.items():
        (temporary / name).write_bytes(data)
        hashes[name] = hashlib.sha256(data).hexdigest()
    manifest = {
        "version": version,
        "gate_sha256": hashes["migate-gate"],
        "manage_sha256": hashes["migate-manage"],
    }
    (temporary / "manifest.json").write_text(json.dumps(manifest), encoding="utf-8")
    if archive:
        (temporary / "package.tar.gz").write_bytes(archive)
    if root.exists():
        for child in root.iterdir():
            if child.is_file():
                child.unlink()
    else:
        root.mkdir(parents=True, exist_ok=True)
    for child in temporary.iterdir():
        child.replace(root / child.name)
    temporary.rmdir()


def status(path, state, message):
    if not path:
        return
    pathlib.Path(path).write_text(json.dumps({"status": state, "message": message}, ensure_ascii=False), encoding="utf-8")


def fetch(repo, tag, token_file, output, status_file):
    token = read_token(token_file)
    if not tag:
        api = "https://api.github.com/repos/" + repo + "/releases/latest"
        with _request(api, token) as response:
            payload = json.load(response)
        tag = str(payload.get("tag_name") or "")
    version = tag[1:] if tag.startswith("v") else tag
    from version import parse

    parse(version)
    archive_url, checksum_url, _ = release_urls(repo, tag, version)
    with _request(checksum_url, token) as response:
        checksum_text = response.read().decode("utf-8")
    with _request(archive_url, token) as response:
        archive = response.read()
    files = verify_archive(checksum_text, archive, version)
    write_manifest(output, version, files, archive)
    status(status_file, "verified", "release checksum verified")
    return version


def main(argv):
    if len(argv) >= 2 and argv[1] == "check-url":
        check_url(argv[2])
        print("ok")
        return
    if len(argv) >= 2 and argv[1] == "verify-local":
        version = argv[2]
        checksum_text = pathlib.Path(argv[3]).read_text(encoding="utf-8")
        archive = pathlib.Path(argv[4]).read_bytes()
        output = argv[5]
        files = verify_archive(checksum_text, archive, version)
        write_manifest(output, version, files)
        return
    if len(argv) >= 2 and argv[1] == "fetch":
        fetch(argv[2], argv[3], argv[4], argv[5], argv[6] if len(argv) > 6 else "")
        return
    raise SystemExit("usage: release_fetch.py check-url|verify-local|fetch")


if __name__ == "__main__":
    try:
        main(sys.argv)
    except Exception as exc:
        print(str(exc), file=sys.stderr)
        raise SystemExit(1) from exc
