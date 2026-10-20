import 'track.dart';

/// A singer or artist from the catalog, as returned by
/// /v1/search?type=artists and /v1/artists/{id}.
class OnlineArtist {
  final String id;
  final String name;
  final String imageUrl;
  final List<Track> topSongs;

  /// How many songs the artist has, when the server counted them.
  final int? songCount;

  OnlineArtist({required this.id, required this.name, this.imageUrl = '', this.topSongs = const [], this.songCount});

  factory OnlineArtist.fromJson(Map<String, dynamic> json) {
    // Profiles use "images"; search results use "image".
    final images = ((json['images'] ?? json['image']) as List?)?.whereType<Map>().toList() ?? const [];
    return OnlineArtist(
      id: json['id'] as String,
      name: (json['name'] as String?) ?? 'Artist',
      imageUrl: images.isEmpty ? '' : (images.last['url'] as String? ?? ''),
      topSongs:
          ((json['topSongs'] as List?) ?? const []).whereType<Map<String, dynamic>>().map(Track.fromJson).toList(),
      songCount: (json['songCount'] as num?)?.toInt(),
    );
  }
}

/// Hides artists known to have no songs and lists the biggest catalogs
/// first. Artists without a count keep their order after the counted ones.
List<OnlineArtist> rankArtists(List<OnlineArtist> artists) {
  final kept = artists.where((a) => a.songCount != 0).toList();
  final indexed = kept.asMap().entries.toList()
    ..sort((a, b) {
      final byCount = (b.value.songCount ?? -1).compareTo(a.value.songCount ?? -1);
      return byCount != 0 ? byCount : a.key.compareTo(b.key);
    });
  return indexed.map((e) => e.value).toList();
}
