"""Verify dangerous publication requests stop before any GitHub write."""
from pathlib import Path
import os
import subprocess
import tempfile
import unittest


SCRIPT = Path(__file__).resolve().parents[1] / 'publish-github-alpha.sh'
SHA = 'a' * 40
VERSION = 'v1.2.0-alpha.1'


class PublicationTests(unittest.TestCase):
    def rejected(self, ref, testing_sha, latest):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            (root / 'candidate').mkdir()
            command = root / 'gh'
            command.write_text("""#!/bin/sh
echo "$*" >> "$RUNNER_TEMP/calls"
case "$2" in
  */git/ref/heads/testing/v1.2) echo "$MOCK_TESTING";;
  */releases/latest) echo "$MOCK_LATEST";;
  *) exit 99;;
esac
""")
            command.chmod(0o755)
            env = dict(os.environ, PATH=str(root) + os.pathsep + os.environ['PATH'],
                       RUNNER_TEMP=temp, GITHUB_WORKSPACE=temp, VERSION=VERSION,
                       GITHUB_REPOSITORY='example/fixture', GITHUB_SHA=SHA,
                       GITHUB_REF=ref, BASELINE='v1.1.0', MOCK_TESTING=testing_sha, MOCK_LATEST=latest)
            result = subprocess.run(['bash', str(SCRIPT)], env=env, capture_output=True)
            self.assertNotEqual(result.returncode, 0)
            log = root / 'calls'
            return log.read_text().splitlines() if log.exists() else []

    def test_testing_branch_builds_without_publishing(self):
        self.assertEqual(self.rejected('refs/heads/testing/v1.2', SHA, 'v1.1.0'), [])

    def test_outdated_candidate_stops_before_release_requests(self):
        calls = self.rejected('refs/heads/release-candidate/' + VERSION, 'b' * 40, 'v1.1.0')
        self.assertEqual(len(calls), 1)
        self.assertIn('/git/ref/heads/testing/v1.2', calls[0])

    def test_different_latest_stops_before_release_writes(self):
        calls = self.rejected('refs/heads/release-candidate/' + VERSION, SHA, 'v1.1.1')
        self.assertEqual(len(calls), 2)
        self.assertIn('/releases/latest', calls[-1])


if __name__ == '__main__':
    unittest.main()
