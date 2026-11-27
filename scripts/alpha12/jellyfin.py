#!/usr/bin/env python3
"""Exercise a real Jellyfin DVR and the release image in a disposable Linux Docker network."""
from array import array
import argparse
import base64
from collections import Counter
from contextlib import ExitStack
import json
import os
from pathlib import Path
import secrets
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parents[2]
JELLYFIN = "ghcr.io/jellyfin/jellyfin:12.4"
http = urllib.request.build_opener(urllib.request.ProxyHandler({}))


def run(*args, **kwargs):
    return subprocess.run(args, check=True, **kwargs)


def docker(*args):
    result = subprocess.run(["docker", *args], capture_output=True, text=True)
    if result.returncode:
        raise RuntimeError(f"docker {args[0]} failed: {result.stderr[-1800:]}")
    return result.stdout.strip()


def request(base, path, data=None, headers=None):
    payload = json.dumps(data).encode() if data is not None else None
    req = urllib.request.Request(base + path, data=payload,
                                 headers={"Content-Type": "application/json", **(headers or {})})
    try:
        with http.open(req, timeout=12) as reply:
            content = reply.read()
            return json.loads(content) if content else None
    except urllib.error.HTTPError as err:
        if err.code in (502, 503, 504):
            raise
        raise RuntimeError(f"{req.method if hasattr(req, 'method') else req.get_method()} {path}: HTTP {err.code}") from err


def wait(label, action, seconds=90):
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        try:
            value = action()
        except urllib.error.URLError:
            value = None
        if value:
            return value
        time.sleep(1)
    raise RuntimeError("Timed out waiting for " + label)


def make_media(directory):
    for name, resolution, colour, tone in [("720", "1280x720", "red", 440), ("480", "854x480", "lime", 880)]:
        # Animated video gives the startup buffer a representative bitrate.
        run("ffmpeg", "-nostdin", "-v", "error", "-f", "lavfi", "-i", f"testsrc2=s={resolution}:r=25",
            "-f", "lavfi", "-i", f"sine=frequency={tone}:sample_rate=48000",
            "-vf", f"drawbox=x=0:y=0:w=64:h=64:color={colour}:t=fill", "-t", "6",
            "-c:v", "libx264", "-preset", "ultrafast", "-g", "25", "-b:v", "900k",
            "-c:a", "aac", "-b:a", "96k", "-f", "mpegts", str(directory / (name + ".ts")))


def verify_recording(path):
    # Decode the entire actual recording. Verify independent audio AND video markers
    # on either side of the forced upstream loss, not just transport availability.
    common = ["ffmpeg", "-nostdin", "-v", "error", "-xerror", "-i", str(path)]
    pixels = run(*common, "-map", "0:v:0", "-an", "-vf", "fps=2,crop=2:2:8:8,scale=1:1",
                 "-pix_fmt", "rgb24", "-f", "rawvideo", "-", stdout=subprocess.PIPE).stdout
    colours = []
    for r, g, b in zip(pixels[::3], pixels[1::3], pixels[2::3]):
        colours.append("red" if r > 160 and g < 80 else "green" if g > 140 and r < 80 else "other")
    audio = array("h")
    audio.frombytes(run(*common, "-vn", "-map", "0:a:0", "-ar", "8000", "-ac", "1",
                        "-f", "s16le", "-", stdout=subprocess.PIPE).stdout)
    if sys.byteorder == "big":
        audio.byteswap()
    tones = []
    for pos in range(0, len(audio) - 7999, 8000):
        samples = audio[pos:pos + 8000]
        frequency = sum(a < 0 <= b for a, b in zip(samples, samples[1:]))
        tones.append("440" if 420 <= frequency <= 460 else "880" if 850 <= frequency <= 910 else "other")
    for seq, first, second in [(colours, "red", "green"), (tones, "440", "880")]:
        if seq.count(first) < 4 or seq.count(second) < 4 or seq.index(first) > seq.index(second):
            raise RuntimeError(f"Recording lacks ordered primary and standby markers: {dict(Counter(seq))}")
    frames = json.loads(run("ffprobe", "-v", "error", "-show_frames", "-show_entries",
                            "frame=media_type,best_effort_timestamp_time", "-of", "json", str(path),
                            stdout=subprocess.PIPE).stdout)["frames"]
    gap = {}
    spans = {}
    for kind in ("video", "audio"):
        stamps = [float(f["best_effort_timestamp_time"]) for f in frames
                  if f.get("media_type") == kind and "best_effort_timestamp_time" in f]
        if len(stamps) < 2:
            raise RuntimeError("Missing decoded timestamps for " + kind)
        deltas = [b - a for a, b in zip(stamps, stamps[1:])]
        gap[kind] = round(max(deltas), 3)
        spans[kind] = stamps[-1] - stamps[0]
        if min(deltas) < -0.1 or max(deltas) > 2:
            raise RuntimeError(f"{kind} timeline discontinuity: {min(deltas):.3f}…{max(deltas):.3f} s")
    if abs(spans["video"] - spans["audio"]) > 2:
        raise RuntimeError(f"Audio and video durations disagree: {spans}")
    return {"max_decoded_gap_seconds": gap,
            "video_samples": dict(Counter(colours)), "audio_seconds": dict(Counter(tones)),
            "bytes": path.stat().st_size}


