"""Exercise upstream synchronization with real, isolated Git repositories."""
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


SCRIPT = Path(__file__).with_name("sync-upstream.sh").resolve()
BASH = shutil.which("bash")
if os.name == "nt":
    BASH = r"C:\Program Files\Git\bin\bash.exe"


class SyncTests(unittest.TestCase):
    def setUp(self):
        self.scratch = tempfile.TemporaryDirectory(prefix="denova-sync-test-")
        self.addCleanup(self.scratch.cleanup)
        root = Path(self.scratch.name).resolve()
        self.upstream = root / "upstream"
        self.origin = root / "origin.git"
        self.fork = root / "fork"
        self.git(root, "init", "-b", "master", str(self.upstream))
        self.identity(self.upstream)
        self.write(self.upstream, "app.txt", "base\n")
        self.write(self.upstream, "Dockerfile", "base\n")
        self.write(self.upstream, ".github/workflows/ci.yml", "base\n")
        self.commit(self.upstream)
        self.git(root, "clone", "--bare", str(self.upstream), str(self.origin))
        self.git(root, "clone", str(self.origin), str(self.fork))
        self.identity(self.fork)
        self.write(self.fork, "Dockerfile", "fork\n")
        self.write(self.fork, ".github/workflows/ci.yml", "fork\n")
        self.commit(self.fork)
        self.git(self.fork, "push", "origin", "master")

    def git(self, cwd, *args):
        result = subprocess.run(["git", *args], cwd=cwd, capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        return result.stdout.strip()

    def identity(self, repo):
        self.git(repo, "config", "user.name", "Sync Test")
        self.git(repo, "config", "user.email", "sync-test@example.invalid")
        self.git(repo, "config", "core.autocrlf", "false")

    def write(self, repo, name, text):
        path = repo / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(text, encoding="utf-8")

    def commit(self, repo):
        self.git(repo, "add", "-A")
        self.git(repo, "commit", "-m", "test change")

    def sync(self):
        env = dict(os.environ, UPSTREAM_URL=self.upstream.as_posix())
        result = subprocess.run([BASH, SCRIPT.as_posix()], cwd=self.fork,
                                env=env, capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(self.git(self.fork, "status", "--porcelain"), "")

    def test_unchanged_upstream_is_noop(self):
        before = self.git(self.fork, "rev-parse", "HEAD")
        self.sync()
        self.assertEqual(self.git(self.fork, "rev-parse", "HEAD"), before)

    def test_sync_source_but_preserve_changed_and_added_workflows(self):
        before = self.git(self.fork, "rev-parse", "HEAD")
        self.write(self.upstream, "app.txt", "upstream\n")
        self.write(self.upstream, ".github/workflows/ci.yml", "upstream\n")
        self.write(self.upstream, ".github/workflows/new.yml", "new\n")
        self.commit(self.upstream)
        self.sync()
        self.assertEqual((self.fork / "app.txt").read_text(), "upstream\n")
        self.assertEqual((self.fork / ".github/workflows/ci.yml").read_text(), "fork\n")
        self.assertFalse((self.fork / ".github/workflows/new.yml").exists())
        self.assertEqual(self.git(self.fork, "diff", before, "HEAD", "--", ".github/workflows"), "")
        self.git(self.fork, "merge-base", "--is-ancestor", self.git(self.upstream, "rev-parse", "HEAD"), "HEAD")
        self.assertEqual(self.git(self.origin, "rev-parse", "master"), self.git(self.fork, "rev-parse", "HEAD"))
        # A second poll must not create another commit.
        after = self.git(self.fork, "rev-parse", "HEAD")
        self.sync()
        self.assertEqual(self.git(self.fork, "rev-parse", "HEAD"), after)

    def test_conflicts_keep_fork_docker_and_take_upstream_source(self):
        self.write(self.fork, "app.txt", "fork\n")
        self.commit(self.fork)
        self.write(self.upstream, "app.txt", "upstream\n")
        self.write(self.upstream, "Dockerfile", "upstream\n")
        self.commit(self.upstream)
        self.sync()
        self.assertEqual((self.fork / "app.txt").read_text(), "upstream\n")
        self.assertEqual((self.fork / "Dockerfile").read_text(), "fork\n")

    def test_upstream_deletion_wins_source_conflict(self):
        self.write(self.fork, "app.txt", "fork\n")
        self.commit(self.fork)
        (self.upstream / "app.txt").unlink()
        self.commit(self.upstream)
        self.sync()
        self.assertFalse((self.fork / "app.txt").exists())


if __name__ == "__main__":
    unittest.main()
