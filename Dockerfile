# syntax=docker/dockerfile:1
#
#   docker build --target api -t 2dph:api .
#
# API: Go + ladybug via Zig CGO (no CPython). The write path is Go
# (brain-index/brain-add); there is no Python sidecar.

# --- Go API: CGO with Zig, not gcc ---
FROM golang:1.26-bookworm AS api-build
WORKDIR /src
RUN apt-get update \
    && apt-get install -y --no-install-recommends curl xz-utils ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY bin/cgo ./bin/cgo
RUN chmod +x bin/cgo/zig bin/cgo/zcc bin/cgo/zc++ \
    && ./bin/cgo/zig env >/dev/null

COPY go.mod go.sum ./
RUN go mod download

COPY . .
ENV CGO_RPATH=/usr/local/lib
RUN eval "$(./bin/cgo/zig env)" \
    && go build -tags brain_serve,system_ladybug -o /out/brain-serve ./bin/brain/serve.go \
    && go build -tags system_ladybug -o /out/brain-search ./bin/brain/search.go \
    && go build -tags system_ladybug,brain_index -o /out/brain-index ./bin/brain/index.go \
    && go build -tags system_ladybug,brain_add -o /out/brain-add ./bin/brain/add.go \
    && CGO_ENABLED=0 go build -tags brain_watch -o /out/brain-watch ./bin/brain/watch.go \
    && CGO_ENABLED=0 go build -tags mail_import -o /out/mail-import ./bin/mail/import.go \
    && CGO_ENABLED=0 go build -o /out/mail-sync ./bin/mail/sync.go

FROM debian:bookworm-slim AS api
RUN apt-get update \
    && apt-get install -y --no-install-recommends libssl3 ca-certificates wget \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --create-home --uid 1001 2dph
COPY --from=api-build /out/brain-serve /usr/local/bin/brain-serve
COPY --from=api-build /out/brain-search /usr/local/bin/brain-search
COPY --from=api-build /out/brain-watch /usr/local/bin/brain-watch
COPY --from=api-build /out/brain-index /usr/local/bin/brain-index
COPY --from=api-build /out/brain-add /usr/local/bin/brain-add
COPY --from=api-build /out/mail-import /usr/local/bin/mail-import
COPY --from=api-build /out/mail-sync /usr/local/bin/mail-sync
COPY --from=api-build /src/lib-ladybug/liblbug.so.0.19.1 /usr/local/lib/liblbug.so.0.19.1
COPY bin/docker-entrypoint /usr/local/bin/docker-entrypoint
RUN chmod +x /usr/local/bin/docker-entrypoint \
    && ln -s liblbug.so.0.19.1 /usr/local/lib/liblbug.so.0 \
    && ln -s liblbug.so.0 /usr/local/lib/liblbug.so \
    && ldconfig
USER 2dph
ENV KB_ROOT=/data \
    KB_PORT=8630 \
    LD_LIBRARY_PATH=/usr/local/lib \
    HF_HOME=/data/hf
WORKDIR /data
EXPOSE 8630
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -qO- http://127.0.0.1:8630/health || exit 1
ENTRYPOINT ["/usr/local/bin/docker-entrypoint"]
CMD ["serve"]
