import 'dart:async';
import 'dart:math' as math;

import 'package:audio_service/audio_service.dart';
import 'package:cached_network_image/cached_network_image.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart' show ScaffoldMessenger, SnackBar, Text;
import 'package:flutter/widgets.dart' show precacheImage;

import '../core/errors.dart';
import '../models/track.dart';
import '../services/api_service.dart';
import '../services/app_settings.dart';
import '../services/audio_handler.dart';
import '../services/download_service.dart';
import '../services/jam_service.dart';
import '../services/library_service.dart';
import '../services/prefetch_planner.dart';
import '../services/youtube_audio.dart';

enum PlaybackRepeatMode { off, all, one }

/// A newer load took over; the older one stops quietly.
class _Superseded implements Exception {
  const _Superseded();
}

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

  // Each load gets a number; a slow load that finishes after the listener
  // moved on must not replace the newer song.
  int _loadGen = 0;
  // Bumped when the queue changes, so preparation for an old order stops.
  int _prefetchGen = 0;
  // With shuffle, the song that plays next is picked in advance so the one
  // being prepared is the one that plays.
  int? _shuffleNext;
  final _random = math.Random();

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
    _queueChanged();
    notifyListeners();
  }

  /// The queue's order or contents changed: prepare what now comes next.
  void _queueChanged() {
    if (_shuffleNext != null && (_shuffleNext! >= _queue.length || _shuffleNext == _queueIndex)) _shuffleNext = null;
    if (currentTrack != null && !_needsLoad && !_loading) _prefetchUpcoming();
  }

  void playNext(Track track) {
    if (_queue.isEmpty || _queueIndex < 0) {
      playSingle(track);
      return;
    }
    _queue.insert(_queueIndex + 1, track);
    _saveSession();
    _shuffleNext = null;
    _queueChanged();
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
      _shuffleNext = null;
      _queueChanged();
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
    _shuffleNext = null;
    _queueChanged();
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
    _shuffleNext = null;
    _prefetchGen++; // nothing left to prepare
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
    final gen = ++_loadGen;
    _prefetchGen++; // what was being prepared may no longer come next
    _shuffleNext = null;
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
      } else if (track.isDeepSearchResult && YouTubeAudio.supported) {
        streamUrl = await _loadFromYouTube(track, start, gen);
      } else {
        streamUrl = await _loadCatalogSong(track, start, gen);
      }
      _check(gen);
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
    } on _Superseded {
      return;
    } catch (err) {
      if (gen != _loadGen) return; // the listener already moved on
      // A Deep Search song that won't load: show the server's reason.
      final e = (err is! ApiException && streamUrl != null ? await api.relayProblem(streamUrl) : null) ?? err;
      _errorMessage = 'Could not play "${track.title}". ${friendlyError(e)}';
      ErrorPopup.show(e, title: 'Couldn\'t play "${track.title}"', onRetry: () => _loadCurrent(autoplay: true));
    } finally {
      if (gen == _loadGen) {
        _loading = false;
        notifyListeners();
      }
    }
  }

  void _check(int gen) {
    if (gen != _loadGen) throw const _Superseded();
  }

  /// JioSaavn songs play straight from the server's URL. Deep Search catalog
  /// songs (Spotify, Deezer, iTunes...) only come with a 30-second preview,
  /// so the full song is played from YouTube with yt-dlp on this device:
  /// the video the server matched, or else one yt-dlp finds here. When
  /// yt-dlp can't run here (the web) or fails, the server's relay (yt-dlp
  /// on the server) gets a try. The preview is the last resort. Returns the
  /// server URL when one was used.
  Future<String?> _loadCatalogSong(Track track, Duration? start, int gen) async {
    StreamResult? stream;
    Object? serverError;
    try {
      stream = await api.resolveAudio(track.id);
    } catch (err) {
      serverError = err;
    }
    _check(gen);
    final item = _mediaItem(stream?.track ?? track);
    // JioSaavn gives the full song directly.
    if (stream != null && stream.url.isNotEmpty && !stream.isPreview) {
      await audioHandler.loadUrl(stream.url, item, startAt: start);
      return stream.url;
    }
    String? ytId = stream?.track.youtubeId ?? track.youtubeId;
    if (YouTubeAudio.supported) {
      try {
        ytId ??= await YouTubeAudio.findVideoId(track.title, track.artist, track.duration);
        if (ytId != null) {
          final s = await YouTubeAudio.resolve(ytId);
          _check(gen);
          await audioHandler.loadUrl(s.url, item, startAt: start, headers: s.headers);
          return null;
        }
      } on _Superseded {
        rethrow;
      } catch (err) {
        debugPrint('Swar: full song from YouTube failed for ${track.id} ($ytId): $err');
        if (ytId != null) YouTubeAudio.forget(ytId);
      }
    }
    if (ytId != null) {
      try {
        final relay = await api.resolveAudio('yt:$ytId');
        _check(gen);
        await audioHandler.loadUrl(relay.url, item, startAt: start);
        return relay.url;
      } on _Superseded {
        rethrow;
      } catch (err) {
        debugPrint('Swar: server relay failed for ${track.id} ($ytId): $err');
      }
    }
    if (stream != null && stream.url.isNotEmpty) {
      _check(gen);
      await audioHandler.loadUrl(stream.url, item, startAt: start);
      if (stream.isPreview) {
        _showNotice(
            'Only a 30-second preview of "${track.title}" could be played. The full song wasn\'t found on YouTube.');
      }
      return stream.url;
    }
    throw serverError ?? const YouTubeAudioException("Couldn't find the full song on YouTube.");
  }

  void _showNotice(String message) {
    final ctx = ErrorPopup.navigatorKey.currentContext;
    if (ctx == null) return;
    ScaffoldMessenger.maybeOf(ctx)?.showSnackBar(SnackBar(content: Text(message)));
  }

  /// Plays a Deep Search song from YouTube with yt-dlp on this device (see
  /// [YouTubeAudio]). A cached URL that
  /// fails to load (it expired or was revoked early) is resolved again
  /// once. Only when the device can't get the song does the server's relay
  /// get one try; when that fails too, the device's reason is the one worth
  /// showing. Returns the relay URL when the relay was used.
  Future<String?> _loadFromYouTube(Track track, Duration? start, int gen) async {
    Object? deviceError;
    for (var attempt = 0; attempt < 2; attempt++) {
      final fromCache = attempt == 0 && YouTubeAudio.cached(track.id) != null;
      try {
        final s = await YouTubeAudio.resolve(track.id, fresh: attempt > 0);
        _check(gen);
        await audioHandler.loadUrl(s.url, _mediaItem(track), startAt: start, headers: s.headers);
        return null;
      } on _Superseded {
        rethrow;
      } catch (err) {
        debugPrint('Swar: YouTube on this device failed for ${track.id}: $err');
        YouTubeAudio.forget(track.id);
        deviceError = err;
        if (!fromCache) break;
      }
    }
    try {
      final stream = await api.resolveAudio(track.id);
      _check(gen);
      await audioHandler.loadUrl(stream.url, _mediaItem(stream.track), startAt: start);
      return stream.url;
    } on _Superseded {
      rethrow;
    } catch (_) {
      throw deviceError is YouTubeAudioException
          ? deviceError
          : const YouTubeAudioException("Couldn't get this song from YouTube. Check your connection and try again.");
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
    _shuffleNext = null;
    _queueChanged();
    notifyListeners();
  }

  void cycleRepeat() {
    _repeat = PlaybackRepeatMode.values[(_repeat.index + 1) % PlaybackRepeatMode.values.length];
    _queueChanged();
    notifyListeners();
  }

  /// A random queue position other than the current one.
  int _randomOther() {
    if (_queue.length < 2) return _queueIndex;
    var i = _random.nextInt(_queue.length - 1);
    if (i >= _queueIndex) i++;
    return i;
  }

  /// Prepares what plays next so skipping is instant (see
  /// [upcomingIndexes]): the next Deep Search song's stream is resolved on
  /// this device, artwork is cached (not in data saver), and the server is
  /// asked to prepare the songs it plays itself. Work for a queue that has
  /// changed since is dropped. Downloaded songs need nothing.
  void _prefetchUpcoming() {
    final gen = ++_prefetchGen;
    final settings = AppSettings.instance;
    if (_shuffle && _queue.length > 1) _shuffleNext ??= _randomOther();
    final indexes = upcomingIndexes(
      length: _queue.length,
      current: _queueIndex,
      count: settings.prefetchCount,
      shuffleNext: _shuffle ? _shuffleNext : null,
      repeatAll: _repeat == PlaybackRepeatMode.all,
    );
    final tracks = [
      for (final i in indexes)
        if (downloads?.localFileFor(_queue[i].id) == null) _queue[i],
    ];
    if (tracks.isEmpty) return;
    bool onDevice(Track t) => t.isDeepSearchResult && YouTubeAudio.supported;
    final serverIds = [for (final t in tracks) if (!onDevice(t)) t.id];
    if (serverIds.isNotEmpty) api.prefetch(serverIds, dataSaver: settings.dataSaver).catchError((_) {});
    // Give the current song's start a head start, then prepare the very
    // next song (only it: a YouTube resolve is the expensive part).
    Timer(const Duration(seconds: 3), () async {
      if (gen != _prefetchGen) return;
      final next = tracks.first;
      if (onDevice(next)) await YouTubeAudio.prefetch(next.id);
      if (gen != _prefetchGen || settings.dataSaver) return;
      final ctx = ErrorPopup.navigatorKey.currentContext;
      if (ctx == null || !ctx.mounted) return;
      for (final t in tracks) {
        if (t.thumbnailUrl.isNotEmpty) {
          precacheImage(CachedNetworkImageProvider(t.thumbnailUrl), ctx).catchError((_) {});
        }
      }
    });
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
  int _jamSync = 0;

  /// Follows the Jam's shared playback. A new song is loaded without
  /// starting, then every device starts it at the moment the server picked,
  /// so phones that load slower don't fall behind.
  Future<void> syncToJam(JamController j) async {
    final sync = ++_jamSync;
    final shared = j.nowPlaying;
    if (shared == null) {
      if (_playing) await audioHandler.pause();
      return;
    }
    if (currentTrack?.id != shared.id) {
      _radioStationId = null;
      _radioLabel = null;
      _queue = [shared];
      _queueIndex = 0;
      await _loadCurrent(autoplay: false);
      if (sync != _jamSync) return; // a newer update took over
    }
    Duration drift() => (j.position - audioHandler.player.position).abs();
    if (!j.playing) {
      if (_playing) await audioHandler.pause();
      if (drift() > const Duration(milliseconds: 500)) await seek(j.position);
      return;
    }
    final wait = j.startsIn;
    if (wait > Duration.zero) {
      if (_playing) await audioHandler.pause();
      await seek(j.position);
      await Future<void>.delayed(j.startsIn);
      if (sync != _jamSync) return;
      await audioHandler.play();
      return;
    }
    if (drift() > const Duration(milliseconds: 400)) await seek(j.position);
    if (!_playing) await audioHandler.play();
    // Joining late or loading slowly: check once more after buffering.
    await Future<void>.delayed(const Duration(seconds: 2));
    if (sync == _jamSync && j.playing && drift() > const Duration(milliseconds: 400)) await seek(j.position);
  }

  Future<void> next() async {
    if (_queue.isEmpty) return;
    if (_shuffle && _queue.length > 1) {
      // The song picked (and prepared) in advance, if it is still valid.
      final picked = _shuffleNext;
      _queueIndex = picked != null && picked < _queue.length && picked != _queueIndex ? picked : _randomOther();
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
