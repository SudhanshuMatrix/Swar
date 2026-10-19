# Migrating to the Swar Go backend

## What changed for the Flutter app

The old backend (`backend/main.go`, yt-dlp only) is replaced by the Swar server in `server/`. The app keeps working without code changes because the old routes are still served with the same fields:

| Old route | Still works | Difference |
|---|---|---|
| `GET /health` | yes | |
| `GET /search?q=&limit=` | yes | Results now come from the Saavn catalog. IDs look like `saavn:abc123` instead of a YouTube ID. Extra fields (`provider`, `album`, `playable`, `downloadable`, `language`) are additive |
| `GET /track/{id}` | yes | Returns `403 {"error", "code": "unavailable"}` when the server does not stream that source, instead of a 500 |
| `GET /video/{id}` | yes | Only YouTube (`yt:`) tracks have video; Saavn tracks return `404` |
| `GET /related/{id}` | yes | Now ranked by the recommendation engine |

Saved data in Hive (liked songs, playlists, recents) keeps bare YouTube IDs. The server still accepts those, so old entries continue to resolve through Deep Search sources when `DEEPSEARCH_PLAYBACK_ENABLED=true`.

## What the updated app adds

The app in this repository has already been updated:

- `lib/models/track.dart` reads both the legacy and `/v1` shapes and keeps the new flags.
- `lib/services/api_service.dart` registers the device once (`POST /v1/auth/device`, token kept in Hive), unwraps the `/v1` envelope into `ApiException(code, message)`, and adds Deep Search, prefetch, radio, downloads and Jam calls.
- **Deep Search**: after any search the screen shows "Didn't find your song? Try Deep Search." which opens `DeepSearchScreen` (`POST /v1/search/deep`). Results that this server will not play are labelled as metadata only.
- **Offline downloads** (`lib/services/download_service.dart`, `DownloadsScreen` under Library): asks the server for eligibility, downloads straight from the authorized source into the app's private storage, resumes with HTTP Range, verifies size and SHA-256, reports status to the server, and the player prefers the local file.
- **Radio**: "Start radio" in a song's menu; the queue keeps extending from the station instead of plain related tracks.
- **Prefetch**: after each track loads, the next one or two are resolved in advance.
- **Swar Jam** (new Jam tab): create or join with a code, copy the code, participant list with admin badge and removal, leave/end, connection status with automatic reconnect, shared queue, and local playback that follows the Jam's track, position and play state.

New packages: `path_provider`, `crypto`, `web_socket_channel`.

## Running both during the switch

There is nothing to migrate on the server side: the Swar server creates its own SQLite database on first start. Point the app at it with `--dart-define=API_BASE_URL=http://<host>:8765` exactly as before.

## For clients of the reference Saavn-Api

The reference project's `/api/...` routes are served with the same `{"success", "data"}` envelope. IDs in `/api` responses are the raw Saavn IDs, as before. New integrations should use `/v1` (docs/API.md).
