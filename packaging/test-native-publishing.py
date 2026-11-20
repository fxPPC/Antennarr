#!/usr/bin/env python3
"""Check release output containment and atomic publication on POSIX build hosts."""
import errno
from concurrent.futures import ThreadPoolExecutor
import importlib.util
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("native_build", Path(__file__).with_name("build-native.py"))
builder = importlib.util.module_from_spec(spec)
spec.loader.exec_module(builder)


class ReleaseOutputTests(unittest.TestCase):
    def setUp(self):
        scratch = tempfile.TemporaryDirectory()
        self.addCleanup(scratch.cleanup)
        self.base = Path(scratch.name)
        self.asset = self.base / "test.zip"
        self.asset.write_bytes(b"verified asset")
        self.output = self.base / "somewhere" / "release files"

    def test_directory_replacement_cannot_redirect_an_open_build(self):
        with builder.release_directory(self.output) as fd:
            original = self.output.with_name("original")
            self.output.rename(original)
            self.output.symlink_to(self.base)
            builder.publish(self.asset, fd)
        self.assertEqual((original / self.asset.name).read_bytes(), self.asset.read_bytes())
        with self.assertRaises(OSError):
            with builder.release_directory(self.output):
                self.fail("output symlink accepted")

    def test_output_name_replaces_a_symlink_without_touching_its_target(self):
        target = self.base / "other"
        target.write_bytes(b"untouched")
        with builder.release_directory(self.output) as fd:
            dest = self.output / self.asset.name
            dest.symlink_to(target)
            builder.publish(self.asset, fd)
        self.assertFalse(dest.is_symlink())
        self.assertEqual(dest.read_bytes(), self.asset.read_bytes())
        self.assertEqual(target.read_bytes(), b"untouched")

    def test_failed_copy_preserves_previous_file_and_cleans_up(self):
        with builder.release_directory(self.output) as fd:
            dest = self.output / self.asset.name
            dest.write_bytes(b"previous")

            def fail_after_write(src, dst):
                dst.write(src.read(1))
                self.assertEqual(dest.read_bytes(), b"previous")
                raise OSError(errno.ENOSPC, "test disk is full")

            with patch.object(builder.shutil, "copyfileobj", fail_after_write), self.assertRaises(OSError):
                builder.publish(self.asset, fd)
        self.assertEqual(list(self.output.iterdir()), [dest])
        self.assertEqual(dest.read_bytes(), b"previous")

    def test_concurrent_builds_publish_whole_assets(self):
        assets = []
        for i in range(8):
            stage = self.base / str(i)
            stage.mkdir()
            asset = stage / self.asset.name
            asset.write_bytes(bytes([i]) * 131072)
            assets.append(asset)

        def publish(asset):
            with builder.release_directory(self.output) as fd:
                builder.publish(asset, fd)

        with ThreadPoolExecutor(max_workers=len(assets)) as executor:
            list(executor.map(publish, assets))
        dest = self.output / self.asset.name
        self.assertEqual(list(self.output.iterdir()), [dest])
        self.assertIn(dest.read_bytes(), [asset.read_bytes() for asset in assets])


if __name__ == "__main__":
    unittest.main()
