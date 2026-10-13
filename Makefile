# Sonivo — Flutter client (android/ios/linux/web/windows) + Go backend (yt-dlp resolver)
#
# First time:
#   make setup
# Then, to run on a given platform:
#   make android | make ios | make web | make linux | make windows
# To build a release artifact:
#   make build-android | make build-ios | make build-web | make build-linux | make build-windows
# The backend must be running for search/playback to work:
#   make backend

FLUTTER ?= flutter
GO ?= go
BACKEND_DIR := backend
BACKEND_BIN := bin/sonivo-backend

.PHONY: setup android ios web linux windows \
        build-android build-appbundle build-ios build-web build-linux build-windows \
        backend backend-build clean doctor check-yt-dlp

## One-time project setup: generates native platform folders, fetches Dart
## packages. Run this again any time after editing pubspec.yaml.
setup: check-yt-dlp
	$(FLUTTER) create --platforms=android,ios,linux,web,windows --org com.sonivo --project-name sonivo .
	$(FLUTTER) pub get
	cd $(BACKEND_DIR) && $(GO) mod tidy

check-yt-dlp:
	@command -v yt-dlp >/dev/null 2>&1 || echo "WARNING: yt-dlp not found on PATH. Install it with 'pip install -U yt-dlp' or see https://github.com/yt-dlp/yt-dlp#installation"

# ---- Run (debug, attached to a connected device/emulator/browser) ----

android:
	$(FLUTTER) run -d android

ios:
	$(FLUTTER) run -d ios

web:
	$(FLUTTER) run -d chrome

linux:
	$(FLUTTER) run -d linux

windows:
	$(FLUTTER) run -d windows

# ---- Build (release artifacts) ----

build-android:
	$(FLUTTER) build apk --release

build-appbundle:
	$(FLUTTER) build appbundle --release

build-ios:
	$(FLUTTER) build ios --release

build-web:
	$(FLUTTER) build web --release

build-linux:
	$(FLUTTER) build linux --release

build-windows:
	$(FLUTTER) build windows --release

# ---- Backend (Go, wraps the yt-dlp CLI) ----

backend: check-yt-dlp
	cd $(BACKEND_DIR) && $(GO) run .

backend-build:
	cd $(BACKEND_DIR) && $(GO) build -o ../$(BACKEND_BIN).exe .

# ---- Misc ----

clean:
	$(FLUTTER) clean
	rm -rf bin

doctor:
	$(FLUTTER) doctor -v
	@echo "---"
	$(GO) version
	@command -v yt-dlp >/dev/null 2>&1 && yt-dlp --version || echo "yt-dlp: not found on PATH"
