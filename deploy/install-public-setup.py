"""Install the published control release and leave the setup wizard for the operator."""
import hashlib
import json
from pathlib import Path
import shlex
import time
from remote import connect, run

ROOT = Path(__file__).resolve().parent.parent
VERSION = "0.1.1"
ARCHIVE = ROOT / f"dist/migate-{VERSION}-linux-amd64.tar.gz"
CHECKSUM = "a996002d04e5cd233c6f3f5262d814bf4d76640ef91760b79fa8cb0051119ba5"
REMOTE_ARCHIVE = f"/var/tmp/migate-control-{VERSION}.tar.gz"
REMOTE_DIR = f"/var/tmp/migate-control-{VERSION}"

digest = hashlib.sha256(ARCHIVE.read_bytes()).hexdigest()
if digest != CHECKSUM:
    raise SystemExit("release checksum mismatch")

remote_script = r"""
import datetime, json, os, shutil, subprocess, pathlib
version = "0.1.1"
archive = "/var/tmp/migate-control-%s.tar.gz" % version
directory = pathlib.Path("/var/tmp/migate-control-%s" % version)
backup_root = pathlib.Path("/var/backups/migate")
backup_root.mkdir(mode=0o700, parents=True, exist_ok=True)
stamp = datetime.datetime.now(datetime.timezone.utc).strftime("%Y%m%dT%H%M%SZ")
config_dir = pathlib.Path("/etc/migate-control")
config_path = config_dir / "config.json"
if config_path.is_file():
    shutil.copy2(config_path, backup_root / ("config-%s.json" % stamp))
    config = json.loads(config_path.read_text(encoding="utf-8"))
    database = config.get("database") or {}
    if database.get("user") and database.get("database"):
        dump = backup_root / ("migate-schema-%s.sql" % stamp)
        env = os.environ.copy()
        if database.get("password"):
            env["PGPASSWORD"] = database["password"]
        with dump.open("wb") as handle:
            result = subprocess.run(["docker", "exec", "-i", "-e", "PGPASSWORD", "1Panel-postgresql-EcCX", "pg_dump", "-U", database["user"], "-d", database["database"], "-n", "migate"], env=env, stdout=handle, stderr=subprocess.PIPE)
        if result.returncode != 0:
            message = result.stderr.decode("utf-8", "replace")
            if "no matching schemas" not in message:
                raise SystemExit("schema backup failed")
        else:
            drop = subprocess.run(["docker", "exec", "-i", "-e", "PGPASSWORD", "1Panel-postgresql-EcCX", "psql", "-U", database["user"], "-d", database["database"], "-v", "ON_ERROR_STOP=1", "-c", "DROP SCHEMA IF EXISTS migate CASCADE"], env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
            if drop.returncode != 0:
                raise SystemExit("schema reset failed")
    config_path.rename(config_dir / ("config.json.before-setup-%s" % stamp))
if directory.exists():
    shutil.rmtree(directory)
directory.mkdir(mode=0o700)
subprocess.run(["tar", "-xzf", archive, "-C", str(directory)], check=True)
subprocess.run(["sh", str(directory / "install.sh"), "control", "127.0.0.1:19810", "http://127.0.0.1:19810", "/etc/migate-control/pg-dump"], check=True)
fresh = json.loads(config_path.read_text(encoding="utf-8"))
fresh["setup_expires"] = (datetime.datetime.now(datetime.timezone.utc) + datetime.timedelta(hours=24)).strftime("%Y-%m-%dT%H:%M:%SZ")
config_path.write_text(json.dumps(fresh), encoding="utf-8")
os.chmod(config_path, 0o600)
subprocess.run(["systemctl", "restart", "migate-control"], check=True)
site = pathlib.Path("/opt/1panel/www/conf.d/migate-setup-ip.conf")
site.write_text("\n".join([
    "server {",
    "    listen 80;",
    "    listen [::]:80;",
    "    server_name 106.55.5.233;",
    "    location / {",
    "        proxy_pass http://127.0.0.1:19810;",
    "        proxy_set_header Host $host;",
    "        proxy_set_header X-Real-IP $remote_addr;",
    "        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;",
    "        proxy_set_header X-Forwarded-Proto $scheme;",
    "        proxy_http_version 1.1;",
    "    }",
    "}",
    ""
]), encoding="utf-8")
subprocess.run(["docker", "exec", "1Panel-openresty-vUZF", "nginx", "-t"], check=True)
subprocess.run(["docker", "exec", "1Panel-openresty-vUZF", "nginx", "-s", "reload"], check=True)
print("SETUP_READY")
print((config_dir / "setup-token").read_text(encoding="utf-8").strip())
print("BACKUP", backup_root)
"""

with connect("control") as client:
    with client.open_sftp() as ftp:
        ftp.put(str(ARCHIVE), REMOTE_ARCHIVE)
        ftp.chmod(REMOTE_ARCHIVE, 0o600)
    code, out, err = run(client, "printf '%s  %s\\n' " + shlex.quote(digest) + " " + shlex.quote(REMOTE_ARCHIVE) + " | sha256sum -c -", timeout=60)
    if code:
        raise SystemExit(out + err)
    with client.open_sftp() as ftp:
        with ftp.open("/var/tmp/migate-setup-install.py", "w") as handle:
            handle.write(remote_script)
        ftp.chmod("/var/tmp/migate-setup-install.py", 0o700)
    code, out, err = run(client, "python3 /var/tmp/migate-setup-install.py", timeout=300)
    print(out)
    if err:
        print(err)
    if code:
        raise SystemExit(code)
print("installed", VERSION, "at", time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()))
