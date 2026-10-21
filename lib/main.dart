import 'package:audio_service/audio_service.dart';
import 'package:just_audio_media_kit/just_audio_media_kit.dart';
import 'dart:ui';

import 'package:flutter/foundation.dart' show defaultTargetPlatform, kIsWeb, TargetPlatform;
import 'package:flutter/material.dart';
import 'package:permission_handler/permission_handler.dart';
import 'package:provider/provider.dart';

import 'app.dart';
import 'core/constants.dart';
import 'core/errors.dart';
import 'services/api_service.dart';
import 'services/audio_handler.dart';
import 'services/download_service.dart';
import 'services/jam_service.dart';
import 'services/library_service.dart';
import 'state/library_provider.dart';
import 'state/player_provider.dart';

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();
  // Errors nothing else caught get a friendly popup instead of failing
  // silently.
  PlatformDispatcher.instance.onError = (error, stack) {
    ErrorPopup.show(error);
    return true;
  };
  // just_audio has no built-in Linux/Windows player; media_kit provides one.
  if (!kIsWeb) JustAudioMediaKit.ensureInitialized(linux: true, windows: true);
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
        // Staying in the foreground while paused keeps the notification up
        // and stops phones that kill background apps (MIUI, HyperOS) from
        // removing it; the notification's close button ends it.
        androidStopForegroundOnPause: false,
        androidNotificationIcon: 'drawable/swar_notif_icon',
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
  final library = LibraryProvider()..refresh();
  player
    ..onTrackStarted = library.refresh
    ..restoreSession();

  // Media notification: the heart likes the current song, close stops it.
  bool currentLiked() {
    final t = player.currentTrack;
    return t != null && library.isLiked(t.id);
  }

  audioHandler
    ..onToggleLike = () async {
      final t = player.currentTrack;
      if (t != null) await library.toggleLiked(t);
    }
    ..onStopped = player.markStopped;
  library.addListener(() => audioHandler.setLiked(currentLiked()));
  player.addListener(() => audioHandler.setLiked(currentLiked()));

  runApp(
    MultiProvider(
      providers: [
        Provider<ApiService>.value(value: api),
        ChangeNotifierProvider.value(value: player),
        ChangeNotifierProvider.value(value: downloads),
        ChangeNotifierProvider.value(value: jam),
        ChangeNotifierProvider.value(value: library),
      ],
      child: const SonivoApp(),
    ),
  );

  // Android 13 and later only show notifications once the user allows them.
  if (!kIsWeb && defaultTargetPlatform == TargetPlatform.android) {
    Permission.notification.request().ignore();
  }
}
