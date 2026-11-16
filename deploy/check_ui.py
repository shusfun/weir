"""Scan console spacing and overflow using ego-browser, without screenshots."""
import argparse
import json
import pathlib

from scenario_run import ScenarioRun


DEFAULT_ROUTES = "/overview,/gates,/proxies,/rules,/users,/links,/subscriptions,/groups,/pages,/kernels,/ops/observe,/ops/audit,/system,/security"


def report_exit(report):
    expected = {(route, width, theme) for route in report["meta"]["routes"]
                for width in report["meta"]["widths"] for theme in report["meta"]["themes"]}
    pages = {(s["route"], s["width"], s["theme"]) for s in report["states"] if s["state"] == "页面"}
    if not expected or report["meta"].get("aborted") or not expected.issubset(pages) or any(v["check"] == "error" for v in report["violations"]):
        return 2
    return 1 if report["violations"] else 0


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--base", default="http://127.0.0.1:22340", help="Console origin, including an isolated browser fixture")
    parser.add_argument("--routes", default=DEFAULT_ROUTES)
    parser.add_argument("--widths", default="390,768,1440")
    parser.add_argument("--themes", default="dark,light")
    parser.add_argument("--popups", choices=["0", "1"], default="0")
    args = parser.parse_args()
    root = pathlib.Path(__file__).resolve().parent.parent
    with ScenarioRun(root, root / "dist" / "checks") as run:
        out = "spacing-" + run.id
        run.run("console-spacing", ["node", "apps/console/scan/scan.mjs", "--base", args.base, "--routes", args.routes,
                "--widths", args.widths, "--themes", args.themes, "--checks", "spacing,overflow",
                "--popups", args.popups, "--tabs", "0", "--layout-states", "1", "--run-id", run.id, "--out", out], check=True, timeout=1800)
        report_path = root / "apps/console/scan/out" / out / "report.json"
        report = json.loads(report_path.read_text(encoding="utf-8"))
        code = report_exit(report)
        print(f"UI_SPACING_RESULT run_id={run.id} exit={code} states={len(report['states'])} findings={len(report['violations'])} report={report_path}", flush=True)
        if code:
            raise SystemExit(code)
    return run.exit_code


if __name__ == "__main__":
    raise SystemExit(main())
