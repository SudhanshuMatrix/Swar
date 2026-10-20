import 'package:flutter/foundation.dart';

import '../models/playlist.dart';
import '../models/track.dart';
import '../services/library_service.dart';

class LibraryProvider extends ChangeNotifier {
  List<Playlist> playlists = [];
  List<Track> liked = [];
  List<Track> recent = [];
  List<Track> mostPlayed = [];
  List<String> searches = [];

  void refresh() {
    playlists = LibraryService.instance.getPlaylists();
    liked = LibraryService.instance.getLiked();
    recent = LibraryService.instance.getRecent();
    mostPlayed = LibraryService.instance.getMostPlayed();
    searches = LibraryService.instance.getSearches();
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

  Future<void> addSearch(String query) async {
    await LibraryService.instance.addSearch(query);
    refresh();
  }

  Future<void> removeSearch(String query) async {
    await LibraryService.instance.removeSearch(query);
    refresh();
  }

  Future<void> clearSearches() async {
    await LibraryService.instance.clearSearches();
    refresh();
  }

  bool isLiked(String trackId) => LibraryService.instance.isLiked(trackId);
}
