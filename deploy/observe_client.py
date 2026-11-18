"""Read-only, bounded observation of mihomo selection and health events."""
import argparse
import collections
import datetime
import hashlib
import ipaddress
import json
import os
import pathlib
import re
import socket
import sys
import threading
import time
import urllib.parse
import urllib.request

from scenario_run import ScenarioRun


def now():
    return datetime.datetime.now(datetime.timezone.utc).isoformat()


class RejectRedirects(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


class ReadOnlyController:
    def __init__(self, address, secret):
        url = urllib.parse.urlsplit(address)
        if (url.scheme != "http" or not url.hostname or not ipaddress.ip_address(url.hostname).is_loopback
                or url.username or url.password or url.path not in ("", "/") or url.query or url.fragment):
            raise ValueError("Controller must be an HTTP loopback address without credentials or a path")
        self.base, self.secret = address.rstrip("/"), secret
        self.opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), RejectRedirects())

    def open(self, path, timeout=2):
        if path not in ("/proxies", "/providers/proxies", "/connections", "/logs?level=debug"):
            raise ValueError("Not a read-only observation endpoint")
        return self.opener.open(urllib.request.Request(self.base + path,
            headers={"Authorization": "Bearer " + self.secret}), timeout=timeout)

    def request(self, path):
        with self.open(path) as response:
            value = json.load(response)
        if not isinstance(value, dict):
            raise ValueError("Invalid controller response")
        return value


def mapping(value):
    if not isinstance(value, dict):
        raise ValueError("Expected object")
    return value


def sequence(value):
    if not isinstance(value, list):
        raise ValueError("Expected array")
    return value


def label(value):
    if not isinstance(value, str):
        raise ValueError("Expected label")
    return value


class Timeline:
    def __init__(self):
        self.groups = {}
        self.connections = None
        self.health_seen = set()
        self.events = []

    def snapshot(self, proxies, providers, connections, observed_at):
        groups = {}
        for name, raw in mapping(proxies.get("proxies")).items():
            p = mapping(raw)
            if "now" in p:
                groups[label(name)] = {"selected": label(p["now"]), "type": label(p.get("type")),
                                       "members": [label(x) for x in sequence(p.get("all"))]}
        health = []
        new_health = []
        for name, raw in mapping(providers.get("providers")).items():
            for p in sequence(mapping(raw).get("proxies")):
                p = mapping(p)
                # Generic history can be alive on HTTP 502; URL-specific history
                # includes expected-status and is the state used by automatic groups.
                extra = mapping(p.get("extra", {}))
                states = [(hashlib.sha256(label(url).encode()).hexdigest()[:16], mapping(state))
                          for url, state in extra.items()] if extra else [(None, p)]
                for probe_id, state in states:
                    history = sequence(state.get("history"))
                    for row in history:
                        row = mapping(row)
                        stamp, delay = label(row.get("time")), row.get("delay")
                        if type(delay) is not int or delay < 0:
                            raise ValueError("Invalid probe delay")
                        key = (label(name), label(p.get("name")), probe_id, stamp, delay)
                        new_health.append((key, {"kind": "probe_result", "observed_at": observed_at,
                            "probe_at": stamp, "provider": name, "node": p["name"], "probe_id": probe_id,
                            "delay_ms": delay, "healthy": delay > 0}))
                    alive = state.get("alive") if history else None
                    if alive is not None and type(alive) is not bool:
                        raise ValueError("Invalid health state")
                    health.append({"provider": name, "node": p.get("name"), "probe_id": probe_id, "alive": alive,
                                   "last_probe": history[-1] if history else None})
        current = {}
        counts = collections.Counter()
        for raw in sequence(connections.get("connections")):
            c = mapping(raw)
            chain = tuple(label(x) for x in sequence(c.get("chains")))
            current[label(c.get("id"))] = (chain, label(c.get("start")))
            counts[chain] += 1
        # Commit only complete snapshots, so a malformed response cannot invent a switch.
        for group, selection in groups.items():
            if group in self.groups and self.groups[group]["selected"] != selection["selected"]:
                self.events.append({"kind": "selection_observed", "observed_at": observed_at,
                    "group": group, "previous": self.groups[group]["selected"], "selected": selection["selected"],
                    "exact_switch_at": None})
        for key, event in new_health:
            if key not in self.health_seen:
                self.events.append(event)
        self.health_seen = {key for key, _ in new_health}
        if self.connections is not None:
            for identity, (chain, started) in current.items():
                if identity not in self.connections:
                    self.events.append({"kind": "new_connection_observed", "observed_at": observed_at,
                                        "connection_started_at": started, "chain": chain})
        residual = []
        for chain, count in counts.items():
            for i, member in enumerate(chain):
                if i and member in groups and groups[member]["selected"] != chain[i - 1]:
                    residual.append({"group": member, "previous_member": chain[i - 1], "count": count, "chain": chain})
        self.groups, self.connections = groups, current
        return {"observed_at": observed_at, "groups": groups, "health": health,
                "connections": [{"chain": chain, "count": count} for chain, count in counts.items()],
                "residual_connections": residual}


