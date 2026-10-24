# Swar API

Base URL: `http://<host>:8765`. All `/v1` responses use one envelope:

```json
{"success": true, "data": { ... }}
{"success": false, "message": "Human readable", "error": {"code": "stable_code"}}
```

Clients should branch on `error.code`, never on `message`.

| Status | Codes |
|---|---|
| 400 | `invalid_request`, `invalid_command`, `unknown_track` |
| 401 | `unauthorized` (missing, invalid or expired token, or deleted device) |
| 403 | `unavailable` (source restriction or operator setting), `removed_from_jam`, `not_a_member`, `admin_only`, `not_allowed` |
| 404 | `not_found`, `jam_not_found`, `entry_not_found` |
| 409 | `stale_revision`, `jam_full`, `queue_full` |
| 410 | `jam_ended`, `jam_expired` |
| 413 | `body_too_large` |
| 429 | `rate_limited` (with `Retry-After`) |
| 502 / 503 / 504 | `upstream_error`, `rate_limited` (upstream throttling), `upstream_timeout` |

## IDs

Every track, album, artist and playlist ID is `provider:providerId`, for example `dz:1827471677` (Deezer), `it:1634567895` (Apple iTunes), `mb:c42d3d9e-1082...` (MusicBrainz), `sp:4cOd...` (Spotify), `yt:dQw4w9WgXcQ` (YouTube / Deep Search), or legacy `saavn:IBEtXv2e`. Bare 11-character IDs are accepted as YouTube IDs.

## Identity

There are no passwords. A device registers once and keeps a signed token.

| Method | Path | Notes |
|---|---|---|
| POST | `/v1/auth/device` | → `{deviceId, token, expiresAt}` (201) |
| POST | `/v1/auth/refresh` | Bearer token → a new token |
| DELETE | `/v1/auth/device` | Erases the device and all of its server-side data |

Send `Authorization: Bearer <token>`. WebSocket clients that cannot set headers may use `?access_token=` on `/ws` paths only (it is never logged).

Endpoints marked 🔒 below need a token.

## Catalog

| Method | Path | Notes |
|---|---|---|
| GET | `/v1/search?q=&type=all\|songs\|albums\|artists\|playlists&page=0&limit=10` | Unified search. Songs are de-duplicated (same ID, or same title + artists within ±2 s). `partial: true` with `errors` when some sections failed. With `type=artists`, each artist carries `songCount`; artists with no songs are dropped and the rest are sorted by song count |
| POST | `/v1/search/deep` | `{"query": "text or YouTube link", "limit": 10, "youtube": true}`. User-triggered only. Returns `{results: [{...track, source, confidence: "metadata_only"}], notice}`. `"youtube": false` searches only the music catalogs (the app ran yt-dlp on the device); the query is validated the same way and yt-dlp does not run. YouTube results are cached per normalized query and limit (`DEEPSEARCH_SEARCH_TTL`). |
| GET | `/v1/search/deep/status` | `{enabled, youtube: {playback, downloads, available, version, reason, running, capacity}}`. Whether yt-dlp works on the server and how busy it is; never paths or error text. The version check runs at most every 10 minutes. |
| GET | `/v1/tracks?ids=a,b,c` | Batch, max 100 |
| GET | `/v1/tracks/{id}` | |
| GET | `/v1/tracks/{id}/related` | Provider-related tracks |
| GET | `/v1/tracks/{id}/lyrics` | `404` when the track has none |
| GET | `/v1/albums/{id}` | With tracks |
| GET | `/v1/artists/{id}?songCount=&albumCount=&sortBy=popularity\|latest\|alphabetical&sortOrder=asc\|desc` | Top songs, albums, singles, similar artists, bio |
| GET | `/v1/artists/{id}/songs?page=&sortBy=` | Paged discography |
| GET | `/v1/artists/{id}/albums?page=&sortBy=` | |
| GET | `/v1/playlists/{id}?page=&limit=` | |
| GET | `/v1/resolve?link=https://www.jiosaavn.com/...` | Song, album, artist or playlist link (jiosaavn.com only) |
| GET | `/v1/browse/trending?lang=hindi` | |
| GET | `/v1/browse/modules?lang=hindi` | Home-screen collections. `verified: false` because these upstream shapes could not be checked live |

