"""Exercise the release selector against paginated GitHub API results."""
import json
from pathlib import Path
import subprocess
import unittest


def preview(tag, date, *, image=True, draft=False, prerelease=True):
    assets = [{"name": "IMAGE-DIGEST.txt", "state": "uploaded"}] if image else []
    return dict(tag_name=tag, published_at=date, assets=assets, draft=draft, prerelease=prerelease)


def select(pages):
    return subprocess.run(["jq", "-er", "-f", str(Path(__file__).with_name("docker-preview.jq"))],
                          input=json.dumps(pages), text=True, capture_output=True)


class DockerPreviewTests(unittest.TestCase):
    def test_github_only_previews_younger_than_published_image(self):
        pages = [[preview("v1.2.0-alpha.1", "2026-11-28", image=False),
                  preview("v1.1.0", "2026-11-27", prerelease=False),
                  preview("v1.1.1-beta.1", "2026-11-26", draft=True)],
                 [preview("v1.1.0-beta.2", "2026-11-23"),
                  preview("v1.1.0-beta.1", "2026-11-20")]]
        result = select(pages)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout.strip(), "v1.1.0-beta.2")

    def test_no_published_image(self):
        self.assertNotEqual(select([[preview("v1.2.0-alpha.1", "2026-11-28", image=False)]]).returncode, 0)


if __name__ == "__main__":
    unittest.main()
