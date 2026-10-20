import 'package:audio_service/audio_service.dart';
import 'package:flutter/material.dart';
import 'package:provider/provider.dart';

import 'app.dart';
import 'core/constants.dart';
import 'services/api_service.dart';
import 'services/audio_handler.dart';
import 'services/download_service.dart';
import 'services/jam_service.dart';
import 'services/library_service.dart';
import 'state/library_provider.dart';
import 'state/player_provider.dart';

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();
  await LibraryService.instance.init();

  // audio_service wires OS-level lock-screen/notification media controls on
  // Android & iOS, which is what keeps audio playing when the screen closes.
  // It isn't meaningful on desktop/web, so fall back to a plain handler there.
  late final SonivoAudioHandler audioHandler;
  try {
    audioHandler = await AudioService.init(
      builder: () => SonivoAudioHandler(),
      config: const AudioServiceConfig(
        androidNotificationChannelId: 'com.sonivo.audio',
        androidNotificationChannelName: 'Swar playback',
        androidNotificationOngoing: true,
        androidStopForegroundOnPause: true,
      ),
    );
  } catch (_) {
    audioHandler = SonivoAudioHandler();
  }

  await ApiConfig.load();
  final api = ApiService();
  final downloads = DownloadService(api: api);
  await downloads.init();
  final jam = JamController(api: api);
  final player = PlayerProvider(api: api, audioHandler: audioHandler)
    ..downloads = downloads
    ..jam = jam;
  jam.onPlaybackChanged = player.syncToJam;

  runApp(
    MultiProvider(
      providers: [
        Provider<ApiService>.value(value: api),
        ChangeNotifierProvider.value(value: player),
        ChangeNotifierProvider.value(value: downloads),
        ChangeNotifierProvider.value(value: jam),
        ChangeNotifierProvider(create: (_) => LibraryProvider()..refresh()),
      ],
      child: const SonivoApp(),
    ),
  );
}
