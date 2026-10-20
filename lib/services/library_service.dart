import 'package:hive_flutter/hive_flutter.dart';
import 'package:uuid/uuid.dart';

import '../models/playlist.dart';
import '../models/track.dart';

/// Local persistence for playlists, liked songs, and recently-played tracks.
///
/// Uses Hive (not sqflite) specifically because Hive works on every target
/// this app ships to, including Flutter Web (IndexedDB) — sqflite does not
/// support web.
class LibraryService {
  LibraryService._();
  static final LibraryService instance = LibraryService._();

  static const _uuid = Uuid();

  late Box _tracks;
  late Box _playlists;
  late Box _liked;
  late Box _recent;
  late Box _plays;
  late Box _searches;
  late Box _session;
  bool _ready = false;

  /// Opens the boxes. Tests pass a [path] to keep data in a temp folder.
  Future<void> init({String? path}) async {
    if (_ready) return;
    path == null ? await Hive.initFlutter() : Hive.init(path);
    _tracks = await Hive.openBox('tracks');
    _playlists = await Hive.openBox('playlists');
    _liked = await Hive.openBox('liked');
    _recent = await Hive.openBox('recent');
    _plays = await Hive.openBox('plays');
    _searches = await Hive.openBox('searches');
    _session = await Hive.openBox('session');
    _ready = true;
  }

  Future<void> cacheTrack(Track t) => _tracks.put(t.id, t.toMap());

  Track? _trackFromId(String id) {
    final raw = _tracks.get(id);
    if (raw == null) return null;
    return Track.fromMap(Map<dynamic, dynamic>.from(raw as Map));
  }

  // ---- Playlists ----

  List<Playlist> getPlaylists() {
    final playlists = _playlists.keys.map((key) {
      final m = Map<String, dynamic>.from(_playlists.get(key) as Map);
      final trackIds = List<String>.from(m['trackIds'] as List? ?? const []);
      final tracks = trackIds.map(_trackFromId).whereType<Track>().toList();
      return _PlaylistWithMeta(
        playlist: Playlist(id: key as String, name: m['name'] as String, tracks: tracks),
        createdAt: m['createdAt'] as int? ?? 0,
      );
    }).toList()
      ..sort((a, b) => b.createdAt.compareTo(a.createdAt));
    return playlists.map((p) => p.playlist).toList();
  }

  Future<Playlist> createPlaylist(String name) async {
    final id = _uuid.v4();
    await _playlists.put(id, {
      'name': name,
      'trackIds': <String>[],
      'createdAt': DateTime.now().millisecondsSinceEpoch,
    });
    return Playlist(id: id, name: name);
  }

  Future<void> deletePlaylist(String id) => _playlists.delete(id);

  Future<void> renamePlaylist(String id, String newName) async {
    final raw = _playlists.get(id);
    if (raw == null) return;
    final m = Map<String, dynamic>.from(raw as Map);
    m['name'] = newName;
    await _playlists.put(id, m);
  }

  Future<void> addTrackToPlaylist(String playlistId, Track track) async {
    await cacheTrack(track);
    final m = Map<String, dynamic>.from(_playlists.get(playlistId) as Map);
    final ids = List<String>.from(m['trackIds'] as List? ?? const []);
    if (!ids.contains(track.id)) ids.add(track.id);
    m['trackIds'] = ids;
    await _playlists.put(playlistId, m);
  }

  Future<void> removeTrackFromPlaylist(String playlistId, String trackId) async {
    final m = Map<String, dynamic>.from(_playlists.get(playlistId) as Map);
    final ids = List<String>.from(m['trackIds'] as List? ?? const []);
    ids.remove(trackId);
    m['trackIds'] = ids;
    await _playlists.put(playlistId, m);
  }

  // ---- Liked songs ----

  bool isLiked(String trackId) => _liked.containsKey(trackId);

  Future<void> toggleLiked(Track track) async {
    await cacheTrack(track);
    if (_liked.containsKey(track.id)) {
      await _liked.delete(track.id);
    } else {
      await _liked.put(track.id, DateTime.now().millisecondsSinceEpoch);
    }
  }

