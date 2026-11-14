import pathlib
import tempfile
import unittest
from unittest.mock import patch
import client_diagnostics as diag


class Controller:
    def __init__(self):
        self.config = {"mode": "rule", "ipv6": True, "log-level": "info", "mixed-port": 7897}
        self.groups = {"PROXY": {"now": "original", "all": ["direct", "链式"]},
                       "链式": {"now": "chain-original", "all": ["chain"]}}

    def request(self, path, method="GET", data=None):
        if path == "/configs":
            if data: self.config.update(data)
            return dict(self.config)
        if path == "/proxies": return {"proxies": self.groups}
        name = diag.urllib.parse.unquote(path.rsplit("/", 1)[1])
        if data: self.groups[name].update(now=data["name"])
        return self.groups[name]


class DiagnosticsTest(unittest.TestCase):
    def test_restore_after_failure(self):
        controller = Controller()
        with tempfile.TemporaryDirectory() as root:
            out = pathlib.Path(root) / "failure"
            with patch.object(diag, "observe"), patch.object(diag, "sample", side_effect=RuntimeError("failed")):
                with self.assertRaises(RuntimeError): diag.run(controller, ["chain"], 1, out, True)
            report = diag.json.loads((out / "report.json").read_text(encoding="utf-8"))
            self.assertTrue(report["restored"])
            self.assertFalse(report["complete"])
            self.assertEqual(controller.groups["链式"]["now"], "chain-original")

    def test_bounded_report(self):
        controller = Controller()
        with tempfile.TemporaryDirectory() as root:
            with patch.object(diag, "observe"), patch.object(diag, "sample", return_value={"exit_code": 0}) as sample:
                report = diag.run(controller, ["direct", "chain"], 2, pathlib.Path(root) / "success", True)
            self.assertEqual(sample.call_count, 12)
            self.assertEqual([n["actual"] for n in report["nodes"]], ["direct", "chain"])
            self.assertTrue(report["restored"] and report["complete"])
            self.assertNotIn("secret", diag.json.dumps(report))


if __name__ == "__main__":
    unittest.main()
