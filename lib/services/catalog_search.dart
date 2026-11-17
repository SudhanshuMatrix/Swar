import 'dart:collection';

import '../models/online_artist.dart';
import '../models/online_playlist.dart';
import '../models/track.dart';

typedef CatalogResults = ({
  List<Track> songs,
  List<OnlinePlaylist> playlists,
  List<OnlineArtist> artists,
});

/// One result cache and one in-flight request per query, shared by the search
/// screen and its typeahead. Each ApiService owns its own cache.
class CatalogSearch {
  CatalogSearch(this._load, {DateTime Function()? clock})
      : _clock = clock ?? DateTime.now;

  final Future<({CatalogResults results, bool partial})> Function(
    String query,
    int limit,
  ) _load;
  final DateTime Function() _clock;
  final _recent = LinkedHashMap<
      (String, int), ({CatalogResults results, DateTime expires})>();
  final _pending = <(String, int), Future<CatalogResults>>{};

  static final _whitespace = RegExp(r'\s+', unicode: true);

  static String normalize(String query) =>
      query.trim().replaceAll(_whitespace, ' ');

  CatalogResults? cached(String query, int limit) {
    final key = (normalize(query).toLowerCase(), limit);
    final hit = _recent.remove(key);
    if (hit == null || !_clock().isBefore(hit.expires)) return null;
    _recent[key] = hit;
    return hit.results;
  }

  Future<CatalogResults> search(String query, int limit) async {
    final normalized = normalize(query);
    final hit = cached(normalized, limit);
    if (hit != null) return hit;
    final key = (normalized.toLowerCase(), limit);
    final pending = _pending[key];
    if (pending != null) return pending;

    final request = Future.sync(() => _load(normalized, limit)).then((hit) {
      _recent[key] = (
        results: hit.results,
        expires: _clock().add(hit.partial
            ? const Duration(seconds: 20)
            : const Duration(minutes: 5)),
      );
      if (_recent.length > 50) _recent.remove(_recent.keys.first);
      return hit.results;
    });
    _pending[key] = request;
    try {
      return await request;
    } finally {
      _pending.remove(key);
    }
  }

  /// Provisional typeahead from the previous query while a longer query loads.
  /// Exact cached responses are returned without filtering or reordering.
  ({List<Track> songs, List<OnlineArtist> artists}) suggestions(
    String query,
    int limit,
  ) {
    final exact = cached(query, limit);
    if (exact != null) {
      return (
        songs: exact.songs.take(5).toList(),
        artists: exact.artists.take(2).toList(),
      );
    }
    final key = normalize(query).toLowerCase();
    CatalogResults? related;
    var length = 1;
    final now = _clock();
    for (final entry in _recent.entries) {
      final (prefix, entryLimit) = entry.key;
      if (entryLimit == limit &&
          prefix.length > length &&
          key.startsWith(prefix) &&
          now.isBefore(entry.value.expires)) {
        related = entry.value.results;
        length = prefix.length;
      }
    }
    if (related == null) return (songs: const [], artists: const []);
    final words = key.split(' ');
    bool matches(String text) {
      final lower = text.toLowerCase();
      return words.every(lower.contains);
    }

    return (
      songs: related.songs
          .where((track) => matches('${track.title} ${track.artist}'))
          .take(5)
          .toList(),
      artists: related.artists
          .where((artist) => matches(artist.name))
          .take(2)
          .toList(),
    );
  }
}
