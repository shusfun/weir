import hashlib
import pathlib
import tempfile
import unittest
from unittest import mock

from public_test_support import NOTES
from release_public import asset_names, create_draft, find_release, marker, parse_checksums, publish, remote_assets, verify_remote


class ReleaseTests(unittest.TestCase):
    def test_create_uses_response_without_eventually_consistent_listing(self):
        self.release["draft"] = True
        with mock.patch("release_public.gh", return_value=self.release) as api:
            self.assertEqual(create_draft("fixture/public", self.tag, self.commit, NOTES)["id"], 7)
            api.assert_called_once()
            self.assertEqual(api.call_args.args[:4], ("api", "repos/fixture/public/releases", "--method", "POST"))

    def setUp(self):
        self.tag, self.commit = "v1.0.0", "a" * 40
        self.contents = {name: ("fixture " + name).encode() for name in asset_names(self.tag)[:-1]}
        self.contents["checksums.txt"] = "".join(f"{hashlib.sha256(data).hexdigest()}  {name}\n" for name, data in self.contents.items()).encode()
        self.digests = {name: hashlib.sha256(data).hexdigest() for name, data in self.contents.items()}
        self.release = {"id": 7, "tag_name": self.tag, "body": NOTES + marker(self.commit), "draft": False, "prerelease": False,
                        "assets": [{"id": i, "name": name, "digest": "sha256:" + digest, "state": "uploaded", "size": len(self.contents[name])}
                                   for i, (name, digest) in enumerate(self.digests.items())]}

    def api(self, *args, **kwargs):
        if args[:2] == ("api", "repos/fixture/public/git/ref/tags/v1.0.0"):
            return {"object": {"type": "commit", "sha": self.commit}}
        if args[:2] == ("api", "repos/fixture/public/releases/tags/v1.0.0"):
            return self.release
        if args[0] == "api" and "releases/assets/" in args[1]:
            return self.contents["checksums.txt"]
        self.fail("unexpected mutation: " + repr(args))

    def test_remote_checksums_and_github_digests_agree(self):
        with mock.patch("release_public.gh", side_effect=self.api):
            verify_remote("fixture/public", self.tag, self.commit, self.digests)

    def test_draft_omitted_by_tag_endpoint_is_found_in_paginated_listing(self):
        self.release["draft"] = True
        first = [{"tag_name": f"v0.0.{i}"} for i in range(100)]
        with mock.patch("release_public.gh", side_effect=[None, first, [self.release]]) as api:
            self.assertEqual(find_release("fixture/public", self.tag)["id"], 7)
            self.assertEqual(api.call_args_list[-1].args[1], "repos/fixture/public/releases?per_page=100&page=2")

    def test_missing_release_returns_none(self):
        with mock.patch("release_public.gh", side_effect=[None, []]):
            self.assertIsNone(find_release("fixture/public", self.tag))

    def test_missing_digest_is_unknown_and_refused(self):
        self.release["assets"][0]["digest"] = None
        with self.assertRaisesRegex(ValueError, "digest"):
            remote_assets(self.release)

    def test_wrong_source_or_incomplete_assets_refused(self):
        with mock.patch("release_public.gh", side_effect=self.api):
            with self.assertRaisesRegex(ValueError, "tag differs"):
                verify_remote("fixture/public", self.tag, "b" * 40)
            self.release["assets"].pop(0)
            with self.assertRaisesRegex(ValueError, "incomplete"):
                verify_remote("fixture/public", self.tag, self.commit)

    def test_release_source_marker_cannot_be_replaced_by_same_tag_name(self):
        self.release["body"] = NOTES + marker("b" * 40)
        with mock.patch("release_public.gh", side_effect=self.api), self.assertRaisesRegex(ValueError, "expected public commit"):
            verify_remote("fixture/public", self.tag, self.commit)

    def test_published_release_is_verified_without_writes(self):
        with tempfile.TemporaryDirectory() as temp:
            for name, data in self.contents.items():
                (pathlib.Path(temp) / name).write_bytes(data)
            with mock.patch("release_public.git", side_effect=[(self.commit + "\n").encode(), NOTES.encode()]), mock.patch("release_public.gh", side_effect=self.api):
                publish("fixture/public", self.tag, self.commit, temp)

    def test_different_published_binary_never_overwritten(self):
        self.release["assets"][0]["digest"] = "sha256:" + "b" * 64
        with mock.patch("release_public.gh", side_effect=self.api), self.assertRaises(ValueError):
            verify_remote("fixture/public", self.tag, self.commit, self.digests)

    def test_checksum_path_injection_rejected(self):
        with self.assertRaisesRegex(ValueError, "invalid release checksums"):
            parse_checksums(("a" * 64 + "  ../file\n").encode(), self.tag)


if __name__ == "__main__":
    unittest.main()
