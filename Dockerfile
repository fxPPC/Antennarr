# Maintained Wolfi runtime. Keep package metadata for complete image scans;
# no fixture video or build toolchain is shipped.
FROM cgr.dev/chainguard/wolfi-base:latest@sha256:580c9ae79c0229cc381475fbfbed0d91fab5d6606e57cd78732f9836de109e90 AS runtime-base
ARG VERSION=v1.1.0-alpha.4
ARG REVISION=unknown
LABEL org.opencontainers.image.title="Antennarr" org.opencontainers.image.version="${VERSION}" org.opencontainers.image.licenses="GPL-3.0-or-later" org.opencontainers.image.source="https://github.com/fxPPC/Antennarr" org.opencontainers.image.revision="${REVISION}"
RUN apk upgrade --no-cache && apk add --no-cache ffmpeg ca-certificates-bundle curl tini util-linux-misc util-linux-bins coreutils findutils shadow && useradd -u 10001 -m antennarr
RUN ffmpeg -version && ffprobe -version && taskset --version && setpriv --version
WORKDIR /opt/antennarr
COPY docs/licenses /usr/share/doc/antennarr/licenses
COPY LICENSE /usr/share/doc/antennarr/LICENSE
COPY packaging/entrypoint.sh /usr/local/bin/antennarr-entrypoint
RUN chmod 755 /usr/local/bin/antennarr-entrypoint
USER 0:0
EXPOSE 9192 9193 9200-9207
HEALTHCHECK --interval=30s --timeout=3s --start-period=30s CMD curl -fsS http://127.0.0.1:9192/healthz || exit 1
ENTRYPOINT ["/usr/local/bin/antennarr-entrypoint"]

FROM golang:1.27.4-trixie AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# CI tests this source revision on both architectures before release builds.
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /antennarr ./cmd/antennarr && sh packaging/source-bundle.sh /antennarr-source.tar.gz

FROM runtime-base AS runtime
COPY --from=build /antennarr /usr/local/bin/antennarr
COPY --from=build /antennarr-source.tar.gz /usr/share/doc/antennarr/source.tar.gz
