"""Exercise a released Linux binary and a candidate against isolated stores."""
import argparse
from contextlib import ExitStack, closing
import hashlib
from http.client import HTTPConnection
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import ipaddress
import json
import os
import pathlib
import re
import secrets
import shutil
import signal
import sqlite3
import subprocess
import tarfile
from threading import Thread
import time
from http.cookiejar import CookieJar
from urllib.error import HTTPError, URLError
from urllib.request import HTTPCookieProcessor, Request, build_opener

from scenario_run import ScenarioRun


ROOT = pathlib.Path(__file__).resolve().parent.parent
ORIGIN = "http://127.0.0.1:18080"
MIGRATION_ERROR = "database migration version or checksum is incompatible with this binary"
KERNEL = pathlib.Path("/core/sing-box")
FIXTURE_NETWORKS = ("192.0.2.0/24", "198.51.100.0/24", "203.0.113.0/24")
PROBE_BODY = b"weir-upgrade-client"


class ProbeHandler(BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200)
        self.send_header("Content-Length", str(len(PROBE_BODY)))
        self.end_headers()
        self.wfile.write(PROBE_BODY)

    def log_message(self, *args):
        pass


def free_fixture_subnet(occupied):
    used = [ipaddress.ip_network(value) for value in occupied]
    for block in FIXTURE_NETWORKS:
        for network in ipaddress.ip_network(block).subnets(new_prefix=27):
            if not any(network.overlaps(other) for other in used if other.version == 4):
                return network
    raise RuntimeError("no unused TEST-NET subnet for the upgrade fixture")


def extract_binary(archive, checksums, output):
    expected = next((line.split()[0] for line in checksums.read_text().splitlines()
                     if line.split()[1:] in ([archive.name], ["*" + archive.name])), "")
    with archive.open("rb") as stream:
        if hashlib.file_digest(stream, "sha256").hexdigest() != expected.lower():
            raise ValueError("released archive checksum mismatch")
    with tarfile.open(archive, "r|gz") as package:
        for entry in package:
            if entry.name in ("weir", "./weir") and entry.isfile():
                with package.extractfile(entry) as source, output.open("wb") as target:
                    shutil.copyfileobj(source, target)
                return
    raise ValueError("released archive has no weir binary")


