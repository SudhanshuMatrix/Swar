#!/usr/bin/env bash
# Generates the platform folders (they are not kept in git) and brands them
# as Swar: app name, window title, icons, and macOS network access.
#
# Usage: bash scripts/prepare_platforms.sh macos,windows,linux,web
# Existing files are kept by `flutter create`; the edits below are idempotent.
set -euo pipefail
cd "$(dirname "$0")/.."

PLATFORMS="${1:-android,ios,linux,macos,web,windows}"
ICONS=assets/icon/platform

flutter create --platforms="$PLATFORMS" --org com.sonivo --project-name sonivo . >/dev/null

has() { [[ ",$PLATFORMS," == *",$1,"* ]]; }
sub() { perl -pi -e "$1" "$2"; }

if has android; then
  M=android/app/src/main/AndroidManifest.xml
  sub 's/android:label="sonivo"/android:label="Swar"/' "$M"
  # Network (release builds get no INTERNET permission by default), then
  # background playback and the media notification (audio_service).
  # keep.xml (copied with the icons below) stops release builds stripping
  # the notification icons.
  for p in INTERNET WAKE_LOCK FOREGROUND_SERVICE FOREGROUND_SERVICE_MEDIA_PLAYBACK POST_NOTIFICATIONS; do
    grep -q "permission.$p\"" "$M" || \
      sub "s|(<manifest[^>]*>)|\$1\n    <uses-permission android:name=\"android.permission.$p\"/>|" "$M"
  done
  grep -q 'audioservice.AudioService"' "$M" || sub 's|(\s*)</application>|$1    <service android:name="com.ryanheise.audioservice.AudioService"\n            android:foregroundServiceType="mediaPlayback"\n            android:exported="true">\n            <intent-filter>\n                <action android:name="android.media.browse.MediaBrowserService" />\n            </intent-filter>\n        </service>\n        <receiver android:name="com.ryanheise.audioservice.MediaButtonReceiver"\n            android:exported="true">\n            <intent-filter>\n                <action android:name="android.intent.action.MEDIA_BUTTON" />\n            </intent-filter>\n        </receiver>\n$1</application>|' "$M"
  # Our MainActivity: shares Flutter with audio_service (the notification
  # needs it) and asks for notification permission on Android 13 and later.
  rm -f android/app/src/main/java/com/sonivo/sonivo/MainActivity.java
  mkdir -p android/app/src/main/kotlin/com/sonivo/sonivo
  cp packaging/android/MainActivity.kt android/app/src/main/kotlin/com/sonivo/sonivo/MainActivity.kt
  # App icon and the notification's icons.
  cp -R "$ICONS"/android/res/. android/app/src/main/res/
fi

if has macos; then
  sub 's/^PRODUCT_NAME = .*/PRODUCT_NAME = Swar/' macos/Runner/Configs/AppInfo.xcconfig
  sub 's/^PRODUCT_COPYRIGHT = .*/PRODUCT_COPYRIGHT = Copyright © 2026 Swar. All rights reserved./' macos/Runner/Configs/AppInfo.xcconfig
  # The macOS sandbox blocks network access unless the app asks for it.
  for f in macos/Runner/DebugProfile.entitlements macos/Runner/Release.entitlements; do
    grep -q 'network.client' "$f" || \
      sub 's|</dict>|\t<key>com.apple.security.network.client</key>\n\t<true/>\n</dict>|' "$f"
  done
  cp "$ICONS"/macos/app_icon_*.png macos/Runner/Assets.xcassets/AppIcon.appiconset/
fi

if has windows; then
  sub 's/set\(BINARY_NAME "sonivo"\)/set(BINARY_NAME "Swar")/' windows/CMakeLists.txt
  sub 's/window\.Create\(L"sonivo"/window.Create(L"Swar"/; s/Win32Window::Size size\(1280, 720\)/Win32Window::Size size(1280, 800)/' windows/runner/main.cpp
  sub 's/"(FileDescription|InternalName|ProductName)", "sonivo"/"$1", "Swar"/; s/"sonivo\.exe"/"Swar.exe"/; s/"com\.sonivo"/"Swar"/' windows/runner/Runner.rc
  cp "$ICONS"/windows/app_icon.ico windows/runner/resources/app_icon.ico
fi

if has linux; then
  sub 's/set\(BINARY_NAME "sonivo"\)/set(BINARY_NAME "swar")/' linux/CMakeLists.txt
  sub 's/set\(APPLICATION_ID "com\.sonivo\.sonivo"\)/set(APPLICATION_ID "com.sonivo.swar")/' linux/CMakeLists.txt
  sub 's/_set_title\((header_bar|window), "sonivo"\)/_set_title($1, "Swar")/' linux/runner/my_application.cc
  sub 's/gtk_window_set_default_size\(window, 1280, 720\)/gtk_window_set_default_size(window, 1280, 800)/' linux/runner/my_application.cc
fi

if has web; then
  sub 's/content="sonivo"/content="Swar"/; s|<title>sonivo</title>|<title>Swar</title>|' web/index.html
  sub 's/"(name|short_name)": "sonivo"/"$1": "Swar"/; s/"(background_color|theme_color)": "#[0-9A-Fa-f]+"/"$1": "#121212"/' web/manifest.json
  cp "$ICONS"/web/favicon.png web/favicon.png
  cp "$ICONS"/web/icons/*.png web/icons/
fi