def start(stack, name, image, network, *options):
    docker("run", "-d", "--name", name, "--network", network, *options, image)
    stack.callback(subprocess.run, ["docker", "rm", "-fv", name], stdout=subprocess.DEVNULL)
    return docker("inspect", "--format={{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}", name)


def jellyfin_setup(base):
    wait("Jellyfin startup", lambda: request(base, "/System/Info/Public"))
    password = secrets.token_urlsafe(24)
    client = 'MediaBrowser Client="Antennarr acceptance", Device="ARM64", DeviceId="alpha-dvr-test", Version="1"'
    headers = {"Authorization": client}
    request(base, "/Startup/Configuration", {"ServerName": "Antennarr DVR fixture", "UICulture": "en-GB",
            "MetadataCountryCode": "GB", "PreferredMetadataLanguage": "en"}, headers)
    request(base, "/Startup/User", headers=headers)  # Initialises the first local user.
    request(base, "/Startup/User", {"Name": "fixture", "Password": password}, headers)
    request(base, "/Startup/Complete", {}, headers)
    result = request(base, "/Users/AuthenticateByName", {"Username": "fixture", "Pw": password}, headers)
    headers["Authorization"] = client + ', Token="' + result["AccessToken"] + '"'
    def jf(path, data=None):
        return request(base, path, data, headers)
    cfg = jf("/System/Configuration/livetv")
    cfg.update(RecordingPath="/recordings", MovieRecordingPath="/recordings", SeriesRecordingPath="/recordings")
    jf("/System/Configuration/livetv", cfg)
    return jf


