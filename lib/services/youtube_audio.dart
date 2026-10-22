import 'package:flutter/foundation.dart';
import 'package:youtube_explode_dart/youtube_explode_dart.dart';

/// A Deep Search song YouTube wouldn't give this device.
class YouTubeAudioException implements Exception {
  final String message;
  const YouTubeAudioException(this.message);

  @override
  String toString() => message;
}

/// Plays Deep Search songs straight from YouTube on this device.
///
/// The server only sends the video's details. The app asks YouTube for the
/// video's audio-only stream itself and plays it like any other song, so
/// nothing is downloaded or converted and YouTube's blocks on the server
/// don't matter. Not used on the web, where browsers refuse these requests.
class YouTubeAudio {
  YouTubeAudio._();

  static bool get supported => !kIsWeb;

  static YoutubeExplode? _yt;
  static final _cache = <String, ({String url, DateTime expires})>{};

  /// The audio stream URL for a track id such as "yt:dQw4w9WgXcQ".
  static Future<String> streamUrl(String trackId) async {
    final id = trackId.startsWith('yt:') ? trackId.substring(3) : trackId;
    final hit = _cache[id];
    if (hit != null && DateTime.now().isBefore(hit.expires)) return hit.url;
    final yt = _yt ??= YoutubeExplode();
    final StreamManifest manifest;
    try {
      manifest = await yt.videos.streamsClient.getManifest(id).timeout(const Duration(seconds: 25));
    } on VideoUnplayableException catch (e) {
      throw YouTubeAudioException(_reason(e.message));
    } on RequestLimitExceededException {
      throw const YouTubeAudioException('YouTube is limiting requests from this device right now. Try again soon.');
    }
    final url = _pick(manifest.audioOnly)?.url;
    if (url == null) throw const YouTubeAudioException('YouTube has no audio for this video.');
    _cache[id] = (url: url.toString(), expires: _expiry(url));
    return url.toString();
  }

  /// Drops a cached URL that failed to play, so the next try asks again.
  static void forget(String trackId) => _cache.remove(trackId.startsWith('yt:') ? trackId.substring(3) : trackId);

  /// The best audio-only stream in the original language. MP4 (AAC) comes
  /// first because every platform's player handles it, Apple's included.
  static AudioOnlyStreamInfo? _pick(List<AudioOnlyStreamInfo> streams) {
    final original = streams.where((s) => s.audioTrack == null || s.audioTrack!.audioIsDefault).toList();
    final pool = original.isNotEmpty ? original : streams;
    if (pool.isEmpty) return null;
    final mp4 = pool.where((s) => s.container == StreamContainer.mp4).toList();
    return (mp4.isNotEmpty ? mp4 : pool).sortByBitrate().first;
  }

  /// YouTube URLs stop working at their "expire" time; leave a margin.
  static DateTime _expiry(Uri url) {
    final expire = int.tryParse(url.queryParameters['expire'] ?? '');
    if (expire == null) return DateTime.now().add(const Duration(hours: 1));
    return DateTime.fromMillisecondsSinceEpoch(expire * 1000).subtract(const Duration(minutes: 10));
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
