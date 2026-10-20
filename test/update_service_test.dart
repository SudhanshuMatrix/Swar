import 'package:flutter/foundation.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sonivo/services/update_service.dart';
import 'package:sonivo/widgets/update_dialog.dart';

void main() {
  test('compares versions part by part', () {
    expect(AppVersion.parse('v0.10.0') > AppVersion.parse('0.9.9'), isTrue);
    expect(AppVersion.parse('1.2').compareTo(AppVersion.parse('1.2.0')), 0);
    expect(AppVersion.parse('1.2.3+7').compareTo(AppVersion.parse('1.2.3')), 0);
    expect(AppVersion.parse('0.2.0') > AppVersion.parse('0.2.0'), isFalse);
  });

  final release = {
    'tag_name': 'v0.3.0',
    'html_url': 'https://github.com/x/y/releases/tag/v0.3.0',
    'body': '### Added\n- **Jam** song picker',
    'assets': [
      {'name': 'app-release.apk', 'browser_download_url': 'https://dl/app.apk'},
      {'name': 'swar_0.3.0_amd64.deb', 'browser_download_url': 'https://dl/swar.deb'},
      {'name': 'Swar-Setup-0.3.0.exe', 'browser_download_url': 'https://dl/setup.exe'},
      {'name': 'Swar-macOS-0.3.0.dmg', 'browser_download_url': 'https://dl/swar.dmg'},
    ],
  };

  test('offers the installer for this platform', () {
    final current = AppVersion.parse('0.2.0');
    UpdateInfo? on(TargetPlatform p) => UpdateService.parseRelease(release, current, platform: p, web: false);
    expect(on(TargetPlatform.android)!.downloadUrl, 'https://dl/app.apk');
    expect(on(TargetPlatform.linux)!.downloadUrl, 'https://dl/swar.deb');
    expect(on(TargetPlatform.windows)!.downloadUrl, 'https://dl/setup.exe');
    expect(on(TargetPlatform.macOS)!.downloadUrl, 'https://dl/swar.dmg');
    expect(on(TargetPlatform.iOS)!.downloadUrl, release['html_url']);
  });

  test('says nothing when this build is current or newer', () {
    expect(UpdateService.parseRelease(release, AppVersion.parse('0.3.0'), web: false), isNull);
    expect(UpdateService.parseRelease(release, AppVersion.parse('1.0.0'), web: false), isNull);
  });

  test('shows release notes as plain text', () {
    expect(readableNotes('### Added\n- **Jam** song picker'), 'Added\n•  Jam song picker');
  });
}
