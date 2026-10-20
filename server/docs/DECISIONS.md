# Design decisions

## Rights and source restrictions come first

The prompt says "Do not bypass DRM, access controls, licensing, or source restrictions". Two parts of the reference Saavn-Api project sit close to that line, so they are **off by default** and need an explicit operator decision:

- **Saavn stream URLs** (`SAAVN_MEDIA_URLS_ENABLED`). The upstream hides stream locations behind a DES-encrypted `encrypted_media_url`. The reference project decrypts it with a fixed key. That is not DRM on the audio, but it is an access control of the source. Swar ships the same decoding so the Flutter app can keep working, but only when the operator turns it on and accepts responsibility for having the rights to stream that catalog where they run it. With it off, catalog features (search, details, lyrics, radio, Jam queues) all work and playback descriptors honestly say `available: false`.
- **Downloads** (`SAAVN_DOWNLOADS_ENABLED`, `DEEPSEARCH_DOWNLOADS_ENABLED`). Even when on, a Saavn track is only eligible when the provider's own rights field marks it `cacheable`. Every eligibility answer carries a reason (`provider_rights_disallow`, `downloads_not_enabled_for_source`, `operator_permitted`, ...).
- **Deep Search** finds metadata only by default. Resolving its results to stream URLs (`DEEPSEARCH_PLAYBACK_ENABLED`) is a separate switch. yt-dlp formats that report DRM are skipped, and private or region-locked videos are reported as unavailable rather than worked around.

The server never proxies, re-encodes or stores audio. It hands the client a descriptor and the client fetches from the source.

## Architecture

```
cmd/server            wiring, graceful shutdown
internal/config       env configuration with validation
internal/catalog      provider-neutral models, typed errors, cached service
internal/cache        bounded LRU + TTL with request coalescing
internal/providers    saavn (HTTP client + normalization), deepsearch (yt-dlp)
internal/search       unified search with de-duplication
internal/playback     queue model, playback descriptors, prefetch
internal/recommendations  explainable ranking
internal/radio        radio sessions
internal/library      likes, playlists, history, preferences
internal/downloads    eligibility and status state machine
internal/jam          Swar Jam (in-memory, server-authoritative)
internal/repository   SQLite store with embedded migrations
internal/auth         device tokens
internal/httpapi      routes, middleware, WebSocket, compatibility layers
```

Only two third-party modules: `modernc.org/sqlite` (pure Go, so the binary builds with `CGO_ENABLED=0`) and `github.com/coder/websocket`.

## Notable choices

- **Internal IDs** are `provider:id`. This keeps Saavn and YouTube results in one list, one queue and one library without collisions.
- **Upstream client**: per-request timeout, bounded concurrency, capped body size, retries with exponential backoff and jitter only on idempotent failures (5xx, 429 with `Retry-After`, network), no redirects followed. The base URL is fixed configuration; user input never becomes a URL the server fetches (link resolution only extracts a token from jiosaavn.com links), which is the SSRF guard.
- **Cache**: one bounded LRU with per-kind TTLs (search 30 min, entities 24 h, related 10 min). Concurrent misses for the same key share one upstream call. Errors are never cached, except "unavailable" playback answers for 5 minutes so a restricted track is not hammered.
- **Search de-duplication**: same internal ID, or same normalized title and artists with durations within ±2 s.
- **Recommendations** are a transparent weighted score over candidate pools (provider related, same artist, playlist context) with the device's likes, dislikes, skips, recent plays and languages. Each result lists its reasons. No opaque model, and no data leaves the server. The current listening session also counts: artists from the last few songs get a fading boost, songs related to earlier session tracks join the pool, artists skipped this session are pushed down, songs from the seed's era get a small boost, other versions of songs already heard (remix, lofi, reprise) are dropped, and the final list is re-ordered with maximal marginal relevance so artists mix instead of clustering.
- **Radio** labels how each station was generated (`generation`), so the app never claims "personalized" when it fell back to discovery.
- **Jam** is server-authoritative with a revision number. Membership and role are re-checked on every command, so a removed participant's open socket or saved token cannot act. Position is anchored to server time to keep clients in sync without streaming audio. Slow sockets are dropped and resync on reconnect. Jams are in memory by design: they are short-lived, and a restart ends them cleanly.
- **Identity** is per device (HMAC-signed token, no passwords, no personal data). Deleting the device deletes its data.
- **yt-dlp** runs with an argument vector (no shell), `--ignore-config`, `--` before the single positional, a timeout, an output cap and a concurrency cap. Queries become `ytsearchN:<text>`; links are reduced to a validated 11-character video ID and rebuilt as a canonical URL.
