#!/usr/bin/env python3
"""Validate release branches and calculate mutable tags without publishing."""

import argparse
import json
import re
from pathlib import Path


VERSION = re.compile(
    r"v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)"
    r"(?:-(alpha|beta|rc)\.([1-9][0-9]*))?"
)


def parse_version(value):
    match = VERSION.fullmatch(value)
    if match is None:
        raise ValueError("Use vMAJOR.MINOR.PATCH or a numbered alpha, beta, or rc release")
    return tuple(map(int, match.group(1, 2, 3))), match.group(4) is not None


def plan(version, ref, line, releases, promote=False):
    number, prerelease = parse_version(version)
    if line != f"{number[0]}.{number[1]}":
        raise ValueError("Release version must match RELEASE_LINE")
    if ref not in ("refs/heads/main", f"refs/heads/release/{line}"):
        raise ValueError("Publish only from main or the matching maintenance branch")
    if prerelease and ref != "refs/heads/main":
        raise ValueError("Prereleases are published from main")
    if prerelease and promote:
        raise ValueError("A prerelease cannot become latest")

    stable = []
    for release in releases:
        if release.get("draft"):
            continue
        tag = release["tag_name"]
        if tag == version:
            raise ValueError("Published versions are immutable")
        # Historical tags outside the current naming policy do not participate.
        try:
            previous, preview = parse_version(tag)
        except ValueError:
            continue
        if not preview and not release.get("prerelease"):
            stable.append(previous)

    if not prerelease:
        if any(old[:2] == number[:2] and old >= number for old in stable):
            raise ValueError("A stable release must advance its minor release line")
        if promote and any(old > number for old in stable):
            raise ValueError("Cannot move latest back to an older stable version")
    return {
        "version": version,
        "prerelease": prerelease,
        "minor_tag": "" if prerelease else line,
        "promote_latest": promote,
    }


def verify_source(version, root):
    # The release label must agree with the executable and the embedded UI.
    for name, pattern in (
        ("cmd/antennarr/main.go", r'slog.Info\("tuner starting", "version", "([^"]+)"'),
        ("internal/admin/index.html", r'<footer class="build-footer">([^ ]+) '),
        ("Dockerfile", r'^ARG VERSION=(\S+)$'),
    ):
        matches = re.findall(pattern, (root / name).read_text(), re.MULTILINE)
        if matches != [version]:
            raise ValueError(f"Update {name} to {version} before releasing")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("version")
    parser.add_argument("ref")
    parser.add_argument("--line-file", default="RELEASE_LINE")
    parser.add_argument("--source-root", default=".")
    parser.add_argument("--releases", required=True, help="GitHub releases, paginated with --slurp")
    parser.add_argument("--promote-latest", action="store_true")
    args = parser.parse_args()
    pages = json.loads(Path(args.releases).read_text())
    releases = [release for page in pages for release in page]
    try:
        result = plan(args.version, args.ref, Path(args.line_file).read_text().strip(), releases, args.promote_latest)
        verify_source(args.version, Path(args.source_root))
    except ValueError as error:
        parser.error(str(error))
    print(json.dumps(result))


if __name__ == "__main__":
    main()
