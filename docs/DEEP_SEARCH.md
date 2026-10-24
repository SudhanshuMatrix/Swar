# Deep Search: yt-dlp, caching and prefetching

This note records how Deep Search is built, what changed in the
"high-performance caching and prefetching" pass, and the limits of each
platform. It is written for whoever works on Swar next.

## What already existed (kept as is)

| Area | Where | Notes |
| --- | --- | --- |
| Normal search, JioSaavn first | `server/internal/catalog`, `server/internal/providers/saavn`, `lib/screens/search_screen.dart` | Untouched. Normal search never starts yt-dlp. |
| Explicit Deep Search | `POST /v1/search/deep`, `lib/screens/deep_search_screen.dart` | Opened from "Didn't find your song? Try Deep Search". |
| yt-dlp provider | `server/internal/providers/deepsearch` | Argument vector (no shell), `--ignore-config`, `--` before the one positional, only `ytsearchN:` or a rebuilt watch URL, output and time limits, DRM and restricted items refused. |
| Common track model | `catalog.Track` (server), `Track` (app) | Provider, provider ID and `providerMeta` keep source details. |
| Playback descriptors | `server/internal/playback` | Cached until shortly before the signed URL expires, never served stale. |
| Server cache | `server/internal/cache` | Bounded LRU with TTLs and request coalescing. Errors are never cached. |
| Device playback of Deep Search songs | `lib/services/youtube_audio.dart` | yt-dlp on the device resolves the audio-only stream (and finds the YouTube video for catalog songs). The server relay, also yt-dlp, is the fallback. |

## yt-dlp runs on the device

YouTube often refuses requests from Swar's server (the "confirm you're not
a bot" check), so Deep Search now runs yt-dlp **on the listener's device**.
YouTube answers the device, for both the search and the song's audio. The
server keeps doing what it did, as the fallback.

