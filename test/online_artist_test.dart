import 'package:flutter_test/flutter_test.dart';
import 'package:sonivo/models/online_artist.dart';

void main() {
  test('parses an artist with top songs', () {
    final a = OnlineArtist.fromJson({
      'id': 'saavn:459320',
      'name': 'Arijit Singh',
      'images': [
        {'quality': '50x50', 'url': 'https://img/a-small.jpg'},
        {'quality': '500x500', 'url': 'https://img/a-big.jpg'},
      ],
      'topSongs': [
        {'id': 'saavn:s1', 'title': 'Kesariya', 'artists': [{'name': 'Arijit Singh'}], 'durationSec': 268},
      ],
    });
    expect(a.name, 'Arijit Singh');
    expect(a.imageUrl, 'https://img/a-big.jpg');
    expect(a.topSongs.single.title, 'Kesariya');
  });
}
