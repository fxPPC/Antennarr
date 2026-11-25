FROM golang:1.27.4-trixie@sha256:f18c3d73a184fb0582828510722e723b63a93f7188d11dd223d9be6f6880e65d AS go-tools

FROM go-tools AS runtime-snapshot
ARG TARGETARCH
WORKDIR /runtime-lock
COPY packaging/runtime/prepare.go packaging/runtime/lock.json ./
RUN --mount=type=bind,source=packaging/runtime,target=/cache \
    go run prepare.go -arch "$TARGETARCH" -cache /cache -out /wolfi-runtime.tar.gz

# Apply the original package-installation layer to its original Wolfi base.
# The layer contains ordinary filesystem entries and retains the APK database.
FROM cgr.dev/chainguard/wolfi-base@sha256:28c39be5a5e1e9b85081c10740e8495f6518a865b23bc6a486637c4b21e74b43 AS runtime-base
ARG VERSION=v1.1.0-beta.2
ARG REVISION=unknown
LABEL org.opencontainers.image.title="Antennarr" org.opencontainers.image.version="${VERSION}" org.opencontainers.image.licenses="GPL-3.0-or-later" org.opencontainers.image.source="https://github.com/fxPPC/Antennarr" org.opencontainers.image.revision="${REVISION}"
RUN --mount=type=bind,from=runtime-snapshot,source=/wolfi-runtime.tar.gz,target=/tmp/wolfi-runtime.tar.gz \
    tar -xzpf /tmp/wolfi-runtime.tar.gz -C / && \
    ffmpeg -version && ffprobe -version && taskset --version && setpriv --version
WORKDIR /opt/antennarr
COPY docs/licenses /usr/share/doc/antennarr/licenses
COPY LICENSE /usr/share/doc/antennarr/LICENSE
COPY packaging/entrypoint.sh /usr/local/bin/antennarr-entrypoint
RUN chmod 755 /usr/local/bin/antennarr-entrypoint
USER 0:0
EXPOSE 39192-39198
HEALTHCHECK --interval=30s --timeout=3s --start-period=30s CMD ["/usr/local/bin/antennarr", "healthcheck"]
ENTRYPOINT ["/usr/local/bin/antennarr-entrypoint"]

FROM go-tools AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# CI tests this source revision on both architectures before release builds.
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /antennarr ./cmd/antennarr && sh packaging/source-bundle.sh /antennarr-source.tar.gz

FROM runtime-base AS runtime
COPY --from=build /antennarr /usr/local/bin/antennarr
COPY --from=build /antennarr-source.tar.gz /usr/share/doc/antennarr/source.tar.gz
