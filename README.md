# Sonivo 🎵

A free, cross-platform Spotify-like music and music-video player built with **Flutter**, powered by a high-performance **Go backend** that leverages `yt-dlp`.

Sonivo streams audio and video directly from CDN endpoints, giving you ad-free music playback with background audio support, offline playlist storage, and the ability to seamlessly switch between Audio and Video mode in real time.

---

## ✨ Features

- **🎧 Audio & Video Dual Mode**: Seamless YT-Music style segmented toggle. Audio is the single source of truth; switching to Video mode overlays a position-synced visual video layer without interrupting playback or buffering.
- **📱 Background Playback & Lock Screen Controls**: Powered by `just_audio` and `audio_service`, audio keeps playing when your phone screen turns off or the app is minimized, with OS-level lock screen notifications and headphone controls (Play/Pause/Skip).
- **🔍 Smart Search & YouTube Link Resolution**: Search by song name, artist, album, or directly paste YouTube URLs (`youtube.com/watch?v=...`, `youtu.be/...`) and 11-character video IDs.
- **🏷️ Browse Categories**: One-tap explore cards for Top Hits, Pop, Rock, Hip-Hop, Lofi & Chill, Indie, Electronic, Acoustic, Jazz & Blues, Workout, Focus, and Gaming.
- **📜 Full Play Queue Management**: Interactive queue sheet showing the currently playing track and upcoming queue. Jump to any track, remove items, clear upcoming songs, or reorder the queue.
- **📻 Radio & Autoplay Similar Tracks**: Continuous playback — automatically resolves and queues related songs when your playlist finishes.
- **💾 Offline Library & Playlists**: Create, rename, and manage playlists, liked songs, and recently played tracks saved locally with `Hive` (supported across Web IndexedDB, Desktop, and Mobile).
- **🎨 Spotify-Inspired Dark Theme**: Sleek Material 3 design with vibrant Spotify Green accent highlights, glowing cards, and a persistent mini-player.

---

## 🏗️ Architecture

```
┌────────────────────────────────────────────────────────┐
│                   Flutter Client                       │
│  (Android / iOS / Windows / macOS / Linux / Web)       │
│                                                        │
│  • Provider State Management (Player & Library)        │
│  • just_audio + audio_service (Audio Engine)           │
│  • video_player (Muted Visual Sync Layer)              │
│  • Hive Local Storage (Playlists, Liked, Recents)      │
└───────────────▲────────────────────────▲───────────────┘
                │                        │
       1. Resolve Stream URLs      3. Direct CDN Stream
                │                  (Audio: m4a / Video: mp4)
                ▼                        │
┌──────────────────────────────┐         │
│          Go Backend          │         │
│  • /search                   │         │
│  • /track/{id}               │         │
│  • /video/{id}               │         │
│  • /related/{id}             │         │
│  • Auto yt-dlp discovery     │         │
│  • In-memory TTL Cache       │         │
└───────────────┬──────────────┘         │
                │                        │
       2. Resolve Metadata               │
                ▼                        │
┌──────────────────────────────┐         │
│            yt-dlp            │─────────┘
└──────────────────────────────┘
```

> **Why a Go backend?**  
> `yt-dlp` cannot run inside browser sandboxes (Flutter Web) or mobile app sandboxes (iOS/Android). The lightweight Go service handles searching and URL resolution by shelling out to `yt-dlp`, returning CDN stream URLs directly to Flutter. The backend never proxies media bytes.

---

## 🚀 Quick Start

### 1. Prerequisites

