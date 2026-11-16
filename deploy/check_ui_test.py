import unittest

from check_ui import report_exit


class ReportResultTest(unittest.TestCase):
    def report(self):
        return {"meta": {"routes": ["/gates"], "widths": [390], "themes": ["light"]},
                "states": [{"route": "/gates", "width": 390, "theme": "light", "state": "页面"}], "violations": []}

    def test_complete_and_clean(self):
        self.assertEqual(report_exit(self.report()), 0)

    def test_spacing_failure(self):
        report = self.report()
        report["violations"] = [{"check": "spacing"}]
        self.assertEqual(report_exit(report), 1)

    def test_incomplete_cannot_pass(self):
        report = self.report()
        report["states"][0]["state"] = "中心详情"
        self.assertEqual(report_exit(report), 2)

    def test_timeout_cannot_pass(self):
        report = self.report()
        report["violations"] = [{"check": "error"}]
        self.assertEqual(report_exit(report), 2)

    def test_wrong_route_cannot_replace_required_route(self):
        report = self.report()
        report['states'][0]['route'] = '/overview'
        self.assertEqual(report_exit(report), 2)

    def test_empty_selection_cannot_pass(self):
        report = self.report()
        report['meta']['routes'] = []
        report['states'] = []
        self.assertEqual(report_exit(report), 2)


if __name__ == "__main__":
    unittest.main()
