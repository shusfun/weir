import unittest
from unittest import mock

import sync_public
from export_public import git, snapshot, tree
from public_test_support import GitFixture, NOTES, commit, write
from sync_public import advance_version, synchronize, wait_for_release


class SyncTests(GitFixture):
    def test_first_export_preserves_public_history_and_is_idempotent(self):
        self.make_public()
        report = synchronize(self.source, self.public, "v1.0.0")
        public_commit = report["public_commit"]
        self.assertEqual(tree(self.public, public_commit)[1], snapshot(self.source)[1])
        self.assertEqual(git(self.public, "rev-parse", public_commit + "^").decode().strip(), self.seed_commit)
        self.assertTrue(self.command_fails(self.public, "cat-file", "-e", self.source_commit))
        self.assertFalse((self.public / "apps/obsolete.go").exists())
        self.assertFalse((self.public / "AGENTS.md").exists())
        self.assertEqual(synchronize(self.source, self.public, "v1.0.0")["public_commit"], public_commit)

    def test_later_release_deletes_files(self):
        self.make_public()
        synchronize(self.source, self.public, "v1.0.0")
        (self.source / "apps/main.go").unlink()
        write(self.source, "apps/new.go", "new")
        write(self.source, "VERSION", "1.0.1\n")
        write(self.source, ".github/release-notes/v1.0.1.md", NOTES)
        latest = commit(self.source)
        git(self.source, "update-ref", "refs/remotes/origin/main", latest)
        git(self.source, "tag", "v1.0.1")
        report = synchronize(self.source, self.public, "v1.0.1")
        self.assertNotIn("apps/main.go", tree(self.public, report["public_commit"])[1])

    def test_preview_does_not_push_or_change_public_worktree(self):
        self.make_public()
        before = git(self.public, "rev-parse", "HEAD")
        report = synchronize(self.source, self.public, "v1.0.0", dry_run=True)
        self.assertIsNone(report["public_commit"])
        self.assertEqual(git(self.public, "rev-parse", "HEAD"), before)
        self.assertEqual(git(self.public, "status", "--porcelain"), b"")
        self.assertTrue(self.command_fails(self.remote, "rev-parse", "--verify", "refs/tags/v1.0.0"))

    def test_main_preview_works_before_a_release_tag_exists(self):
        self.make_public()
        git(self.source, "tag", "--delete", "v1.0.0")
        (self.source / ".github/release-notes/v1.0.0.md").unlink()
        latest = commit(self.source)
        git(self.source, "update-ref", "refs/remotes/origin/main", latest)
        report = synchronize(self.source, self.public, "main", dry_run=True)
        self.assertEqual(report["tag"], "v1.0.0")
        self.assertIsNone(report["public_commit"])

    def test_release_notes_and_main_ancestry_are_required_to_publish(self):
        self.make_public()
        write(self.source, "VERSION", "1.0.1\n")
        latest = commit(self.source)
        git(self.source, "tag", "v1.0.1")
        with self.assertRaises(ValueError):
            synchronize(self.source, self.public, "v1.0.1")
        git(self.source, "update-ref", "refs/remotes/origin/main", latest)
        with self.assertRaises(ValueError):
            synchronize(self.source, self.public, "v1.0.1")

    def test_conflicting_existing_tag_refused(self):
        self.make_public()
        git(self.public, "tag", "v1.0.0")
        git(self.public, "push", "origin", "v1.0.0")
        with self.assertRaisesRegex(ValueError, "does not match"):
            synchronize(self.source, self.public, "v1.0.0")

    def test_atomic_push_rejects_concurrent_main_without_creating_tag(self):
        self.make_public()
        other = self.root / "other"
        git(self.root, "clone", str(self.remote), str(other))
        git(other, "config", "user.name", "Concurrent")
        git(other, "config", "user.email", "concurrent@example.invalid")
        real_git = sync_public.git

        def concurrent(repo, *args, **kwargs):
            if args[:2] == ("push", "--atomic"):
                write(other, "README.md", "concurrent change\n")
                commit(other)
                git(other, "push", "origin", "main")
            return real_git(repo, *args, **kwargs)

        with mock.patch("sync_public.git", side_effect=concurrent), self.assertRaises(ValueError):
            synchronize(self.source, self.public, "v1.0.0")
        self.assertTrue(self.command_fails(self.remote, "rev-parse", "--verify", "refs/tags/v1.0.0"))

    def test_failed_and_timed_out_public_build_never_confirms_release(self):
        run = {"id": 7, "head_sha": "abc", "head_branch": "v1.0.0", "status": "completed", "conclusion": "failure", "html_url": "https://example.invalid/run/7"}
        with mock.patch("sync_public.gh", return_value={"workflow_runs": [run]}), mock.patch("sync_public.verify_remote") as verify:
            with self.assertRaisesRegex(ValueError, "workflow failed"):
                wait_for_release("fixture/public", "v1.0.0", "abc", timeout=1)
            verify.assert_not_called()
        with self.assertRaisesRegex(ValueError, "timed out"):
            wait_for_release("fixture/public", "v1.0.0", "abc", timeout=0)

    def test_success_matches_sha_and_tag_not_latest_unrelated_run(self):
        good = {"id": 1, "head_sha": "abc", "head_branch": "v1.0.0", "status": "completed", "conclusion": "success"}
        unrelated = {**good, "id": 99, "head_branch": "v2.0.0", "conclusion": "failure"}
        with mock.patch("sync_public.gh", return_value={"workflow_runs": [good, unrelated]}), mock.patch("sync_public.verify_remote") as verify:
            wait_for_release("fixture/public", "v1.0.0", "abc", timeout=1)
            verify.assert_called_once_with("fixture/public", "v1.0.0", "abc")

    def test_advance_private_only_and_retry(self):
        remote = self.root / "private.git"
        git(self.root, "init", "--bare", "--initial-branch=main", str(remote))
        git(self.source, "remote", "add", "origin", str(remote))
        git(self.source, "push", "origin", "main")
        advance_version(self.source, "1.0.0", self.source_commit)
        self.assertEqual(git(remote, "show", "main:VERSION"), b"1.0.1\n")
        advance_version(self.source, "1.0.0", self.source_commit)
        self.assertEqual(git(remote, "rev-list", "--count", "main").strip(), b"2")


if __name__ == "__main__":
    unittest.main()
