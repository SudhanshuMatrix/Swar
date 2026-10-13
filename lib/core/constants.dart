/// Base URL of the Sonivo backend (see /backend — it wraps yt-dlp over HTTP).
///
/// Override at build/run time, since "localhost" means something different
/// on every platform:
///   flutter run -d chrome   --dart-define=API_BASE_URL=http://127.0.0.1:8765
///   flutter run -d android  --dart-define=API_BASE_URL=http://10.0.2.2:8765   (emulator)
///   flutter run -d windows  --dart-define=API_BASE_URL=http://127.0.0.1:8765
/// For a physical phone, use your computer's LAN IP, e.g. http://192.168.1.50:8765
class ApiConfig {
  static const String baseUrl = String.fromEnvironment(
    'API_BASE_URL',
    defaultValue: 'http://127.0.0.1:8765',
  );
}
