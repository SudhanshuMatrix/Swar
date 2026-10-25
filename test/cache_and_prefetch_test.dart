import 'dart:async';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:hive/hive.dart';
import 'package:sonivo/models/track.dart';
import 'package:sonivo/services/api_service.dart';
import 'package:sonivo/services/deep_search_service.dart';
import 'package:sonivo/services/prefetch_planner.dart';
import 'package:sonivo/services/ttl_cache.dart';

Track _t(String id, [String? title]) =>
    Track(id: id, title: title ?? id, artist: 'a', thumbnailUrl: '', duration: Duration.zero);

/// Counts deep searches; can be told to fail.
class _FakeApi extends ApiService {
  _FakeApi() : super(baseUrl: 'http://test.invalid');
  int full = 0, catalogOnly = 0;
  bool fail = false;
  @override
  Future<DeepSearchResult> deepSearch(String query, {int limit = 10, bool youtube = true}) async {
    if (fail) throw ApiException(503, 'upstream_error', 'down');
    youtube ? full++ : catalogOnly++;
    return DeepSearchResult([_t('deezer:1', 'Catalog Song'), _t('deezer:2', 'Shared')], 'notice');
  }
}

void main() {
  _noSelfAwaitingRemove();

  group('TtlCache', () {
    test('a miss completes (regression: it waited for itself forever)', () async {
      final c = TtlCache<String, int>(maxEntries: 10, fresh: const Duration(minutes: 1), stale: const Duration(minutes: 5));
      final hit = await c.get('k', () async => 42).timeout(const Duration(seconds: 2));
      expect(hit.value, 42);
      expect(c.isLoading('k'), isFalse);
    });

    test('fresh hit, stale-while-revalidate, expiry, errors not stored', () async {
      var now = DateTime(2026);
      final c = TtlCache<String, int>(maxEntries: 10, fresh: const Duration(minutes: 1), stale: const Duration(minutes: 5), clock: () => now);
      var loads = 0;
      Future<int> load() async => ++loads;

      expect((await c.get('k', load)).value, 1);
      expect((await c.get('k', load)).value, 1);
      expect(loads, 1);

      now = now.add(const Duration(minutes: 2));
      final refreshed = Completer<int>();
      final hit = await c.get('k', load, onRefreshed: refreshed.complete);
      expect(hit.stale, isTrue);
      expect(hit.value, 1, reason: 'stale value returned at once');
      expect(await refreshed.future, 2);

      now = now.add(const Duration(minutes: 10));
      expect((await c.get('k', load)).value, 3, reason: 'expired: loaded again');

      Future<int> failing() async => throw StateError('down');
      await expectLater(c.get('other', failing), throwsStateError);
      expect(c.peek('other'), isNull);
    });

    test('concurrent misses share one load', () async {
      final c = TtlCache<String, int>(maxEntries: 10, fresh: const Duration(minutes: 1), stale: const Duration(minutes: 1));
      var loads = 0;
      final gate = Completer<void>();
      Future<int> load() async {
        loads++;
        await gate.future;
        return 7;
      }

      final all = Future.wait(List.generate(5, (_) => c.get('k', load)));
      gate.complete();
      expect((await all).map((h) => h.value), everyElement(7));
      expect(loads, 1);
    });

    test('least recently used entry is evicted', () async {
      final c = TtlCache<String, int>(maxEntries: 2, fresh: const Duration(hours: 1), stale: const Duration(hours: 1));
      c.put('a', 1);
      c.put('b', 2);
      c.peek('a');
      await c.get('a', () async => 0); // touch a
      c.put('c', 3);
      expect(c.peek('b'), isNull);
      expect(c.peek('a'), 1);
      expect(c.length, 2);
    });
  });

  group('Slots', () {
    test('limits concurrency and keeps a slot for listeners', () async {
      final s = Slots(2);
      final gate = Completer<void>();
      final first = s.run(() => gate.future);
      expect(s.running, 1);
      // One slot left: background work is skipped so a listener can use it.
      expect(await s.tryRunBackground(() async => 'bg'), isNull);
      var started = false;
      final second = s.run(() async => started = true);
      await second;
      expect(started, isTrue);
      gate.complete();
      await first;
      expect(s.running, 0);
      expect(await s.tryRunBackground(() async => 'bg'), 'bg', reason: 'idle: background runs');
    });
  });

  group('upcomingIndexes', () {
    test('next first, two at most, never the current song', () {
      expect(upcomingIndexes(length: 5, current: 1, count: 2), [2, 3]);
      expect(upcomingIndexes(length: 5, current: 1, count: 1), [2]);
      expect(upcomingIndexes(length: 5, current: 4, count: 2), isEmpty);
      expect(upcomingIndexes(length: 5, current: 4, count: 2, repeatAll: true), [0, 1]);
      expect(upcomingIndexes(length: 2, current: 1, count: 2, repeatAll: true), [0]);
      expect(upcomingIndexes(length: 5, current: 1, count: 0), isEmpty, reason: 'prefetch off');
      expect(upcomingIndexes(length: 1, current: 0, count: 2), isEmpty);
    });

    test('shuffle prepares only the song picked to play next', () {
      expect(upcomingIndexes(length: 5, current: 1, count: 2, shuffleNext: 3), [3]);
      expect(upcomingIndexes(length: 5, current: 1, count: 2, shuffleNext: 1), isEmpty);
      expect(upcomingIndexes(length: 5, current: 1, count: 2, shuffleNext: 9), isEmpty);
    });
  });

  group('DeepSearchService', () {
    setUpAll(() => Hive.init(Directory.systemTemp.createTempSync('swar-deep').path));

    test('device YouTube search plus catalog-only server search, merged', () async {
      final api = _FakeApi();
      var deviceRuns = 0;
      final svc = DeepSearchService(api, deviceReady: () => true, device: (q, limit) async {
        deviceRuns++;
        return [_t('yt:AAAAAAAAAAA', 'Shared'), _t('yt:BBBBBBBBBBB', 'Rare')];
      });
      final hit = await svc.search('Rare  Song');
      expect(hit.value.tracks.map((t) => t.id), ['yt:AAAAAAAAAAA', 'yt:BBBBBBBBBBB', 'deezer:1']);
      expect(api.catalogOnly, 1);
      expect(api.full, 0, reason: 'the server does not run yt-dlp when the device did');

      await svc.search('rare song'); // same key: case and spaces folded
      expect(deviceRuns, 1);
    });

    test('falls back to the server when yt-dlp is missing or fails', () async {
      final api = _FakeApi();
      final missing = DeepSearchService(api, deviceReady: () => false, device: (q, l) async => []);
      await missing.search('song one');
      expect(api.full, 1);

      final failing = DeepSearchService(api, deviceReady: () => true, device: (q, l) async => throw Exception('x'));
      await failing.search('song two');
      expect(api.full, 2);
    });

    test('bad input never reaches yt-dlp or the server', () async {
      final api = _FakeApi();
      var ran = false;
      final svc = DeepSearchService(api, deviceReady: () => true, device: (q, l) async {
        ran = true;
        return [];
      });
      await expectLater(svc.search('https://evil.example/x'), throwsA(anything));
      expect(ran, isFalse);
      expect(api.full + api.catalogOnly, 0);
    });

    test('errors are not cached', () async {
      final api = _FakeApi()..fail = true;
      final svc = DeepSearchService(api, deviceReady: () => false);
      await expectLater(svc.search('song'), throwsA(isA<ApiException>()));
      api.fail = false;
      expect((await svc.search('song')).value.tracks, isNotEmpty);
    });
  });
}

/// `f.whenComplete(() => map.remove(key))` where the map holds `f` makes `f`
/// wait for itself forever: remove returns `f`, and whenComplete waits for
/// a returned future. It hung every Deep Search and every yt-dlp run in
/// 1.2.7.
void _noSelfAwaitingRemove() {
  test('no whenComplete callback returns map.remove(...)', () {
    final bad = RegExp(r'whenComplete\(\(\)\s*=>\s*[\w.]+\.remove\(');
    final hits = <String>[];
    for (final f in Directory('lib').listSync(recursive: true).whereType<File>()) {
      if (!f.path.endsWith('.dart')) continue;
      final lines = f.readAsLinesSync();
      for (var i = 0; i < lines.length; i++) {
        if (bad.hasMatch(lines[i])) hits.add('${f.path}:${i + 1}');
      }
    }
    expect(hits, isEmpty);
  });
}
