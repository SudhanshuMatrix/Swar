import 'dart:async';

import 'package:audio_service/audio_service.dart';
import 'package:flutter/foundation.dart';

import '../core/errors.dart';
import '../models/track.dart';
import '../services/api_service.dart';
import '../services/audio_handler.dart';
import '../services/download_service.dart';
import '../services/jam_service.dart';
import '../services/library_service.dart';

enum PlaybackRepeatMode { off, all, one }

/// Owns the queue and drives playback.
///
/// The background-capable [SonivoAudioHandler] (just_audio + audio_service)
/// is the single source of truth for position/duration/playing, so audio
/// keeps playing in the background when the screen closes.
class PlayerProvider extends ChangeNotifier {
  final ApiService api;
  final SonivoAudioHandler audioHandler;

  /// When set, fully downloaded tracks play from disk instead of streaming.
  DownloadService? downloads;

  /// When set and in a Jam, the local player follows the shared state.
  JamController? jam;

  /// Called after a song starts, so the library can show it in recents.
  VoidCallback? onTrackStarted;

  PlayerProvider({required this.api, required this.audioHandler}) {
    audioHandler.onSkipToNext = next;
    audioHandler.onSkipToPrevious = previous;

    audioHandler.playbackState.listen((state) {
      final changed = state.playing != _playing;
      _playing = state.playing;
      if (state.processingState == AudioProcessingState.completed) {
        _onTrackFinished();
      }
      if (changed) notifyListeners();
    });
    // Position ticks very often. Only the progress bars follow it (through
    // [progress], a few times a second); everything else listening to the
    // player redraws on real changes only, which keeps slow phones smooth.
    audioHandler.player.positionStream.listen((pos) {
      // A restored song shows where it was paused until it is loaded.
      if (_needsLoad) return;
      _position = pos;
      if (_playingId != null && (pos - _lastSavedPosition).abs() >= const Duration(seconds: 5)) {
        _lastSavedPosition = pos;
        LibraryService.instance.savePosition(pos);
      }
      if ((pos - _lastNotifiedPosition).abs() >= const Duration(milliseconds: 250)) {
        _lastNotifiedPosition = pos;
        progress.value = pos;
      }
    });
    audioHandler.player.durationStream.listen((d) {
      if (_needsLoad) return;
      if (d != null) _duration = d;
      notifyListeners();
    });
  }

  List<Track> _queue = [];
  int _queueIndex = -1;
  bool _playing = false;
  bool _loading = false;
  Duration _position = Duration.zero;

  /// The playback position for progress bars and times.
  final ValueNotifier<Duration> progress = ValueNotifier(Duration.zero);
  Duration _lastNotifiedPosition = Duration.zero;
  Duration _duration = Duration.zero;
  bool _shuffle = false;
  PlaybackRepeatMode _repeat = PlaybackRepeatMode.off;
  bool _autoPlayRelated = true;
  double _volume = 1.0;
  String? _errorMessage;
  String? _radioStationId;

  // Listening session, used to pick better next songs.
  final List<String> _sessionPlayed = []; // newest first
  final Set<String> _sessionSkipped = {};
  String? _playingId;
  DateTime _playStartedAt = DateTime.now();
  bool _endedNaturally = false;
  Future<void>? _topUp;
  String? _radioLabel;
  Duration _lastSavedPosition = Duration.zero;

  // Restored from the last session: the song shows, and loads on first play.
  bool _needsLoad = false;
  Duration _resumeAt = Duration.zero;

  // Sleep timer.
  Timer? _sleepTimer;
  DateTime? _sleepAt;
  bool _sleepAfterTrack = false;

  @override
  void notifyListeners() {
    progress.value = _position;
    super.notifyListeners();
  }

  /// Whether the queue is exactly [tracks], in any order (shuffle keeps a
  /// playlist's songs but reorders them).
  bool isQueueOf(List<Track> tracks) {
    if (tracks.isEmpty || tracks.length != _queue.length) return false;
    final ids = {for (final t in _queue) t.id};
    return tracks.every((t) => ids.contains(t.id));
  }

  Track? get currentTrack => (_queueIndex >= 0 && _queueIndex < _queue.length) ? _queue[_queueIndex] : null;
  List<Track> get queue => List.unmodifiable(_queue);
  int get queueIndex => _queueIndex;
  bool get playing => _playing;
  bool get loading => _loading;
  Duration get position => _position;
  Duration get duration => _duration;
  bool get shuffle => _shuffle;
  PlaybackRepeatMode get repeat => _repeat;
  bool get autoPlayRelated => _autoPlayRelated;
  double get volume => _volume;
  String? get errorMessage => _errorMessage;

