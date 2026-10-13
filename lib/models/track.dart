class Track {
  final String id; // YouTube video id, also used as the stable key everywhere
  final String title;
  final String artist;
  final String thumbnailUrl;
  final Duration duration;

  const Track({
    required this.id,
    required this.title,
    required this.artist,
    required this.thumbnailUrl,
    required this.duration,
  });

  factory Track.fromJson(Map<String, dynamic> json) {
    final rawDuration = json['duration'];
    final seconds = rawDuration is int ? rawDuration : (rawDuration as num?)?.toInt() ?? 0;
    return Track(
      id: json['id'] as String,
      title: (json['title'] as String?) ?? 'Unknown title',
      artist: (json['artist'] as String?) ?? '',
      thumbnailUrl: (json['thumbnail'] as String?) ?? '',
      duration: Duration(seconds: seconds),
    );
  }

  Map<String, dynamic> toMap() => {
        'id': id,
        'title': title,
        'artist': artist,
        'thumbnail': thumbnailUrl,
        'duration': duration.inSeconds,
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
    );
  }

  @override
  bool operator ==(Object other) => other is Track && other.id == id;

  @override
  int get hashCode => id.hashCode;
}
