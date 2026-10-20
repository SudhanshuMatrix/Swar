import 'track.dart';

/// A public playlist from the catalog (e.g. JioSaavn), as returned by
/// /v1/search?type=playlists and /v1/playlists/{id}.
class OnlinePlaylist {
  final String id;
  final String name;
  final String imageUrl;
  final int songCount;
  final List<Track> tracks;

  OnlinePlaylist({
    required this.id,
    required this.name,
    this.imageUrl = '',
    this.songCount = 0,
    this.tracks = const [],
  });

  factory OnlinePlaylist.fromJson(Map<String, dynamic> json) {
    final images = (json['images'] as List?)?.whereType<Map>().toList() ?? const [];
    final tracks = ((json['tracks'] as List?) ?? const [])
        .whereType<Map<String, dynamic>>()
        .map(Track.fromJson)
        .toList();
    return OnlinePlaylist(
      id: json['id'] as String,
      name: (json['name'] as String?) ?? 'Playlist',
      imageUrl: images.isEmpty ? '' : (images.last['url'] as String? ?? ''),
      songCount: (json['songCount'] as num?)?.toInt() ?? tracks.length,
      tracks: tracks,
    );
  }
}