| Platform | How yt-dlp runs | Code |
| --- | --- | --- |
| Linux, Windows, macOS | The official standalone binary (`yt-dlp_linux`, `yt-dlp_linux_aarch64`, `yt-dlp.exe`, `yt-dlp_arm64.exe`, `yt-dlp_macos`), started directly with an argument list (no shell). | `lib/services/ytdlp/ytdlp_runner.dart` (`ProcessRunner`) |
| Android | The official `yt-dlp` zipapp, run by the Python inside [youtubedl-android](https://github.com/yausername/youtubedl-android) (`io.github.junkfood02.youtubedl-android:library:0.18.1`) through a method channel, off the main thread, two runs at most. | `packaging/android/YtDlpChannel.kt`, `ChannelRunner` |
| Web | Not available; the server is used. | |

### Shipping without yt-dlp, onboarding and updates

- **No yt-dlp in the app.** Desktop builds never contained it. On Android,
  youtubedl-android carries its own copy; release builds strip it
  (`tools:discard="@raw/ytdlp"` in `res/raw/keep.xml`), and Swar never starts
  the library before its own verified copy is in place (the library only
  copies its bundled one when the file is missing). Python itself has to
  ship: there is no way to run yt-dlp on Android without it.
- **First launch** shows "Getting Swar ready" and downloads yt-dlp
  (`lib/widgets/ytdlp_gate.dart`). If it fails, the user can try again or
  skip; Swar works without it (JioSaavn, downloads, and the server for Deep
  Search), and the background keeps trying.
- **Every download is verified** before it is run at all: its SHA-256 must
  match the release's `SHA2-256SUMS`. Then `--version` runs as a smoke test.
  A good download becomes current by an atomic rename of `current.json`. On
  Android the Kotlin side checks the hash again while copying the file into
  place. The installed file is re-hashed before its first run in each
  session; a file that no longer matches is treated as missing and
  re-downloaded.
- **Background updates.** About once a day (checked every 30 minutes while
  the app runs, first 20 seconds after start) the app downloads the small
  checksum list. Only when the release changed is the new binary
  downloaded. Failures back off from 5 minutes to 3 hours.
- **Auto clean.** The version an update replaced is kept until the new one
  completes one real run (so a broken release can be rolled back), then
  deleted. Partial downloads left by a crash are removed at start.
- **Two-day rule.** If updates (or the first download) keep failing for 48
  hours, a popup covers the app and downloads until it finishes, retrying by
  itself with back-off. After three failures in a row it also offers "Not
  now" until the next launch, so someone offline can still play their
  downloaded songs.
- The Library shows the installed version, when it was last checked, and
  an "Update now" button.

### Search and playback on the device

- **Deep Search** (`lib/services/deep_search_service.dart`): yt-dlp searches
  YouTube on the device while the server searches the other catalogs
  (`POST /v1/search/deep` with `"youtube": false`, so the server does not
  start yt-dlp). Without yt-dlp on the device, or when it fails, the
  server's full Deep Search runs, as before. Input is validated with the
  server's rules first: search text, or a YouTube link rebuilt from its
  video ID; other URLs are refused.
- **Playback** (`lib/services/youtube_audio.dart`): only yt-dlp is used. The
  stream is resolved on the device by yt-dlp, and catalog songs (Deezer,
  iTunes, Spotify) without a server match are looked up with a yt-dlp
  search on the device. Until yt-dlp is downloaded, or when it fails, the
  server relay (yt-dlp on the server, needs `DEEPSEARCH_PLAYBACK_ENABLED`)
  is the last resort.
  A restricted video (private, members-only, DRM, age-gated) is reported, not
  retried elsewhere.

## Caching, coalescing and prefetching

App (Flutter):

1. **Reusable cache** (`lib/services/ttl_cache.dart`): bounded LRU, fresh and
   stale times, stale-while-revalidate, one load per key at a time. Errors
   are never stored.
2. **Deep Search cache**: its own namespace, keys from the normalized query
   (case and spaces folded) and limit, fresh for 15 minutes, shown at once
   and refreshed in the background for up to 12 hours, the last 40 searches
   kept on disk (Hive). The screen ignores answers to searches the user has
   moved on from.
3. **Stream cache** (`YouTubeAudio`): at most 64 streams, each until 10
   minutes before its signed URL's `expire`. One resolve per video at a
   time. A cached URL that fails to load is resolved again once. Stream URLs
   are never written to disk or used as identifiers.
4. **yt-dlp runs**: identical searches and resolves share one process; at
   most two at a time on desktop, one on Android. Prefetch runs only when a
   slot is free and never waits.
5. **Next-song preparation** (`lib/services/prefetch_planner.dart`,
   `PlayerProvider`): 3 seconds after a song starts, the next Deep Search
   song's stream is resolved on the device, artwork is cached, and the
   server is asked to prepare the songs it plays itself. Data saver (on by
   default on phones) prepares one song and skips artwork; "Prepare the next
   song" turns it off. With shuffle, the next song is picked in advance so
   the prepared song is the one that plays. Work for a queue that has
   changed is dropped.
6. **Rapid skips**: each load has a generation number; a slow load that
   finishes after the listener moved on never replaces the newer song, and
   its errors are not shown.

Server (Go), now the fallback:

1. YouTube Deep Search results are cached per normalized query and limit
   (`DEEPSEARCH_SEARCH_TTL`, `DEEPSEARCH_CACHE_ENTRIES`); identical concurrent
   searches and resolves share one yt-dlp process (`cache.Group`).
2. Shared work stops when every caller waiting for it has gone.
3. Prefetching is background work: it never waits for a yt-dlp slot and
   leaves one free for listeners; a listener who joined a refused prefetch
   retries on their own.
4. `deepsearch.Service.Close` stops every yt-dlp process (the whole process
   group on Linux and macOS) on shutdown.
5. `GET /v1/search/deep/status` reports whether yt-dlp works, its version and
   how busy it is, without paths.
6. Histograms for yt-dlp start and run time, Deep Search and playback
   resolution, and counters for cache hits and misses and prefetch results
   (`/metrics`). `PREFETCH_ENABLED=false` turns server prefetching off.

## Platform limits (honest list)

- **Android APK size.** youtubedl-android adds Python, roughly 20 to 30 MB
  per CPU type. CI builds arm and arm64. Measure the APK after the first
  build; ABI splits would halve what each phone downloads.
- **Android is not build-tested here.** The channel, Gradle and manifest
  changes follow the library's documentation and source, but no Android SDK
  was available while writing them. Check on a device: the first Deep
  Search, playback, an update, and that `res/raw/ytdlp` is gone from the
  release APK (`aapt2 dump resources`).
- **JavaScript runtime.** YouTube now needs one for most videos. On desktop,
  yt-dlp uses Deno when it is on `PATH`, and Swar passes Node.js when only
  that is installed. Android has none, so the APK ships QuickJS-ng as
  `libqjs.so`: `scripts/build_quickjs_android.sh` (run by
  `prepare_platforms.sh`) builds it with the NDK from the engine source,
  pinned by SHA-256, with a small command line
  (`packaging/android/quickjs/swar_qjs.c`), and `YtDlpChannel.kt` passes it
  to every run with a cache folder, so YouTube's player script is prepared
  once rather than for every song. The first song after an install is the
  slow one. Not yet checked on a phone: that the binary starts there and
  solves a live challenge.
- **Checksums, not signatures.** The checksum list comes from the same
  GitHub release over HTTPS; its GPG signature is not checked (no OpenPGP
  library in the app).
- **Windows process tree.** Stopping a run ends yt-dlp's whole process tree
  with `taskkill /T`. On Linux and macOS, SIGTERM (which the standalone
  binary passes to its child) then SIGKILL.
- **Prefetch is not cancelled mid-run** on the device: a superseded prefetch
  finishes and only fills the cache. At most one runs at a time.
- **Network-aware prefetching** is the data saver switch; Swar does not
  detect metered networks by itself (that would need another plugin).

## Rights and safety

On the device, a Deep Search song is streamed by the listener's own app
from YouTube with yt-dlp; nothing is downloaded for
keeping. The server only fetches YouTube audio (its relay) when the
operator turns on `DEEPSEARCH_PLAYBACK_ENABLED`, and offline copies of Deep
Search songs still need `DEEPSEARCH_DOWNLOADS_ENABLED`. DRM-protected,
private, members-only and age-gated items are refused, and no access
control is bypassed. Stream URLs are treated as temporary, are not logged,
and are not cached past their expiry. Neither the app nor the server will
point yt-dlp at an arbitrary URL: Deep Search accepts search text or a
YouTube link, which is rebuilt from the validated video ID, and the Android
channel checks that again.

## Server configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `DEEPSEARCH_SEARCH_TTL` | `30m` | How long YouTube Deep Search results are reused; `0` turns the cache off. |
| `DEEPSEARCH_CACHE_ENTRIES` | `500` | Maximum cached Deep Search queries. |
| `PREFETCH_ENABLED` | `true` | Server-side next-song preparation. |

The server's caches are in memory and per instance. With more than one
instance, each keeps its own; nothing assumes they are shared.
