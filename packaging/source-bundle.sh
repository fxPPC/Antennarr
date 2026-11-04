#!/bin/sh
# Run from the repository root. This allowlist deliberately excludes Git history,
# personal operations files, evidence screenshots, and all runtime appdata.
set -eu
output=${1:?Usage: sh packaging/source-bundle.sh /absolute/output.tar.gz}
case "$output" in /*) ;; *) echo 'Output must be an absolute path' >&2; exit 1 ;; esac
COPYFILE_DISABLE=1 tar --no-xattrs --no-acls --exclude=__pycache__ --exclude='*.pyc' -czf "$output" \
  .dockerignore .gitignore .gitleaks.toml LICENSE README.md CONTRIBUTING.md SECURITY.md go.mod go.sum Dockerfile \
  compose.yaml compose.portable.yaml compose.network.yaml portable.env.example config.example.json \
  cmd internal scripts packaging .github docs/images docs/RELEASING.md docs/licenses docs/API.md docs/MODULES.md docs/ARCHITECTURE.md docs/INSTALL.md docs/TESTING.md docs/CHANNEL-MATCHING.md docs/RELEASE-NOTES.md docs/DOCKER-HUB.md