def exercise(stack, work, image):
    network = "ant-dvr-" + secrets.token_hex(6)
    uid = f"{os.getuid()}:{os.getgid()}"
    recordings = work / "recordings"
    recordings.mkdir()
    media = work / "media"
    media.mkdir()
    make_media(media)
    docker("pull", JELLYFIN)
    docker("network", "create", "--internal", network)
    stack.callback(subprocess.run, ["docker", "network", "rm", network], stdout=subprocess.DEVNULL)
    jf_ip = start(stack, network + "-jf", JELLYFIN, network, "--user", uid,
                  "--tmpfs", "/config:mode=1777", "--tmpfs", "/cache:mode=1777",
                  "-v", f"{recordings}:/recordings")
    jf = jellyfin_setup(f"http://{jf_ip}:8096")
    root = work / "data"
    run("go", "test", "-count=1", "-run", "^TestSeedJellyfinDVR$", "./scripts/alpha12",
        cwd=ROOT, env={**os.environ, "ANTENNARR_ALPHA_SEED": str(root)})
    ant_ip = start(stack, network + "-ant", image, network, "--user", uid, "--network-alias", "antennarr",
                   "-v", f"{root}:/data", "-v", f"{media}:/fixtures:ro", "-e", "ANTENNARR_FIXTURES=/fixtures",
                   "-e", "ANTENNARR_BASE_URL=http://antennarr:39193", "-e", "ANTENNARR_TEST_FAILOVER=1",
                   "-e", "ANTENNARR_PORT_RANGE=39192-39198")
    admin = f"http://{ant_ip}:39192"
    auth = {"Authorization": "Basic " + base64.b64encode(b"admin:admin").decode(), "X-Antennarr-Request": "1"}
    def an(path, data=None):
        return request(admin, path, data, auth)
    profiles = wait("Antennarr ready", lambda: an("/api/profiles"))
    hosts = {}
    for p in profiles:
        port = 39193 + p["port_offset"]
        lan = p["tuner_url"]
        plex = request(f"http://{ant_ip}:{port}", "/lineup.json")
        if len(plex) != 1 or plex[0]["GuideNumber"] != "7":
            raise RuntimeError(f"Unexpected Plex lineup on {port}")
        host = jf("/LiveTv/TunerHosts", {"Type": "m3u", "Url": lan + "/playlist.m3u",
                  "FriendlyName": p["name"], "TunerCount": 3})
        jf("/LiveTv/ListingProviders", {"Type": "xmltv", "Path": lan + "/guide.xml",
           "EnableAllTuners": False, "EnabledTuners": [host["Id"]]})
        hosts[plex[0]["GuideName"]] = (p, port)
    def programmes():
        items = jf("/LiveTv/Programs?limit=100")["Items"]
        found = {v["Name"]: v for v in items}
        if {"London Fixture on Air", "New York Fixture on Air"} <= found.keys():
            return found
        return None
    tasks = jf("/ScheduledTasks")
    guide_task = next(t for t in tasks if t.get("Key") == "RefreshGuide" or
                      ("guide" in t.get("Name", "").lower() and "refresh" in t["Name"].lower()))
    if guide_task["State"] == "Idle":
        jf("/ScheduledTasks/Running/" + guide_task["Id"], {})
    entries = wait("two distinct Jellyfin guide entries", programmes)
    channels = {v["Name"]: v for v in jf("/LiveTv/Channels")["Items"]}
    for name in hosts:
        if entries[name + " on Air"]["ChannelId"] != channels[name]["Id"]:
            raise RuntimeError(f"Wrong programme for {name}")
    if len(hosts) != 2 or len({v["Id"] for v in channels.values()}) != 2:
        raise RuntimeError("Same-number channels collided")
    ny, port = hosts["New York Fixture"]
    item = entries["New York Fixture on Air"]
    timer = jf("/LiveTv/Timers/Defaults?programId=" + item["Id"])
    timer.update(ProgramId=item["Id"], ChannelId=item["ChannelId"], PrePaddingSeconds=0, PostPaddingSeconds=0)
    jf("/LiveTv/Timers", timer)
    def live():
        return [v for v in an("/api/monitor")["live"] or [] if v["channel_id"] == "test-1"]
    wait("Jellyfin DVR primary and hot standby",
         lambda: any(v["primary"] == -1 and v["ghost"] != 0 for v in live()), 120)
    with http.open(f"http://{ant_ip}:{port}/auto/v7", timeout=15) as plex:
        if not plex.read(188):
            raise RuntimeError("Empty Plex-compatible stream")
        snap = an("/api/monitor")
        if snap["playback"]["viewers"] < 2 or len(live()) != 1:
            raise RuntimeError("Jellyfin and Plex-compatible reader did not share one controller")
    switched = wait("forced failover during DVR",
                    lambda: next((v for v in live() if v["failovers"] and v["primary"] == -2), None))
    def completed():
        files = [p for p in recordings.rglob("*") if p.suffix.lower() in {".ts", ".mkv", ".mp4"}]
        active = jf("/LiveTv/Timers?isActive=true")["Items"]
        return files[0] if len(files) == 1 and not active and not live() else None
    recording = wait("completed Jellyfin recording", completed, 245)
    outcome = verify_recording(recording)
    # Renumbering after a recording must preserve Jellyfin's persistent channel key.
    before = http.open(f"http://{ant_ip}:{port}/playlist.m3u").read().decode().splitlines()
    an("/api/channels", {"profile_id": ny["id"], "id":"test-1", "name":"New York Fixture",
                         "number":"17", "region":"US", "enabled":True})
    after = http.open(f"http://{ant_ip}:{port}/playlist.m3u").read().decode().splitlines()
    urls = lambda lines: [s for s in lines if s.startswith("http://")]
    if len(urls(before)) != 1 or urls(before) != urls(after):
        raise RuntimeError("Renumbering changed the Jellyfin stream URL")
    return {**outcome, "jellyfin": jf("/System/Info/Public")["Version"],
            "jellyfin_image_id": docker("image", "inspect", "--format={{.Id}}", JELLYFIN),
            "candidate_image_id": docker("image", "inspect", "--format={{.Id}}", image),
            "profiles": sorted(hosts), "shared_controller": True, "failovers": switched["failovers"],
            "stable_playback_url": True, "recording_decoded": True}


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--image", required=True)
    parser.add_argument("--report", type=Path, required=True)
    args = parser.parse_args()
    with tempfile.TemporaryDirectory(prefix="ant-dvr-") as temp, ExitStack() as cleanup:
        report = exercise(cleanup, Path(temp), args.image)
    args.report.write_text(json.dumps(report, indent=2) + "\n")
    print(json.dumps(report))
