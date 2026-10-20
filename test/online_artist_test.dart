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
        {
          'id': 'saavn:s1',
          'title': 'Kesariya',
          'artists': [
            {'name': 'Arijit Singh'}
          ],
          'durationSec': 268
        },
      ],
    });
    expect(a.name, 'Arijit Singh');
    expect(a.imageUrl, 'https://img/a-big.jpg');
    expect(a.topSongs.single.title, 'Kesariya');
  });

  test('reads the search result image key and song count', () {
    final a = OnlineArtist.fromJson({
      'id': 'saavn:1',
      'name': 'A',
      'image': [
        {'url': 'https://img/a.jpg'}
      ],
      'songCount': 12,
    });
    expect(a.imageUrl, 'https://img/a.jpg');
    expect(a.songCount, 12);
  });

  test('hides artists with no songs and sorts by song count', () {
    final ranked = rankArtists([
      OnlineArtist(id: '1', name: 'Few', songCount: 3),
      OnlineArtist(id: '2', name: 'None', songCount: 0),
      OnlineArtist(id: '3', name: 'Unknown'),
      OnlineArtist(id: '4', name: 'Many', songCount: 300),
    ]);
    expect(ranked.map((a) => a.name), ['Many', 'Few', 'Unknown']);
  });
}
