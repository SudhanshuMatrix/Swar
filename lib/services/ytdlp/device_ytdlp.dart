import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:math' as math;

import 'package:flutter/foundation.dart';
import 'package:path_provider/path_provider.dart';

import '../../models/track.dart';
import '../app_settings.dart';
import '../ttl_cache.dart';
import 'ytdlp_input.dart';
import 'ytdlp_installer.dart';
import 'ytdlp_parse.dart';
import 'ytdlp_runner.dart';

/// Where the yt-dlp setup is, for the onboarding page and the download
/// popup.
enum YtDlpPhase { idle, downloading, ready, failed, unsupported }

class YtDlpSetup {
  final YtDlpPhase phase;

  /// Download progress 0..1, or null when the size is unknown.
  final double? progress;
  final String? error;
  const YtDlpSetup(this.phase, {this.progress, this.error});
}

/// yt-dlp on this device: Linux, Windows and macOS run the verified binary;
/// Android runs the verified zipapp on youtubedl-android's Python. YouTube
/// then answers this device, not Swar's server, which YouTube often blocks.
///
/// Swar ships without yt-dlp. The first launch downloads it
/// ([AppSettings.ytdlpOnboarded]); afterwards it is checked for updates in
/// the background about once a day. When updates have kept failing for
/// [forceAfter], [mustDownloadNow] turns true and the app shows a popup
/// that stays until the download finishes.
class DeviceYtDlp extends ChangeNotifier {
  DeviceYtDlp._();
  static final DeviceYtDlp instance = DeviceYtDlp._();

  static const updateEvery = Duration(hours: 24);
  static const forceAfter = Duration(hours: 48);
  static const _searchTimeout = Duration(seconds: 30);
  static const _resolveTimeout = Duration(seconds: 40);

  static bool get supported =>
      !kIsWeb && (Platform.isAndroid || Platform.isLinux || Platform.isWindows || Platform.isMacOS);

  YtDlpInstaller? _installer;
  YtDlpRunner? _runner;
  // Python on a phone is slow and heavy: one run at a time there.
  final Slots _slots = Slots(!kIsWeb && Platform.isAndroid ? 1 : 2);
  final _inflight = <String, Future<Object>>{};
  List<List<String>> _jsRuntime = const [];

  bool _ready = false;
  bool _installed = false;
  String? _version;
  DateTime? _lastCheck;
  bool _confirmPending = false;
  int _failures = 0;
  DateTime? _nextTry;
  Timer? _timer;
  Future<bool>? _installing;

  final setup = ValueNotifier(const YtDlpSetup(YtDlpPhase.idle));

  bool get installed => _installed;
  String? get version => _version;
  DateTime? get lastCheck => _lastCheck;

  /// First launch: yt-dlp has not been set up or skipped yet.
  bool get needsOnboarding => supported && _ready && !AppSettings.instance.ytdlpOnboarded;

  /// Updates (or the first download) have failed for [forceAfter]: the app
  /// shows a popup that downloads until it is done.
  bool get mustDownloadNow {
    if (!supported || !_ready || !AppSettings.instance.ytdlpOnboarded) return false;
    final since = AppSettings.instance.ytdlpFailingSince;
    return since != null && DateTime.now().difference(since) >= forceAfter;
  }

  /// Prepares the installer and checks what is installed. Quick: no network
  /// and no hashing.
  Future<void> init() async {
    if (!supported || _ready) return;
    try {
      final base = await getApplicationSupportDirectory();
      final dir = Directory('${base.path}${Platform.pathSeparator}yt-dlp');
      if (Platform.isAndroid) {
        _installer = YtDlpInstaller(
          dir: dir,
          os: 'android',
          deploy: ChannelRunner.activate,
          smokeTest: (_) => ChannelRunner.version(),
        );
        _runner = ChannelRunner();
      } else {
        _installer = YtDlpInstaller(dir: dir);
        _runner = ProcessRunner(_installer!.verifiedPath, extraOptions: () => _jsRuntime);
      }
      await _installer!.cleanUp();
      await _refreshInstalled();
      if (!Platform.isAndroid) unawaited(_detectJsRuntime());
    } catch (e) {
      debugPrint('Swar: yt-dlp setup unavailable: $e');
      setup.value = YtDlpSetup(YtDlpPhase.unsupported, error: '$e');
    }
    _ready = true;
    notifyListeners();
  }

