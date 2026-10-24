import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:crypto/crypto.dart';
import 'package:http/http.dart' as http;

/// Downloads, verifies, installs and updates yt-dlp. Swar ships without it:
/// the first launch downloads it, and it is updated in the background.
///
/// Desktop (Linux, Windows, macOS) gets the official standalone binary for
/// its OS and CPU. Android gets the official `yt-dlp` zipapp, which runs on
/// the Python bundled with the youtubedl-android library.
///
/// Every download is checked against the release's SHA2-256SUMS before it
/// is run at all; then `--version` is run as a smoke test. A good download
/// becomes current by an atomic rename of `current.json`. The version it
/// replaced is kept until the new one has completed one real run
/// ([confirmWorking]), so a bad update can still be rolled back, and is
/// then deleted. Partial downloads left by a crash are removed by
/// [cleanUp]. The installed file is hashed again before its first use in
/// each session; a file that no longer matches is not run.
///
/// Limitation: the checksum list comes from the same GitHub release over
/// HTTPS. Its GPG signature (SHA2-256SUMS.sig) is not checked, because that
/// needs an OpenPGP library Swar does not otherwise carry.
class YtDlpInstaller {
  static const defaultReleases = 'https://github.com/yt-dlp/yt-dlp/releases';
  static const _maxBinaryBytes = 150 << 20;
  static const _maxSumsBytes = 64 << 10;

  final Directory dir;
  final String releasesUrl;
  final http.Client _http;

  /// Runs the installed yt-dlp with --version and returns the version, or
  /// null when it does not run. Swappable for tests and for Android.
  final Future<String?> Function(String path) smokeTest;

  /// Puts a verified file where the runtime runs it from (Android copies it
  /// into the library's folder). Desktop runs the file in place.
  final Future<void> Function(String path, String sha256) deploy;

  /// The OS name the asset is chosen for ("linux", "windows", "macos",
  /// "android"); defaults to this one.
  final String os;

  YtDlpInstaller({
    required this.dir,
    this.releasesUrl = defaultReleases,
    http.Client? client,
    Future<String?> Function(String path)? smokeTest,
    Future<void> Function(String path, String sha256)? deploy,
    String? os,
  })  : _http = client ?? http.Client(),
        smokeTest = smokeTest ?? runVersion,
        deploy = deploy ?? _noDeploy,
        os = os ?? Platform.operatingSystem;

  static Future<void> _noDeploy(String path, String sha256) async {}

  File get _state => File('${dir.path}${Platform.pathSeparator}current.json');
  Directory get _bin => Directory('${dir.path}${Platform.pathSeparator}bin');

  String? _verifiedPath;

  /// The release file for this computer, or null where there is none.
  static String? assetName({String? os, String? arch}) {
    os ??= Platform.operatingSystem;
    arch ??= currentArch();
    switch (os) {
      case 'linux':
        return switch (arch) {
          'x64' => 'yt-dlp_linux',
          'arm64' => 'yt-dlp_linux_aarch64',
          _ => null,
        };
      case 'windows':
        return switch (arch) {
          'x64' => 'yt-dlp.exe',
          'arm64' => 'yt-dlp_arm64.exe',
          'ia32' => 'yt-dlp_x86.exe',
          _ => null,
        };
      case 'macos':
        return 'yt-dlp_macos';
      case 'android':
        // The platform-independent zipapp; the library's Python runs it.
        return 'yt-dlp';
    }
    return null;
  }

  /// CPU architecture from the Dart VM's version string ("... on
  /// "linux_x64""), which needs no dart:ffi (so web builds still compile).
  static String currentArch() {
    final m = RegExp(r'on "[a-z]+_([a-z0-9]+)"').firstMatch(Platform.version);
    return m?.group(1) ?? '';
  }

  /// Reads "<sha256>  <file name>" lines.
  static Map<String, String> parseSums(String text) {
    final out = <String, String>{};
    for (final line in const LineSplitter().convert(text)) {
      final m = RegExp(r'^([0-9a-fA-F]{64})\s+\*?(\S+)\s*$').firstMatch(line.trim());
      if (m != null) out[m.group(2)!] = m.group(1)!.toLowerCase();
    }
    return out;
  }

  Future<Map<String, dynamic>?> _readState() async {
    try {
      return (jsonDecode(await _state.readAsString()) as Map).cast<String, dynamic>();
    } catch (_) {
      return null;
    }
  }

  Future<void> _writeState(Map<String, dynamic> s) async {
    final tmp = File('${_state.path}.tmp');
    await tmp.writeAsString(jsonEncode(s), flush: true);
    await tmp.rename(_state.path); // atomic on the same volume
  }