A track looks like:

```json
{
  "id": "dz:1827471677", "provider": "dz", "providerId": "1827471677",
  "title": "Kesariya", "artists": [{"id": "dz:4443585", "name": "Pritam", "role": "primary"}],
  "album": {"id": "dz:339462717", "name": "Brahmastra"},
  "durationSec": 268, "images": [{"quality": "1000x1000", "url": "https://..."}],
  "genre": "Bollywood", "year": 2022, "explicit": false, "hasLyrics": false,
  "url": "https://www.deezer.com/track/1827471677",
  "playable": true,
  "download": {"eligible": false, "reason": "preview_only"},
  "providerMeta": {"deezer_id": "1827471677", "isrc": "INS172200547", "preview_url": "https://..."}
}
```

`playable` and `download` describe what *this server* will do for the track, given its configuration (see DECISIONS.md).

## Playback

| Method | Path | Notes |
|---|---|---|
| GET | `/v1/playback/{id}` | Playback descriptor (below). Never audio bytes |
| POST | `/v1/playback/{id}/invalidate` | Call when a stream URL failed (expired), then fetch again |
| GET | `/v1/playback/{id}/audio` | Deep Search (YouTube) audio relayed by the server, with Range support. `/track/{id}` returns this path as `audio_url` for those tracks |
| POST | `/v1/playback/prefetch` | `{"trackIds": [next, nextNext], "dataSaver": false}`. Resolves at most 2 (1 with data saver) in the background; a newer call cancels the previous one |

```json
{
  "trackId": "saavn:s1", "available": true,
  "sources": [{"kind": "audio", "quality": "160kbps", "url": "https://...", "mimeType": "audio/mp4", "bitrate": 160}],
  "expiresAt": "2026-10-19T20:00:00Z", "refreshAfter": "2026-10-19T19:30:00Z",
  "track": { ... }
}
```

When `available` is false, `reason` is a short human-readable explanation (for example "streaming for this source is not enabled on this server"). Re-fetch after `refreshAfter`.

## Queue 🔒

Server-side queue per device, so it survives reinstalls and can follow the user across sessions.

| Method | Path | Notes |
|---|---|---|
| GET | `/v1/queue` | `{entries, order, current, shuffle, repeat, revision}` |
| PUT | `/v1/queue` | `{"trackIds": [...], "startIndex": 0, "shuffle": false, "repeat": "off"}` |
| POST | `/v1/queue/ops` | `{"op": "add\|play_next\|remove\|move\|jump\|next\|previous\|shuffle\|repeat\|clear", "rev": 7, ...}`. With `rev`, a stale revision gets `409 stale_revision` |

## Recommendations and radio

| Method | Path | Notes |
|---|---|---|
| POST | `/v1/recommendations/next` | `{"seedTrackId", "exclude": [], "playlistContext": [], "session": [], "skipped": [], "limit": 20}` (`session`: tracks played earlier this session, newest first; `skipped`: tracks skipped this session) → `{seed, results: [{track, score, reasons: [...]}], sources, degraded}` |
| POST | `/v1/radio` | `{"kind": "song\|artist\|album\|personalized\|genre\|discovery", "seed": "...", "count": 10}` |
| GET | `/v1/radio/{id}` | |
| POST | `/v1/radio/{id}/next` | `{"count": 10}` |
| POST | `/v1/radio/{id}/feedback` | `{"trackId", "action": "like\|dislike\|skip"}` |
| DELETE | `/v1/radio/{id}` | |

Every recommendation carries `reasons` (`provider_related`, `same_artist`, `liked_artist`, `preferred_language`, `recently_played`, `often_skipped`, `session_related`, `session_artist`, `skipped_this_session`, `same_era`, ...). Radio sessions carry `generation`, one of `provider_station_ranked`, `swar_recommendations`, `catalog_search`, `random_discovery`, so the app can label honestly how a station was built. `personalized` needs a device token and falls back to `discovery` without history.

