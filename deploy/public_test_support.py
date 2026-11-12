import contextlib
import os
import pathlib
import subprocess
import tempfile
import unittest

from export_public import git


NOTES = "\n".join(("## 概览", "release", "## 主要变化", "product", "## 修复", "fix", "## 升级与兼容", "none", "## 验证", "tests")) + "\n"


def write(repo, name, data):
    path = pathlib.Path(repo) / name
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(data if isinstance(data, bytes) else data.encode())


def init(repo):
    pathlib.Path(repo).mkdir(parents=True, exist_ok=True)
    git(repo, "init", "-b", "main")
    git(repo, "config", "user.name", "Fixture")
    git(repo, "config", "user.email", "fixture@example.invalid")
    git(repo, "config", "core.autocrlf", "false")


def commit(repo):
    git(repo, "add", "--all")
    git(repo, "commit", "-m", "fixture")
    return git(repo, "rev-parse", "HEAD").decode().strip()


@contextlib.contextmanager
def working_directory(path):
    original = os.getcwd()
    os.chdir(path)
    try:
        yield
    finally:
        os.chdir(original)


class GitFixture(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="weir-export-test-")
        self.addCleanup(self.temp.cleanup)
        self.root = pathlib.Path(self.temp.name)
        self.source = self.root / "private"
        init(self.source)
        for name, data in {
            "README.md": "Public\n<!-- private-only:start -->\n[design](docs/design.html)\n<!-- private-only:end -->\n[image](docs/images/demo.png)\n",
            "VERSION": "1.0.0\n", "apps/main.go": "package main\n", "apps/nested/AGENTS.md": "private rules",
            "AGENTS.md": "private root rules", ".agents/skills/local/SKILL.md": "private skill",
            ".claude/launch.json": "private config", "docs/design.html": "private design",
            "docs/implementation.html": "private implementation", "docs/images/demo.png": b"image",
            "internal/center/dshskills/product/SKILL.md": "product skill",
            ".github/workflows/export-public.yml": "private orchestration",
            ".github/release-notes/v1.0.0.md": NOTES,
            "deploy/install.sh": "#!/bin/sh\nexit 0\n", "deploy/sources.json": "{}\n", "deploy/version.py": "# fixture\n",
            "third_party/product/LICENSE": "fixture license",
        }.items():
            write(self.source, name, data)
        self.source_commit = commit(self.source)
        git(self.source, "update-ref", "refs/remotes/origin/main", self.source_commit)
        git(self.source, "tag", "v1.0.0")

    def make_public(self):
        seed = self.root / "seed"
        init(seed)
        write(seed, "README.md", "Public predecessor\n")
        write(seed, "VERSION", "0.9.0\n")
        write(seed, "AGENTS.md", "already public history")
        write(seed, "apps/obsolete.go", "obsolete")
        self.seed_commit = commit(seed)
        self.remote = self.root / "public.git"
        git(self.root, "init", "--bare", "--initial-branch=main", str(self.remote))
        git(seed, "remote", "add", "origin", str(self.remote))
        git(seed, "push", "origin", "main")
        self.public = self.root / "public"
        git(self.root, "clone", str(self.remote), str(self.public))
        return self.public

    def command_fails(self, repo, *args):
        return subprocess.run(["git", "-C", str(repo), *args], capture_output=True).returncode != 0
