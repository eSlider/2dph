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

# Go serve: static binary, no interpreter at runtime
FROM golang:1.25 AS serve-build
WORKDIR /src/serve
COPY serve/go.mod serve/go.sum* ./
COPY serve .
RUN CGO_ENABLED=0 go build -o /serve -ldflags="-s -w" .

# runtime: python toolchain + Go server
FROM base
COPY . .
COPY --from=serve-build /serve /app/serve/serve
RUN chmod +x /app/bin/kb-watch /app/bin/docker-entrypoint \
    && chown -R 2dph:2dph /app
USER 2dph

ENV PATH="/app/bin:${PATH}" \
    KB_PY=python3
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD python -c "import model2vec, ladybug, mistune; print('ok')" || exit 1

ENTRYPOINT ["/app/bin/docker-entrypoint"]