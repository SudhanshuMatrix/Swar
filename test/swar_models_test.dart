import 'package:flutter_test/flutter_test.dart';
import 'package:sonivo/models/track.dart';
import 'package:sonivo/screens/downloads_screen.dart';
import 'package:sonivo/services/download_service.dart';

void main() {
  test('Track parses the legacy shape the old app used', () {
    final t = Track.fromJson({
      'id': 'saavn:s1',
      'title': 'Kesariya',
      'artist': 'Arijit Singh',
      'thumbnail': 'https://img/500.jpg',
      'duration': 268,
      'provider': 'saavn',
      'playable': false,
      'downloadable': true,
    });
    expect(t.artist, 'Arijit Singh');
    expect(t.duration, const Duration(seconds: 268));
    expect(t.playable, isFalse);
    expect(t.downloadable, isTrue);
    expect(t.isDeepSearchResult, isFalse);
  });

  test('Track parses the /v1 shape', () {
    final t = Track.fromJson({
      'id': 'yt:dQw4w9WgXcQ',
      'provider': 'yt',
      'title': 'Song',
      'artists': [
        {'id': 'a', 'name': 'One'},
        {'id': 'b', 'name': 'Two'},
      ],
      'images': [
        {'quality': '50x50', 'url': 'https://img/50.jpg'},
        {'quality': '500x500', 'url': 'https://img/500.jpg'},
      ],
      'album': {'id': 'x', 'name': 'Album'},
      'durationSec': 200,
      'playable': true,
      'download': {'eligible': false, 'reason': 'downloads_not_enabled_for_source'},
    });
    expect(t.artist, 'One, Two');
    expect(t.thumbnailUrl, 'https://img/500.jpg');
    expect(t.album, 'Album');
    expect(t.duration.inSeconds, 200);
    expect(t.downloadable, isFalse);
    expect(t.isDeepSearchResult, isTrue);
  });

  test('Saved tracks from before the update still load', () {
    final t = Track.fromMap({'id': 'dQw4w9WgXcQ', 'title': 'Old', 'artist': 'A', 'thumbnail': '', 'duration': 10});
    expect(t.playable, isTrue);
    expect(Track.fromMap(t.toMap()), t);
  });

  test('Download items round-trip through storage', () {
    final item = DownloadItem(
      track: const Track(id: 'saavn:s1', title: 'T', artist: 'A', thumbnailUrl: '', duration: Duration(seconds: 5)),
      status: DownloadStatus.paused,
      bytesDone: 50,
      bytesTotal: 200,
      path: '/x/dl.audio',
    );
    final back = DownloadItem.fromMap(item.toMap());
    expect(back.status, DownloadStatus.paused);
    expect(back.progress, 0.25);
    expect(back.path, '/x/dl.audio');
  });

  test('formatBytes', () {
    expect(formatBytes(512), '512 B');
    expect(formatBytes(5 * 1024 * 1024), '5.0 MB');
  });
}