## Library 🔒

| Method | Path | Notes |
|---|---|---|
| GET / PUT / DELETE | `/v1/library/likes[/{trackId}]` | |
| GET / PUT / DELETE | `/v1/library/dislikes[/{trackId}]` | Dislikes are used by recommendations |
| GET / POST | `/v1/library/playlists` | `{"name", "trackIds"}` |
| GET / PATCH / DELETE | `/v1/library/playlists/{id}` | PATCH `{"name"}` |
| PUT | `/v1/library/playlists/{id}/tracks` | `{"trackIds": [...]}` replaces the list |
| POST | `/v1/library/history` | `{"events": [{"trackId", "playedAt": unixMs, "msPlayed", "skipped", "context"}]}` up to 100 per call, last 30 days |
| GET / DELETE | `/v1/library/history` | |
| GET | `/v1/library/recent` | Distinct recently played tracks |
| GET / PUT | `/v1/library/preferences` | String map. Keys: `languages` (comma-separated), `explicit`, `dataSaver`, `preferOffline` (`"true"`/`"false"`), `streamQuality` |

## Downloads

The server never stores or serves audio. It decides eligibility and keeps the device's download status so the app can show and resume them.

| Method | Path | Notes |
|---|---|---|
| GET | `/v1/downloads/eligibility/{trackId}` | `{eligible, reason, descriptor?}`. When eligible, the descriptor holds the source URL the app downloads from |
| GET 🔒 | `/v1/downloads` | The device's download records |
| PUT 🔒 | `/v1/downloads/{trackId}` | `{"status", "bytesTotal", "bytesDone", "sha256", "error"}` |
| DELETE 🔒 | `/v1/downloads/{trackId}` | |

Statuses: `queued → downloading ⇄ paused → verifying → completed`, plus `failed` and `canceled` (retry goes back to `queued`). `queued` re-checks eligibility. `completed` requires `bytesDone == bytesTotal > 0` and a 64-hex `sha256`.

## Swar Jam 🔒

| Method | Path | Notes |
|---|---|---|
| POST | `/v1/jams` | `{"name", "displayName", "settings": {"allowParticipantAdd", "allowVoting"}}` → snapshot (201). Creator is admin |
| POST | `/v1/jams/join` | `{"code": "K7M-Q2P", "displayName"}`. Codes are 6 characters, case and dashes ignored. 10 attempts per minute per device |
| GET | `/v1/jams/{id}` | Snapshot |
| POST | `/v1/jams/{id}/leave` | Admin leaving ends the Jam |
| POST | `/v1/jams/{id}/end` | Admin only |
| POST | `/v1/jams/{id}/commands` | A command (below) over HTTP |
| GET | `/v1/jams/{id}/participants` | |
| DELETE | `/v1/jams/{id}/participants/{participantId}` | Admin only. The removed device's sockets close and it cannot rejoin until `allow_rejoin` |
| GET | `/v1/jams/{id}/ws` | WebSocket |

Commands: `{"type": ..., "rev"?: n, ...}`