  /// The installed file, if its record and file exist. Cheap: no hashing
  /// (use [verifiedPath] before running it).
  Future<String?> installedPath() async {
    final path = (await _readState())?['path'] as String?;
    return path != null && File(path).existsSync() ? path : null;
  }

  /// The installed file's SHA-256, or null.
  Future<String?> installedSha() async => (await _readState())?['sha256'] as String?;

  /// The installed version, or null.
  Future<String?> installedVersion() async => (await _readState())?['version'] as String?;

  /// When the last update check succeeded (whether or not it found a newer
  /// version), or null when none ever has.
  Future<DateTime?> lastSuccessfulCheck() async {
    final v = (await _readState())?['checkedAt'] as String?;
    return v == null ? null : DateTime.tryParse(v);
  }

  Future<void> _markChecked(Map<String, dynamic> state) async {
    state['checkedAt'] = DateTime.now().toIso8601String();
    await _writeState(state);
  }

  /// Removes partial downloads and binaries no longer referenced (left by a
  /// crash or an interrupted update).
  Future<void> cleanUp() async {
    final s = await _readState();
    final prev = (s?['previous'] as Map?)?['path'];
    await _prune(keep: {if (s?['path'] is String) s!['path'] as String, if (prev is String) prev});
    await _delete(File('${_state.path}.tmp'));
  }

  /// Called after the current version finished a real run: the version it
  /// replaced is no longer needed for a rollback, so it is deleted.
  Future<void> confirmWorking() async {
    final s = await _readState();
    if (s == null || s['previous'] == null) return;
    s.remove('previous');
    await _writeState(s);
    await _prune(keep: {s['path'] as String});
  }

  /// The verified binary to run, or null when none is installed or the file
  /// no longer matches its recorded checksum.
  Future<String?> verifiedPath() async {
    if (_verifiedPath != null && File(_verifiedPath!).existsSync()) return _verifiedPath;
    final s = await _readState();
    final path = s?['path'] as String?;
    final sha = s?['sha256'] as String?;
    if (path == null || sha == null || !File(path).existsSync()) return null;
    if (await sha256OfFile(File(path)) != sha) return null;
    return _verifiedPath = path;
  }

  /// Downloads, verifies and switches to the latest release. Returns the
  /// version now installed. Does nothing (beyond reading the small checksum
  /// list) when the installed binary is already the latest.
  ///
  /// [onProgress] hears the download's progress from 0 to 1 (or null when
  /// the size is unknown).
  Future<String> installLatest({void Function(double? progress)? onProgress}) async {
    final asset = assetName(os: os);
    if (asset == null) throw const YtDlpInstallException('yt-dlp has no release for this computer.');
    await dir.create(recursive: true);
    await _bin.create(recursive: true);

    final sums = parseSums(await _getText('$releasesUrl/latest/download/SHA2-256SUMS'));
    final want = sums[asset];
    if (want == null) throw YtDlpInstallException('The yt-dlp release has no checksum for $asset.');
    final state = await _readState();
    if (state?['sha256'] == want && await verifiedPath() != null) {
      await deploy(state!['path'] as String, want);
      await _markChecked(state);
      return state['version'] as String;
    }

    final ext = os == 'windows' ? '.exe' : '';
    final tmp = File('${_bin.path}${Platform.pathSeparator}download.tmp');
    final got = await _download('$releasesUrl/latest/download/$asset', tmp, onProgress);
    if (got != want) {
      await _delete(tmp);
      throw const YtDlpInstallException('The downloaded yt-dlp did not match its checksum and was deleted.');
    }
    final dest = File('${_bin.path}${Platform.pathSeparator}yt-dlp-${want.substring(0, 12)}$ext');
    await _delete(dest);
    await tmp.rename(dest.path);
    if (os == 'linux' || os == 'macos') {
      final r = await Process.run('chmod', ['755', dest.path]);
      if (r.exitCode != 0) throw const YtDlpInstallException('Could not make yt-dlp executable.');
    }
    // Only a file whose checksum matched is ever run, and this is its
    // first run.
    await deploy(dest.path, want);
    final version = await smokeTest(dest.path);
    if (version == null) {
      await _delete(dest);
      // Put the working version back where the runtime looks for it.
      final oldPath = state?['path'] as String?;
      final oldSha = state?['sha256'] as String?;
      if (oldPath != null && oldSha != null && File(oldPath).existsSync()) {
        try {
          await deploy(oldPath, oldSha);
        } catch (_) {}
      }
      throw const YtDlpInstallException('The new yt-dlp did not start; the previous version is kept.');
    }
    final next = <String, dynamic>{
      'path': dest.path,
      'sha256': want,
      'version': version,
      'installedAt': DateTime.now().toIso8601String(),
      if (state != null && state['path'] != null && state['path'] != dest.path)
        'previous': {'path': state['path'], 'sha256': state['sha256'], 'version': state['version']},
    };
    await _markChecked(next);
    _verifiedPath = dest.path;
    // Anything older than the version just replaced goes now; that one goes
    // after the new version's first real run.
    await _prune(keep: {dest.path, if (state?['path'] is String) state!['path'] as String});
    return version;
  }

