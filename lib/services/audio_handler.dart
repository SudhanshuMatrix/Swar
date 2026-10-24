import 'dart:async';

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

  /// The heart in the notification.
  Future<void> Function()? onToggleLike;

  /// Called when the notification's close button stops playback.
  void Function()? onStopped;

  bool _liked = false;

  /// Shows the current song as liked or not in the notification.
  void setLiked(bool liked) {
    if (liked == _liked) return;
    _liked = liked;
    _broadcastState(null);
  }

  SonivoAudioHandler() {
    player.playbackEventStream.listen(_broadcastState, onError: (Object e, StackTrace st) {});
  }

  /// [headers] are sent with the request (yt-dlp names the User-Agent
  /// YouTube expects); most sources need none.
  Future<void> loadUrl(String url, MediaItem item, {Duration? startAt, Map<String, String>? headers}) async {
    mediaItem.add(item);
    final source = headers == null || headers.isEmpty
        ? AudioSource.uri(Uri.parse(url))
        : AudioSource.uri(Uri.parse(url), headers: headers);
    await player.setAudioSource(source, initialPosition: startAt);
  }

  /// just_audio's play() only completes when playback stops, so awaiting it
  /// would block callers (and keep loading spinners up) for the whole song.
  @override
  Future<void> play() async => unawaited(player.play());

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
    onStopped?.call();
  }

  @override
  Future<void> customAction(String name, [Map<String, dynamic>? extras]) async {
    if (name == 'like') await onToggleLike?.call();
  }

  Future<void> setVolume(double volume) => player.setVolume(volume);

  void _broadcastState(PlaybackEvent? event) {
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
        // Like, previous, play or pause, next, close; the small view shows
        // the middle three.
        controls: [
          MediaControl.custom(
            androidIcon: _liked ? 'drawable/swar_notif_liked' : 'drawable/swar_notif_like',
            label: _liked ? 'Unlike' : 'Like',
            name: 'like',
          ),
          MediaControl.skipToPrevious,
          if (playing) MediaControl.pause else MediaControl.play,
          MediaControl.skipToNext,
          const MediaControl(androidIcon: 'drawable/swar_notif_close', label: 'Close', action: MediaAction.stop),
        ],
        systemActions: const {MediaAction.seek, MediaAction.seekForward, MediaAction.seekBackward},
        androidCompactActionIndices: const [1, 2, 3],
        processingState: procState,
        playing: playing,
        updatePosition: player.position,
        bufferedPosition: player.bufferedPosition,
        speed: player.speed,
      ),
    );
  }
}
