# Standard Debian stable runtime; no fixture video or build toolchain is shipped.
FROM debian:trixie-slim AS runtime-base
ARG VERSION=v1.1.0-alpha.2
ARG REVISION=unknown
LABEL org.opencontainers.image.title="Antennarr" org.opencontainers.image.version="${VERSION}" org.opencontainers.image.licenses="GPL-3.0-or-later" org.opencontainers.image.source="https://github.com/fxPPC/Antennarr" org.opencontainers.image.revision="${REVISION}"
RUN apt-get update && apt-get install -y --no-install-recommends ffmpeg ca-certificates curl tini util-linux && rm -rf /var/lib/apt/lists/* && useradd -u 10001 -m antennarr
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
RUN go test -race -timeout 15m ./... && CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /antennarr ./cmd/antennarr && sh packaging/source-bundle.sh /antennarr-source.tar.gz

FROM runtime-base AS runtime
COPY --from=build /antennarr /usr/local/bin/antennarr
COPY --from=build /antennarr-source.tar.gz /usr/share/doc/antennarr/source.tar.gz
