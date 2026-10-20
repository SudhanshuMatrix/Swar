#!/usr/bin/env bash
# Packages the Linux release bundle as a .deb for Ubuntu, Lubuntu and other
# Ubuntu/Debian flavours. Usage: bash packaging/build_deb.sh <version>
set -euo pipefail
cd "$(dirname "$0")/.."

VERSION="$1"
BUNDLE=build/linux/x64/release/bundle
ROOT=build/deb/swar_${VERSION}_amd64

rm -rf "$ROOT"
mkdir -p "$ROOT/DEBIAN" "$ROOT/opt/swar" "$ROOT/usr/bin" \
  "$ROOT/usr/share/applications" "$ROOT/usr/share/icons/hicolor/512x512/apps"
cp -r "$BUNDLE"/. "$ROOT/opt/swar/"
ln -s /opt/swar/swar "$ROOT/usr/bin/swar"
cp assets/icon/platform/linux/swar.png "$ROOT/usr/share/icons/hicolor/512x512/apps/swar.png"

cat > "$ROOT/usr/share/applications/swar.desktop" <<DESKTOP
[Desktop Entry]
Type=Application
Name=Swar
Comment=Stream and download music
Exec=/opt/swar/swar
Icon=swar
Categories=AudioVideo;Audio;Player;
Terminal=false
StartupWMClass=com.sonivo.swar
DESKTOP

cat > "$ROOT/DEBIAN/control" <<CONTROL
Package: swar
Version: ${VERSION}
Section: sound
Priority: optional
Architecture: amd64
Maintainer: Swar
Depends: libgtk-3-0, libmpv2 | libmpv1
Description: Swar music player
 Stream music, play radio, listen together with Jam and keep songs offline.
CONTROL

dpkg-deb --build --root-owner-group "$ROOT" "dist/swar_${VERSION}_amd64.deb"
