import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:hive/hive.dart';
import 'package:http/http.dart' as http;
import 'package:package_info_plus/package_info_plus.dart';

/// Where releases are published. Each release is made by the Build workflow
/// when the version in pubspec.yaml changes (see CHANGELOG.md).
const releaseRepo = 'SudhanshuMatrix/Swar';

/// A dotted version such as 0.2.0, compared part by part. A leading "v" and
/// anything after "+" or "-" are ignored.
class AppVersion implements Comparable<AppVersion> {
  final List<int> parts;
  const AppVersion(this.parts);

  factory AppVersion.parse(String raw) {
    var v = raw.trim();
    if (v.startsWith('v') || v.startsWith('V')) v = v.substring(1);
    v = v.split(RegExp(r'[+\-\s]')).first;
    final parts = v.split('.').map((p) => int.tryParse(p) ?? 0).toList();
    while (parts.length < 3) {
      parts.add(0);
    }
    return AppVersion(parts);
  }

  @override
  int compareTo(AppVersion other) {
    final n = parts.length > other.parts.length ? parts.length : other.parts.length;
    for (var i = 0; i < n; i++) {
      final a = i < parts.length ? parts[i] : 0;
      final b = i < other.parts.length ? other.parts[i] : 0;
      if (a != b) return a.compareTo(b);
    }
    return 0;
  }

  bool operator >(AppVersion other) => compareTo(other) > 0;

  @override
  String toString() => parts.join('.');
}

/// A newer release than the one running.
class UpdateInfo {
  final AppVersion version;
  final AppVersion current;
  final String notes;
  final String pageUrl;

  /// The installer for this platform, or the release page when there is none.
  final String downloadUrl;

  const UpdateInfo({
    required this.version,
    required this.current,
    required this.notes,
    required this.pageUrl,
    required this.downloadUrl,
  });
}

/// The release file that suits this platform, if the release has one.
String? pickAsset(List<({String name, String url})> assets, TargetPlatform platform, {bool web = false}) {
  if (web) return null;
  // Most wanted first: the Windows installer beats the zip.
  final List<bool Function(String)> wanted = switch (platform) {
    TargetPlatform.android => [(n) => n.endsWith('.apk')],
    TargetPlatform.windows => [(n) => n.endsWith('.exe'), (n) => n.contains('windows') && n.endsWith('.zip')],
    TargetPlatform.macOS => [(n) => n.endsWith('.dmg')],
    TargetPlatform.linux => [(n) => n.endsWith('.deb'), (n) => n.contains('linux') && n.endsWith('.tar.gz')],
    _ => const [],
  };
  for (final match in wanted) {
    for (final a in assets) {
      if (match(a.name.toLowerCase())) return a.url;
    }
  }
  return null;
}

/// Asks GitHub for the latest release and says whether it is newer than
/// this build. Failures are silent: an update check must never get in the
/// listener's way.
class UpdateService {
  static const _box = 'swar_settings';

  final http.Client _client;
  UpdateService({http.Client? client}) : _client = client ?? http.Client();

  static Future<AppVersion> currentVersion() async {
    final info = await PackageInfo.fromPlatform();
    return AppVersion.parse(info.version);
  }

  /// The newer release, or null when this build is the latest. Throws when
  /// GitHub can't be reached, so a manual check can say so.
  Future<UpdateInfo?> check() async {
    final current = await currentVersion();
    final res = await _client.get(
      Uri.parse('https://api.github.com/repos/$releaseRepo/releases/latest'),
      headers: {'Accept': 'application/vnd.github+json'},
    );
    if (res.statusCode == 404) return null; // nothing released yet
    if (res.statusCode != 200) throw Exception('GitHub answered ${res.statusCode}');
    return parseRelease(jsonDecode(res.body) as Map<String, dynamic>, current);
  }

  static UpdateInfo? parseRelease(Map<String, dynamic> json, AppVersion current,
      {TargetPlatform? platform, bool web = kIsWeb}) {
    final tag = json['tag_name'] as String? ?? '';
    if (tag.isEmpty || json['draft'] == true || json['prerelease'] == true) return null;
    final latest = AppVersion.parse(tag);
    if (!(latest > current)) return null;
    final page = json['html_url'] as String? ?? 'https://github.com/$releaseRepo/releases/latest';
    final assets = ((json['assets'] as List?) ?? const [])
        .whereType<Map>()
        .map((a) => (name: '${a['name']}', url: '${a['browser_download_url']}'))
        .toList();
    return UpdateInfo(
      version: latest,
      current: current,
      notes: (json['body'] as String? ?? '').trim(),
      pageUrl: page,
      downloadUrl: pickAsset(assets, platform ?? defaultTargetPlatform, web: web) ?? page,
    );
  }

  /// Whether the listener chose "Skip this version" for [v].
  static Future<bool> isSkipped(AppVersion v) async {
    final box = await Hive.openBox(_box);
    return box.get('skippedVersion') == v.toString();
  }

  static Future<void> skip(AppVersion v) async {
    final box = await Hive.openBox(_box);
    await box.put('skippedVersion', v.toString());
  }
}
