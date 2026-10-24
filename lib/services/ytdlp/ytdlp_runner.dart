import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:flutter/services.dart';

import 'ytdlp_parse.dart';

/// Why a yt-dlp run failed. [message] is safe to show.
class YtDlpRunException implements Exception {
  /// not_installed, timeout, too_large, canceled or failed.
  final String code;
  final String message;
  const YtDlpRunException(this.code, this.message);
  @override
  String toString() => message;
}

/// Runs yt-dlp once. Options are given as [flag] or [flag, value] pairs and
/// the single target last, so nothing is ever built into a shell string.
abstract class YtDlpRunner {
  Future<String> run({
    required List<List<String>> options,
    required String target,
    required Duration timeout,
    required int maxBytes,
  });
}

/// Desktop: starts the verified binary directly (no shell). Arguments are a
/// list, so spaces, quotes and non-ASCII characters in paths or searches
/// reach yt-dlp unchanged. "--" goes before the target.
class ProcessRunner implements YtDlpRunner {
  /// The executable to run, or null when yt-dlp is not installed.
  final Future<String?> Function() binary;
  final List<List<String>> Function() extraOptions;
  ProcessRunner(this.binary, {List<List<String>> Function()? extraOptions})
      : extraOptions = extraOptions ?? (() => const []);

  @override
  Future<String> run({
    required List<List<String>> options,
    required String target,
    required Duration timeout,
    required int maxBytes,
  }) async {
    final path = await binary();
    if (path == null) throw const YtDlpRunException('not_installed', 'yt-dlp is not installed yet.');
    final args = [
      for (final o in [...options, ...extraOptions()]) ...o,
      '--',
      target,
    ];
    final Process p;
    try {
      p = await Process.start(path, args, runInShell: false);
    } on ProcessException {
      throw const YtDlpRunException('not_installed', "yt-dlp couldn't be started on this computer.");
    }
    final out = BytesBuilder(copy: false);
    var tooLarge = false;
    var timedOut = false;
    var err = '';
    final outDone = p.stdout.listen((chunk) {
      if (tooLarge) return;
      if (out.length + chunk.length > maxBytes) {
        tooLarge = true;
        _stop(p);
      } else {
        out.add(chunk);
      }
    }).asFuture<void>().catchError((_) {});
    final errDone = p.stderr.transform(utf8.decoder).listen((s) {
      err = (err + s);
      if (err.length > 4000) err = err.substring(err.length - 4000);
    }).asFuture<void>().catchError((_) {});
    final code = await p.exitCode.timeout(timeout, onTimeout: () {
      timedOut = true;
      _stop(p);
      return p.exitCode;
    });
    await Future.wait([outDone, errDone]).timeout(const Duration(seconds: 3), onTimeout: () => const []);
    if (timedOut) throw const YtDlpRunException('timeout', 'YouTube took too long to answer.');
    if (tooLarge) throw const YtDlpRunException('too_large', 'yt-dlp returned more data than expected.');
    if (code != 0) throw YtDlpRunException('failed', explainFailure(err));
    return utf8.decode(out.takeBytes(), allowMalformed: true);
  }

  /// Stops yt-dlp and the process it unpacks itself into. On Windows the
  /// whole tree is ended with taskkill; elsewhere SIGTERM (which the
  /// standalone binary passes on to its child), then SIGKILL.
  static void _stop(Process p) {
    if (Platform.isWindows) {
      Process.run('taskkill', ['/PID', '${p.pid}', '/T', '/F']).catchError((_) => ProcessResult(0, 1, '', ''));
      return;
    }
    p.kill(ProcessSignal.sigterm);
    Timer(const Duration(seconds: 2), () => p.kill(ProcessSignal.sigkill));
  }
}

/// Android: runs yt-dlp through YtDlpChannel.kt (the youtubedl-android
/// library's Python). The channel enforces its own timeout and limits too.
class ChannelRunner implements YtDlpRunner {
  static const channel = MethodChannel('swar/ytdlp');
  int _next = 0;

  @override
  Future<String> run({
    required List<List<String>> options,
    required String target,
    required Duration timeout,
    required int maxBytes,
  }) async {
    final id = 'swar-${DateTime.now().microsecondsSinceEpoch}-${_next++}';
    try {
      final out = await channel.invokeMethod<String>('run', {
        'id': id,
        'options': options,
        'target': target,
        'timeoutMs': timeout.inMilliseconds,
        'maxBytes': maxBytes,
      }).timeout(timeout + const Duration(seconds: 5), onTimeout: () {
        channel.invokeMethod('cancel', {'id': id}).catchError((_) => null);
        throw const YtDlpRunException('timeout', 'YouTube took too long to answer.');
      });
      return out ?? '';
    } on PlatformException catch (e) {
      switch (e.code) {
        case 'not_installed':
          throw const YtDlpRunException('not_installed', 'yt-dlp is not installed yet.');
        case 'timeout':
          throw const YtDlpRunException('timeout', 'YouTube took too long to answer.');
        case 'too_large':
          throw const YtDlpRunException('too_large', 'yt-dlp returned more data than expected.');
        case 'canceled':
          throw const YtDlpRunException('canceled', 'Canceled.');
      }
      throw YtDlpRunException('failed', explainFailure(e.message ?? ''));
    } on MissingPluginException {
      throw const YtDlpRunException('not_installed', 'yt-dlp is not available in this build.');
    }
  }

  static Future<bool> installed() async {
    try {
      return await channel.invokeMethod<bool>('installed') ?? false;
    } catch (_) {
      return false;
    }
  }

  /// Copies a verified download to where the library runs yt-dlp from.
  static Future<void> activate(String path, String sha256) async {
    await channel.invokeMethod('activate', {'path': path, 'sha256': sha256});
  }

  static Future<String?> version() async {
    try {
      final v = await channel.invokeMethod<String>('version').timeout(const Duration(seconds: 60));
      return v != null && RegExp(r'^[0-9][0-9A-Za-z.+-]{3,40}$').hasMatch(v) ? v : null;
    } catch (_) {
      return null;
    }
  }
}
