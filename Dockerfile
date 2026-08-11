# syntax=docker/dockerfile:1
FROM python:3.12-slim AS base

ENV PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    PIP_NO_CACHE_DIR=1 \
    HF_HOME=/home/2dph/.cache/huggingface

WORKDIR /app
RUN id -u 2dph 2>/dev/null || useradd --create-home --uid 1001 2dph

# deps layer-first: rebuild only on dependency change
COPY requirements.lock.txt /tmp/requirements.lock.txt
RUN python -m pip install --no-cache-dir -r /tmp/requirements.lock.txt \
    && rm /tmp/requirements.lock.txt

# Go services: static binaries, no interpreter at runtime
FROM golang:1.25 AS go-build
WORKDIR /src
COPY go.mod ./
COPY bin/server ./bin/server
COPY bin/watch ./bin/watch
RUN CGO_ENABLED=0 go build -o /serve ./bin/server \
    && CGO_ENABLED=0 go build -o /watch ./bin/watch

# runtime: python toolchain + Go services
FROM base
COPY . .
COPY --from=go-build /serve /app/bin/serve
COPY --from=go-build /watch /app/bin/watch
RUN chmod +x /app/bin/docker-entrypoint \
    && chown -R 2dph:2dph /app
USER 2dph

ENV PATH="/app/bin:${PATH}" \
    KB_PY=python3 \
    KB_ROOT=/app
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD python -c "import model2vec, ladybug, mistune; print('ok')" || exit 1

ENTRYPOINT ["/app/bin/docker-entrypoint"]