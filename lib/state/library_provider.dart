import 'package:flutter/foundation.dart';

import '../models/playlist.dart';
import '../models/track.dart';
import '../services/library_service.dart';

class LibraryProvider extends ChangeNotifier {
  List<Playlist> playlists = [];
  List<Track> liked = [];
  List<Track> recent = [];

  void refresh() {
    playlists = LibraryService.instance.getPlaylists();
    liked = LibraryService.instance.getLiked();
    recent = LibraryService.instance.getRecent();
    notifyListeners();
  }

  Future<void> createPlaylist(String name) async {
    await LibraryService.instance.createPlaylist(name);
    refresh();
  }

  Future<void> deletePlaylist(String id) async {
    await LibraryService.instance.deletePlaylist(id);
    refresh();
  }

  Future<void> renamePlaylist(String id, String newName) async {
    await LibraryService.instance.renamePlaylist(id, newName);
    refresh();
  }

  Future<void> addToPlaylist(String playlistId, Track track) async {
    await LibraryService.instance.addTrackToPlaylist(playlistId, track);
    refresh();
  }

  Future<void> removeFromPlaylist(String playlistId, String trackId) async {
    await LibraryService.instance.removeTrackFromPlaylist(playlistId, trackId);
    refresh();
  }

  Future<void> toggleLiked(Track track) async {
    await LibraryService.instance.toggleLiked(track);
    refresh();
  }

  bool isLiked(String trackId) => LibraryService.instance.isLiked(trackId);
}
