import 'dart:async';
import 'dart:collection';

import 'package:flutter/foundation.dart';

import 'ytdlp/device_ytdlp.dart';
import 'ytdlp/ytdlp_parse.dart';
import 'ytdlp/ytdlp_runner.dart' show YtDlpRunException;

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
/// don't matter. Only yt-dlp on this device ([DeviceYtDlp]) does that, and
/// it also finds the YouTube video for catalog songs; until it has been
/// downloaded, songs fail here and the player falls back to the server's
/// relay (which runs yt-dlp too).
///
/// Resolved streams are cached (at most [_maxCached]) until shortly before
/// their signed URL expires. A video is never resolved twice at the same
/// time: callers share the request. Not used where yt-dlp can't run on the
/// device (the web, iOS).
class YouTubeAudio {
  YouTubeAudio._();

  static bool get supported => DeviceYtDlp.supported;

  static const _notReady = YouTubeAudioException("yt-dlp isn't ready on this device yet. Try again in a moment.");

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
    }).whenComplete(() {
      // Not "=> _inflight.remove(id)", which returns this future and makes
      // it wait for itself forever.
      _inflight.remove(id);
    });
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

  static Future<DeviceStream> _resolveChain(String id, bool background) async {
    if (!DeviceYtDlp.instance.installed) throw _notReady;
    try {
      return await DeviceYtDlp.instance.resolve(id, background: background);
    } on YtDlpUnplayable catch (e) {
      throw YouTubeAudioException(e.message);
    } on YouTubeAudioException {
      rethrow;
    } catch (e) {
      debugPrint('Swar: resolving $id with yt-dlp failed: $e');
      throw YouTubeAudioException(e is YtDlpRunException ? e.message : "Couldn't get this song from YouTube.");
    }
  }

  static final _matches = LinkedHashMap<String, String>();

  /// Finds the YouTube video for a catalog song (Spotify, Deezer...) by
  /// searching YouTube with yt-dlp on this device. Prefers a video whose
  /// length matches the song and skips live, cover and remix uploads unless
  /// the song itself is one. Returns null when nothing fits.
  static Future<String?> findVideoId(String title, String artist, Duration duration) async {
    final key = '${title.toLowerCase()}|${artist.toLowerCase()}';
    final known = _matches[key];
    if (known != null) return known;
    if (!DeviceYtDlp.instance.installed) throw _notReady;
    final results = await DeviceYtDlp.instance.search('$title $artist audio', limit: 10);
    final wanted = title.toLowerCase();
    const variants = ['live', 'cover', 'remix', 'karaoke', 'instrumental', 'lofi', 'lo-fi', 'slowed', 'reverb', '8d'];
    String? best;
    var bestScore = -1 << 30;
    var rank = 0;
    for (final v in results) {
      final name = v.title.toLowerCase();
      final author = v.artist.toLowerCase();
      var score = -rank * 2;
      rank++;
      if (name.contains(wanted)) score += 30;
      if (artist.isNotEmpty &&
          (name.contains(artist.toLowerCase()) || author.contains(artist.toLowerCase()))) {
        score += 15;
      }
      if (author.endsWith(' - topic')) score += 10;
      for (final word in variants) {
        if (name.contains(word) && !wanted.contains(word)) score -= 40;
      }
      final d = v.duration;
      if (d > Duration.zero && duration > Duration.zero) {
        final off = (d - duration).inSeconds.abs();
        score += off <= 5 ? 25 : (off <= 20 ? 10 : (off > 90 ? -50 : -10));
      }
      if (score > bestScore) {
        bestScore = score;
        best = v.youtubeId;
      }
    }
    if (best == null || bestScore < 0) return null;
    _matches[key] = best;
    if (_matches.length > 500) _matches.remove(_matches.keys.first);
    return best;
  }

  /// Drops a cached URL that failed to play, so the next try asks again.
  static void forget(String trackId) => _cache.remove(_id(trackId));
}
