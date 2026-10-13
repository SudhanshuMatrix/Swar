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
  bool _ready = false;

  Future<void> init() async {
    if (_ready) return;
    await Hive.initFlutter();
    _tracks = await Hive.openBox('tracks');
    _playlists = await Hive.openBox('playlists');
    _liked = await Hive.openBox('liked');
    _recent = await Hive.openBox('recent');
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
}

class _PlaylistWithMeta {
  final Playlist playlist;
  final int createdAt;
  _PlaylistWithMeta({required this.playlist, required this.createdAt});
}