  /// Label of the radio station feeding the queue, if any.
  String? get radioLabel => _radioLabel;

  /// When the sleep timer will pause playback, if it is set to a time.
  DateTime? get sleepAt => _sleepAt;

  /// Whether playback pauses when the current song ends.
  bool get sleepAfterTrack => _sleepAfterTrack;

  bool get sleepTimerActive => _sleepAt != null || _sleepAfterTrack;

  /// Pauses after [after], or turns the timer off when null.
  void setSleepTimer(Duration? after) {
    _sleepTimer?.cancel();
    _sleepTimer = null;
    _sleepAfterTrack = false;
    _sleepAt = null;
    if (after != null) {
      _sleepAt = DateTime.now().add(after);
      _sleepTimer = Timer(after, _sleepNow);
    }
    notifyListeners();
  }

  /// Pauses when the current song finishes.
  void setSleepAfterTrack() {
    setSleepTimer(null);
    _sleepAfterTrack = true;
    notifyListeners();
  }

  void _sleepNow() {
    _sleepTimer = null;
    _sleepAt = null;
    _sleepAfterTrack = false;
    if (_playing) playPause();
    notifyListeners();
  }

  /// Playback was stopped from outside (the notification's close button):
  /// keep the song and its place so play starts it again.
  void markStopped() {
    if (currentTrack == null) return;
    _closePlay();
    _resumeAt = _position;
    _needsLoad = true;
    _playing = false;
    notifyListeners();
  }

  /// Puts back the queue and song from last time, paused where it was.
  void restoreSession() {
    final saved = LibraryService.instance.loadSession();
    if (saved == null || currentTrack != null) return;
    _queue = saved.queue;
    _queueIndex = saved.index;
    _resumeAt = saved.position;
    _position = saved.position;
    _duration = saved.queue[saved.index].duration;
    _needsLoad = true;
    notifyListeners();
  }

  void _saveSession() {
    if (currentTrack == null) return;
    LibraryService.instance.saveSession(_queue, _queueIndex, _position).catchError((_) {});
  }

  void toggleAutoPlayRelated() {
    _autoPlayRelated = !_autoPlayRelated;
    notifyListeners();
  }

  Future<void> setVolume(double v) async {
    _volume = v.clamp(0.0, 1.0);
    await audioHandler.setVolume(_volume);
    notifyListeners();
  }

  Future<void> playQueue(List<Track> tracks, int startIndex, {bool keepRadio = false}) async {
    if (!keepRadio) {
      _radioStationId = null;
      _radioLabel = null;
    }
    _queue = List.of(tracks);
    _queueIndex = startIndex;
    await _loadCurrent(autoplay: true);
  }

  Future<void> playSingle(Track track) => playQueue([track], 0);

  void addToQueue(Track track) {
    _queue.add(track);
    _saveSession();
    notifyListeners();
  }

  void playNext(Track track) {
    if (_queue.isEmpty || _queueIndex < 0) {
      playSingle(track);
      return;
    }
    _queue.insert(_queueIndex + 1, track);
    _saveSession();
    notifyListeners();
  }

  void removeFromQueue(int index) {
    if (index < 0 || index >= _queue.length) return;
    if (index == _queueIndex) {
      if (_queue.length == 1) {
        _queue.clear();
        _queueIndex = -1;
        audioHandler.stop();
        notifyListeners();
        return;
      }
      next();
      _queue.removeAt(index);
      if (_queueIndex > index) _queueIndex--;
    } else {
      _queue.removeAt(index);
      if (index < _queueIndex) _queueIndex--;
      notifyListeners();
    }
  }

  Future<void> jumpToQueueIndex(int index) async {
    if (index < 0 || index >= _queue.length || index == _queueIndex) return;
    _queueIndex = index;
    await _loadCurrent(autoplay: true);
  }

  void reorderQueue(int oldIndex, int newIndex) {
    if (oldIndex < 0 || oldIndex >= _queue.length) return;
    if (newIndex < 0 || newIndex > _queue.length) return;
    if (oldIndex < newIndex) {
      newIndex -= 1;
    }
    final item = _queue.removeAt(oldIndex);
    _queue.insert(newIndex, item);

    if (_queueIndex == oldIndex) {
      _queueIndex = newIndex;
    } else if (oldIndex < _queueIndex && newIndex >= _queueIndex) {
      _queueIndex--;
    } else if (oldIndex > _queueIndex && newIndex <= _queueIndex) {
      _queueIndex++;
    }
    _saveSession();
    notifyListeners();
  }

