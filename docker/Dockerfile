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

COPY . .

# wait for the actual tools (bin/*) to exist before wiring docker helpers
COPY docker/kb-watch /app/.dockerbin/kb-watch
COPY docker/serve /app/.dockerbin/serve
RUN chmod +x /app/.dockerbin/kb-watch /app/.dockerbin/serve \
    && chown -R 2dph:2dph /app
USER 2dph

ENV PATH="/app/.dockerbin:${PATH}"
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD python -c "import model2vec, ladybug, mistune; print('ok')" || exit 1

ENTRYPOINT ["/app/docker-entrypoint.sh"]