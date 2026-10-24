import 'dart:async';
import 'dart:collection';
import 'dart:io' show Platform;

import 'package:flutter/foundation.dart';
import 'package:youtube_explode_dart/youtube_explode_dart.dart';

import 'ytdlp/device_ytdlp.dart';
import 'ytdlp/ytdlp_parse.dart';

/// A Deep Search song YouTube wouldn't give this device.
class YouTubeAudioException implements Exception {
  final String message;
  const YouTubeAudioException(this.message);

  @override
  String toString() => message;
}

/// Plays YouTube songs straight from YouTube on this device.
///
/// The server only sends the video's details. The app asks YouTube for the
/// video's audio-only stream itself, so YouTube's blocks on the server
/// don't matter. Two tools can do that here, tried in order:
///
/// - yt-dlp on this device ([DeviceYtDlp]), which keeps up with YouTube's
///   changes best. First on desktop.
/// - youtube_explode_dart, which needs no download and starts faster. First
///   on Android, where starting Python for yt-dlp takes a few seconds.
///
/// Resolved streams are cached (at most [_maxCached]) until shortly before
/// their signed URL expires. A video is never resolved twice at the same
/// time: callers share the request. Not used on the web, where browsers
/// refuse these requests.
class YouTubeAudio {
  YouTubeAudio._();

  static bool get supported => !kIsWeb;

  static YoutubeExplode? _yt;
  static const _maxCached = 64;
  static final _cache = LinkedHashMap<String, DeviceStream>();
  static final _inflight = <String, Future<DeviceStream>>{};

  static String _id(String trackId) => trackId.startsWith('yt:') ? trackId.substring(3) : trackId;

  /// A cached stream for [trackId] that is still valid, or null.
  static DeviceStream? cached(String trackId) {
    final id = _id(trackId);
    final hit = _cache.remove(id);
    if (hit == null) return null;
    if (!hit.validAt(DateTime.now())) return null;
    _cache[id] = hit; // most recently used
    return hit;
  }

  static void _remember(String id, DeviceStream s) {
    _cache.remove(id);
    _cache[id] = s;
    while (_cache.length > _maxCached) {
      _cache.remove(_cache.keys.first);
    }
  }

  /// The audio stream for a track id such as "yt:dQw4w9WgXcQ". Set [fresh]
  /// to skip the cache (the cached URL just failed to load).
  static Future<DeviceStream> resolve(String trackId, {bool fresh = false, bool background = false}) {
    final id = _id(trackId);
    if (!fresh) {
      final hit = cached(id);
      if (hit != null) return Future.value(hit);
    } else {
      _cache.remove(id);
    }
    final running = _inflight[id];
    if (running != null) return running;
    final f = _resolveChain(id, background).then((s) {
      _remember(id, s);
      return s;
    }).whenComplete(() => _inflight.remove(id));
    _inflight[id] = f;
    return f;
  }

  /// Compatibility: just the URL.
  static Future<String> streamUrl(String trackId) async => (await resolve(trackId)).url;

  /// Resolves ahead of time so the song starts at once. Never throws; does
  /// nothing when the stream is already cached or yt-dlp is busy with songs
  /// the listener asked for.
  static Future<void> prefetch(String trackId) async {
    if (cached(trackId) != null) return;
    try {
      await resolve(trackId, background: true);
    } catch (_) {}
  }

  static bool get _deviceYtDlpFirst => !kIsWeb && !Platform.isAndroid;

  static Future<DeviceStream> _resolveChain(String id, bool background) async {
    final ytdlp = DeviceYtDlp.supported && DeviceYtDlp.instance.installed;
    final order = <Future<DeviceStream> Function()>[
      if (ytdlp && _deviceYtDlpFirst) () => DeviceYtDlp.instance.resolve(id, background: background),
      () => _explode(id),
      if (ytdlp && !_deviceYtDlpFirst) () => DeviceYtDlp.instance.resolve(id, background: background),
    ];
    Object? firstError;
    for (final attempt in order) {
      try {
        return await attempt();
      } on YtDlpUnplayable catch (e) {
        // The video itself is restricted: another tool won't change that.
        throw YouTubeAudioException(e.message);
      } catch (e) {
        debugPrint('Swar: resolving $id failed: $e');
        firstError ??= e;
      }
    }
    if (firstError is YouTubeAudioException) throw firstError;
    throw YouTubeAudioException(firstError?.toString() ?? "Couldn't get this song from YouTube.");
  }

