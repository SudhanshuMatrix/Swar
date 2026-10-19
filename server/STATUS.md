# Status report

Date: 2026-10-19. Everything below was run in a Linux container with Go 1.24.7 and Flutter 3.47.7. The real Saavn and YouTube services were **not reachable** from that container, so all automated tests use a local fake of the Saavn API and a fake yt-dlp runner.

## Verification results

| Check | Command | Result |
|---|---|---|
| Formatting | `gofmt -l .` | no files listed |
| Static analysis | `go vet ./...` | no findings |
| Go tests (race detector) | `go test -race -count=1 -timeout 180s ./...` | **all 15 test packages pass**, 91 test functions |
| Static build | `CGO_ENABLED=0 go build -trimpath ./cmd/server` | builds, 17 MB binary |
| Live smoke test | built binary, then curl `/healthz`, `/readyz`, `POST /v1/auth/device`, `/v1/search`, `POST /v1/search/deep`, `/metrics`, then SIGTERM | health/ready/auth/metrics OK; search returned `502 upstream_error` and Deep Search `502` because the container has no access to Saavn or YouTube (the expected, honest failure); SIGTERM logged "shutting down" and exited cleanly |
| Flutter analyzer | `flutter analyze` | 9 issues, all pre-existing in the original code (7 deprecation infos, 2 unused imports in the old `test/widget_test.dart`); none in new code |
| Flutter tests | `flutter test` | 6 tests pass |

Statement coverage per package (`go test -cover`): search 94%, playback 91%, cache 89%, recommendations 86%, jam 81%, library 81%, auth 78%, radio 77%, deepsearch 76%, downloads 76%, config 76%, saavn 66%, httpapi 37%. The repository package has no tests of its own but is exercised through the library, downloads and HTTP tests.

### What the tests cover

- **Saavn client**: field normalization from inconsistent upstream JSON, retries with backoff on 5xx/429, no retry on 4xx, timeouts, body size cap, concurrency limit, media URL decoding and its off-by-default gate, download eligibility reasons.
- **Cache**: TTL, LRU eviction, request coalescing, errors not cached, detached loads surviving a cancelled caller.
- **Search**: unified results, de-duplication, partial failure reporting, query validation.
- **Deep Search**: argument safety (`--`, `--ignore-config`, injection-shaped queries), link parsing and SSRF-shaped inputs rejected, timeouts, output cap, concurrency cap, DRM formats skipped, playback gate.
- **Playback**: every queue operation, shuffle/repeat, descriptor caching and refresh, prefetch limits and cancellation.
- **Recommendations / radio**: ranking reasons, penalties, artist diversity, degraded pools, all radio kinds and honest generation labels.
- **Library / downloads**: per-device isolation, playlists, history limits, preferences whitelist, download state machine, eligibility re-check, checksum and size rules.
- **Jam**: create/join, code normalization, invalid/expired/ended/full codes, join rate limit, admin-only commands, participant permissions, removal closing sockets and blocking commands/reconnect/rejoin until allowed, host leaving, stale revisions, reconnect resync, concurrent queue adds, votes, queue limit, server-time anchored positions.
- **HTTP API**: old Flutter route shapes, `/api` compatibility, `/v1` envelope and error codes, 401/413/429, unknown JSON fields, device deletion revoking tokens, Deep Search endpoint, Jam over real WebSockets with two clients (state, ack, error, pong, removal, end), health and metrics.

## Implemented

Everything in the prompt's server scope: Saavn catalog (search, songs, albums, artists with discography, playlists, lyrics, trending, browse modules, related, link resolution), unified search with de-duplication, Deep Search, playback descriptors without proxying audio, server-side queue, prefetch of the next one or two items, explainable recommendations, radio (song, artist, album, personalized, genre, discovery), download eligibility and status, Swar Jam, library (likes, dislikes, playlists, history, preferences), SQLite with migrations, device identity, validation, size limits, timeouts, rate limiting, CORS, SSRF protection, safe process execution, graceful shutdown, health checks, Prometheus metrics, Dockerfile, `.env.example`, `.gitignore`, docs.

Flutter: Deep Search button and screen, Offline Downloads screen and service, Radio from the song menu, prefetch, Jam tab, compatibility with saved data.

## Not verified / known limitations

- **No live upstream test.** Field mappings follow the reference Saavn-Api project and its fixtures. Trending and browse-module shapes are the least certain, so those responses carry `verified: false`. Run the server somewhere with access and try a few searches before relying on it.
- **Playback is off by default** (`SAAVN_MEDIA_URLS_ENABLED=false`), so out of the box the app can search but not play catalog songs. This is deliberate; see docs/DECISIONS.md.
- **Docker image not built**: the container had the Docker CLI but no daemon. The Dockerfile follows standard multi-stage patterns but has not been run.
- **Android APK not built**: no Android SDK in the container. The Dart code passes the analyzer and unit tests; it has not been run on a device or emulator, so the Jam sync feel, downloads on real storage, and the new screens still need a manual check.
- Jams and radio sessions are in memory and end on restart; run one instance.
- Jam audio sync is best effort (corrections above 2 s of drift); it is not sample-accurate.
- Offline downloads are not available in the web build.
