#!/usr/bin/env python3
"""Verify upgrades from 1.1.0 and the previous alpha on disposable data copies."""
from pathlib import Path
import argparse
import importlib.util
import json
import os
import shutil
import sqlite3
import subprocess
import tempfile
from urllib.parse import urlencode
from xml.etree import ElementTree

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


def previous_alpha_snapshot(app, profile, channel):
    key = urlencode({"profile_id": profile, "id": channel})
    summary = app.request("/api/guide/channel?" + key)
    title = summary["now"]["title"]
    account, = app.request("/api/input/accounts")["items"]
    compat.check((account["provider"], account["kind"], account["streams"]),
                 ("upgrade.example.invalid", "portal", 1), "subscriber account")
    tuning = app.request("/api/profiles")
    output = next(p for p in tuning if p["id"] == profile)["tuner_url"]
    with app.http.open(output + "/guide.xml", timeout=10) as response:
        xml = ElementTree.parse(response)
    ch, = xml.findall("channel")
    show, = xml.findall("programme")
    compat.check(show.get("channel"), ch.get("id"), "XMLTV channel reference")
    compat.check(show.findtext("title"), title, "XMLTV programme")
    with app.http.open(output + "/playlist.m3u", timeout=10) as response:
        playlist = response.read().decode()
    if f'tvg-id="{ch.get("id")}"' not in playlist:
        raise RuntimeError("M3U/XMLTV channel identity mismatch")
    compat.check([line for line in playlist.splitlines() if line.startswith("http")],
                 [output + "/stream/" + ch.get("id")], "M3U playback URL")
    return {"profile": profile, "device": next(p["device_id"] for p in tuning if p["id"] == profile),
            "channel": ch.get("id"), "playlist": playlist, "account": account["id"],
            "title": title, "pin": summary["pin"], "start": show.get("start"), "stop": show.get("stop")}


def exercise_previous_alpha(root, old, new, source):
    live = root / "appdata"
    seed = Path(__file__).with_name("upgrade_seed_test.go").resolve()
    overlay = root / "seed-overlay.json"
    overlay.write_text(json.dumps({"Replace": {str(source / "scripts/alpha12" / seed.name): str(seed)}}))
    subprocess.run(["go", "test", "-count=1", "-overlay", str(overlay), "-run", "^TestSeedPreviousAlpha$",
                    "./scripts/alpha12"], cwd=source, env={**os.environ, "ANTENNARR_UPGRADE_SEED": str(live)},
                   check=True)
    app = compat.Installation(live)
    with app.running(old):
        profile = next(p["id"] for p in app.request("/api/profiles") if p["preset_id"] == "us-nyc")
        query = urlencode({"profile_id": profile, "q": "NYFIX"})
        station, = app.request("/api/guide/stations?" + query)["candidates"]
        channel = "test-1"
        app.request("/api/guide/pin", {"profile_id": profile, "channel_id": channel,
                                       "candidate_id": station["id"]})
        before = previous_alpha_snapshot(app, profile, channel)
        compat.check(before["title"], "New York Fixture on Air", "previous-alpha listing")
        compat.check(before["pin"], station["id"], "previous-alpha station pin")
        backup = app.request("/api/backup", {})
        archive = app.request(backup["download"], raw=True)
    # Upgrade a copy. Keep both the previous installation and its application backup.
    saved = root / "original"
    live.rename(saved)
    shutil.copytree(saved, live)
    with app.running(new):
        compat.check(previous_alpha_snapshot(app, profile, channel), before, "in-place alpha upgrade")
    # The candidate must also be able to import that alpha's backup into a fresh install.
    live.rename(root / "upgraded")
    with app.running(new):
        app.restore(backup["backup"], archive)
    with app.running(new):
        compat.check(previous_alpha_snapshot(app, profile, channel), before, "previous-alpha backup restore")
    print("Passed alpha 1 upgrade and backup restore with a saved guide pin, programme, subscription, and tuner IDs.")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("baseline", type=Path)
    parser.add_argument("candidate", type=Path)
    parser.add_argument("--previous-alpha-source", type=Path, help="Extracted previous alpha release source")
    args = parser.parse_args()
    with tempfile.TemporaryDirectory(prefix="antennarr-alpha-upgrade-") as root:
        if args.previous_alpha_source:
            exercise_previous_alpha(Path(root), args.baseline.resolve(), args.candidate.resolve(),
                                    args.previous_alpha_source.resolve())
        else:
            exercise(Path(root), args.baseline.resolve(), args.candidate.resolve())
