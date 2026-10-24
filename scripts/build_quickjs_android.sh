#!/usr/bin/env bash
# Builds the JavaScript runtime yt-dlp needs on Android and puts it in the
# APK's native libraries (android/app/src/main/jniLibs/<abi>/libqjs.so).
#
# YouTube hides its audio formats behind JavaScript challenges; yt-dlp
# solves them with an external runtime, and without one most videos have no
# playable audio. Android has no runtime, so the app ships QuickJS-ng with a
# small command line (packaging/android/quickjs/swar_qjs.c). It is named
# lib*.so so Android installs it, executable, in the app's native library
# folder; packaging/android/YtDlpChannel.kt hands its path to yt-dlp.
#
# The engine source comes from the quickjs-ng PyPI source package (an
# unmodified copy of QuickJS-ng's engine files), pinned by version and
# SHA-256. Needs the Android NDK: ANDROID_NDK_HOME, ANDROID_NDK_ROOT,
# ANDROID_NDK_LATEST_HOME (GitHub's runners), or the newest NDK under the
# Android SDK. Without one it warns and skips (in CI it fails), and the app
# still builds; yt-dlp then plays only videos that need no JavaScript.
#
# Usage: bash scripts/build_quickjs_android.sh
set -euo pipefail
cd "$(dirname "$0")/.."

VERSION=0.17.0.2
SHA256=43d2e48272208721a7aaea8e3f02d4b9318f12ef20cc4f5421b59d9a470706ef
URL=https://files.pythonhosted.org/packages/d6/a4/611b1c2e3a505dd66cc03fb6e5963f0550a09ef2bd7c51243a9237d64db8/quickjs_ng-$VERSION.tar.gz
# Flutter's minSdk: the binary runs on every phone the app installs on.
API=24
# The CPUs the release APK is built for (android-arm, android-arm64).
ABIS="arm64-v8a:aarch64-linux-android armeabi-v7a:armv7a-linux-androideabi"

RUNNER=packaging/android/quickjs/swar_qjs.c
WORK=build/quickjs-android
OUT=android/app/src/main/jniLibs

find_ndk() {
  for d in "${ANDROID_NDK_HOME:-}" "${ANDROID_NDK_ROOT:-}" "${ANDROID_NDK_LATEST_HOME:-}"; do
    [[ -n "$d" && -d "$d/toolchains/llvm" ]] && { echo "$d"; return; }
  done
  for sdk in "${ANDROID_HOME:-}" "${ANDROID_SDK_ROOT:-}" "$HOME/Android/Sdk" "$HOME/Library/Android/sdk"; do
    [[ -n "$sdk" && -d "$sdk/ndk" ]] || continue
    local latest
    latest=$(ls -1 "$sdk/ndk" | sort -V | tail -1)
    [[ -n "$latest" && -d "$sdk/ndk/$latest/toolchains/llvm" ]] && { echo "$sdk/ndk/$latest"; return; }
  done
}

# sha256sum on Linux, shasum on macOS.
sha256() {
  if command -v sha256sum >/dev/null; then sha256sum "$1" | cut -d' ' -f1; else shasum -a 256 "$1" | cut -d' ' -f1; fi
}

NDK=$(find_ndk || true)
if [[ -z "$NDK" ]]; then
  msg="build_quickjs_android: no Android NDK found; the APK will have no JavaScript runtime for yt-dlp"
  if [[ -n "${CI:-}" ]]; then echo "error: $msg" >&2; exit 1; fi
  echo "warning: $msg" >&2
  exit 0
fi
case "$(uname -s)" in
  Linux) HOST=linux-x86_64 ;;
  Darwin) HOST=darwin-x86_64 ;; # the NDK's macOS toolchain is universal
  *) HOST=windows-x86_64 ;;
esac
BIN="$NDK/toolchains/llvm/prebuilt/$HOST/bin"

# Skip the build when nothing changed (the engine takes a while to compile).
STAMP="$VERSION $(sha256 "$RUNNER") $API $(basename "$NDK")"
up_to_date=1
for pair in $ABIS; do
  abi=${pair%%:*}
  [[ -f "$OUT/$abi/libqjs.so" && "$(cat "$WORK/$abi.stamp" 2>/dev/null)" == "$STAMP" ]] || up_to_date=0
done
if [[ $up_to_date == 1 ]]; then
  echo "build_quickjs_android: up to date"
  exit 0
fi

mkdir -p "$WORK"
TARBALL="$WORK/quickjs_ng-$VERSION.tar.gz"
if [[ ! -f "$TARBALL" || "$(sha256 "$TARBALL")" != "$SHA256" ]]; then
  curl -fsSL --retry 3 -o "$TARBALL.tmp" "$URL"
  if [[ "$(sha256 "$TARBALL.tmp")" != "$SHA256" ]]; then
    rm -f "$TARBALL.tmp"
    echo "error: quickjs-ng source does not match its pinned SHA-256" >&2
    exit 1
  fi
  mv "$TARBALL.tmp" "$TARBALL"
fi
rm -rf "$WORK/src"
mkdir -p "$WORK/src"
tar xzf "$TARBALL" -C "$WORK/src" --strip-components=1
SRC="$WORK/src/upstream-quickjs"

for pair in $ABIS; do
  abi=${pair%%:*}
  triple=${pair##*:}
  echo "build_quickjs_android: building $abi"
  mkdir -p "$OUT/$abi"
  # A position-independent executable linked only against Android's libc
  # and libm. The engine uses no thread-local storage.
  "$BIN/$triple$API-clang" -O2 -std=gnu11 -D_GNU_SOURCE -fPIE -pie -I"$SRC" \
    -o "$OUT/$abi/libqjs.so" "$RUNNER" \
    "$SRC/quickjs.c" "$SRC/dtoa.c" "$SRC/libregexp.c" "$SRC/libunicode.c" -lm
  "$BIN/llvm-strip" "$OUT/$abi/libqjs.so"
  echo "$STAMP" > "$WORK/$abi.stamp"
done
