/// Turns yt-dlp's JSON into Swar tracks and playable streams. Pure functions
/// with no platform code, so they are unit tested. Everything yt-dlp prints
/// comes from YouTube and is treated as untrusted: only https URLs are
/// kept, strings are bounded, and IDs are validated.
library;

import '../../models/track.dart';
import 'ytdlp_input.dart';

/// An audio stream for one video, resolved on this device.
class DeviceStream {
  /// Signed, temporary URL. Never stored on disk or used as an identifier.
  final String url;
  final DateTime expires;

  /// Headers YouTube expects with [url] (for example User-Agent).
  final Map<String, String> headers;
  final int bitrateKbps;

  /// Which tool resolved it ("yt-dlp").
  final String via;

  const DeviceStream({
    required this.url,
    required this.expires,
    this.headers = const {},
    this.bitrateKbps = 0,
    required this.via,
  });

  bool validAt(DateTime now) => now.isBefore(expires);
}

/// A video yt-dlp says cannot be played (private, members-only, DRM...).
class YtDlpUnplayable implements Exception {
  final String message;
  const YtDlpUnplayable(this.message);
  @override
  String toString() => message;
}

String _str(Object? v, [int max = 300]) {
  if (v is! String) return '';
  final s = v.trim();
  return s.length > max ? s.substring(0, max) : s;
}

String _first(List<Object?> vs) {
  for (final v in vs) {
    final s = _str(v);
    if (s.isNotEmpty) return s;
  }
  return '';
}

/// Search output (or a single video's metadata) as tracks: deduplicated,
/// live streams and malformed entries skipped, https artwork only.
List<Track> parseSearch(Map<String, dynamic> root, int limit) {
  final entries = <Map<String, dynamic>>[];
  final raw = root['entries'];
  if (raw is List) {
    entries.addAll(raw.whereType<Map>().map((e) => e.cast<String, dynamic>()));
  } else if (root['id'] != null) {
    entries.add(root);
  }
  final out = <Track>[];
  final seen = <String>{};
  for (final e in entries) {
    final id = _str(e['id'], 11);
    if (!isVideoId(id) || seen.contains(id) || e['live_status'] == 'is_live') continue;
    seen.add(id);
    out.add(trackFromEntry(e));
    if (out.length >= limit) break;
  }
  return out;
}

Track trackFromEntry(Map<String, dynamic> e) {
  final id = _str(e['id'], 11);
  var artist = _first([e['artist'], e['channel'], e['uploader']]);
  if (artist.endsWith('VEVO')) artist = artist.substring(0, artist.length - 4).trim();
  final duration = e['duration'];
  return Track(
    id: 'yt:$id',
    title: _first([e['track'], e['title']]).isEmpty ? 'Unknown title' : _first([e['track'], e['title']]),
    artist: artist,
    thumbnailUrl: _thumbnail(e, id),
    duration: Duration(seconds: duration is num && duration > 0 && duration < 86400 ? duration.toInt() : 0),
    provider: 'yt',
    album: _str(e['album']),
    // Found and played on this device; the server's playback switch is
    // about the server fetching audio, which it does not do here.
    playable: true,
    downloadable: false,
    youtubeId: id,
  );
}

String _thumbnail(Map<String, dynamic> e, String id) {
  String best = '';
  var bestArea = -1;
  final list = e['thumbnails'];
  if (list is List) {
    for (final t in list.whereType<Map>()) {
      final url = _str(t['url'], 1000);
      if (!url.startsWith('https://')) continue;
      final w = t['width'], h = t['height'];
      final area = (w is num && h is num) ? (w * h).toInt() : 0;
      // Big enough for the player, small enough for lists.
      if (area <= 640 * 480 && area > bestArea) {
        best = url;
        bestArea = area;
      }
    }
  }
  if (best.isNotEmpty) return best;
  final single = _str(e['thumbnail'], 1000);
  if (single.startsWith('https://')) return single;
  return 'https://i.ytimg.com/vi/$id/hqdefault.jpg';
}

