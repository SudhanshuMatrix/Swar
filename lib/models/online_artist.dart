import 'track.dart';

/// A singer or artist from the catalog, as returned by
/// /v1/search?type=artists and /v1/artists/{id}.
class OnlineArtist {
  final String id;
  final String name;
  final String imageUrl;
  final List<Track> topSongs;

  OnlineArtist({required this.id, required this.name, this.imageUrl = '', this.topSongs = const []});

  factory OnlineArtist.fromJson(Map<String, dynamic> json) {
    final images = (json['images'] as List?)?.whereType<Map>().toList() ?? const [];
    return OnlineArtist(
      id: json['id'] as String,
      name: (json['name'] as String?) ?? 'Artist',
      imageUrl: images.isEmpty ? '' : (images.last['url'] as String? ?? ''),
      topSongs: ((json['topSongs'] as List?) ?? const [])
          .whereType<Map<String, dynamic>>()
          .map(Track.fromJson)
          .toList(),
    );
  }
}
