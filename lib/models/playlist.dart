import 'track.dart';

class Playlist {
  final String id;
  final String name;
  final List<Track> tracks;

  Playlist({required this.id, required this.name, List<Track>? tracks})
      : tracks = tracks ?? const [];

  Playlist copyWith({String? id, String? name, List<Track>? tracks}) =>
      Playlist(id: id ?? this.id, name: name ?? this.name, tracks: tracks ?? this.tracks);
}
