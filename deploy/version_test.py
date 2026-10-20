import unittest

from version import next_version, parse


class VersionTests(unittest.TestCase):
    def test_bounds(self):
        self.assertEqual(parse("0.2.0"), (0, 2, 0))
        self.assertEqual(parse("v9.99.999"), (9, 99, 999))
        for raw in ("0.2.0-rc114", "10.0.0", "9.100.0", "9.99.1000", "0.02.1"):
            with self.assertRaises(ValueError):
                parse(raw)

    def test_next_only_advances_patch_until_ceiling(self):
        self.assertEqual(next_version("0.2.0"), "0.2.1")
        self.assertEqual(next_version("0.2.999"), "0.3.0")
        self.assertEqual(next_version("0.99.999"), "1.0.0")
        with self.assertRaises(ValueError):
            next_version("9.99.999")


if __name__ == "__main__":
    unittest.main()