| type | fields | who |
|---|---|---|
| `play`, `pause`, `next`, `previous` | | admin |
| `seek` | `positionMs` | admin |
| `jump` | `entryId` | admin |
| `shuffle` | `shuffle: bool` | admin |
| `repeat` | `repeat: off\|all\|one` | admin |
| `queue_add` | `trackIds` (1-50) | admin, or anyone when `allowParticipantAdd` |
| `queue_play_next`, `queue_move` (`entryId`, `to`) | | admin |
| `queue_remove` | `entryId` | admin, or whoever added the entry |
| `vote`, `unvote` | `entryId` | anyone when `allowVoting`; votes reorder upcoming entries |
| `track_ended` | `entryId` | admin (the admin's player reports the end) |
| `remove_participant`, `allow_rejoin` | `participantId` | admin |
| `update_settings` | `settings` | admin |
| `end` | | admin |

`play/pause/seek/next/previous/jump/queue_move/queue_remove/track_ended` honour `rev`: if it is not the current revision the command is rejected with `stale_revision` and the client receives a fresh `state`.

### WebSocket protocol

Client → server: a command plus an optional `id` echoed back, or `{"type": "ping", "id", "clientTime"}`, or `{"type": "resync"}`.

Server → client:

| type | body |
|---|---|
| `state` | `{rev, serverTime, jam: snapshot}`. Sent on connect, after every change, and after a stale command |
| `sync` | `{rev, serverTime, playback}` every 5 s for drift correction |
| `ack` | `{id, rev}` |
| `error` | `{id, code, message}` |
| `pong` | `{id, clientTime, serverTime}` for clock-offset estimation |
| `removed` / `ended` | then the socket closes |

Playback is anchored to server time: while `playing`, position = `positionMs + (now_server - updatedAt)`. Estimate `now_server` from `pong` (offset ≈ `serverTime - (clientSend + clientRecv)/2`). Each client plays audio from its own authorized source; the Jam synchronizes only state.

Jams live in memory. They end after `JAM_IDLE_TTL` with nobody connected, after `JAM_MAX_LIFETIME`, or on restart.

## App status and admin

Every response carries `X-Swar-Maintenance: on|off` and `X-Swar-Popup: <key>|none`. The popup key changes whenever a popup is activated or edited.

`GET /v1/status` → `{maintenance: {enabled, message}, popup: {id, kind, title, message, buttonText, buttonUrl, dismissible, createdAt, updatedAt} | null, popupKey}`.

While maintenance is on, every route except `/healthz`, `/readyz`, `/health`, `/metrics`, `/v1/status` and `/v1/admin/*` answers `503` with code `maintenance` and the maintenance message.

Admin routes need HTTP Basic auth with `ADMIN_USERNAME` / `ADMIN_PASSWORD` (`404 admin_disabled` when those are unset, `401` on wrong credentials; failed attempts are rate limited hard):

| Route | Body | Does |
| --- | --- | --- |
| `GET /v1/admin/maintenance` | | current state |
| `PUT /v1/admin/maintenance` | `{enabled, message?}` | turn maintenance on or off |
| `DELETE /v1/admin/cache` | | empty every server cache (catalog responses, playback descriptors, Deep Search results, YouTube matches, device checks); answers `{cleared: {catalog, playback, deepSearch, youtubeMatches, devices}, total}` with entry counts. All caches are in memory; nothing is lost, the next requests reload from the providers and the database |
| `GET /v1/admin/popups` | | `{popups: [...], activeId}` (0 when none) |
| `POST /v1/admin/popups` | `{kind, title, message, buttonText?, buttonUrl?, dismissible?, activate?}` | create; `kind` is `update`, `support` or `info` |
| `GET /v1/admin/popups/{id}` | | one popup |
| `PUT /v1/admin/popups/{id}` | same as create | replace its fields (`activate: true` also shows it) |
| `DELETE /v1/admin/popups/{id}` | | delete (deactivates it if active) |
| `GET /v1/admin/popups/active` | | `{active, popup}` |
| `PUT /v1/admin/popups/active` | `{id}` | show this popup, replacing the active one |
| `DELETE /v1/admin/popups/active` | | show no popup |

## Health and metrics

`GET /healthz` (liveness), `GET /readyz` (database check), `GET /metrics` (Prometheus text format, prefix `swar_`).

## Compatibility routes

Kept so existing clients keep working:

- Old Flutter client: `GET /health`, `/search?q=&limit=` → `{"results": [{id, title, artist, thumbnail, duration}]}`, `/track/{id}` → `{..., audio_url}`, `/video/{id}` → `{..., video_url, audio_url}`, `/related/{id}`.
- Reference Saavn-Api shape: `/api/search`, `/api/search/{songs,albums,artists,playlists}?query=&page=&limit=`, `/api/songs?ids=|link=`, `/api/songs/{id}`, `/api/songs/{id}/suggestions`, `/api/albums?id=|link=`, `/api/artists?id=|link=`, `/api/artists/{id}`, `/api/artists/{id}/songs`, `/api/artists/{id}/albums`, `/api/playlists?id=|link=`.
