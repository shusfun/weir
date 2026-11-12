import contextlib
import io
import json
import unittest

from export_public import manifest, policy, public_path, snapshot, stage, strip_private, write_tree
from public_test_support import GitFixture, commit, write


class ExportTests(GitFixture):
    def test_committed_snapshot_filters_private_and_keeps_runtime_skills(self):
        write(self.source, "apps/untracked.go", "must not escape")
        write(self.source, "apps/main.go", "dirty worktree")
        commit_id, files = snapshot(self.source)
        self.assertEqual(commit_id, self.source_commit)
        self.assertEqual(files["apps/main.go"][1], b"package main\n")
        self.assertNotIn("apps/untracked.go", files)
        for name in files:
            self.assertNotIn("AGENTS.md", name)
            self.assertNotIn(".agents/", name)
            self.assertNotIn("design.html", name)
        self.assertIn("internal/center/dshskills/product/SKILL.md", files)
        self.assertIn("third_party/product/LICENSE", files)
        self.assertNotIn(b"private", files["README.md"][1])
        exported = self.root / "export"
        write_tree(files, exported)
        self.assertFalse((exported / ".git").exists())
        self.assertEqual(manifest(files), manifest(dict(reversed(list(files.items())))))

    def test_unknown_top_level_requires_review(self):
        write(self.source, "unreviewed/file.txt", "new data")
        commit(self.source)
        with self.assertRaisesRegex(ValueError, "explicit export rule"):
            snapshot(self.source)

    def test_no_private_files_allowed_in_strict_public_source(self):
        with self.assertRaisesRegex(ValueError, "private file"):
            snapshot(self.source, strict=True)

    def test_broken_relative_link_rejected(self):
        write(self.source, "README.md", "[private](docs/design.html)\n")
        commit(self.source)
        with self.assertRaisesRegex(ValueError, "broken public document link"):
            snapshot(self.source)

    def test_malformed_private_blocks_rejected(self):
        for text in ("<!-- private-only:end -->", "<!-- private-only:start -->", "<!-- private-only:start -->\n<!-- private-only:start -->", "prefix <!-- private-only:start -->"):
            with self.subTest(text=text), self.assertRaises(ValueError):
                strip_private(text.encode())

    def test_private_names_case_and_credential_paths(self):
        rules = policy()
        for path in ("apps/a/agents.MD", "packages/a/CLAUDE.md", "apps/a/.claude/skills/SKILL.md", "examples/.env", "examples/server.key"):
            self.assertFalse(public_path(path, rules), path)
        self.assertTrue(public_path("examples/.env.example", rules))
        with self.assertRaises(ValueError):
            public_path("../outside", rules)

    def test_success_and_failure_logs_share_operation(self):
        stream = io.StringIO()
        with contextlib.redirect_stderr(stream):
            with stage("success"):
                pass
            with self.assertRaises(ValueError), stage("rejected"):
                raise ValueError("fixture rejection")
        records = [json.loads(line) for line in stream.getvalue().splitlines()]
        self.assertEqual(len({r["operation_id"] for r in records}), 1)
        self.assertEqual(records[-1]["status"], "failed")
        self.assertIn("elapsed_ms", records[-1])


if __name__ == "__main__":
    unittest.main()
