import 'package:flutter/services.dart' show rootBundle;

/// Base URL of the Swar Go backend (see /server).
///
/// Set it once in the `.env` file at the project root:
///   API_BASE_URL=https://your-service.onrender.com
/// That file is bundled into every build, so a plain `flutter run` or
/// `flutter build apk` uses it. A `--dart-define=API_BASE_URL=...` flag still
/// wins when given. For a local server use http://10.0.2.2:8765 (Android
/// emulator) or your computer's LAN IP (physical phone).
class ApiConfig {
  static const _fromDefine = String.fromEnvironment('API_BASE_URL');
  static const _fallback = 'http://127.0.0.1:8765';

  static String baseUrl = _fromDefine.isNotEmpty ? _fromDefine : _fallback;

  /// Reads API_BASE_URL from the bundled `.env`. Call once before using
  /// [baseUrl].
  static Future<void> load() async {
    if (_fromDefine.isNotEmpty) return;
    try {
      final text = await rootBundle.loadString('.env');
      for (final line in text.split('\n')) {
        final l = line.trim();
        if (l.isEmpty || l.startsWith('#') || !l.contains('=')) continue;
        final key = l.substring(0, l.indexOf('=')).trim();
        var value = l.substring(l.indexOf('=') + 1).trim();
        if (value.length >= 2 && (value.startsWith('"') || value.startsWith("'"))) {
          value = value.substring(1, value.length - 1);
        }
        if (key == 'API_BASE_URL' && value.isNotEmpty) {
          baseUrl = value.endsWith('/') ? value.substring(0, value.length - 1) : value;
        }
      }
    } catch (_) {
      // No .env bundled: keep the fallback.
    }
  }
}
