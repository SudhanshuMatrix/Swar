# Design decisions

## Rights and source restrictions come first

The architecture adheres to "Do not bypass DRM, access controls, licensing, or source restrictions":

- **OMMR Native Go Music Metadata Engine**: Replaced the obsolete unofficial JioSaavn scraper with a native Go music metadata engine inspired by OMMR (Open Music Metadata Resolver). Leverages legitimate zero-key public APIs (Deezer, Apple iTunes Search API, MusicBrainz) and optional authenticated Spotify Web API without bypassing DRM or authentication.
- **Deep Search** finds metadata only by default. Resolving its results to stream URLs (`DEEPSEARCH_PLAYBACK_ENABLED`) is a separate switch. yt-dlp formats that report DRM are skipped, and private or region-locked videos are reported as unavailable rather than worked around.

The server never re-encodes or stores audio. For Deezer and Apple iTunes, authorized 30-second audio previews are available when configured. Deep Search audio (only when `DEEPSEARCH_PLAYBACK_ENABLED` is on) is relayed through the server for signed URLs.

## Architecture

```
cmd/server            wiring, graceful shutdown
internal/config       env configuration with validation
internal/catalog      provider-neutral models, typed errors, cached service
internal/cache        bounded LRU + TTL with request coalescing
internal/music        native music engine inspired by OMMR
internal/music/providers   deezer, itunes, musicbrainz, spotify
internal/music/normalize   unicode, Indian script preservation, variations, Jaro-Winkler, TokenSet
internal/music/search      federated search, canonical deduplication, ranking
internal/providers    deepsearch (yt-dlp)
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

- **Internal IDs** are `provider:id` (e.g. `dz:1827471677`, `it:1634567895`, `mb:uuid`, `sp:id`, `yt:videoId`, and legacy `saavn:id`). This keeps multi-provider results in one list, one queue and one library without collisions.
- **Upstream providers**: Deezer, Apple iTunes Search API, MusicBrainz WS/2, and Spotify. Per-request timeouts, bounded concurrency, context cancellations, and partial failure resilience.
- **Cache**: one bounded LRU with per-kind TTLs (search 30 min, entities 24 h, related 10 min). Concurrent misses for the same key share one upstream call via singleflight coalescing.
- **Search de-duplication & ranking**: OMMR-inspired multi-provider deduplication that synthesizes matching recordings into rich canonical records, while strictly preserving variations (Remixes, Live versions, Acoustic, Covers, and Lo-Fi recordings are never merged with original studio recordings). Relevance ranking combines exact/prefix title matches, token sets, artist matches, variation intent alignment, and popularity signals.
- **Recommendations** are a transparent weighted score over candidate pools (provider related, same artist, playlist context) with the device's likes, dislikes, skips, recent plays and languages. Each result lists its reasons. No opaque model, and no data leaves the server. The current listening session also counts: artists from the last few songs get a fading boost, songs related to earlier session tracks join the pool, artists skipped this session are pushed down, songs from the seed's era get a small boost, other versions of songs already heard (remix, lofi, reprise) are dropped, and the final list is re-ordered with maximal marginal relevance so artists mix instead of clustering.
- **Radio** labels how each station was generated (`generation`), so the app never claims "personalized" when it fell back to discovery.
- **Jam** is server-authoritative with a revision number. Membership and role are re-checked on every command, so a removed participant's open socket or saved token cannot act. Position is anchored to server time to keep clients in sync without streaming audio. Slow sockets are dropped and resync on reconnect. Jams are in memory by design: they are short-lived, and a restart ends them cleanly.
- **Identity** is per device (HMAC-signed token, no passwords, no personal data). Deleting the device deletes its data.
- **yt-dlp** runs with an argument vector (no shell), `--ignore-config`, `--` before the single positional, a timeout, an output cap and a concurrency cap. Queries become `ytsearchN:<text>`; links are reduced to a validated 11-character video ID and rebuilt as a canonical URL.
