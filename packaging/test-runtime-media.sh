#!/bin/sh
# Run synthetic media and CPU-affinity checks with the final image's own FFmpeg.
# Go/fixture generation stay on the runner; the disposable container has no network.
set -eu
image=${ANTENNARR_TEST_IMAGE:?Set ANTENNARR_TEST_IMAGE}
fixtures=${ANTENNARR_TEST_FIXTURES:?Set ANTENNARR_TEST_FIXTURES}
tests=$(mktemp -d)
trap 'rm -rf "$tests"' EXIT HUP INT TERM
chmod 755 "$tests"
for package in media engine checkcpu stream; do
  CGO_ENABLED=0 GOOS=linux go test -c -o "$tests/$package.test" "./internal/$package"
done
docker run --rm --network none --user 10001:10001 --read-only --tmpfs /tmp \
  -v "$tests:/tests:ro" -v "$fixtures:/fixtures:ro" \
  -e ANTENNARR_TEST_FIXTURES=/fixtures --entrypoint /bin/sh "$image" \
  -c 'set -eu; taskset --version; for test in /tests/*.test; do "$test" -test.v -test.timeout=10m; done'
