"""Replace only the release downloader so signed asset redirects are accepted."""
from pathlib import Path
from remote import connect

local = Path(__file__).resolve().parent / "release_fetch.py"
remote = "/usr/local/lib/migate/release_fetch.py"
client = connect("control")
try:
    with client.open_sftp() as sftp:
        try:
            sftp.stat(remote + ".bak")
        except FileNotFoundError:
            sftp.posix_rename(remote, remote + ".bak")
        sftp.put(str(local), remote)
        sftp.chmod(remote, 0o644)
finally:
    client.close()
print("release downloader replaced; backup kept beside it")
