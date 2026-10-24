/// What a Deep Search box may contain, checked before yt-dlp sees it. The
/// same rules as the server (server/internal/providers/deepsearch): search
/// text, or a YouTube link that is rebuilt from its validated video ID.
/// Any other URL is refused, so yt-dlp is never pointed at arbitrary sites.
library;

final _idRe = RegExp(r'^[A-Za-z0-9_-]{11}$');
final _urlRe = RegExp(
    r'^(?:https?://)?(?:www\.|m\.|music\.)?(?:youtube\.com/(?:watch\?(?:.*&)?v=|shorts/|embed/)|youtu\.be/)([A-Za-z0-9_-]{11})(?:[?&#].*)?$');

/// Thrown for input Deep Search will not run.
class YtDlpInputException implements Exception {
  final String message;
  const YtDlpInputException(this.message);
  @override
  String toString() => message;
}

/// A yt-dlp target: either a search or one video.
class YtDlpTarget {
  /// Normalized search text, or null for a video.
  final String? query;

  /// Validated 11-character video ID, or null for a search.
  final String? videoId;
  const YtDlpTarget._({this.query, this.videoId});

  bool get isVideo => videoId != null;

  /// The single positional argument yt-dlp gets. It never starts with "-",
  /// so it cannot be read as an option.
  String argument(int limit) => isVideo ? watchUrl(videoId!) : 'ytsearch$limit:$query';

  static YtDlpTarget parse(String input) {
    final s = input.trim();
    final m = _urlRe.firstMatch(s);
    if (m != null) return YtDlpTarget._(videoId: m.group(1));
    if (looksLikeVideoId(s)) return YtDlpTarget._(videoId: s);
    final lower = s.toLowerCase();
    if (lower.contains('://') || lower.startsWith('www.')) {
      throw const YtDlpInputException('Only YouTube links are supported in Deep Search.');
    }
    final q = normalizeQuery(s);
    if (q.isEmpty) throw const YtDlpInputException('Type something to search for.');
    if (q.runes.length < 2) throw const YtDlpInputException('That search is too short.');
    if (q.runes.length > 200) throw const YtDlpInputException('That search is too long (200 characters at most).');
    if (q.runes.any((r) => r < 0x20 || r == 0x7f)) {
      throw const YtDlpInputException('That search contains characters that are not allowed.');
    }
    return YtDlpTarget._(query: q);
  }
}

/// Collapses whitespace. Case is kept for yt-dlp; cache keys lower it.
String normalizeQuery(String q) => q.trim().split(RegExp(r'\s+')).where((w) => w.isNotEmpty).join(' ');

/// Bare tokens count as IDs only when they look like one (mixed case plus a
/// digit or symbol), so ordinary words stay searches.
bool looksLikeVideoId(String s) =>
    _idRe.hasMatch(s) && RegExp(r'[0-9_-]').hasMatch(s) && s.toLowerCase() != s;

bool isVideoId(String s) => _idRe.hasMatch(s);

String watchUrl(String id) => 'https://www.youtube.com/watch?v=$id';
