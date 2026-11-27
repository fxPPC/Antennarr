#!/usr/bin/env python3
"""Verify a released 1.1.0 backup and an upgrade to the 1.2 schema on copies."""
from pathlib import Path
import argparse
import importlib.util
import shutil
import sqlite3
import subprocess
import tempfile

spec = importlib.util.spec_from_file_location(
    "compat", Path(__file__).resolve().parents[1] / "test-patch-compatibility.py")
compat = importlib.util.module_from_spec(spec)
spec.loader.exec_module(compat)


def check_saved(current, baseline):
    for key in ("identity", "plex", "advertised", "channels"):
        compat.check(current[key], baseline[key], key)
    for key, value in baseline["settings"].items():
        compat.check(current["settings"][key], value, key)


def exercise(root, old, new):
    live = root / "appdata"
    app = compat.Installation(live)
    with app.running(old):
        tuner = app.request("/api/profiles", {"preset_id": "us-nyc"})["id"]
        app.request("/api/profiles/" + tuner, {"enabled": True, "name": "Restorable 1.1 tuner"})
        app.request("/api/channels", {"profile_id": tuner, "number": "998", "name": "Upgrade fixture",
                                     "station": "WTEST", "region": "US", "enabled": True})
        app.request("/api/settings", {"roster_target": 11})
        expected = app.snapshot()
        info = app.request("/api/backup", {})
        archive = app.request(info["download"], raw=True)
    original = root / "stable"
    live.rename(original)
    shutil.copytree(original, live)
    with app.running(new):
        check_saved(app.snapshot(), expected)
        app.request("/api/settings", {"roster_target": 18})
    db = live / "database" / "antennarr.db"
    with sqlite3.connect(db.as_uri() + "?mode=ro&immutable=1", uri=True) as conn:
        compat.check(conn.execute("pragma user_version").fetchone()[0], 10, "migrated schema")
    with subprocess.Popen([old], env=app.env, stdout=subprocess.PIPE, stderr=subprocess.STDOUT) as proc:
        try:
            log, _ = proc.communicate(timeout=10)
        except subprocess.TimeoutExpired:
            proc.kill()
            proc.wait()
            raise RuntimeError("1.1 opened the newer schema")
        if proc.returncode == 0 or b"check schema compatibility" not in log:
            raise RuntimeError("Expected 1.1 to reject newer catalogue: " + log[-800:].decode(errors="replace"))
    # Restore using the old binary and its own backup format on a fresh directory.
    live.rename(root / "alpha")
    with app.running(old):
        app.restore(info["backup"], archive)
    with app.running(old):
        check_saved(app.snapshot(), expected)
    print("Passed 1.1.0 upgrade, newer-schema refusal, and recovery from the original archive.")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("baseline", type=Path)
    parser.add_argument("candidate", type=Path)
    args = parser.parse_args()
    with tempfile.TemporaryDirectory(prefix="antennarr-alpha-upgrade-") as root:
        exercise(Path(root), args.baseline.resolve(), args.candidate.resolve())
