# syntax=docker/dockerfile:1

# ---- build ----
FROM golang:1.24-alpine AS build
WORKDIR /src
ENV CGO_ENABLED=0 GOTOOLCHAIN=local
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -trimpath -ldflags="-s -w" -o /out/swar-server ./cmd/server

# ---- runtime ----
FROM alpine:3.20
# yt-dlp is only needed for Deep Search. Build with --build-arg WITH_YTDLP=false
# to leave it out (Deep Search then reports itself unavailable).
ARG WITH_YTDLP=true
RUN apk add --no-cache ca-certificates tzdata \
 && if [ "$WITH_YTDLP" = "true" ]; then apk add --no-cache yt-dlp; fi \
 && addgroup -S swar && adduser -S -G swar -H swar \
 && mkdir -p /data && chown swar:swar /data
COPY --from=build /out/swar-server /usr/local/bin/swar-server
USER swar
ENV SWAR_ENV=production \
    SWAR_ADDR=:8765 \
    SWAR_DB_PATH=/data/swar.db
VOLUME ["/data"]
EXPOSE 8765
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8765/healthz >/dev/null || exit 1
ENTRYPOINT ["/usr/local/bin/swar-server"]
