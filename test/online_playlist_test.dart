import 'package:flutter_test/flutter_test.dart';
import 'package:sonivo/models/online_playlist.dart';

void main() {
  test('parses a playlist from the /v1 API', () {
    final p = OnlinePlaylist.fromJson({
      'id': 'saavn:123',
      'name': 'Bollywood Hits',
      'songCount': 2,
      'images': [
        {'quality': '50x50', 'url': 'https://img/small.jpg'},
        {'quality': '500x500', 'url': 'https://img/big.jpg'},
      ],
      'tracks': [
        {'id': 'saavn:a', 'title': 'Song A', 'artists': [{'name': 'X'}], 'durationSec': 200},
      ],
    });
    expect(p.name, 'Bollywood Hits');
    expect(p.imageUrl, 'https://img/big.jpg');
    expect(p.songCount, 2);
    expect(p.tracks.single.artist, 'X');
  });

  test('missing images and tracks are fine', () {
    final p = OnlinePlaylist.fromJson({'id': 'saavn:1', 'name': 'Empty'});
    expect(p.imageUrl, '');
    expect(p.tracks, isEmpty);
  });
}