  static Future<DeviceStream> _explode(String id) async {
    final yt = _yt ??= YoutubeExplode();
    final StreamManifest manifest;
    try {
      manifest = await yt.videos.streamsClient.getManifest(id).timeout(const Duration(seconds: 25));
    } on VideoUnplayableException catch (e) {
      throw YouTubeAudioException(_reason(e.message));
    } on RequestLimitExceededException {
      throw const YouTubeAudioException('YouTube is limiting requests from this device right now. Try again soon.');
    }
    final best = _pick(manifest.audioOnly);
    if (best == null) throw const YouTubeAudioException('YouTube has no audio for this video.');
    final url = best.url.toString();
    return DeviceStream(
      url: url,
      expires: streamExpiry(url),
      bitrateKbps: best.bitrate.kiloBitsPerSecond.round(),
      via: 'youtube_explode',
    );
  }

  static final _matches = LinkedHashMap<String, String>();

  /// Finds the YouTube video for a catalog song (Spotify, Deezer...) by
  /// searching YouTube from this device. Prefers a video whose length
  /// matches the song and skips live, cover and remix uploads unless the
  /// song itself is one. Returns null when nothing fits.
  static Future<String?> findVideoId(String title, String artist, Duration duration) async {
    final key = '${title.toLowerCase()}|${artist.toLowerCase()}';
    final known = _matches[key];
    if (known != null) return known;
    final yt = _yt ??= YoutubeExplode();
    final results = await yt.search.search('$title $artist audio').timeout(const Duration(seconds: 15));
    final wanted = title.toLowerCase();
    const variants = ['live', 'cover', 'remix', 'karaoke', 'instrumental', 'lofi', 'lo-fi', 'slowed', 'reverb', '8d'];
    String? best;
    var bestScore = -1 << 30;
    var rank = 0;
    for (final v in results.take(10)) {
      final name = v.title.toLowerCase();
      var score = -rank * 2;
      rank++;
      if (name.contains(wanted)) score += 30;
      if (artist.isNotEmpty &&
          (name.contains(artist.toLowerCase()) || v.author.toLowerCase().contains(artist.toLowerCase()))) {
        score += 15;
      }
      if (v.author.endsWith(' - Topic')) score += 10;
      for (final word in variants) {
        if (name.contains(word) && !wanted.contains(word)) score -= 40;
      }
      final d = v.duration;
      if (d != null && duration > Duration.zero) {
        final off = (d - duration).inSeconds.abs();
        score += off <= 5 ? 25 : (off <= 20 ? 10 : (off > 90 ? -50 : -10));
      }
      if (score > bestScore) {
        bestScore = score;
        best = v.id.value;
      }
    }
    if (best == null || bestScore < 0) return null;
    _matches[key] = best;
    if (_matches.length > 500) _matches.remove(_matches.keys.first);
    return best;
  }

  /// Drops a cached URL that failed to play, so the next try asks again.
  static void forget(String trackId) => _cache.remove(_id(trackId));

  /// The best audio-only stream in the original language. MP4 (AAC) comes
  /// first because every platform's player handles it, Apple's included.
  static AudioOnlyStreamInfo? _pick(List<AudioOnlyStreamInfo> streams) {
    final original = streams.where((s) => s.audioTrack == null || s.audioTrack!.audioIsDefault).toList();
    final pool = original.isNotEmpty ? original : streams;
    if (pool.isEmpty) return null;
    final mp4 = pool.where((s) => s.container == StreamContainer.mp4).toList();
    return (mp4.isNotEmpty ? mp4 : pool).sortByBitrate().first;
  }

  static String _reason(String message) {
    final m = message.toLowerCase();
    if (m.contains('sign in') || m.contains('age')) return 'YouTube only plays this video to signed-in adults.';
    if (m.contains('private')) return 'This YouTube video is private.';
    if (m.contains('country') || m.contains('region')) return "This YouTube video isn't available in your country.";
    if (m.contains('removed') || m.contains('unavailable')) return 'This YouTube video is no longer available.';
    return "YouTube won't play this video.";
  }
}