def log_event(raw):
    raw = mapping(raw)
    message = label(raw.get("payload"))
    kind = None
    if "Health Checking" in message or "Health Checked" in message:
        kind = "health_log"
    elif "activate health check" in message or "ProxyGroup:" in message:
        kind = "group_failure_log"
    elif "timeout" in message.lower():
        kind = "direct_timeout" if re.search(r"\bdial DIRECT\b", message) else "proxy_timeout"
    elif "connection reset" in message.lower():
        kind = "connection_reset"
    if kind is None:
        return None
    # Never retain arbitrary log text, destinations, URLs or authentication material.
    event = {"kind": kind, "observed_at": now()}
    correlation = re.search(r"\{([0-9a-f-]{36})\}", message)
    if correlation:
        event["health_check_id"] = correlation[1]
    return event


def observe(controller, duration, output, interval=1):
    stop = threading.Event()
    events, gaps, snapshots = [], [], []
    timeline = Timeline()
    stream_socket = []

    def logs():
        while not stop.is_set():
            try:
                with controller.open("/logs?level=debug", timeout=1) as stream:
                    sock = stream.fp.raw._sock
                    stream_socket[:] = [sock]
                    sock.settimeout(None)
                    while not stop.is_set():
                        line = stream.readline()
                        if not line:
                            raise EOFError()
                        event = log_event(json.loads(line))
                        if event:
                            events.append(event)
            except Exception as error:
                if not stop.is_set():
                    gaps.append({"kind": "log_gap", "observed_at": now(), "error_type": type(error).__name__})
                    stop.wait(.1)

    worker = threading.Thread(target=logs, name="client-observer")
    worker.start()
    start = time.monotonic()
    started_at = now()
    try:
        while time.monotonic() - start < duration:
            tick = time.monotonic()
            try:
                proxies = controller.request("/proxies")
                providers = controller.request("/providers/proxies")
                connections = controller.request("/connections")
                snapshots.append(timeline.snapshot(proxies, providers, connections, now()))
            except Exception as error:
                gaps.append({"kind": "snapshot_gap", "observed_at": now(), "error_type": type(error).__name__})
            stop.wait(max(0, min(interval - (time.monotonic() - tick), duration - (time.monotonic() - start))))
    finally:
        stop.set()
        for sock in stream_socket:
            try:
                sock.shutdown(socket.SHUT_RDWR)
            except OSError:
                pass
        worker.join(timeout=3)
    report = {"started_at": started_at, "finished_at": now(), "requested_seconds": duration,
        "readonly": True, "complete": bool(snapshots) and not gaps and not worker.is_alive(),
        "historical_selection_changes": None, "snapshots": snapshots,
        "events": timeline.events + events, "gaps": gaps,
        "direct_timeouts": sum(e["kind"] == "direct_timeout" for e in events),
        "proxy_timeouts": sum(e["kind"] == "proxy_timeout" for e in events),
        "limits": ["Selection changes are observations, not exact switch timestamps.",
                   "Existing connections are not migrated or closed.",
                   "Only connections still present at sampling time are visible."]}
    pathlib.Path(output).write_text(json.dumps(report, ensure_ascii=False, indent=2), encoding="utf-8")
    return report


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--controller", default="http://127.0.0.1:9090")
    parser.add_argument("--secret-env", default="WEIR_CLIENT_CONTROLLER_SECRET")
    parser.add_argument("--duration", type=int, default=180)
    parser.add_argument("--worker-output", type=pathlib.Path, help=argparse.SUPPRESS)
    args = parser.parse_args(argv)
    if not 1 <= args.duration <= 3600:
        parser.error("Duration must be 1-3600 seconds")
    if not re.fullmatch(r"[A-Za-z_][A-Za-z0-9_]*", args.secret_env):
        parser.error("Invalid secret environment variable name")
    controller = ReadOnlyController(args.controller, os.environ.get(args.secret_env, ""))
    if args.worker_output:
        report = observe(controller, args.duration, args.worker_output)
        print(json.dumps({"complete": report["complete"], "samples": len(report["snapshots"]),
                          "direct_timeouts": report["direct_timeouts"], "proxy_timeouts": report["proxy_timeouts"]}))
        return 0 if report["complete"] else 2
    root = pathlib.Path(__file__).resolve().parent.parent
    with ScenarioRun(root, root / "dist" / "diagnostics") as run:
        output = run.path / "observation.json"
        run.run("observe-client", [sys.executable, __file__, "--controller", args.controller,
            "--secret-env", args.secret_env, "--duration", str(args.duration), "--worker-output", str(output)],
            timeout=args.duration + 20, recovery="Check the loopback controller and log stream; no client configuration was changed")
        if output.is_file():
            run.artifact("client-observation", output)
    return run.exit_code


if __name__ == "__main__":
    raise SystemExit(main())