  Future<void> _refreshInstalled() async {
    final i = _installer!;
    // No hashing at startup: the desktop runner verifies the file before
    // its first run, and Android re-checks it while copying it into place.
    final path = await i.installedPath();
    _installed = path != null;
    _version = _installed ? await i.installedVersion() : null;
    _lastCheck = await i.lastSuccessfulCheck();
    if (_installed && Platform.isAndroid) {
      // Data cleared, or a different file in place: put the verified copy
      // back where the library runs it from.
      final sha = await i.installedSha();
      try {
        await ChannelRunner.activate(path!, sha!);
      } catch (_) {
        _installed = false;
      }
    }
    if (_installed) setup.value = const YtDlpSetup(YtDlpPhase.ready);
  }

  /// YouTube needs a JavaScript runtime for many videos. yt-dlp finds Deno
  /// by itself; Node.js has to be named.
  Future<void> _detectJsRuntime() async {
    Future<bool> has(String exe) async {
      try {
        return (await Process.run(exe, ['--version']).timeout(const Duration(seconds: 5))).exitCode == 0;
      } catch (_) {
        return false;
      }
    }

    if (await has('deno')) return;
    if (await has('node') && (_version ?? '').compareTo('2025.11') >= 0) {
      _jsRuntime = const [
        ['--js-runtimes', 'node']
      ];
    }
  }

  /// Downloads (or updates) yt-dlp. Concurrent calls share one download.
  /// Returns whether a working yt-dlp is installed afterwards.
  Future<bool> install({bool background = false}) {
    if (!supported || _installer == null) return Future.value(false);
    return _installing ??= _install(background).whenComplete(() => _installing = null);
  }

  Future<bool> _install(bool background) async {
    if (!background || !_installed) setup.value = const YtDlpSetup(YtDlpPhase.downloading, progress: null);
    try {
      final v = await _installer!.installLatest(onProgress: (p) {
        if (!background || !_installed) setup.value = YtDlpSetup(YtDlpPhase.downloading, progress: p);
      });
      final changed = v != _version;
      _version = v;
      _installed = true;
      _lastCheck = DateTime.now();
      _failures = 0;
      _nextTry = null;
      if (changed) _confirmPending = true;
      await AppSettings.instance.setYtdlpFailingSince(null);
      setup.value = const YtDlpSetup(YtDlpPhase.ready);
      notifyListeners();
      return true;
    } catch (e) {
      debugPrint('Swar: yt-dlp download failed: $e');
      _failures++;
      // Back off: 5 minutes, doubling, at most 3 hours.
      _nextTry = DateTime.now().add(Duration(minutes: math.min(5 * (1 << math.min(_failures - 1, 6)), 180)));
      if (AppSettings.instance.ytdlpFailingSince == null) {
        await AppSettings.instance.setYtdlpFailingSince(DateTime.now());
      }
      final message = e is YtDlpInstallException ? e.message : "Couldn't download yt-dlp. Check your connection.";
      setup.value = _installed && background
          ? const YtDlpSetup(YtDlpPhase.ready)
          : YtDlpSetup(YtDlpPhase.failed, error: message);
      notifyListeners();
      return false;
    }
  }

  /// Checks for updates now and then while the app runs: once a day after
  /// the last good check, sooner after a failure (with back-off). Never
  /// blocks anything; a working version keeps being used meanwhile.
  void startBackgroundUpdates() {
    if (!supported || _timer != null) return;
    Timer(const Duration(seconds: 20), _maybeUpdate);
    _timer = Timer.periodic(const Duration(minutes: 30), (_) => _maybeUpdate());
  }

