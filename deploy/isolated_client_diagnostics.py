"""Compare cached proxy nodes without changing the user's running client."""
import argparse
import collections
import json
import os
import pathlib
import socket
import subprocess
import tempfile
import time

from client_diagnostics import sample
from scenario_run import ScenarioRun


def load_nodes(files, names):
    import yaml
    found = {}
    for filename in files:
        doc = yaml.safe_load(pathlib.Path(filename).read_text(encoding="utf-8"))
        if not isinstance(doc, dict) or not isinstance(doc.get("proxies"), list):
            raise ValueError("Expected a proxy provider file")
        for node in doc["proxies"]:
            if not isinstance(node, dict) or not isinstance(node.get("name"), str):
                raise ValueError("Invalid proxy provider member")
            if node["name"] in names:
                if node["name"] in found and found[node["name"]] != node:
                    raise ValueError("Conflicting definitions of a selected node")
                found[node["name"]] = node
    if any(name not in found for name in names):
        raise ValueError("A selected node is missing from the provider files")
    for name in names:
        seen = set()
        current = name
        while current:
            if current in seen or current not in found:
                raise ValueError("A dependent proxy needs its complete upstream configuration without cycles")
            seen.add(current)
            current = found[current].get("dialer-proxy")
    return [found[name] for name in names]


def free_ports(count):
    sockets = []
    try:
        for _ in range(count):
            s = socket.socket()
            sockets.append(s)
            s.bind(("127.0.0.1", 0))
        return [s.getsockname()[1] for s in sockets]
    finally:
        for s in sockets:
            s.close()


def stop_process(process):
    if process.poll() is None:
        process.terminate()
        try:
            process.wait(timeout=5)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait(timeout=5)


def compare(binary, nodes, interface, targets, repeats, out, measure=sample):
    ports = free_ports(len(nodes))
    report = {"interface": interface, "nodes": [], "complete": False,
              "process_stopped": False, "tun": False, "controller": False}
    out = pathlib.Path(out)
    out.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="client-", dir=out) as directory:
        proxies = [{**node, "interface-name": interface} for node in nodes]
        config = {"allow-lan": False, "mode": "rule", "log-level": "warning",
                  "ipv6": False, "interface-name": interface, "tun": {"enable": False},
                  "external-controller": "", "external-controller-pipe": "",
                  "dns": {"enable": False}, "proxies": proxies,
                  "listeners": [{"name": f"listener-{i}", "type": "mixed", "listen": "127.0.0.1",
                                 "port": port, "proxy": nodes[i]["name"]} for i, port in enumerate(ports)],
                  "rules": ["MATCH,REJECT"], "profile": {"store-selected": False}}
        path = pathlib.Path(directory) / "config.json"
        path.write_text(json.dumps(config, ensure_ascii=False), encoding="utf-8")
        command = [str(binary), "-d", directory, "-f", str(path)]
        options = {"creationflags": subprocess.CREATE_NO_WINDOW} if os.name == "nt" else {}
        check = subprocess.run(command + ["-t"], capture_output=True, timeout=15, **options)
        if check.returncode:
            raise RuntimeError("Isolated client configuration check failed; no client was started")
        with (pathlib.Path(directory) / "client.log").open("wb") as log:
            process = subprocess.Popen(command, stdout=log, stderr=log, **options)
            report["pid"] = process.pid
            try:
                deadline = time.monotonic() + 10
                for port in ports:
                    while True:
                        if process.poll() is not None or time.monotonic() >= deadline:
                            raise RuntimeError("Isolated client failed to listen")
                        try:
                            with socket.create_connection(("127.0.0.1", port), timeout=.2):
                                break
                        except OSError:
                            time.sleep(.05)
                report["nodes"] = [{"name": n["name"], "type": n.get("type"), "samples": []} for n in nodes]
                # Alternate paths so a time-varying upstream is not compared in separate blocks.
                for iteration in range(repeats):
                    for target in targets:
                        for port, result in zip(ports, report["nodes"]):
                            result["samples"].append({"iteration": iteration + 1, "target": target,
                                                      **measure(port, target, 12)})
                report["complete"] = True
            finally:
                stop_process(process)
                report["process_stopped"] = process.poll() is not None
                (out / "comparison.json").write_text(json.dumps(report, ensure_ascii=False, indent=2), encoding="utf-8")
        log_text = (pathlib.Path(directory) / "client.log").read_text(encoding="utf-8", errors="replace")
        report["log_counts"] = dict(collections.Counter(
            label for line in log_text.splitlines() for label in
            ("timeout", "502 Bad Gateway", "connection reset", "unreachable") if label in line))
    (out / "comparison.json").write_text(json.dumps(report, ensure_ascii=False, indent=2), encoding="utf-8")
    return report


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=pathlib.Path, required=True)
    parser.add_argument("--provider", action="append", required=True)
    parser.add_argument("--node", action="append", required=True)
    parser.add_argument("--interface", required=True)
    parser.add_argument("--target", action="append", required=True)
    parser.add_argument("--repeats", type=int, choices=range(1, 4), default=2)
    args = parser.parse_args()
    if not args.binary.is_file() or not 1 <= len(args.node) <= 4 or not 1 <= len(args.target) <= 4:
        parser.error("Provide a binary, 1-4 nodes and 1-4 targets")
    from urllib.parse import urlsplit
    for target in args.target:
        url = urlsplit(target)
        if url.scheme != "https" or not url.hostname or url.username or url.password or url.fragment:
            parser.error("Targets must be public HTTPS URLs without credentials or fragments")
    nodes = load_nodes(args.provider, args.node)
    root = pathlib.Path(__file__).resolve().parent.parent
    with ScenarioRun(root, root / "dist" / "diagnostics") as run:
        run.run("client-version", [str(args.binary), "-v"], timeout=10)
        report = compare(args.binary, nodes, args.interface, args.target, args.repeats, run.path)
        run.require("comparison completed and isolated process stopped",
                    report["complete"] and report["process_stopped"], "Inspect comparison.json before rerunning")
        print(json.dumps({"run_id": run.id, "comparison": str(run.path / "comparison.json"),
                          "complete": report["complete"]}, ensure_ascii=True), flush=True)
    return run.exit_code


if __name__ == "__main__":
    raise SystemExit(main())
