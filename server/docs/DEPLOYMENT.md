# Deployment

## Run locally

```bash
cd server
cp .env.example .env        # optional; defaults work for development
make run                    # or: go run ./cmd/server
```

The server listens on `:8765` and creates `data/swar.db`. Go 1.24+ is required. Install `yt-dlp` for Deep Search (`pip install -U yt-dlp`).

## Docker

```bash
docker build -t swar-server .
docker run -d --name swar -p 8765:8765 \
  -e SWAR_TOKEN_SECRET="$(openssl rand -hex 32)" \
  -v swar-data:/data swar-server
```

The image runs as a non-root user, keeps the database in `/data`, and has a health check on `/healthz`. Build with `--build-arg WITH_YTDLP=false` to leave yt-dlp out.

Keep the same `SWAR_TOKEN_SECRET` across restarts; changing it signs every device out.

## Production checklist

- `SWAR_ENV=production` and a 32+ character `SWAR_TOKEN_SECRET`.
- Put it behind HTTPS (Caddy, nginx, a cloud load balancer). WebSockets must be allowed on `/v1/jams/*/ws`. Set `SWAR_TRUST_PROXY=true` only when that proxy overwrites `X-Forwarded-For`.
- Restrict `SWAR_CORS_ORIGINS` if a web client is served from a known origin. The mobile app does not need CORS.
- Back up `/data/swar.db` (SQLite in WAL mode; copy with `sqlite3 swar.db ".backup out.db"`).
- Decide the media switches deliberately (see DECISIONS.md).
- Scrape `/metrics` and alert on `/readyz`.

## Sizing

Run a **single instance**: Jam sessions, radio sessions, rate-limit buckets and the cache are in process memory.

| Users | CPU / RAM | Notes |
|---|---|---|
| Personal / family (≤ 20) | 0.5 vCPU, 256 MB | A Raspberry Pi 4 is fine |
| Small community (≤ 500) | 1 vCPU, 512 MB | Default cache of 20 000 entries uses roughly 50-150 MB |
| Larger | 2 vCPU, 1 GB+ | Lower `DEEPSEARCH_CONCURRENCY` if yt-dlp processes compete for CPU |

Each Deep Search starts a yt-dlp (Python) process using about 50-100 MB for a few seconds; the concurrency limit caps that.

## Hosting caveats

- **Region**: the Saavn catalog is region-dependent and may refuse or return less from outside India. Host where your users are, and confirm you are allowed to use the catalog there.
- **Datacenter IPs**: YouTube often throttles or challenges cloud IP ranges, so Deep Search can fail on some hosts. It degrades to a clear error; the rest of the app is unaffected.
- **Serverless platforms** (Vercel, Lambda) do not suit this server: it needs a persistent process for WebSockets and in-memory Jams, a writable disk for SQLite, and the yt-dlp binary. Use a small VM, a container host (Fly.io, Railway, Render) or a home server.
- Stream URLs from upstream expire (about 3 hours for Saavn); the app must re-fetch descriptors as documented, which the server makes cheap through caching.