/// Picks the audio stream to play from a video's full metadata. Refuses
/// restricted videos and DRM formats; prefers m4a (AAC), which every
/// platform's player handles, then the higher bitrate.
DeviceStream parseResolve(Map<String, dynamic> root, {DateTime? now}) {
  final availability = _str(root['availability']);
  if (availability.isNotEmpty && availability != 'public' && availability != 'unlisted') {
    throw const YtDlpUnplayable('This video is restricted and cannot be played.');
  }
  if (root['live_status'] == 'is_live') {
    throw const YtDlpUnplayable('Live streams cannot be played as songs.');
  }
  Map<String, dynamic>? best;
  final formats = root['formats'];
  if (formats is List) {
    for (final f in formats.whereType<Map>().map((f) => f.cast<String, dynamic>())) {
      final url = _str(f['url'], 8000);
      final protocol = _str(f['protocol']);
      if (!url.startsWith('https://') || !protocol.startsWith('http') || _drm(f['has_drm'])) continue;
      final vcodec = _str(f['vcodec']);
      final acodec = _str(f['acodec']);
      if (vcodec != 'none' || acodec.isEmpty || acodec == 'none') continue;
      if (best == null || _better(f, best)) best = f;
    }
  }
  if (best == null) throw const YtDlpUnplayable('No playable audio was found for this video.');
  final url = _str(best['url'], 8000);
  final headers = <String, String>{};
  final h = best['http_headers'];
  if (h is Map) {
    final ua = _str(h['User-Agent']);
    if (ua.isNotEmpty) headers['User-Agent'] = ua;
  }
  final abr = best['abr'];
  return DeviceStream(
    url: url,
    expires: streamExpiry(url, now: now),
    headers: headers,
    bitrateKbps: abr is num ? abr.round() : 0,
    via: 'yt-dlp',
  );
}

bool _better(Map<String, dynamic> a, Map<String, dynamic> b) {
  final am4a = a['ext'] == 'm4a', bm4a = b['ext'] == 'm4a';
  if (am4a != bm4a) return am4a;
  final aa = a['abr'], ba = b['abr'];
  return (aa is num ? aa : 0) > (ba is num ? ba : 0);
}

bool _drm(Object? v) => v == true || (v is String && v.isNotEmpty && v != 'false');

/// When a signed URL stops working: its "expire" parameter minus a margin,
/// or 30 minutes when it has none. Never more than six hours ahead.
DateTime streamExpiry(String url, {DateTime? now}) {
  final t = now ?? DateTime.now();
  final expire = int.tryParse(Uri.tryParse(url)?.queryParameters['expire'] ?? '');
  if (expire != null) {
    final at = DateTime.fromMillisecondsSinceEpoch(expire * 1000).subtract(const Duration(minutes: 10));
    if (at.isAfter(t) && at.isBefore(t.add(const Duration(hours: 6)))) return at;
    if (!at.isAfter(t)) return t; // already expired
  }
  return t.add(const Duration(minutes: 30));
}

/// A short reason for a failed run, from yt-dlp's stderr, safe to show.
String explainFailure(String stderr) {
  final m = stderr.toLowerCase();
  if (m.contains('not a bot')) return 'YouTube is asking this device to sign in. Try again later.';
  if (m.contains('private video')) return 'This YouTube video is private.';
  if (m.contains('sign in') || m.contains('age')) return 'YouTube only plays this video to signed-in adults.';
  if (m.contains('drm')) return 'This video is DRM-protected and cannot be played.';
  if (m.contains('country') || m.contains('region')) return "This video isn't available in your country.";
  if (m.contains('unavailable') || m.contains('removed')) return 'This YouTube video is no longer available.';
  if (m.contains('timed out') || m.contains('timeout')) return 'YouTube took too long to answer.';
  if (m.contains('network') || m.contains('connection') || m.contains('resolve')) {
    return "Couldn't reach YouTube. Check your connection.";
  }
  return 'yt-dlp could not read this from YouTube.';
}
