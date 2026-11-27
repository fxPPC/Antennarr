#!/usr/bin/env bash
# Run only in the alpha release-candidate job, after native tests and scan review.
set -euo pipefail
cd "$RUNNER_TEMP/candidate"
api="repos/$GITHUB_REPOSITORY"
test "$GITHUB_REF" = "refs/heads/release-candidate/$VERSION"
test "$(gh api "$api/git/ref/heads/testing/v1.2" --jq .object.sha)" = "$GITHUB_SHA"
test "$(gh api "$api/releases/latest" --jq .tag_name)" = "$BASELINE"
test -s "$GITHUB_WORKSPACE/docs/V1.2-ALPHA.1.md"

# Resume a draft from an interrupted upload only for this exact version and commit.
# A published release is immutable; reruns only verify its contents.
view=$(gh api --paginate --slurp "$api/releases?per_page=100" |
  jq -c --arg v "$VERSION" 'flatten | map(select(.tag_name == $v)) | first // {}')
if [[ "$view" == '{}' ]]; then
  gh release create "$VERSION" --repo "$GITHUB_REPOSITORY" --target "$GITHUB_SHA" --draft --prerelease --latest=false \
    --title "Antennarr $VERSION (Linux ARM64)" --notes-file "$GITHUB_WORKSPACE/docs/V1.2-ALPHA.1.md"
else
  jq -e --arg c "$GITHUB_SHA" '.prerelease and (.target_commitish == $c)' <<< "$view" >/dev/null
fi
if [[ $(jq -r '.draft // false' <<< "$view") == true || "$view" == '{}' ]]; then
  gh release upload "$VERSION" --repo "$GITHUB_REPOSITORY" --clobber ./*
  gh release edit "$VERSION" --repo "$GITHUB_REPOSITORY" --draft=false --prerelease --latest=false
fi
test "$(gh api "$api/releases/latest" --jq .tag_name)" = "$BASELINE"
gh api "$api/releases/tags/$VERSION" | jq -e '.prerelease and (.draft|not)' >/dev/null
test "$(gh api "$api/commits/$VERSION" --jq .sha)" = "$GITHUB_SHA"
download=$(mktemp -d)
trap 'rm -rf "$download"' EXIT
gh release download "$VERSION" --repo "$GITHUB_REPOSITORY" --dir "$download"
(cd "$download" && sha256sum --check "$RUNNER_TEMP/candidate/SHA256SUMS")
cmp SHA256SUMS "$download/SHA256SUMS"
docker load -i "$download/antennarr-$VERSION-linux-arm64.docker.tar.gz"
test "$(docker image inspect --format '{{.Id}}' "$ANTENNARR_TEST_IMAGE")" = "$(jq -r .image_id BUILD.json)"
cd "$GITHUB_WORKSPACE"
python3 packaging/test-permissions.py