  List<Track> getLiked() {
    final entries = _liked.keys.map((k) => MapEntry(k as String, _liked.get(k) as int)).toList()
      ..sort((a, b) => b.value.compareTo(a.value));
    return entries.map((e) => _trackFromId(e.key)).whereType<Track>().toList();
  }

  // ---- Recently played ----

  Future<void> addRecent(Track track) async {
    await cacheTrack(track);
    await _recent.put(track.id, DateTime.now().millisecondsSinceEpoch);
  }

  List<Track> getRecent({int limit = 20}) {
    final entries = _recent.keys.map((k) => MapEntry(k as String, _recent.get(k) as int)).toList()
      ..sort((a, b) => b.value.compareTo(a.value));
    return entries.take(limit).map((e) => _trackFromId(e.key)).whereType<Track>().toList();
  }

  // ---- Play counts ----

  Future<void> countPlay(Track track) async {
    await cacheTrack(track);
    final count = (_plays.get(track.id) as int? ?? 0) + 1;
    await _plays.put(track.id, count);
  }

  /// Songs played at least twice, most played first.
  List<Track> getMostPlayed({int limit = 50}) {
    final entries = _plays.keys
        .map((k) => MapEntry(k as String, _plays.get(k) as int))
        .where((e) => e.value >= 2)
        .toList()
      ..sort((a, b) => b.value.compareTo(a.value));
    return entries.take(limit).map((e) => _trackFromId(e.key)).whereType<Track>().toList();
  }

  int playCount(String trackId) => _plays.get(trackId) as int? ?? 0;

  // ---- Recent searches ----

  Future<void> addSearch(String query) async {
    final q = query.trim();
    if (q.isEmpty) return;
    await _searches.put(q.toLowerCase(), {'q': q, 'at': DateTime.now().millisecondsSinceEpoch});
    // Keep the list short.
    final keys = _searchEntries().map((e) => e.key).toList();
    for (final k in keys.skip(12)) {
      await _searches.delete(k);
    }
  }

  List<MapEntry<String, Map>> _searchEntries() =>
      _searches.keys.map((k) => MapEntry(k as String, Map.from(_searches.get(k) as Map))).toList()
        ..sort((a, b) => (b.value['at'] as int).compareTo(a.value['at'] as int));

  List<String> getSearches({int limit = 8}) => _searchEntries().take(limit).map((e) => e.value['q'] as String).toList();

  Future<void> removeSearch(String query) => _searches.delete(query.trim().toLowerCase());

  Future<void> clearSearches() => _searches.clear();

  // ---- Listening session (resume after restart) ----

  Future<void> saveSession(List<Track> queue, int index, Duration position) async {
    for (final t in queue) {
      if (!_tracks.containsKey(t.id)) await cacheTrack(t);
    }
    await _session.put('state', {
      'ids': queue.map((t) => t.id).toList(),
      'index': index,
      'positionMs': position.inMilliseconds,
    });
  }

  Future<void> savePosition(Duration position) async {
    final raw = _session.get('state');
    if (raw == null) return;
    final m = Map<String, dynamic>.from(raw as Map);
    m['positionMs'] = position.inMilliseconds;
    await _session.put('state', m);
  }

  ({List<Track> queue, int index, Duration position})? loadSession() {
    final raw = _session.get('state');
    if (raw == null) return null;
    final m = Map<String, dynamic>.from(raw as Map);
    final queue = List<String>.from(m['ids'] as List? ?? const []).map(_trackFromId).whereType<Track>().toList();
    final index = m['index'] as int? ?? -1;
    if (queue.isEmpty || index < 0 || index >= queue.length) return null;
    return (queue: queue, index: index, position: Duration(milliseconds: m['positionMs'] as int? ?? 0));
  }
}

class _PlaylistWithMeta {
  final Playlist playlist;
  final int createdAt;
  _PlaylistWithMeta({required this.playlist, required this.createdAt});
}