  void _maybeUpdate() {
    if (!AppSettings.instance.ytdlpOnboarded || _installing != null) return;
    final now = DateTime.now();
    if (_nextTry != null && now.isBefore(_nextTry!)) return;
    final due = !_installed || _lastCheck == null || now.difference(_lastCheck!) >= updateEvery;
    if (due) unawaited(install(background: true));
  }

  @override
  void dispose() {
    _timer?.cancel();
    super.dispose();
  }

  // ---- Running ----

  Future<String> _run(List<List<String>> options, String target, Duration timeout, int maxBytes,
      {bool background = false}) async {
    if (!_installed || _runner == null) {
      throw const YtDlpRunException('not_installed', 'yt-dlp is not installed yet.');
    }
    Future<String> go() => _runner!.run(options: options, target: target, timeout: timeout, maxBytes: maxBytes);
    String out;
    try {
      if (background) {
        final r = await _slots.tryRunBackground(go);
        if (r == null) throw const YtDlpRunException('canceled', 'Busy with songs you asked for.');
        out = r;
      } else {
        out = await _slots.run(go);
      }
    } on YtDlpRunException catch (e) {
      if (e.code == 'not_installed' && _installed) {
        // The file is gone or no longer matches its checksum: treat it as
        // missing so the background download replaces it.
        _installed = false;
        _nextTry = null;
        notifyListeners();
      }
      rethrow;
    }
    if (_confirmPending) {
      // The new version did a real run: the old one can go.
      _confirmPending = false;
      unawaited(_installer!.confirmWorking());
    }
    return out;
  }

  /// One run per key at a time; later callers share it.
  Future<T> _shared<T extends Object>(String key, Future<T> Function() fn) {
    final running = _inflight[key];
    if (running != null) return running.then((v) => v as T);
    final f = fn().whenComplete(() => _inflight.remove(key));
    _inflight[key] = f;
    return f;
  }

  static const _common = [
    ['--ignore-config'],
    ['--no-warnings'],
    ['--skip-download'],
    ['--dump-single-json'],
    ['--no-playlist'],
    ['--socket-timeout', '10'],
  ];

  /// Searches YouTube from this device. [input] is search text or a
  /// YouTube link (anything else throws [YtDlpInputException]).
  Future<List<Track>> search(String input, {int limit = 10}) => Future.sync(() {
        final arg = YtDlpTarget.parse(input).argument(limit);
        return _searchRun(arg, limit);
      });

  Future<List<Track>> _searchRun(String arg, int limit) {
    return _shared('search|${arg.toLowerCase()}', () async {
      final out = await _run([..._common, ['--flat-playlist']], arg, _searchTimeout, 8 << 20);
      final json = await _decode(out);
      return parseSearch(json, limit);
    });
  }

  /// The audio stream for one video, resolved on this device.
  Future<DeviceStream> resolve(String videoId, {bool background = false}) {
    if (!isVideoId(videoId)) return Future.error(const YtDlpInputException('Not a YouTube video.'));
    return _shared('resolve|$videoId', () async {
      final out = await _run([
        ..._common,
        // Popular videos list captions in every language, which makes the
        // output much larger.
        ['--extractor-args', 'youtube:skip=translated_subs'],
      ], watchUrl(videoId), _resolveTimeout, 32 << 20, background: background);
      return parseResolve(await _decode(out));
    });
  }

  /// Large outputs are decoded off the UI isolate.
  static Future<Map<String, dynamic>> _decode(String out) async {
    try {
      return out.length > 256 << 10 ? await compute(_parseJson, out) : _parseJson(out);
    } on FormatException {
      throw const YtDlpRunException('failed', 'yt-dlp returned something unreadable.');
    }
  }
}

Map<String, dynamic> _parseJson(String s) => (jsonDecode(s) as Map).cast<String, dynamic>();
