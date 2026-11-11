import importlib.util
from pathlib import Path
import unittest
import tempfile


spec = importlib.util.spec_from_file_location("release_policy", Path(__file__).with_name("release-policy.py"))
policy = importlib.util.module_from_spec(spec)
spec.loader.exec_module(policy)


def released(tag, **fields):
    return {"tag_name": tag, **fields}


class ReleasePolicyTests(unittest.TestCase):
    def test_preview_never_moves_stable_tags(self):
        for stage in ("alpha", "beta", "rc"):
            result = policy.plan(f"v1.1.0-{stage}.1", "refs/heads/main", "1.1", [released("v1.0.3")])
            self.assertTrue(result["prerelease"])
            self.assertEqual(result["minor_tag"], "")
            self.assertFalse(result["promote_latest"])
            with self.assertRaises(ValueError):
                policy.plan(f"v1.1.0-{stage}.1", "refs/heads/main", "1.1", [], True)

    def test_maintenance_continues_after_next_minor(self):
        history = [released("v1.0.4"), released("v1.1.0")]
        result = policy.plan("v1.0.5", "refs/heads/release/1.0", "1.0", history)
        self.assertEqual(result["minor_tag"], "1.0")
        self.assertFalse(result["promote_latest"])
        with self.assertRaises(ValueError):
            policy.plan("v1.0.5", "refs/heads/release/1.0", "1.0", history, True)

    def test_stable_promotion_is_explicit(self):
        history = [released("v1.0.9"), released("v1.1.0-rc.2", prerelease=True)]
        self.assertFalse(policy.plan("v1.1.0", "refs/heads/main", "1.1", history)["promote_latest"])
        self.assertTrue(policy.plan("v1.1.0", "refs/heads/main", "1.1", history, True)["promote_latest"])

    def test_rejects_wrong_branch_line_and_noncanonical_versions(self):
        for version, ref, line in [
            ("v1.1.0", "refs/heads/release/1.0", "1.1"),
            ("v1.1.0", "refs/heads/feature", "1.1"),
            ("v1.0.4", "refs/heads/main", "1.1"),
            ("v1.01.0", "refs/heads/main", "1.1"),
            ("v1.1.0-beta.01", "refs/heads/main", "1.1"),
            ("v1.1.0-beta.1", "refs/heads/release/1.1", "1.1"),
        ]:
            with self.subTest(version=version, ref=ref), self.assertRaises(ValueError):
                policy.plan(version, ref, line, [])

    def test_no_republishing_or_minor_tag_regression(self):
        history = [released("v1.0.5"), released("v1.1.0")]
        for version in ("v1.0.5", "v1.0.4"):
            with self.assertRaises(ValueError):
                policy.plan(version, "refs/heads/release/1.0", "1.0", history)
        with self.assertRaises(ValueError):
            policy.plan("v1.1.0-beta.1", "refs/heads/main", "1.1", [released("v1.1.0-beta.1", prerelease=True)])

    def test_numeric_precedence(self):
        result = policy.plan("v1.10.0", "refs/heads/main", "1.10", [released("v1.9.9")], True)
        self.assertTrue(result["promote_latest"])

    def test_mislabelled_release_is_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            files = {
                "cmd/antennarr/main.go": 'slog.Info("tuner starting", "version", "v1.1.0-beta.1")',
                "internal/admin/index.html": '<footer class="build-footer">v1.1.0-beta.1 · test</footer>',
                "Dockerfile": 'ARG VERSION=v1.1.0-beta.1\n',
            }
            for name, content in files.items():
                (root / name).parent.mkdir(parents=True, exist_ok=True)
                (root / name).write_text(content)
            policy.verify_source("v1.1.0-beta.1", root)
            with self.assertRaises(ValueError):
                policy.verify_source("v1.1.0", root)
            (root / "Dockerfile").write_text('ARG VERSION=v1.0.3\n')
            with self.assertRaises(ValueError):
                policy.verify_source("v1.1.0-beta.1", root)


if __name__ == "__main__":
    unittest.main()
