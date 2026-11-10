"""Synchronise reviewed documentation after publication; never print credentials."""
import json
import os
from pathlib import Path
import sys
import urllib.error
import urllib.request


def request(path, method="GET", data=None, token=None):
    headers = {"Content-Type": "application/json"}
    if token:
        headers["Authorization"] = "Bearer " + token
    payload = json.dumps(data).encode() if data is not None else None
    req = urllib.request.Request("https://hub.docker.com/v2/" + path,
                                 data=payload, headers=headers, method=method)
    with urllib.request.urlopen(req, timeout=30) as response:
        return json.load(response)


def main():
    text = Path("docs/DOCKER-HUB.md").read_text()
    token = request("auth/token", "POST", {
        "identifier": os.environ["DOCKERHUB_USERNAME"],
        "secret": os.environ["DOCKERHUB_TOKEN"],
    })["access_token"]
    request("repositories/antennarr/antennarr", "PATCH",
            {"full_description": text}, token)
    saved = request("repositories/antennarr/antennarr")
    if saved.get("full_description") != text:
        raise ValueError("Docker Hub description readback differed")
    print("Docker Hub overview updated and verified")


if __name__ == "__main__":
    try:
        main()
    except urllib.error.HTTPError as error:
        sys.exit(f"Docker Hub documentation update failed: HTTP {error.code}")
    except Exception as error:
        # Error bodies and request objects can contain authentication material.
        sys.exit(f"Docker Hub documentation update failed: {type(error).__name__}")
