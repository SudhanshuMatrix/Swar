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
# Debian rather than Alpine so the latest yt-dlp and the Deno JavaScript
# runtime it needs for YouTube can be installed (distribution packages lag
# behind YouTube's changes and many songs then fail to play).
FROM debian:bookworm-slim
# yt-dlp is only needed for Deep Search. Build with --build-arg WITH_YTDLP=false
# to leave it out (Deep Search then reports itself unavailable).
ARG WITH_YTDLP=true
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates tzdata wget \
 && if [ "$WITH_YTDLP" = "true" ]; then \
      apt-get install -y --no-install-recommends python3 python3-pip unzip \
      && pip3 install --break-system-packages --no-cache-dir -U "yt-dlp[default]" \
      && wget -qO /tmp/deno.zip https://github.com/denoland/deno/releases/latest/download/deno-x86_64-unknown-linux-gnu.zip \
      && unzip -q /tmp/deno.zip -d /usr/local/bin && rm /tmp/deno.zip \
      && apt-get purge -y unzip; \
    fi \
 && rm -rf /var/lib/apt/lists/* \
 && groupadd -r swar && useradd -r -g swar -M swar \
 && mkdir -p /data && chown swar:swar /data
COPY --from=build /out/swar-server /usr/local/bin/swar-server
USER swar
ENV SWAR_ENV=production \
    SWAR_ADDR=:8765 \
    XDG_CACHE_HOME=/tmp/swar-cache \
    DENO_DIR=/tmp/swar-cache/deno \
    SWAR_DB_PATH=/data/swar.db
VOLUME ["/data"]
EXPOSE 8765
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8765/healthz >/dev/null || exit 1
ENTRYPOINT ["/usr/local/bin/swar-server"]
