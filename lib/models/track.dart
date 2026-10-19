class Track {
  /// Swar internal id: "saavn:<id>" or "yt:<videoId>". Older saved tracks
  /// may hold a bare YouTube id; the backend still accepts those.
  final String id;
  final String title;
  final String artist;
  final String thumbnailUrl;
  final Duration duration;

  // Additive fields from the Swar backend. Older responses and saved
  // tracks simply leave them at their defaults.
  final String provider;
  final String album;

  /// Whether this server will hand out a stream for the track.
  final bool playable;

  /// Whether this server allows an offline copy of the track.
  final bool downloadable;

  const Track({
    required this.id,
    required this.title,
    required this.artist,
    required this.thumbnailUrl,
    required this.duration,
    this.provider = '',
    this.album = '',
    this.playable = true,
    this.downloadable = false,
  });

  bool get isDeepSearchResult => provider == 'yt' || id.startsWith('yt:');

  /// Parses both the legacy shape ({artist, thumbnail, duration}) and the
  /// /v1 shape ({artists: [...], images: [...], durationSec}).
  factory Track.fromJson(Map<String, dynamic> json) {
    var artist = json['artist'] as String?;
    if (artist == null && json['artists'] is List) {
      artist = (json['artists'] as List)
          .whereType<Map>()
          .map((a) => a['name'])
          .whereType<String>()
          .join(', ');
    }
    var thumbnail = json['thumbnail'] as String?;
    if (thumbnail == null && json['images'] is List && (json['images'] as List).isNotEmpty) {
      final images = (json['images'] as List).whereType<Map>().toList();
      thumbnail = images.isEmpty ? null : images.last['url'] as String?;
    }
    final rawDuration = json['duration'] ?? json['durationSec'];
    final seconds = rawDuration is int ? rawDuration : (rawDuration as num?)?.toInt() ?? 0;
    var album = json['album'];
    if (album is Map) album = album['name'];
    final download = json['download'];
    return Track(
      id: json['id'] as String,
      title: (json['title'] as String?) ?? 'Unknown title',
      artist: artist ?? '',
      thumbnailUrl: thumbnail ?? '',
      duration: Duration(seconds: seconds),
      provider: (json['provider'] as String?) ?? '',
      album: album is String ? album : '',
      playable: (json['playable'] as bool?) ?? true,
      downloadable: (json['downloadable'] as bool?) ?? (download is Map ? download['eligible'] == true : false),
    );
  }

  Map<String, dynamic> toMap() => {
        'id': id,
        'title': title,
        'artist': artist,
        'thumbnail': thumbnailUrl,
        'duration': duration.inSeconds,
        'provider': provider,
        'album': album,
        'playable': playable,
        'downloadable': downloadable,
      };

  factory Track.fromMap(Map<dynamic, dynamic> map) {
    final rawDuration = map['duration'];
    final seconds = rawDuration is int ? rawDuration : (rawDuration as num?)?.toInt() ?? 0;
    return Track(
      id: map['id'] as String,
      title: (map['title'] as String?) ?? 'Unknown title',
      artist: (map['artist'] as String?) ?? '',
      thumbnailUrl: (map['thumbnail'] as String?) ?? '',
      duration: Duration(seconds: seconds),
      provider: (map['provider'] as String?) ?? '',
      album: (map['album'] as String?) ?? '',
      playable: (map['playable'] as bool?) ?? true,
      downloadable: (map['downloadable'] as bool?) ?? false,
    );
  }

  @override
  bool operator ==(Object other) => other is Track && other.id == id;

  @override
  int get hashCode => id.hashCode;
}
