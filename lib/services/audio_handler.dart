import 'package:audio_service/audio_service.dart';
import 'package:just_audio/just_audio.dart';

/// Wraps a just_audio [AudioPlayer] as an [AudioHandler] so the OS can show
/// lock-screen / notification media controls and keep audio alive when the
/// screen turns off or the app goes to the background (Android/iOS).
///
/// On platforms where audio_service's native integration isn't available
/// (desktop/web), this class still works as a plain audio engine — see the
/// try/catch fallback in main.dart.
class SonivoAudioHandler extends BaseAudioHandler with QueueHandler, SeekHandler {
  final AudioPlayer player = AudioPlayer();

  Future<void> Function()? onSkipToNext;
  Future<void> Function()? onSkipToPrevious;

  SonivoAudioHandler() {
    player.playbackEventStream.listen(_broadcastState, onError: (Object e, StackTrace st) {});
  }

  Future<void> loadUrl(String url, MediaItem item) async {
    mediaItem.add(item);
    await player.setAudioSource(AudioSource.uri(Uri.parse(url)));
  }

  @override
  Future<void> play() => player.play();

  @override
  Future<void> pause() => player.pause();

  @override
  Future<void> seek(Duration position) => player.seek(position);

  @override
  Future<void> skipToNext() async => onSkipToNext?.call();

  @override
  Future<void> skipToPrevious() async => onSkipToPrevious?.call();

  @override
  Future<void> stop() async {
    await player.stop();
    await super.stop();
  }

  Future<void> setVolume(double volume) => player.setVolume(volume);

  void _broadcastState(PlaybackEvent event) {
    final playing = player.playing;
    final procState = const {
      ProcessingState.idle: AudioProcessingState.idle,
      ProcessingState.loading: AudioProcessingState.loading,
      ProcessingState.buffering: AudioProcessingState.buffering,
      ProcessingState.ready: AudioProcessingState.ready,
      ProcessingState.completed: AudioProcessingState.completed,
    }[player.processingState] ?? AudioProcessingState.idle;

    playbackState.add(
      playbackState.value.copyWith(
        controls: [
          MediaControl.skipToPrevious,
          if (playing) MediaControl.pause else MediaControl.play,
          MediaControl.skipToNext,
          MediaControl.stop,
        ],
        systemActions: const {MediaAction.seek},
        androidCompactActionIndices: const [0, 1, 2],
        processingState: procState,
        playing: playing,
        updatePosition: player.position,
        bufferedPosition: player.bufferedPosition,
        speed: player.speed,
      ),
    );
  }
}
