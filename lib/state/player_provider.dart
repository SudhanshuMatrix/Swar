import 'dart:async';

import 'package:audio_service/audio_service.dart';
import 'package:flutter/foundation.dart';

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
    // Position ticks very often; redrawing every listener on each one makes
    // taps (like play/pause) feel slow, so only notify a few times a second.
    audioHandler.player.positionStream.listen((pos) {
      _position = pos;
      if ((pos - _lastNotifiedPosition).abs() >= const Duration(milliseconds: 250)) {
        _lastNotifiedPosition = pos;
        notifyListeners();
      }
    });
    audioHandler.player.durationStream.listen((d) {
      if (d != null) _duration = d;
      notifyListeners();
    });
  }

  List<Track> _queue = [];
  int _queueIndex = -1;
  bool _playing = false;
  bool _loading = false;
  Duration _position = Duration.zero;
  Duration _lastNotifiedPosition = Duration.zero;
  Duration _duration = Duration.zero;
  bool _shuffle = false;
  PlaybackRepeatMode _repeat = PlaybackRepeatMode.off;
  bool _autoPlayRelated = true;
  double _volume = 1.0;
  String? _errorMessage;
  String? _radioStationId;
  String? _radioLabel;

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
    notifyListeners();
  }

  void playNext(Track track) {
    if (_queue.isEmpty || _queueIndex < 0) {
      playSingle(track);
      return;
    }
    _queue.insert(_queueIndex + 1, track);
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
    notifyListeners();
  }

  MediaItem _mediaItem(Track t) => MediaItem(
        id: t.id,
        title: t.title,
        artist: t.artist,
        artUri: t.thumbnailUrl.isNotEmpty ? Uri.tryParse(t.thumbnailUrl) : null,
        duration: t.duration,
      );

  Future<void> _loadCurrent({required bool autoplay}) async {
    final track = currentTrack;
    if (track == null) return;
    _loading = true;
    _errorMessage = null;
    _position = Duration.zero;
    notifyListeners();
    try {
      final local = downloads?.localFileFor(track.id);
      if (local != null) {
        await audioHandler.loadUrl(Uri.file(local).toString(), _mediaItem(track));
      } else {
        final stream = await api.resolveAudio(track.id);
        await audioHandler.loadUrl(stream.url, _mediaItem(stream.track));
      }
      if (autoplay) await audioHandler.play();
      _prefetchUpcoming();
      await LibraryService.instance.addRecent(track);
    } catch (e) {
      _errorMessage = 'Could not play "${track.title}": $e';
    } finally {
      _loading = false;
      notifyListeners();
    }
  }

  /// Flips the button right away, then tells the audio engine. just_audio's
  /// play() only completes when playback stops, so it is not awaited.
  Future<void> playPause() async {
    final wasPlaying = _playing;
    _playing = !wasPlaying;
    notifyListeners();
    try {
      if (wasPlaying) {
        await audioHandler.pause();
      } else {
        unawaited(audioHandler.play());
      }
    } catch (_) {
      _playing = wasPlaying;
      notifyListeners();
    }
  }

  Future<void> seek(Duration pos) async {
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
      _errorMessage = 'Could not start radio: $e';
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
          try {
            final related = _radioStationId != null
                ? await api.radioNext(_radioStationId!)
                : await api.getRelated(currentTrack!.id);
            final existingIds = _queue.map((t) => t.id).toSet();
            final newTracks = related.where((t) => !existingIds.contains(t.id)).toList();
            if (newTracks.isNotEmpty) {
              _queue.addAll(newTracks);
              _queueIndex++;
              await _loadCurrent(autoplay: true);
              return;
            }
          } catch (_) {}
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

  Future<void> _onTrackFinished() async {
    final j = jam;
    if (j != null && j.inJam) {
      // In a Jam the server decides what plays next.
      j.reportTrackEnded();
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