  void clearQueue() {
    if (_queue.isEmpty) return;
    final current = currentTrack;
    if (current != null) {
      _queue = [current];
      _queueIndex = 0;
    } else {
      _queue.clear();
      _queueIndex = -1;
    }
    _saveSession();
    notifyListeners();
  }

  MediaItem _mediaItem(Track t) => MediaItem(
        id: t.id,
        title: t.title,
        artist: t.artist,
        artUri: t.thumbnailUrl.isNotEmpty ? Uri.tryParse(t.thumbnailUrl) : null,
        duration: t.duration,
      );

  Future<void> _loadCurrent({required bool autoplay, Duration startAt = Duration.zero}) async {
    final track = currentTrack;
    if (track == null) return;
    _needsLoad = false;
    _closePlay();
    _loading = true;
    _errorMessage = null;
    _position = Duration.zero;
    notifyListeners();
    String? streamUrl;
    try {
      final local = downloads?.localFileFor(track.id);
      final start = startAt > Duration.zero ? startAt : null;
      if (local != null) {
        await audioHandler.loadUrl(Uri.file(local).toString(), _mediaItem(track), startAt: start);
      } else {
        final stream = await api.resolveAudio(track.id);
        streamUrl = stream.url;
        await audioHandler.loadUrl(stream.url, _mediaItem(stream.track), startAt: start);
      }
      if (start != null) _position = start;
      if (autoplay) await audioHandler.play();
      _playingId = track.id;
      _playStartedAt = DateTime.now();
      _endedNaturally = false;
      _prefetchUpcoming();
      _maybeTopUpQueue();
      await LibraryService.instance.addRecent(track);
      await LibraryService.instance.countPlay(track);
      _lastSavedPosition = _position;
      _saveSession();
      onTrackStarted?.call();
    } catch (err) {
      // A Deep Search song that won't load: show the server's reason.
      final e = (err is! ApiException && streamUrl != null ? await api.relayProblem(streamUrl) : null) ?? err;
      _errorMessage = 'Could not play "${track.title}". ${friendlyError(e)}';
      ErrorPopup.show(e, title: 'Couldn\'t play "${track.title}"', onRetry: () => _loadCurrent(autoplay: true));
    } finally {
      _loading = false;
      notifyListeners();
    }
  }

  /// Flips the button right away, then tells the audio engine.
  Future<void> playPause() async {
    if (_needsLoad) {
      await _loadCurrent(autoplay: true, startAt: _resumeAt);
      return;
    }
    final wasPlaying = _playing;
    _playing = !wasPlaying;
    notifyListeners();
    try {
      wasPlaying ? await audioHandler.pause() : await audioHandler.play();
    } catch (e) {
      _playing = wasPlaying;
      notifyListeners();
      ErrorPopup.show(e, title: 'Playback problem');
    }
  }

  Future<void> seek(Duration pos) async {
    if (_needsLoad) {
      _resumeAt = pos;
      _position = pos;
      notifyListeners();
      return;
    }
    await audioHandler.player.seek(pos);
    _position = pos;
    _lastNotifiedPosition = pos;
    notifyListeners();
  }

  void toggleShuffle() {
    _shuffle = !_shuffle;
    notifyListeners();
  }

  void cycleRepeat() {
    _repeat = PlaybackRepeatMode.values[(_repeat.index + 1) % PlaybackRepeatMode.values.length];
    notifyListeners();
  }

  /// Asks the server to resolve the next one or two tracks ahead of time
  /// so skipping is instant. Downloaded tracks need no prefetch.
  void _prefetchUpcoming() {
    final upcoming = <String>[];
    for (var i = _queueIndex + 1; i < _queue.length && upcoming.length < 2; i++) {
      final id = _queue[i].id;
      if (downloads?.localFileFor(id) == null) upcoming.add(id);
    }
    api.prefetch(upcoming).catchError((_) {});
  }

  /// Starts a radio station seeded by [track] and plays it.
  Future<void> startRadio(Track track, {String kind = 'song'}) async {
    _errorMessage = null;
    _loading = true;
    notifyListeners();
    try {
      final station = await api.startRadio(kind, track.id);
      final tracks = [track, ...station.tracks.where((t) => t.id != track.id)];
      await playQueue(tracks, 0, keepRadio: true);
      _radioStationId = station.id;
      _radioLabel = station.label;
    } catch (e) {
      _errorMessage = 'Could not start radio. ${friendlyError(e)}';
      ErrorPopup.show(e, title: "Couldn't start radio", onRetry: () => startRadio(track, kind: kind));
    } finally {
      _loading = false;
      notifyListeners();
    }
  }

