# Migrating to the Swar Go backend

## What changed for the Flutter app

The old backend (`backend/main.go`, yt-dlp only) is replaced by the Swar server in `server/`. The app keeps working without code changes because the old routes are still served with the same fields:

| Old route | Still works | Difference |
|---|---|---|
| `GET /health` | yes | |
| `GET /search?q=&limit=` | yes | Results come from the federated OMMR-inspired music metadata engine (Deezer, iTunes, MusicBrainz, Spotify). IDs are prefixed (e.g. `dz:1827471677`, `it:1634567895`). Extra fields (`provider`, `album`, `playable`, `downloadable`, `genre`) are additive |
| `GET /track/{id}` | yes | Resolves tracks across providers, returns audio URL if available (e.g. preview stream or Deep Search relay) |
| `GET /video/{id}` | yes | Only YouTube (`yt:`) tracks have video; other tracks return `404` |
| `GET /related/{id}` | yes | Ranked by the recommendation engine |

Saved data in Hive and SQLite (liked songs, playlists, recents, downloads) with legacy `saavn:` or bare IDs continue to parse and resolve cleanly.

## OMMR Metadata Engine Migration

The unofficial JioSaavn scraper has been removed and replaced by a native Go metadata engine adapted from OMMR (Open Music Metadata Resolver):
- **Providers implemented**: Deezer (`dz`), Apple iTunes (`it`), MusicBrainz (`mb`), Spotify (`sp`), plus Deep Search (`yt`).
- **Federated Search & Matching**: Zero-key concurrent resolution, canonical deduplication, variation preservation (remix, acoustic, live, cover), and Jaro-Winkler/TokenSet relevance ranking.
- **Obsolete `/api/...` routes**: Dropped in favor of standard `/v1/...` and Flutter compatibility routes (`/search`, `/track/{id}`).