  /// Switches back to the version before the last update. Returns false
  /// when there is none or its file no longer matches its checksum.
  Future<bool> rollback() async {
    final s = await _readState();
    final prev = (s?['previous'] as Map?)?.cast<String, dynamic>();
    final path = prev?['path'] as String?;
    if (prev == null || path == null || !File(path).existsSync()) return false;
    if (await sha256OfFile(File(path)) != prev['sha256']) return false;
    await deploy(path, prev['sha256'] as String);
    await _writeState({
      ...prev,
      'installedAt': DateTime.now().toIso8601String(),
      'previous': {'path': s!['path'], 'sha256': s['sha256'], 'version': s['version']},
    });
    _verifiedPath = path;
    return true;
  }

  Future<String> _getText(String url) async {
    final res = await _http.get(Uri.parse(url)).timeout(const Duration(seconds: 30));
    if (res.statusCode != 200) throw YtDlpInstallException('Could not reach the yt-dlp release (${res.statusCode}).');
    if (res.bodyBytes.length > _maxSumsBytes) throw const YtDlpInstallException('Unexpected checksum file.');
    return utf8.decode(res.bodyBytes, allowMalformed: true);
  }

  /// Streams [url] to [to], hashing as it goes. Returns the hex SHA-256.
  Future<String> _download(String url, File to, void Function(double?)? onProgress) async {
    final req = http.Request('GET', Uri.parse(url));
    final res = await _http.send(req).timeout(const Duration(seconds: 30));
    if (res.statusCode != 200) {
      throw YtDlpInstallException('Could not download yt-dlp (${res.statusCode}).');
    }
    final sink = to.openWrite();
    final hashOut = _DigestSink();
    final hasher = sha256.startChunkedConversion(hashOut);
    var total = 0;
    final size = res.contentLength;
    onProgress?.call(size == null || size <= 0 ? null : 0);
    try {
      await for (final chunk in res.stream.timeout(const Duration(seconds: 60))) {
        total += chunk.length;
        if (total > _maxBinaryBytes) throw const YtDlpInstallException('The yt-dlp download was unexpectedly large.');
        hasher.add(chunk);
        sink.add(chunk);
        if (size != null && size > 0) onProgress?.call((total / size).clamp(0, 1).toDouble());
      }
    } catch (_) {
      await sink.close();
      await _delete(to);
      rethrow;
    }
    await sink.flush();
    await sink.close();
    hasher.close();
    return hashOut.value.toString();
  }

  Future<void> _prune({required Set<String> keep}) async {
    try {
      await for (final f in _bin.list()) {
        if (f is File && !keep.contains(f.path)) await _delete(f);
      }
    } catch (_) {}
  }

  static Future<void> _delete(File f) async {
    try {
      if (f.existsSync()) await f.delete();
    } catch (_) {}
  }
}

class YtDlpInstallException implements Exception {
  final String message;
  const YtDlpInstallException(this.message);
  @override
  String toString() => message;
}

/// SHA-256 of a file, read in chunks.
Future<String> sha256OfFile(File f) async {
  final out = _DigestSink();
  final hasher = sha256.startChunkedConversion(out);
  await for (final chunk in f.openRead()) {
    hasher.add(chunk);
  }
  hasher.close();
  return out.value.toString();
}

/// `<binary> --ignore-config --version`, or null when it does not run.
Future<String?> runVersion(String path) async {
  try {
    final r = await Process.run(path, ['--ignore-config', '--version'], runInShell: false)
        .timeout(const Duration(seconds: 30));
    if (r.exitCode != 0) return null;
    final v = (r.stdout as String).trim().split('\n').first.trim();
    return RegExp(r'^[0-9][0-9A-Za-z.+-]{3,40}$').hasMatch(v) ? v : null;
  } catch (_) {
    return null;
  }
}

class _DigestSink implements Sink<Digest> {
  late Digest value;
  @override
  void add(Digest data) => value = data;
  @override
  void close() {}
}
