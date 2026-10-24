import 'package:flutter/foundation.dart';
import 'package:hive_flutter/hive_flutter.dart';

/// Small app settings kept on the device (Hive). Defaults are conservative
/// on phones, where data may be metered.
class AppSettings extends ChangeNotifier {
  AppSettings._();
  static final AppSettings instance = AppSettings._();

  static const _boxName = 'swar_settings';
  Box? _box;

  /// Opens the settings box. Without it (tests, a failed open) the
  /// defaults are used and nothing is saved.
  Future<void> init() async {
    try {
      _box = await Hive.openBox(_boxName);
    } catch (_) {}
  }

  T _get<T>(String key, T fallback) {
    final v = _box?.get(key);
    return v is T ? v : fallback;
  }

  Future<void> _put(String key, Object? value) async {
    await _box?.put(key, value);
    notifyListeners();
  }

  static bool get _isPhone =>
      !kIsWeb && (defaultTargetPlatform == TargetPlatform.android || defaultTargetPlatform == TargetPlatform.iOS);

  /// Prepare the next song in the queue while the current one plays.
  bool get prefetchEnabled => _get('prefetch', true);
  Future<void> setPrefetchEnabled(bool v) => _put('prefetch', v);

  /// Prepare only the next song (not two) and skip artwork. On by default
  /// on phones.
  bool get dataSaver => _get('dataSaver', _isPhone);
  Future<void> setDataSaver(bool v) => _put('dataSaver', v);

  /// How many upcoming songs to prepare.
  int get prefetchCount => !prefetchEnabled ? 0 : (dataSaver ? 1 : 2);

  /// Whether the first-launch yt-dlp setup was completed or skipped.
  bool get ytdlpOnboarded => _get('ytdlpOnboarded', false);
  Future<void> setYtdlpOnboarded(bool v) => _put('ytdlpOnboarded', v);

  /// When yt-dlp updates started failing (cleared by a success).
  DateTime? get ytdlpFailingSince {
    final ms = _get<int?>('ytdlpFailingSince', null);
    return ms == null ? null : DateTime.fromMillisecondsSinceEpoch(ms);
  }

  Future<void> setYtdlpFailingSince(DateTime? at) => _put('ytdlpFailingSince', at?.millisecondsSinceEpoch);
}
