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

# Ubuntu 24.04+ (and Lubuntu) renamed libgtk-3-0 to libgtk-3-0t64. apt
# accepts the old name through "Provides", but graphical installers such as
# Lubuntu's Package Installer do not, so both real names are listed.
# libmpv2 is on 24.04 and newer, libmpv1 on 22.04.
INSTALLED_KB=$(du -sk "$ROOT/opt" | cut -f1)

cat > "$ROOT/DEBIAN/control" <<CONTROL
Package: swar
Version: ${VERSION}
Section: sound
Priority: optional
Architecture: amd64
Maintainer: Swar <swar@users.noreply.github.com>
Installed-Size: ${INSTALLED_KB}
Depends: libgtk-3-0t64 | libgtk-3-0, libmpv2 | libmpv1
Homepage: https://github.com/SudhanshuMatrix/Swar
Description: Swar music player
 Stream music, play radio, listen together with Jam and keep songs offline.
CONTROL

dpkg-deb --build --root-owner-group "$ROOT" "dist/swar_${VERSION}_amd64.deb"
