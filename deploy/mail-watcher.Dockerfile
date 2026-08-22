# deploy/mail-watcher.Dockerfile — IMAP → markdown → live brain ingest loop.
#
# Build (from the 2dph repo root):
#   docker build -f deploy/mail-watcher.Dockerfile -t 2dph/mail-watcher .
#
# Run needs: IMAP_* env, BRAIN_URL, and a volume for the mail tree + state:
#   docker run --network host \
#     --env-file .secrets/mail.env \
#     -e BRAIN_URL=http://127.0.0.1:8630 \
#     -v "$PWD/var/mail:/mail" \
#     2dph/mail-watcher
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY internal/ internal/
COPY pkg/ pkg/
COPY bin/ bin/
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/mail-watch ./bin/mail/watch

FROM alpine:3.22
RUN adduser -D -u 1000 watcher
COPY --from=build /out/mail-watch /usr/local/bin/mail-watch
USER watcher
WORKDIR /mail
# watch.go defaults to http://127.0.0.1:8630 when BRAIN_URL is unset; make it
# explicit so the container's ingest target is visible at a glance.
ENV BRAIN_URL=http://127.0.0.1:8630
ENTRYPOINT ["mail-watch", "--source", "imap", "--out", "/mail"]