- **Go**: 1.24+ ([Download Go](https://go.dev/dl/))
- **Flutter**: 3.19+ ([Download Flutter](https://docs.flutter.dev/get-started/install))
- **Python 3 & yt-dlp** (only for Deep Search):
  ```bash
  pip install -U yt-dlp
  ```
- **FFmpeg** (Recommended for audio/video format processing)

---

### 2. Run the Backend

From the repository root:

```bash
# Using Makefile
make backend

# Or directly with Go:
cd server
go run ./cmd/server
```

The backend is the Swar Go server in `server/` (catalog from Saavn, Deep Search through yt-dlp, radio, library, offline downloads and Swar Jam). It starts at `http://127.0.0.1:8765`.

Song playback needs `SAAVN_MEDIA_URLS_ENABLED=true`; it is off by default because turning it on is the server operator's decision about streaming rights. See `server/docs/DECISIONS.md` and `server/.env.example`.

You can test that the backend is running by opening:
`http://127.0.0.1:8765/healthz` or `http://127.0.0.1:8765/search?q=kesariya`

Full API: `server/docs/API.md`. Moving from the old backend: `server/docs/MIGRATION.md`.

---

### 3. Setup & Run the Flutter App

First time project setup (generates native platform directories and fetches packages):

```bash
# Using Makefile
make setup

# Or manually:
flutter create --platforms=android,ios,linux,web,windows --org com.sonivo --project-name sonivo .
flutter pub get
```

#### Run on Desktop / Web / Mobile:

```bash
# Windows
make windows
# or: flutter run -d windows

# Web (Chrome)
make web
# or: flutter run -d chrome

# Android Emulator (uses 10.0.2.2 alias for host localhost)
flutter run -d android --dart-define=API_BASE_URL=http://10.0.2.2:8765

# Physical Device on local Wi-Fi:
flutter run -d <device-id> --dart-define=API_BASE_URL=http://<YOUR_LAN_IP>:8765
```

---

## 📁 Project Structure

```
Sonivo/
├── backend/
│   ├── go.mod                 # Go module definition
│   └── main.go                # HTTP API wrapping yt-dlp (/search, /track, /video, /related)
├── lib/
│   ├── core/
│   │   ├── constants.dart     # API base URL configuration
│   │   └── theme.dart         # Spotify dark theme palette & styling
│   ├── models/
│   │   ├── playlist.dart      # Playlist data model
│   │   └── track.dart         # Track data model with serialization
│   ├── screens/
│   │   ├── home_shell.dart    # Main navigation shell & Home tab
│   │   ├── search_screen.dart # Search screen with genre cards & YouTube link parser
│   │   ├── library_screen.dart# User playlists & Liked songs
│   │   ├── playlist_screen.dart# Playlist detail view with shuffle & reorder
│   │   └── now_playing_screen.dart # Full-screen player with Queue & Video toggle
│   ├── services/
│   │   ├── api_service.dart   # Client HTTP service communicating with Go backend
│   │   ├── audio_handler.dart # just_audio + audio_service background handler
│   │   └── library_service.dart# Hive local database for persistent library
│   ├── state/
│   │   ├── library_provider.dart # ChangeNotifier for playlists & liked songs
│   │   └── player_provider.dart  # Core playback engine, queue, & video sync
│   ├── widgets/
│   │   ├── audio_video_switch.dart # Animated tab switch between audio and video
│   │   ├── mini_player.dart   # Persistent bottom mini player bar
│   │   └── track_tile.dart    # Reusable track list tile with active highlight
│   ├── app.dart               # Top-level MaterialApp
│   └── main.dart              # App bootstrap & audio service initialization
├── Makefile                   # Automation commands for build, test, and run
├── pubspec.yaml               # Flutter dependencies
└── analysis_options.yaml      # Dart linter configurations
```

---

## 🛠️ API Reference (Backend)

| Method | Endpoint | Description |
|---|---|---|
| `GET` | `/health` | Health check endpoint returning `{"status": "ok"}` |
| `GET` | `/search?q={query}&limit={n}` | Searches YouTube or resolves direct YouTube URLs |
| `GET` | `/track/{id}` | Resolves audio stream URL (`m4a` / `webm`) and metadata |
| `GET` | `/video/{id}` | Resolves both video-only (`mp4`) and real audio stream URLs |
| `GET` | `/related/{id}` | Resolves similar recommended tracks for Radio autoplay |

---

## 📦 Builds (GitHub Actions)

`.github/workflows/build.yml` runs on every push to `master` and starts one small workflow per platform. Each can also be run on its own from the **Actions** tab. Download the files from the run page.

| Workflow | Files |
| --- | --- |
| `android.yml` | `Swar-Android.apk` |
| `macos.yml` | `Swar-macOS.dmg` (unsigned) |
| `linux.yml` | `swar_<version>_amd64.deb` and `swar-linux-x64-<version>.tar.gz` for Ubuntu / Lubuntu 22.04 and newer |
| `windows.yml` | `Swar-Setup-<version>.exe` installer and a portable `.zip` |

Shared setup (Flutter, `.env`, platform folders) lives in `.github/actions/setup`. Pushing a tag such as `v0.2.0` also publishes every file as a GitHub Release.

- **macOS:** the app is unsigned. The first time, right-click it and choose **Open** (or System Settings → Privacy & Security → **Open Anyway**).
- **Windows:** on the "Windows protected your PC" screen, click **More info** → **Run anyway**.
- **Linux:** `sudo apt install ./swar_<version>_amd64.deb` also installs libmpv, which plays the audio.

## ⚖️ Legal & Disclaimer

For personal, educational, and non-commercial research use only. Streaming media from third-party platforms may be subject to their respective Terms of Service. You are solely responsible for how you use this software.