class CenterFixture:
    def __init__(self, root, driver, phase):
        self.root, self.driver, self.phase = root, driver, phase
        self.config = root / "center.json"
        self.db = root / "center.db"
        self.state = root / "fixture.json"
        self.gate_host = os.getenv("WEIR_UPGRADE_GATE_HOST")
        self.http = build_opener(HTTPCookieProcessor(CookieJar()))

    def version(self, binary, role):
        command = ["center", "--version"] if role == "center" else ["gate", "version"]
        output = subprocess.check_output([str(binary), *command], text=True, timeout=15)
        return output.split()[2]

    def request(self, path, expected, body=None, method=None, **headers):
        endpoint = "/sub/:token" if path.startswith("/sub/") else path
        req = Request(ORIGIN + path, None if body is None else json.dumps(body).encode(),
                      {"Content-Type": "application/json", "Sec-Fetch-Site": "same-origin", **headers},
                      method=method)
        try:
            response = self.http.open(req, timeout=50)
        except HTTPError as error:
            response = error
        with response:
            if response.status != expected:
                trace = response.headers.get("X-Trace-Id", "")
                raise RuntimeError(f"{endpoint} returned {response.status}, expected {expected} (trace {trace})")
            return json.loads(response.read())

    def source_and_grant(self, source_id, profile_id, account):
        sources = self.request("/api/v1/admin/subscriptions/sources", 200)["items"]
        source = next((item for item in sources if item["id"] == source_id), None)
        if source is None:
            raise RuntimeError("original subscription source is missing")
        identity = {key: source[key] for key in ("id", "name", "url", "enabled", "user_agent")}
        if self.phase in {"upgrade", "reopen"}:
            if (source.get("gate_id") != 0 or source.get("refresh_hours") != 0 or source.get("last_location") != ""
                    or source.get("last_attempt_at") is not None):
                raise RuntimeError("migrated subscription source defaults are incorrect")
        admin = self.http
        self.http = build_opener(HTTPCookieProcessor(CookieJar()))
        try:
            self.request("/api/v1/auth/login", 200, account)
            groups = self.request("/api/v1/me/groups", 200)["items"]
        finally:
            self.http = admin
        if len(groups) != 1 or groups[0].get("id") != profile_id or not groups[0].get("url"):
            raise RuntimeError("original group grant is missing")
        return {"source": identity, "profile": profile_id, "grant_url": groups[0]["url"]}

    def command(self, binary):
        return [str(binary), "center", "--serve", "--listen", "127.0.0.1:18080", "--config", str(self.config)]

    def start(self, args, stack):
        with (self.root / f"center-{self.phase}.log").open("ab") as log:
            process = subprocess.Popen(args, stdout=log, stderr=log)
        stack.callback(self.stop, process)
        return process

    def launch(self, binary, expected, stack):
        process = self.start(self.command(binary), stack)
        for attempt in range(120):
            if process.poll() is not None:
                raise RuntimeError("center stopped before readiness")
            try:
                if self.request("/healthz", 200)["status"] == expected:
                    return
            except (URLError, TimeoutError):
                pass
            time.sleep(.25)
        raise TimeoutError("center readiness")

    def stop(self, process):
        if process.poll() is not None:
            return
        process.send_signal(signal.SIGINT)
        try:
            process.wait(10)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait(10)
            print("fixture used a forced stop to test crash recovery", flush=True)

    def sqlite_backup(self, source, target):
        with closing(sqlite3.connect(source, timeout=10)) as original:
            if original.execute("PRAGMA integrity_check").fetchone() != ("ok",):
                raise RuntimeError("SQLite integrity check failed")
            original.execute("VACUUM INTO ?", (str(target),))

    def stage_kernel(self):
        output = subprocess.check_output([str(KERNEL), "version"], text=True, timeout=15)
        version = re.search(r"\b\d+\.\d+\.\d+\b", output)
        if not version:
            raise RuntimeError("could not identify sing-box fixture version")
        version = version[0]
        cache = self.root / "center-data" / "kernels" / "sing-box" / version / "amd64"
        cache.mkdir(parents=True, exist_ok=True)
        with KERNEL.open("rb") as stream:
            digest = hashlib.file_digest(stream, "sha256").hexdigest()
        if not (cache / "sing-box").exists():
            shutil.copyfile(KERNEL, cache / "sing-box")
            (cache / "sing-box").chmod(0o755)
        (cache / "sha256").write_text(digest)
        installed = pathlib.Path("/opt/weir/cores")
        installed.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(KERNEL, installed / ("sing-box-" + version))
        (installed / ("sing-box-" + version)).chmod(0o755)
        (installed / "sing-box.version").write_text(version)
        return version

    def launch_gate(self, binary, stack, pairing=None):
        state_dir = self.root / "gate-state"
        state_dir.mkdir(exist_ok=True)
        if self.phase == "restore":
            shutil.copytree(self.root / "before-gate", state_dir, dirs_exist_ok=True)
        socket = "/tmp/weir-upgrade.sock"
        device = state_dir / "agent.json"
        common = ["--config", str(device), "--data", str(state_dir / "data"), "--socket", socket]
        if pairing:
            with (self.root / f"center-{self.phase}.log").open("ab") as log:
                result = subprocess.run([str(binary), "gate", "pair", *common, "--center", ORIGIN, "--token", pairing],
                                        stdout=log, stderr=log, timeout=25)
            if result.returncode:
                raise RuntimeError("fixture Gate pairing failed")
        if subprocess.run(["id", "-u", "weir-gate"], capture_output=True).returncode:
            subprocess.run(["useradd", "--system", "--user-group", "--no-create-home", "--no-log-init", "weir-gate"],
                           check=True, timeout=90)
        group_id = int(subprocess.check_output(["id", "-g", "weir-gate"], text=True, timeout=5))
        for name in ("cores", "certs"):
            directory = pathlib.Path("/etc/weir/gate") / name
            directory.mkdir(parents=True, exist_ok=True)
            directory.chmod(0o750)
            os.chown(directory, 0, group_id)
        helper = self.start([str(binary), "gate", "helper", *common], stack)
        for attempt in range(80):
            if pathlib.Path(socket).is_socket():
                break
            if helper.poll() is not None:
                raise RuntimeError("fixture Gate helper exited")
            time.sleep(.25)
        else:
            raise TimeoutError("fixture Gate helper socket")
        self.start([str(binary), "gate", "run", *common], stack)
        return json.loads(device.read_text())["gate_id"]

    def wait_for_gate(self, gate_id, require_revision=False):
        path = f"/api/v1/admin/gates/{gate_id}"
        for attempt in range(100):
            gate = self.request(path, 200)
            revision = gate.get("config_revision", 0)
            if (gate.get("online") and gate.get("version") == self.gate_version and
                    gate.get("center_version") == self.center_version and
                    (not require_revision or revision > 0 and gate.get("applied_revision") == revision)):
                print(f"{self.phase} center={self.center_version} gate={gate_id} version={self.gate_version} "
                      f"revision={revision} applied={gate.get('applied_revision')}", flush=True)
                return
            time.sleep(.5)
        detail = gate.get("last_error", "").lower()
        hint = next((reason for reason in ("permission denied", "operation not permitted", "not installed", "timeout")
                     if reason in detail), "unknown")
        raise TimeoutError(f"Gate {gate_id} revision={revision} applied={gate.get('applied_revision')} "
                           f"state={gate.get('state')} error_class={hint}")

    def subscription_outbound(self, token):
        document = self.request("/sub/" + token, 200, **{"User-Agent": "sing-box/1.14.3"})
        outbounds = [outbound for outbound in document.get("outbounds", [])
                     if outbound.get("type") == "shadowsocks" and outbound.get("server") == self.gate_host]
        if len(outbounds) != 1:
            raise RuntimeError("expected one fixture node in the original subscription")
        return outbounds[0]

    def check_client(self, outbound, stack):
        with ThreadingHTTPServer(("127.0.0.1", 18081), ProbeHandler) as target:
            worker = Thread(target=target.serve_forever, daemon=True)
            worker.start()
            try:
                config = {"log": {"level": "error"},
                          "inbounds": [{"type": "mixed", "listen": "127.0.0.1", "listen_port": 18082}],
                          "outbounds": [outbound], "route": {"final": outbound["tag"]}}
                path = pathlib.Path("/tmp/client.json")
                path.write_text(json.dumps(config))
                client = self.start([str(KERNEL), "run", "-c", str(path)], stack)
                for attempt in range(30):
                    if client.poll() is not None:
                        raise RuntimeError("fixture sing-box client exited")
                    connection = HTTPConnection("127.0.0.1", 18082, timeout=2)
                    try:
                        connection.request("GET", "http://127.0.0.1:18081/upgrade-proof")
                        response = connection.getresponse()
                        if response.status == 200 and response.read() == PROBE_BODY:
                            print(f"{self.phase} original client connected", flush=True)
                            return
                    except OSError:
                        pass
                    finally:
                        connection.close()
                    time.sleep(.25)
                raise TimeoutError("original client could not reach task-owned target through the Gate")
            finally:
                target.shutdown()
                worker.join()

    def run(self, binary, gate_binary):
        if self.phase == "old-rejected":
            result = subprocess.run(self.command(binary), capture_output=True, text=True, timeout=30)
            if result.returncode == 0 or MIGRATION_ERROR not in result.stderr:
                raise RuntimeError("released binary did not reject the migrated database")
            return
        if self.phase == "restore":
            shutil.copyfile(self.root / "before.json", self.config)
            if self.driver == "sqlite":
                for suffix in ("-wal", "-shm"):
                    self.db.with_name(self.db.name + suffix).unlink(missing_ok=True)
                self.db.unlink()
                self.sqlite_backup(self.root / "before.db", self.db)
        self.center_version = self.version(binary, "center")
        self.gate_version = self.version(gate_binary, "gate")
        if self.phase in {"forward-gate", "upgrade"} and self.center_version == self.gate_version:
            raise RuntimeError("mixed-version phase must use two different build versions")
        with ExitStack() as stack:
            kernel_version = self.stage_kernel()
            if self.phase == "seed":
                account = {"email": "upgrade@example.test", "password": "Fixture-" + secrets.token_hex(16)}
                token = subprocess.check_output([str(binary), "center", "--init", "--config", str(self.config)],
                                                text=True).split()[-1]
                config = json.loads(self.config.read_text())
                config["data_dir"] = str(self.root / "center-data")
                config["gate_updates_paused"] = True
                self.config.write_text(json.dumps(config))
                self.launch(binary, "setup_required", stack)
                storage = {"driver": self.driver, "sqlite_path": str(self.db)}
                if self.driver == "postgres":
                    password = os.environ["WEIR_UPGRADE_FIXTURE_PASSWORD"]
                    storage = {"driver": "postgres",
                               "postgres": {"host": "postgres", "port": 5432, "user": "weir", "password": password,
                                            "database": "weir", "ssl_mode": "disable"},
                               "redis": {"address": "redis:6379", "username": "default", "password": password}}
                self.request("/api/v1/setup/install", 201, {**account, "store": storage},
                             Authorization="Bearer " + token)
                invite = self.request("/api/v1/admin/gates/invite", 201, {"name": "Upgrade fixture"})
                gate_id = self.launch_gate(gate_binary, stack, invite["token"])
                self.wait_for_gate(gate_id)
                self.request(f"/api/v1/admin/gates/{gate_id}", 200,
                             {"public_host": self.gate_host, "kernel_pins": {"sing-box": kernel_version}}, method="PATCH")
                node = self.request("/api/v1/admin/nodes", 200,
                                    {"gate_id": gate_id, "preset": "ss2022", "skip_probe": True, "name": "Upgrade fixture"})
                client = {"email": "client@example.test", "password": secrets.token_hex(20)}
                user = self.request("/api/v1/admin/users", 200, {**client, "node_ids": [node["id"]], "unlimited": True})
                link = self.request(f"/api/v1/admin/users/{user['id']}/links", 200,
                                    {"name": "Upgrade fixture", "node_ids": [node["id"]]})
                self.wait_for_gate(gate_id, require_revision=True)
                outbound = self.subscription_outbound(link["token"])
                self.check_client(outbound, stack)
                sources = "/api/v1/admin/subscriptions/sources"
                profiles = "/api/v1/admin/subscriptions/profiles"
                source = self.request(sources, 200, {"name": "Upgrade source", "url": "https://subscription.example.test/fixture",
                                                      "enabled": False, "user_agent": "weir-upgrade-fixture"})
                profile = self.request(profiles, 200, {"name": "Upgrade group", "source_ids": [source["id"]]})
                self.request(f"{profiles}/{profile['id']}/grants", 200, {"user_ids": [user["id"]]}, method="PUT")
                subscription = self.source_and_grant(source["id"], profile["id"], client)
                self.state.write_text(json.dumps({"account": account, "link": link, "gate": gate_id, "user": user["id"],
                                                  "node": node["id"], "outbound": outbound, "client": client, "subscription": subscription}))
            else:
                state = json.loads(self.state.read_text(encoding="utf-8"))
                self.launch(binary, "ok", stack)
                self.request("/api/v1/auth/login", 200, state["account"])
                gate_id = self.launch_gate(gate_binary, stack)
                if gate_id != state["gate"]:
                    raise RuntimeError("fixture Gate identity changed")
                self.request(f"/api/v1/admin/gates/{gate_id}", 200, {"name": "Upgrade fixture"}, method="PATCH")
                self.wait_for_gate(gate_id, require_revision=True)
                link = state["link"]
                links = self.request(f"/api/v1/admin/users/{state['user']}/links", 200)["items"]
                if not any(item["id"] == link["id"] and item["token"] == link["token"] for item in links):
                    raise RuntimeError("original subscription link changed")
                if self.request(f"/api/v1/admin/nodes/{state['node']}", 200)["id"] != state["node"]:
                    raise RuntimeError("original node ID changed")
                if self.subscription_outbound(link["token"]) != state["outbound"]:
                    raise RuntimeError("original node outbound or credentials changed")
                self.check_client(state["outbound"], stack)
                original = state["subscription"]
                if self.source_and_grant(original["source"]["id"], original["profile"], state["client"]) != original:
                    raise RuntimeError("original subscription source or grant changed")
        if self.phase == "seed":
            shutil.copytree(self.root / "gate-state", self.root / "before-gate")
            shutil.copyfile(self.config, self.root / "before.json")
            if self.driver == "sqlite":
                self.sqlite_backup(self.db, self.root / "before.db")


