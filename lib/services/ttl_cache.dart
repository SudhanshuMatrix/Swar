import 'dart:async';
import 'dart:collection';

/// What [TtlCache.get] returns: the value and whether it is past its fresh
/// time (still usable, being refreshed in the background).
class CacheHit<V> {
  final V value;
  final bool stale;
  const CacheHit(this.value, {this.stale = false});
}

/// A small in-memory cache: bounded (least recently used goes first), with
/// a fresh time and a longer stale time, stale-while-revalidate, and one
/// load per key at a time.
///
/// - Fresh entries are returned without loading.
/// - Stale entries are returned at once and refreshed in the background;
///   [onRefreshed] hears about the new value.
/// - Expired or missing entries are loaded; concurrent callers share the
///   load.
/// - Errors are never stored. A failed background refresh keeps the stale
///   value until it expires.
class TtlCache<K, V> {
  final int maxEntries;
  final Duration fresh;
  final Duration stale;
  final DateTime Function() _now;

  final _entries = LinkedHashMap<K, _Entry<V>>();
  final _loading = <K, Future<V>>{};

  TtlCache({required this.maxEntries, required this.fresh, required this.stale, DateTime Function()? clock})
      : assert(maxEntries > 0),
        assert(stale >= fresh),
        _now = clock ?? DateTime.now;

  int get length => _entries.length;

  /// Returns the cached value or loads it.
  Future<CacheHit<V>> get(K key, Future<V> Function() load, {void Function(V value)? onRefreshed}) async {
    final e = _entries.remove(key);
    if (e != null) {
      final age = _now().difference(e.storedAt);
      if (age < stale) {
        _entries[key] = e; // most recently used
        if (age < fresh) return CacheHit(e.value);
        unawaited(_load(key, load).then((v) => onRefreshed?.call(v), onError: (_) {}));
        return CacheHit(e.value, stale: true);
      }
    }
    return CacheHit(await _load(key, load));
  }

  /// The value without loading, if it has not expired.
  V? peek(K key) {
    final e = _entries[key];
    if (e == null || _now().difference(e.storedAt) >= stale) return null;
    return e.value;
  }

  /// Whether a load for [key] is running.
  bool isLoading(K key) => _loading.containsKey(key);

  void put(K key, V value, {DateTime? storedAt}) {
    _entries.remove(key);
    _entries[key] = _Entry(value, storedAt ?? _now());
    while (_entries.length > maxEntries) {
      _entries.remove(_entries.keys.first);
    }
  }

  void remove(K key) => _entries.remove(key);

  void clear() => _entries.clear();

  /// Entries that have not expired, oldest first (for saving to disk).
  Iterable<MapEntry<K, ({V value, DateTime storedAt})>> get live => _entries.entries
      .where((e) => _now().difference(e.value.storedAt) < stale)
      .map((e) => MapEntry(e.key, (value: e.value.value, storedAt: e.value.storedAt)));

  Future<V> _load(K key, Future<V> Function() load) {
    final running = _loading[key];
    if (running != null) return running;
    final f = Future.sync(load).then((v) {
      put(key, v);
      return v;
    }).whenComplete(() => _loading.remove(key));
    _loading[key] = f;
    return f;
  }
}

class _Entry<V> {
  final V value;
  final DateTime storedAt;
  _Entry(this.value, this.storedAt);
}

/// Runs at most [max] tasks at a time. Background tasks never wait: they
/// run only when a slot is free and nobody is waiting, and otherwise are
/// skipped (they return null).
class Slots {
  final int max;
  int _used = 0;
  final _waiters = Queue<Completer<void>>();
  Slots(this.max) : assert(max > 0);

  int get running => _used;

  Future<T> run<T>(Future<T> Function() task) async {
    if (_used >= max) {
      final c = Completer<void>();
      _waiters.add(c);
      await c.future;
    } else {
      _used++;
    }
    try {
      return await task();
    } finally {
      if (_waiters.isNotEmpty) {
        _waiters.removeFirst().complete(); // hand the slot straight over
      } else {
        _used--;
      }
    }
  }

  /// Runs [task] only if a slot is free right now (and, with more than one
  /// slot, leaves one free for listeners); otherwise returns null at once.
  Future<T?> tryRunBackground<T>(Future<T> Function() task) async {
    final reserve = max > 1 ? 1 : 0;
    if (_waiters.isNotEmpty || _used >= max - reserve) return null;
    _used++;
    try {
      return await task();
    } finally {
      if (_waiters.isNotEmpty) {
        _waiters.removeFirst().complete();
      } else {
        _used--;
      }
    }
  }
}
