import json
import pathlib
import tempfile
import unittest
from unittest.mock import Mock, patch

import isolated_client_diagnostics as diag


class IsolatedDiagnosticsTest(unittest.TestCase):
    def test_measure_failure_stops_owned_process_and_removes_credentials(self):
        process = Mock(pid=123)
        process.poll.side_effect = lambda: 0 if process.terminate.called else None
        node = {"name": "test", "type": "http", "server": "example.com", "port": 80,
                "password": "must-not-survive"}
        with tempfile.TemporaryDirectory() as directory:
            with patch.object(diag, "free_ports", return_value=[32123]), \
                 patch.object(diag.subprocess, "run", return_value=Mock(returncode=0)), \
                 patch.object(diag.subprocess, "Popen", return_value=process), \
                 patch.object(diag.socket, "create_connection"), \
                 self.assertRaisesRegex(RuntimeError, "sample failed"):
                diag.compare("client", [node], "WLAN", ["https://example.com"], 1, directory,
                             measure=Mock(side_effect=RuntimeError("sample failed")))
            process.terminate.assert_called_once()
            files = list(pathlib.Path(directory).rglob("*"))
            self.assertEqual([p.name for p in files], ["comparison.json"])
            text = files[0].read_text(encoding="utf-8")
            report = json.loads(text)
            self.assertFalse(report["complete"])
            self.assertTrue(report["process_stopped"])
            self.assertNotIn(node["password"], text)

    def test_provider_rejects_unresolved_dependencies(self):
        with tempfile.TemporaryDirectory() as directory:
            path = pathlib.Path(directory) / "provider.json"
            path.write_text(json.dumps({"proxies": [{"name": "node", "dialer-proxy": "missing"}]}))
            with self.assertRaisesRegex(ValueError, "complete upstream"):
                diag.load_nodes([path], ["node"])


if __name__ == "__main__":
    unittest.main()
