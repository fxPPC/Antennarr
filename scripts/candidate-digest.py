#!/usr/bin/env python3
"""Read and verify the single-platform OCI manifest in a release candidate."""
import hashlib
import json
import re
import sys
import tarfile

with tarfile.open(sys.argv[1]) as archive:
    index = json.load(archive.extractfile("index.json"))
    manifests = index["manifests"]
    if len(manifests) != 1:
        raise SystemExit("Expected exactly one platform manifest")
    digest = manifests[0]["digest"]
    if not re.fullmatch(r"sha256:[0-9a-f]{64}", digest):
        raise SystemExit("Invalid candidate manifest digest")
    blob = archive.extractfile("blobs/sha256/" + digest.split(":", 1)[1]).read()
    if "sha256:" + hashlib.sha256(blob).hexdigest() != digest:
        raise SystemExit("Candidate manifest hash mismatch")
    print(digest)
