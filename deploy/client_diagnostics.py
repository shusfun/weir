"""Bounded, repeatable mihomo diagnostics; controller credentials stay local."""
import argparse
import datetime
import json
import pathlib
import re
import subprocess
import threading
import time
import urllib.parse
import urllib.request


class Controller:
    def __init__(self, config):
        import yaml
        self.config = yaml.safe_load(pathlib.Path(config).read_text(encoding="utf-8"))
        address = self.config["external-controller"]
        if not address.startswith("127.0.0.1:"):
            raise ValueError("controller must listen on loopback")
        self.base = "http://" + address
        self.opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))

    def open(self, path, method="GET", data=None, timeout=10):
        return self.opener.open(urllib.request.Request(
            self.base + path, method=method,
            data=None if data is None else json.dumps(data).encode(),
            headers={"Authorization": "Bearer " + self.config.get("secret", ""),
                     "Content-Type": "application/json"}), timeout=timeout)

    def request(self, path, method="GET", data=None):
        with self.open(path, method, data) as response:
            body = response.read()
            return json.loads(body) if body else None


def sample(port, url, timeout=20):
    result = subprocess.run([
        "curl.exe" if __import__("os").name == "nt" else "curl",
        "--proxy", f"http://127.0.0.1:{port}", "--noproxy", "", "--max-time", str(timeout),
        "--silent", "--show-error", "--output", __import__("os").devnull,
        "--write-out", "%{json}", url], capture_output=True, text=True, timeout=timeout + 5)
    try:
        raw = json.loads(result.stdout)
    except ValueError:
        raw = {}
    # Do not save arbitrary curl errors, URLs or controller payloads.
    return {"exit_code": result.returncode, **{key: raw.get(key) for key in (
        "http_code", "time_connect", "time_appconnect", "time_starttransfer", "time_total",
        "size_download", "speed_download")}}


def observe(controller, stop, counters):
    while not stop.is_set():
        try:
            with controller.open("/logs?level=debug", timeout=2) as stream:
                while not stop.is_set():
                    line = stream.readline()
                    if not line:
                        break
                    event = json.loads(line)
                    payload = event.get("payload", "")
                    message = payload.lower()
                    scoped = "speed.cloudflare.com:443" in message
                    if scoped:
                        route = re.search(r"using ([^\r\n]+)$", payload)
                        if route:
                            routes = counters.setdefault("test_routes", {})
                            routes[route.group(1)] = routes.get(route.group(1), 0) + 1
                    for name, needle in (("ipv6_unreachable", "unreachable network"),
                                         ("timeout", "timeout"), ("udp_unsupported", "udp is not supported")):
                        if needle in message:
                            counters[name] = counters.get(name, 0) + 1
                            if scoped:
                                counters["test_" + name] = counters.get("test_" + name, 0) + 1
                    level = event.get("type", "unknown")
                    counters[level] = counters.get(level, 0) + 1
        except (OSError, ValueError):
            stop.wait(.25)


def run(controller, nodes, repeats, out, ipv4=False):
    groups = controller.request("/proxies")["proxies"]
    previous = groups["PROXY"]["now"]
    chain_previous = groups.get("链式", {}).get("now")
    config = controller.request("/configs")
    before = {k: config.get(k) for k in ("mode", "ipv6", "log-level", "mixed-port")}
    report = {"run_id": out.name, "started": datetime.datetime.now(datetime.timezone.utc).isoformat(),
              "before": before, "previous_selection": previous, "ipv4_trial": ipv4, "nodes": [], "complete": False}
    out.mkdir(parents=True, exist_ok=False)
    (out / "report.json").write_text(json.dumps(report, ensure_ascii=False, indent=2), encoding="utf-8")
    try:
        controller.request("/configs", "PATCH", {"log-level": "debug"})
        if ipv4:
            controller.request("/configs", "PATCH", {"ipv6": False})
        for name in nodes:
            counters, stop = {}, threading.Event()
            group = "链式" if name in groups.get("链式", {}).get("all", []) else "PROXY"
            if group != "PROXY":
                controller.request("/proxies/PROXY", "PUT", {"name": group})
            path = "/proxies/" + urllib.parse.quote(group, safe="")
            controller.request(path, "PUT", {"name": name})
            actual = controller.request(path)["now"]
            if actual != name:
                raise RuntimeError("proxy selection was not applied")
            worker = threading.Thread(target=observe, args=(controller, stop, counters), daemon=True)
            worker.start()
            record = {"name": name, "actual": actual, "samples": []}
            try:
                for _ in range(repeats):
                    for size in (0, 262144, 2097152):
                        url = f"https://speed.cloudflare.com/__down?bytes={size}"
                        record["samples"].append({"bytes_requested": size, **sample(before["mixed-port"], url)})
            finally:
                stop.set()
                worker.join(timeout=4)
            record["log_counts"] = dict(counters)
            report["nodes"].append(record)
            (out / "report.json").write_text(json.dumps(report, ensure_ascii=False, indent=2), encoding="utf-8")
        report["complete"] = True
    finally:
        restore_errors = []
        operations = [("/proxies/PROXY", "PUT", {"name": previous})]
        if chain_previous:
            operations.append(("/proxies/" + urllib.parse.quote("链式"), "PUT", {"name": chain_previous}))
        operations.append(("/configs", "PATCH", {"ipv6": before["ipv6"], "log-level": before["log-level"]}))
        for path, method, data in operations:
            try:
                controller.request(path, method, data)
            except (OSError, ValueError):
                restore_errors.append(path)
        report["finished"] = datetime.datetime.now(datetime.timezone.utc).isoformat()
        report["restore_errors"] = restore_errors
        report["restored"] = not restore_errors
        after = controller.request("/configs")
        report["after"] = {k: after.get(k) for k in before}
        report["restored"] = report["restored"] and report["after"] == before and controller.request("/proxies/PROXY")["now"] == previous
        (out / "report.json").write_text(json.dumps(report, ensure_ascii=False, indent=2), encoding="utf-8")
    return report


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--config", required=True)
    parser.add_argument("--out", default="dist/diagnostics")
    parser.add_argument("--repeats", type=int, choices=range(1, 4), default=2)
    parser.add_argument("--ipv4", action="store_true")
    parser.add_argument("--node", action="append")
    parser.add_argument("--compare")
    args = parser.parse_args()
    controller = Controller(args.config)
    groups = controller.request("/proxies")["proxies"]
    nodes = args.node or [name for name in groups["PROXY"].get("all", []) if name.startswith("[直连]")]
    # Chained nodes are deliberately nested, so include provider members too.
    if not args.node:
        nodes = list(dict.fromkeys(nodes + [name for name in groups.get("链式", {}).get("all", []) if name.startswith("[链式]")]))
    if not nodes:
        raise RuntimeError("no testable nodes")
    out = pathlib.Path(args.out) / datetime.datetime.now().strftime("%Y%m%d-%H%M%S")
    report = run(controller, nodes, args.repeats, out, args.ipv4)
    if args.compare:
        previous = json.loads(pathlib.Path(args.compare).read_text(encoding="utf-8"))
        report["compared_to"] = previous["run_id"]
        report["baseline_nodes"] = previous["nodes"]
        (out / "report.json").write_text(json.dumps(report, ensure_ascii=False, indent=2), encoding="utf-8")
    print(json.dumps({"complete": report["complete"], "report": str(out.resolve() / "report.json")}, ensure_ascii=True))


if __name__ == "__main__":
    main()
