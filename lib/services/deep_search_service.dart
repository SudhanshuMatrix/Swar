import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:hive_flutter/hive_flutter.dart';

import '../models/track.dart';
import 'api_service.dart';
import 'ttl_cache.dart';
import 'ytdlp/device_ytdlp.dart';
import 'ytdlp/ytdlp_input.dart';

/// Deep Search with a cache in front of it.
///
/// The YouTube part runs on this device with yt-dlp when it is installed
/// (YouTube often blocks Swar's server), next to the server's search of
/// the other music catalogs. Without yt-dlp on the device, or when it
/// fails, the server's full Deep Search is used, as before.
///
/// Results are cached by provider namespace, the query (case and spaces
/// folded) and the limit: fresh for [fresh], then shown at once while a
/// background refresh runs, until [stale]. The last [_maxSaved] searches
/// are also kept on disk. Stream URLs are never part of a result.
class DeepSearchService {
  DeepSearchService(this.api, {DateTime Function()? clock, DeepSearchRunner? device, bool Function()? deviceReady})
      : _device = device ?? _defaultDevice,
        _deviceReady = deviceReady ?? (() => DeviceYtDlp.supported && DeviceYtDlp.instance.installed),
        _cache = TtlCache(maxEntries: 60, fresh: fresh, stale: stale, clock: clock);

  final ApiService api;
  final DeepSearchRunner? _device;
  final bool Function() _deviceReady;
  final TtlCache<String, DeepSearchResult> _cache;

  static const fresh = Duration(minutes: 15);
  static const stale = Duration(hours: 12);
  static const _boxName = 'swar_deep_search';
  static const _maxSaved = 40;
  Box? _box;
  bool _restored = false;

  static DeepSearchRunner? get _defaultDevice =>
      DeviceYtDlp.supported ? (q, limit) => DeviceYtDlp.instance.search(q, limit: limit) : null;

  /// The cache key: provider namespace, normalized query, limit.
  static String keyFor(String query, int limit) => 'deep:v2|${normalizeQuery(query).toLowerCase()}|$limit';

  /// Results for [query]. A stale hit returns at once and [onRefreshed]
  /// later receives the refreshed results.
  Future<CacheHit<DeepSearchResult>> search(String query,
      {int limit = 10, void Function(DeepSearchResult)? onRefreshed}) async {
    await _restore();
    final key = keyFor(query, limit);
    final hit = await _cache.get(key, () => _load(query, limit), onRefreshed: (v) {
      unawaited(_save());
      onRefreshed?.call(v);
    });
    unawaited(_save());
    return hit;
  }

  Future<DeepSearchResult> _load(String query, int limit) async {
    // Same input rules as the server, checked here first.
    YtDlpTarget.parse(query);
    if (_device == null || !_deviceReady()) return api.deepSearch(query, limit: limit);

    Object? deviceError;
    final deviceF = _device!(query, limit).then<List<Track>?>((v) => v, onError: (Object e) {
      deviceError = e;
      return null;
    });
    final serverF = api.deepSearch(query, limit: limit, youtube: false).then<DeepSearchResult?>((v) => v,
        onError: (Object e) {
      debugPrint('Swar: catalog Deep Search failed: $e');
      return null;
    });
    final youtube = await deviceF;
    final catalogs = await serverF;
    if (youtube == null) {
      debugPrint('Swar: Deep Search on this device failed: $deviceError');
      // The server's own YouTube search is the fallback.
      return api.deepSearch(query, limit: limit);
    }
    return DeepSearchResult(
      mergeResults(youtube, catalogs?.tracks ?? const []),
      catalogs?.notice ?? 'Deep Search looks on YouTube from this device and in every music catalog main search skips.',
    );
  }

  /// YouTube results first, then catalog songs whose title is not already
  /// listed (same rule as the server).
  static List<Track> mergeResults(List<Track> youtube, List<Track> catalogs) {
    final out = <Track>[];
    final ids = <String>{};
    final titles = <String>{};
    for (final t in [...youtube, ...catalogs]) {
      final title = t.title.toLowerCase();
      if (ids.contains(t.id) || titles.contains(title)) continue;
      ids.add(t.id);
      titles.add(title);
      out.add(t);
    }
    return out;
  }

  Future<void> _restore() async {
    if (_restored) return;
    _restored = true;
    try {
      _box = await Hive.openBox(_boxName);
      final saved = _box!.get('entries');
      if (saved is! List) return;
      for (final e in saved.whereType<Map>()) {
        final tracks = ((e['tracks'] as List?) ?? const [])
            .whereType<Map>()
            .map((m) => Track.fromMap(m))
            .toList();
        _cache.put(e['key'] as String, DeepSearchResult(tracks, (e['notice'] as String?) ?? ''),
            storedAt: DateTime.fromMillisecondsSinceEpoch(e['at'] as int));
      }
    } catch (e) {
      debugPrint('Swar: could not restore Deep Search cache: $e');
    }
  }

  Future<void> _save() async {
    final box = _box;
    if (box == null) return;
    final entries = _cache.live.toList();
    final keep = entries.length > _maxSaved ? entries.sublist(entries.length - _maxSaved) : entries;
    try {
      await box.put('entries', [
        for (final e in keep)
          {
            'key': e.key,
            'at': e.value.storedAt.millisecondsSinceEpoch,
            'notice': e.value.value.notice,
            'tracks': [for (final t in e.value.value.tracks) t.toMap()],
          }
      ]);
    } catch (_) {}
  }
}

/// Searches YouTube on the device (yt-dlp); swappable for tests.
typedef DeepSearchRunner = Future<List<Track>> Function(String query, int limit);
