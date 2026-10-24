# Changelog

All notable changes to Swar are listed here, newest first. The format
follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the
project uses [semantic versioning](https://semver.org/).

To publish a release, raise `version:` in `pubspec.yaml`, add a section
below named after the new version (for example `## [0.3.0] - 2026-11-02`),
and push to `master`. The Build workflow then publishes GitHub Release
`v0.3.0` with every platform's build attached and this section as the
release notes.

## [Unreleased]

### Fixed

- JioSaavn songs play again. Since 1.2.6 the server asked JioSaavn for "saavn:<id>" instead of the song's own ID, so every song came back "track not found" (album, artist, playlist, lyrics and related lookups were affected the same way).
- Deep Search songs play on Android: the app now ships the JavaScript runtime yt-dlp needs for YouTube (QuickJS-ng), and yt-dlp keeps a cache on the phone, so only the first song after an install waits for YouTube's player script.

### Changed

- Deep Search songs, and YouTube Music songs in main search, now play only through yt-dlp: on the device, or the server's relay (also yt-dlp) when it can't. youtube_explode_dart is no longer used, including for finding the YouTube video of Deezer, iTunes and Spotify songs.

### Added

- `DELETE /v1/admin/cache` empties every server cache. It uses the existing admin login (`ADMIN_USERNAME` / `ADMIN_PASSWORD`).

## [1.2.7] - 2026-10-24

### Added

- yt-dlp now runs on your device for Deep Search and Deep Search songs, so YouTube blocking Swar's server no longer stops them. Linux, Windows and macOS use the official yt-dlp program; Android runs it with a small built-in Python. The server is still used when yt-dlp isn't on the device.
- Swar doesn't ship with yt-dlp: the first launch downloads it, checks it against the release's checksums before running it, and it then updates itself in the background about once a day, removing the old version once the new one works. If updating fails for two days, a popup downloads it before you continue.
- Library settings: "Prepare the next song", "Data saver", and yt-dlp's version with an "Update now" button.

### Changed

- The next song is prepared while the current one plays (its stream on your device, its cover art, and the server for catalog songs), so skipping starts at once. With shuffle, the next song is chosen ahead so the prepared one is the one that plays.
- Deep Search remembers recent searches (also across restarts) and shows them at once while refreshing in the background.
- Server: repeated or simultaneous Deep Searches and song lookups share one yt-dlp run and are cached; work nobody is waiting for any more is stopped; prefetching never takes capacity from someone listening; yt-dlp processes are stopped cleanly on shutdown. New `GET /v1/search/deep/status` and timing metrics.

### Fixed

- Skipping quickly through several songs could let a slow earlier song start playing over the one you picked.
- A song whose saved YouTube link had expired could fail to play instead of fetching a fresh link.

## [1.2.6] - 2026-10-22

### Changed

- Main search is JioSaavn and YouTube Music together: JioSaavn's songs, singers, albums and playlists, with YouTube Music songs mixed in. JioSaavn songs play in full from JioSaavn, and YouTube Music songs play in full from YouTube on your device. Search never waits more than 3 seconds for YouTube Music.
- Deep Search now also searches Deezer, iTunes and MusicBrainz (and Spotify when the server has keys) next to YouTube, and still shows those results when YouTube is blocking the server.
- Full songs for Deep Search catalog results: Deezer, iTunes and Spotify only give 30-second previews, so the app finds the song on YouTube and plays the full audio on your device. A preview only plays when no full song is found, and the app says so.
- Songs saved from JioSaavn before play again.

## [1.2.5] - 2026-10-22

### Changed

- Deep Search songs now play straight from YouTube on your device: the app asks YouTube for the video's sound only and plays it with the normal player controls, lock screen and notification. The server no longer has to fetch the song, so YouTube blocking the server stops these songs failing. The server relay is still tried if YouTube refuses the device, and the web version still uses the server.
- Server popups are redesigned: everything is centered, and the buttons are full width and stacked, including when a popup can't be closed.

## [1.2.4] - 2026-10-22

### Added

- Maintenance mode: when the server is switched into maintenance, the app shows a maintenance screen with the server's message, pauses playback, and opens again by itself when maintenance ends.
- Server popups: the server can show one announcement at a time, such as "update the app" or "support us", with an optional button that opens a link. A popup that was closed stays closed until a different one is turned on or it is edited.
- Admin API on the server (`/v1/admin`, protected by `ADMIN_USERNAME` and `ADMIN_PASSWORD`) to turn maintenance on or off and to create, list, edit, delete, activate and deactivate popups. Every server response now carries `X-Swar-Maintenance` and `X-Swar-Popup` headers, and `GET /v1/status` returns the details.

## [1.2.3] - 2026-10-21

### Added

- Search suggestions while you type: matching singers, songs and your recent searches. Tap a song to play it, or a singer to open their page.

### Changed

- Faster search: songs show as soon as they arrive and singers and playlists fill in after, and going back to a recent search is instant.
- Jam: when someone presses play or a new song starts, every device waits for the same moment and starts together, instead of each one starting whenever it finished loading. Seeking moves everyone too.

## [1.2.2] - 2026-10-21

### Changed

- Smoother on low-end phones: while a song plays, only the progress bars redraw, not the whole player, and the blurred Now Playing background is drawn once instead of on every change.
- Faster on every platform: the app keeps one connection to the server open instead of opening a new one for each request, and the server compresses its answers (song lists shrink several times over).
- When a Deep Search song won't play, the popup now says why (for example, that YouTube is blocking the server) instead of a generic error.

### Fixed

- The play button on playlists and singer pages turns into a pause button while that list is playing, and pauses and resumes it instead of starting over.

- Android and Windows builds failed in 1.2.1 because of the notification permission package. Swar now asks for the permission itself on Android, so every platform builds again.

## [1.2.1] - 2026-10-21

### Changed

- Smaller downloads: the Android app only carries phone code, debug symbols are left out of every build, the Inter font keeps only Latin, Greek and Cyrillic, and the macOS disk image and Windows installer use stronger compression.
- Lighter on memory: cover art is decoded at the size it's shown instead of full size.
- Releases attach the .apk, .exe, .dmg and .deb files directly, next to their zips, and a platform that fails to build no longer holds back the others.

### Fixed

- Android: the playing-song notification now shows when the app is minimized. Release builds were stripping its icons, so Android refused to show it.
- Android: the notification stays while paused, so phones that close background apps (MIUI, HyperOS) no longer remove it.
- Android 13 and later: Swar asks for notification permission.
- Library: Most played and Recently played showed your liked songs instead of their own.

## [1.2.0] - 2026-10-20

### Added
- Sleep timer: pause after 15, 30, 45, 60 or 90 minutes, or when the song
  ends. The moon button is in the player bar and on the Now Playing screen.
- Pick up where you left off: the app remembers the queue, the song and
  the position, and resumes from there after a restart.
- Recent searches on the Search page, with tap to search again and remove.
- Most played: a list in the Library and an "On repeat" row on Home.
- Recently played in the Library.
- A Release workflow you can run by hand from the Actions tab.
- The phone notification and lock screen controls now have like and close
  buttons next to previous, play or pause and next, plus a seek bar.

### Fixed
- Android builds from GitHub now set up background playback and the media
  notification, ask for internet access, and use the Swar app icon.
- The round play button no longer shows a dark square behind it.
- "Jump back in" on Home now updates as soon as a song starts.

### Changed
- The Jam tab is redesigned: start and join side by side, and inside a Jam
  the song, invite code, queue and people sit in aligned panels.

## [0.2.0] - 2026-10-20

### Added
- A new look of its own: saffron and plum colours, the Inter typeface with
  one type scale across the app, and Phosphor icons.
- Desktop layout for Linux, macOS, Windows and the web: a sidebar with your
  playlists, pages that open beside it, and a full-width player bar with
  seek, queue and volume.
- Discover cards with cover art on Home and Search.
- Artist pages list every song the catalog has for the artist, with Play
  and Shuffle.
- Tap the artist name in the player to open their page, or use "Go to
  artist" in any song's menu.
- "Add to Jam" in every song's menu, a Jam button in the player, and an
  "Add songs" search inside a Jam.
- Singers and playlists in search results. Singers without songs are
  hidden and the rest are sorted by how many songs they have.
- Session-aware next-song recommendations that follow what you play and
  skip.
- A download button on every song for offline listening.
- Update check: the app tells you when a newer release is out and links to
  the installer for your platform. "Check for updates" is in the Library.
- Builds for Android, macOS (unsigned), Linux (.deb and .tar.gz) and
  Windows (installer and .zip) from one Build workflow, with automatic
  releases from this changelog.

### Changed
- Deep Search songs play through the server, which falls back to another
  public upload of the same song when a video is age-restricted or
  blocked. The server image now ships the latest yt-dlp with Deno.
- Faster play and pause.
- Friendly error popups instead of raw errors.
- The app is called Swar everywhere and uses the hosted server by default.

### Removed
- The video player mode.

### Fixed
- Typing a space in search on Linux.
- The Linux .deb installs on Ubuntu and Lubuntu 24.04 and newer.
- Android builds in CI.

## [0.1.0] - 2026-10-19

### Added
- First version: Flutter app with search, queue, playlists, liked songs,
  background playback, radio and Jam listening sessions, backed by the Go
  server with catalog search, Deep Search, recommendations and offline
  download tracking.
