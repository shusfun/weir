import copy
import io
import json
import pathlib
import tempfile
import time
import unittest
import urllib.request

import observe_client as diag


def fixtures(selected="A", connections=None, delay=20):
    return ({"proxies": {"chain": {"now": selected, "all": ["A", "B"], "type": "URLTest"}}},
            {"providers": {"chain": {"proxies": [{"name": "A", "alive": delay > 0,
                "history": [{"time": "2026-11-18T00:00:00Z", "delay": delay}]}]}}},
            {"connections": connections or []})


class ObservationTest(unittest.TestCase):
    def test_status_specific_failure_overrides_generic_alive(self):
        data = fixtures()
        proxy = data[1]["providers"]["chain"]["proxies"][0]
        proxy["extra"] = {"https://probe.test/secret": {"alive": False,
            "history": [{"time": "2026-11-18T00:00:00Z", "delay": 0}]}}
        timeline = diag.Timeline()
        state = timeline.snapshot(*data, "now")
        self.assertFalse(state["health"][0]["alive"])
        self.assertFalse(timeline.events[0]["healthy"])
        self.assertNotIn("secret", json.dumps(state))

    def test_switch_time_is_observed_and_old_connections_remain(self):
        timeline = diag.Timeline()
        conn = {"id": "old", "chains": ["A", "chain"], "start": "before-observation"}
        timeline.snapshot(*fixtures(connections=[conn]), "first")
        new = {"id": "new", "chains": ["B", "chain"], "start": "after-switch"}
        snapshot = timeline.snapshot(*fixtures("B", [conn, new], 0), "second")
        switches = [e for e in timeline.events if e["kind"] == "selection_observed"]
        self.assertEqual(switches[0]["observed_at"], "second")
        self.assertIsNone(switches[0]["exact_switch_at"])
        self.assertEqual(snapshot["residual_connections"][0]["count"], 1)
        self.assertEqual(snapshot["residual_connections"][0]["previous_member"], "A")
        self.assertEqual(sum(e["kind"] == "new_connection_observed" for e in timeline.events), 1)
        self.assertFalse([e for e in timeline.events if e["kind"] == "probe_result"][-1]["healthy"])

    def test_malformed_input_does_not_change_selection(self):
        timeline = diag.Timeline()
        timeline.snapshot(*fixtures(), "first")
        data = fixtures("B")
        data[2]["connections"] = "malformed"
        before = copy.deepcopy(timeline.events)
        with self.assertRaises(ValueError): timeline.snapshot(*data, "bad")
        self.assertEqual(timeline.groups["chain"]["selected"], "A")
        self.assertEqual(timeline.events, before)

    def test_log_redaction_and_direct_classification(self):
        event = diag.log_event({"payload": "[TCP] dial DIRECT (GeoIP/CN) 10.0.0.1 --> https://secret-token@example.test:443 timeout"})
        self.assertEqual(event["kind"], "direct_timeout")
        self.assertNotIn("secret", json.dumps(event))
        self.assertNotIn("example", json.dumps(event))

    def test_readonly_endpoint_boundary(self):
        for address in ("http://example.test:9090", "http://127.0.0.1:9090/path", "http://user:pass@127.0.0.1"):
            with self.assertRaises(ValueError): diag.ReadOnlyController(address, "secret")
        controller = diag.ReadOnlyController("http://127.0.0.1:9090", "secret")
        redirects = [handler for handler in controller.opener.handlers if isinstance(handler, urllib.request.HTTPRedirectHandler)]
        self.assertEqual(len(redirects), 1)
        self.assertIsNone(redirects[0].redirect_request(None, None, 302, "Found", {}, "http://localhost/"))
        for path in ("/providers/proxies/chain/healthcheck", "/restart", "/configs"):
            with self.assertRaises(ValueError): controller.open(path)

    def test_bounded_collection_survives_log_failure_without_fake_success(self):
        class Controller:
            def open(self, path, timeout): raise OSError("secret must not appear")
            def request(self, path):
                return dict(zip(("/proxies", "/providers/proxies", "/connections"), fixtures()))[path]
        with tempfile.TemporaryDirectory() as directory:
            output = pathlib.Path(directory) / "report.json"
            report = diag.observe(Controller(), .03, output, interval=.01)
            self.assertFalse(report["complete"])
            self.assertTrue(report["readonly"])
            self.assertTrue(report["snapshots"])
            self.assertIsNone(report["historical_selection_changes"])
            self.assertNotIn("secret", output.read_text())


if __name__ == "__main__":
    unittest.main()