def exercise(run, args):
    fixtures = run.path / "fixture"
    fixtures.mkdir(mode=0o700)
    extract_binary(args.archive, args.checksums, fixtures / "released")
    shutil.copyfile(args.candidate, fixtures / "candidate")
    docker = ["docker"]
    driver = args.driver
    password = secrets.token_hex(16)
    network = "weir-upgrade-" + run.id
    volume = "weir-upgrade-data-" + run.id
    postgres, redis = "weir-pg-" + run.id, "weir-redis-" + run.id
    networks = run.run("list-networks", docker + ["network", "ls", "-q"], check=True).stdout.decode().split()
    subnets = run.run("fixture-subnets", docker + ["network", "inspect", "--format",
                      "{{range .IPAM.Config}}{{println .Subnet}}{{end}}", *networks], check=True).stdout.decode().split()
    subnet = free_fixture_subnet(subnets)
    host = str(subnet.broadcast_address - 2)
    phase_env = dict(os.environ, WEIR_UPGRADE_FIXTURE_PASSWORD=password, POSTGRES_PASSWORD=password,
                     REDISCLI_AUTH=password, WEIR_UPGRADE_GATE_HOST=host)
    with ExitStack() as stack:
        run.run("volume", docker + ["volume", "create", volume], check=True)
        stack.callback(run.run, "remove-volume", docker + ["volume", "rm", volume])
        run.run("network", docker + ["network", "create", "--internal", "--subnet", str(subnet), network], check=True)
        stack.callback(run.run, "remove-network", docker + ["network", "rm", network])
        if driver == "postgres":
            run.run("postgres", docker + ["run", "-d", "--name", postgres, "--network", network, "--network-alias",
                    "postgres", "-e", "POSTGRES_USER=weir", "-e", "POSTGRES_DB=weir", "-e", "POSTGRES_PASSWORD",
                    "--tmpfs", "/var/lib/postgresql", "postgres:18-alpine"], env=phase_env, check=True)
            stack.callback(run.run, "remove-postgres", docker + ["rm", "-fv", postgres])
            run.run("redis", docker + ["run", "-d", "--name", redis, "--network", network, "--network-alias", "redis",
                    "-e", "WEIR_UPGRADE_FIXTURE_PASSWORD", "redis:8-alpine", "sh", "-c",
                    'exec docker-entrypoint.sh redis-server --requirepass "$WEIR_UPGRADE_FIXTURE_PASSWORD"'],
                    env=phase_env, check=True)
            stack.callback(run.run, "remove-redis", docker + ["rm", "-fv", redis])
            ready = run.run("postgres-ready", docker + ["exec", postgres, "sh", "-c",
                    "for attempt in $(seq 1 150); do pg_isready -h 127.0.0.1 -U weir -d weir && exit 0; sleep 1; done; exit 1"],
                    timeout=170)
            if ready.returncode:
                run.run("postgres-startup-log", docker + ["logs", "--tail", "50", postgres])
                raise RuntimeError("isolated PostgreSQL did not become ready")
        for phase, binary, gate_binary in (("seed", "released", "released"),
                                           ("forward-gate", "released", "candidate"),
                                           ("upgrade", "candidate", "released"),
                                           ("reopen", "candidate", "candidate"),
                                           ("old-rejected", "released", "released"),
                                           ("restore", "released", "released")):
            container = f"weir-{phase}-{run.id}"
            try:
                run.run(phase, docker + ["run", "--rm", "--name", container, "--network", network, "--ip", host,
                    "-e", "WEIR_UPGRADE_FIXTURE_PASSWORD", "-e", "WEIR_UPGRADE_GATE_HOST", "-v", f"{ROOT / 'deploy'}:/runner:ro",
                    "-v", f"{fixtures}:/inputs", "-v", f"{volume}:/fixture",
                    "-v", f"{args.singbox.resolve()}:{KERNEL.as_posix()}:ro", "python:3.12-slim", "python", "-B",
                    "/runner/check_upgrade.py", "--fixture", phase, driver, binary, gate_binary],
                    env=phase_env, check=True, timeout=420)
            except subprocess.TimeoutExpired:
                run.run(phase + "-cleanup", docker + ["rm", "-f", container], timeout=30)
                raise
            if driver == "postgres" and phase == "seed":
                run.run("pg-backup", docker + ["exec", postgres, "pg_dump", "-U", "weir", "-Fc", "-f", "/tmp/release.dump",
                        "weir"], check=True)
                run.run("pg-verify-backup", docker + ["exec", postgres, "pg_restore", "-l", "/tmp/release.dump"], check=True)
                run.run("redis-backup", docker + ["exec", "-e", "REDISCLI_AUTH", redis, "redis-cli", "SAVE"],
                        env=phase_env, check=True)
                run.run("redis-copy", docker + ["cp", f"{redis}:/data/dump.rdb", str(fixtures / "before.rdb")], check=True)
            if driver == "postgres" and phase == "old-rejected":
                run.run("pg-reset", docker + ["exec", postgres, "dropdb", "-U", "weir", "weir"], check=True)
                run.run("pg-create", docker + ["exec", postgres, "createdb", "-U", "weir", "weir"], check=True)
                run.run("pg-restore", docker + ["exec", postgres, "pg_restore", "-U", "weir", "-d", "weir",
                        "--exit-on-error", "/tmp/release.dump"], check=True)
                run.run("redis-stop", docker + ["stop", redis], check=True)
                run.run("redis-restore", docker + ["cp", str(fixtures / "before.rdb"), f"{redis}:/data/dump.rdb"], check=True)
                run.run("redis-start", docker + ["start", redis], check=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--fixture", nargs=4, help=argparse.SUPPRESS)
    parser.add_argument("--driver", choices=["sqlite", "postgres"], default="sqlite")
    parser.add_argument("--archive", type=pathlib.Path)
    parser.add_argument("--checksums", type=pathlib.Path)
    parser.add_argument("--candidate", type=pathlib.Path)
    parser.add_argument("--singbox", type=pathlib.Path, default=ROOT / "dist/cores/sing-box-1.14.3-stats/sing-box")
    args = parser.parse_args()
    if args.fixture:
        phase, driver, binary, gate_binary = args.fixture
        root = pathlib.Path("/fixture")
        def program(name):
            path = pathlib.Path("/tmp") / name
            shutil.copyfile(pathlib.Path("/inputs") / name, path)
            path.chmod(0o700)
            return path
        center_program = program(binary)
        gate_program = center_program if gate_binary == binary else program(gate_binary)
        try:
            CenterFixture(root, driver, phase).run(center_program, gate_program)
        finally:
            log = root / f"center-{phase}.log"
            if log.is_file():
                shutil.copyfile(log, pathlib.Path("/inputs") / log.name)
        print(f"{driver} {phase} passed")
        return 0
    if not all((args.archive, args.checksums, args.candidate)):
        parser.error("--archive, --checksums and --candidate are required")
    if not args.singbox.is_file():
        parser.error("--singbox must be the Linux sing-box fixture binary")
    with ScenarioRun(ROOT, ROOT / "dist" / "checks") as run:
        exercise(run, args)
    return run.exit_code


if __name__ == "__main__":
    raise SystemExit(main())
