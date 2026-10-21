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
