import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:sonivo/models/track.dart';
import 'package:sonivo/services/library_service.dart';

Track _t(int i) => Track(
      id: 'saavn:$i',
      title: 'Song $i',
      artist: 'Artist',
      thumbnailUrl: '',
      duration: const Duration(minutes: 3),
    );

void main() {
  final lib = LibraryService.instance;

  setUpAll(() async {
    final dir = await Directory.systemTemp.createTemp('swar_test');
    await lib.init(path: dir.path);
  });

  test('recent searches are newest first, without duplicates', () async {
    await lib.addSearch('arijit');
    await lib.addSearch('Katy Perry');
    await lib.addSearch('ARIJIT');
    expect(lib.getSearches(), ['ARIJIT', 'Katy Perry']);
    await lib.removeSearch('katy perry');
    expect(lib.getSearches(), ['ARIJIT']);
    await lib.clearSearches();
    expect(lib.getSearches(), isEmpty);
  });

  test('most played lists songs played twice or more, most first', () async {
    for (var i = 0; i < 3; i++) {
      await lib.countPlay(_t(1));
    }
    await lib.countPlay(_t(2));
    await lib.countPlay(_t(2));
    await lib.countPlay(_t(3));
    expect(lib.getMostPlayed().map((t) => t.id), ['saavn:1', 'saavn:2']);
  });

  test('the listening session comes back after a restart', () async {
    await lib.saveSession([_t(4), _t(5), _t(6)], 1, const Duration(seconds: 42));
    await lib.savePosition(const Duration(seconds: 50));
    final s = lib.loadSession()!;
    expect(s.queue.map((t) => t.id), ['saavn:4', 'saavn:5', 'saavn:6']);
    expect(s.index, 1);
    expect(s.position, const Duration(seconds: 50));
  });
}