  /// Follows the Jam's shared track and play state.
  Future<void> syncToJam(JamController j) async {
    final shared = j.nowPlaying;
    if (shared == null) {
      if (_playing) await audioHandler.pause();
      return;
    }
    if (currentTrack?.id != shared.id) {
      await playQueue([shared], 0);
    }
    final drift = (j.position - _position).abs();
    if (drift > const Duration(seconds: 2)) {
      await seek(j.position);
    }
    if (j.playing && !_playing) await audioHandler.play();
    if (!j.playing && _playing) await audioHandler.pause();
  }

  Future<void> next() async {
    if (_queue.isEmpty) return;
    if (_shuffle && _queue.length > 1) {
      int candidate;
      do {
        candidate = DateTime.now().microsecondsSinceEpoch % _queue.length;
      } while (candidate == _queueIndex);
      _queueIndex = candidate;
    } else {
      final atEnd = _queueIndex + 1 >= _queue.length;
      if (atEnd) {
        if (_repeat == PlaybackRepeatMode.all) {
          _queueIndex = 0;
        } else if (_autoPlayRelated && currentTrack != null) {
          // Usually the queue was already topped up in the background; if
          // not, fetch now.
          final before = _queue.length;
          await (_topUp ?? _fetchMore());
          if (_queue.length == before) await _fetchMore();
          if (_queue.length > before) {
            _queueIndex++;
            await _loadCurrent(autoplay: true);
          }
          return;
        } else {
          return;
        }
      } else {
        _queueIndex++;
      }
    }
    await _loadCurrent(autoplay: true);
  }

  Future<void> previous() async {
    if (_queue.isEmpty) return;
    if (_position > const Duration(seconds: 3)) {
      await seek(Duration.zero);
      return;
    }
    _queueIndex = (_queueIndex - 1).clamp(0, _queue.length - 1);
    await _loadCurrent(autoplay: true);
  }

  /// Ends the current play: remembers it for this session and reports it
  /// (as a skip if less than half was heard and it didn't finish).
  void _closePlay() {
    final id = _playingId;
    if (id == null) return;
    _playingId = null;
    final heard = _position;
    final skipped = !_endedNaturally && _duration > Duration.zero && heard < _duration * 0.5;
    _sessionPlayed
      ..remove(id)
      ..insert(0, id);
    if (_sessionPlayed.length > 30) _sessionPlayed.removeLast();
    if (skipped) _sessionSkipped.add(id);
    api.recordPlay(id, startedAt: _playStartedAt, played: heard, skipped: skipped).catchError((_) {});
  }

  /// Keeps a couple of songs ready so the next one starts without a wait.
  void _maybeTopUpQueue() {
    if (!_autoPlayRelated || _repeat == PlaybackRepeatMode.all || _topUp != null) return;
    if (jam?.inJam ?? false) return;
    if (_queueIndex < _queue.length - 2) return;
    _topUp = _fetchMore().whenComplete(() => _topUp = null);
  }

  Future<void> _fetchMore() async {
    final seed = _queue.isNotEmpty ? _queue.last : currentTrack;
    if (seed == null) return;
    try {
      final existing = _queue.map((t) => t.id).toSet();
      final List<Track> more;
      if (_radioStationId != null) {
        more = await api.radioNext(_radioStationId!);
      } else {
        more = await api.recommendNext(
          seed.id,
          session: _sessionPlayed.where((id) => id != seed.id).toList(),
          skipped: _sessionSkipped.toList(),
          exclude: [...existing, ..._sessionPlayed],
        );
      }
      final fresh = more.where((t) => !existing.contains(t.id) && !_sessionPlayed.contains(t.id)).toList();
      if (fresh.isNotEmpty) {
        _queue.addAll(fresh.take(10));
        notifyListeners();
      }
    } catch (_) {
      // Autoplay is best effort; the current song keeps playing.
    }
  }

  Future<void> _onTrackFinished() async {
    _endedNaturally = true;
    final j = jam;
    if (j != null && j.inJam) {
      // In a Jam the server decides what plays next.
      j.reportTrackEnded();
      return;
    }
    if (_sleepAfterTrack) {
      _sleepAfterTrack = false;
      await audioHandler.pause();
      notifyListeners();
      return;
    }
    if (_repeat == PlaybackRepeatMode.one) {
      await seek(Duration.zero);
      await audioHandler.play();
      return;
    }
    await next();
  }
}
