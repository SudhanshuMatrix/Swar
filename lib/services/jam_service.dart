import 'dart:async';
import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:web_socket_channel/web_socket_channel.dart';

import '../models/track.dart';
import 'api_service.dart';

enum JamConnection { offline, connecting, connected, reconnecting }

class JamParticipant {
  final String id;
  final String name;
  final bool isAdmin;
  final bool connected;
  JamParticipant(this.id, this.name, this.isAdmin, this.connected);
}

class JamEntry {
  final String entryId;
  final Track track;
  final String addedBy;
  final int votes;
  JamEntry(this.entryId, this.track, this.addedBy, this.votes);
}

/// Client side of Swar Jam.
///
/// The server owns the state. This keeps a WebSocket open, applies every
/// `state` message as the new truth, reconnects with backoff (each
/// reconnect receives a full state, so nothing is lost), and estimates the
/// server clock from ping/pong so playback positions line up.
class JamController extends ChangeNotifier {
  final ApiService api;
  JamController({required this.api});

  String? jamId;
  String code = '';
  String name = '';
  String you = '';
  bool isAdmin = false;
  int rev = 0;
  bool allowParticipantAdd = false;
  List<JamParticipant> participants = [];
  List<JamParticipant> removed = [];
  List<JamEntry> queue = [];
  int currentIndex = -1;
  String? nowPlayingEntryId;
  Track? nowPlaying;
  bool playing = false;
  int _positionMs = 0;
  int _anchorServerMs = 0;
  JamConnection connection = JamConnection.offline;

  /// Set when the Jam is over for this device ("ended", "removed", ...).
  String? closedReason;
  String? lastError;

  /// Called whenever the shared track or play state changes, so the local
  /// player can follow along.
  void Function(JamController jam)? onPlaybackChanged;

  WebSocketChannel? _channel;
  StreamSubscription? _sub;
  Timer? _reconnectTimer;
  Timer? _pingTimer;
  int _attempt = 0;
  int _clockOffsetMs = 0; // serverTime - localTime
  int _msgId = 0;
  bool _disposed = false;

  bool get inJam => jamId != null && closedReason == null;

  /// The shared position right now, using the estimated server clock.
  Duration get position {
    var ms = _positionMs;
    if (playing) {
      final serverNow = DateTime.now().millisecondsSinceEpoch + _clockOffsetMs;
      ms += (serverNow - _anchorServerMs).clamp(0, 24 * 3600 * 1000);
    }
    return Duration(milliseconds: ms);
  }

  /// How long until a scheduled start: the server sets playback to begin a
  /// moment ahead so every device starts the song together.
  Duration get startsIn {
    if (!playing) return Duration.zero;
    final serverNow = DateTime.now().millisecondsSinceEpoch + _clockOffsetMs;
    final ms = _anchorServerMs - serverNow;
    return ms > 0 ? Duration(milliseconds: ms) : Duration.zero;
  }

  Future<void> create(String displayName) async {
    final snap = await api.createJam(displayName);
    _start(snap);
  }

  Future<void> join(String code, String displayName) async {
    final snap = await api.joinJam(code, displayName);
    _start(snap);
  }

  void _start(Map<String, dynamic> snap) {
    _teardown();
    closedReason = null;
    lastError = null;
    _apply(snap);
    _connect();
  }

  Future<void> leave() async {
    final id = jamId;
    _teardown();
    _reset();
    if (id != null) {
      try {
        await api.leaveJam(id);
      } catch (_) {}
    }
  }

  Future<void> end() async {
    final id = jamId;
    if (id == null) return;
    await api.endJam(id);
    _teardown();
    _reset();
  }

  Future<void> removeParticipant(String participantId) async {
    if (jamId == null) return;
    await api.removeJamParticipant(jamId!, participantId);
  }

  void allowRejoin(String participantId) => send({'type': 'allow_rejoin', 'participantId': participantId});
  void addTrack(Track t) => send({
        'type': 'queue_add',
        'trackIds': [t.id]
      });
  void playPause() => send({'type': playing ? 'pause' : 'play', 'rev': rev});
  void next() => send({'type': 'next', 'rev': rev});
  void previous() => send({'type': 'previous', 'rev': rev});
  void jump(String entryId) => send({'type': 'jump', 'entryId': entryId, 'rev': rev});
  void removeEntry(String entryId) => send({'type': 'queue_remove', 'entryId': entryId, 'rev': rev});
  void seek(Duration to) => send({'type': 'seek', 'positionMs': to.inMilliseconds, 'rev': rev});

  /// The admin's player reports when the shared track finishes.
  void reportTrackEnded() {
    if (isAdmin && nowPlayingEntryId != null) {
      send({'type': 'track_ended', 'entryId': nowPlayingEntryId});
    }
  }

  void send(Map<String, dynamic> msg) {
    final ch = _channel;
    if (ch == null || connection != JamConnection.connected) {
      lastError = 'Not connected to the Jam right now';
      notifyListeners();
      return;
    }
    ch.sink.add(jsonEncode({...msg, 'id': 'm${_msgId++}'}));
  }

  Future<void> _connect() async {
    final id = jamId;
    if (id == null || _disposed) return;
    connection = _attempt == 0 ? JamConnection.connecting : JamConnection.reconnecting;
    notifyListeners();
    try {
      final uri = await api.jamSocketUri(id);
      final ch = WebSocketChannel.connect(uri);
      await ch.ready;
      _channel = ch;
      _sub = ch.stream.listen(_onMessage, onDone: _onClosed, onError: (_) => _onClosed());
      _pingTimer = Timer.periodic(const Duration(seconds: 15), (_) => _ping());
      _ping();
    } catch (_) {
      _scheduleReconnect();
    }
  }

  void _ping() {
    _channel?.sink.add(jsonEncode({'type': 'ping', 'clientTime': DateTime.now().millisecondsSinceEpoch}));
  }

  void _onMessage(dynamic raw) {
    final Map<String, dynamic> m;
    try {
      m = jsonDecode(raw as String) as Map<String, dynamic>;
    } catch (_) {
      return;
    }
    switch (m['type']) {
      case 'state':
        _attempt = 0;
        connection = JamConnection.connected;
        _apply(m['jam'] as Map<String, dynamic>);
        break;
      case 'sync':
        final pb = m['playback'] as Map<String, dynamic>;
        rev = (m['rev'] as num).toInt();
        _applyPlayback(pb);
        break;
      case 'pong':
        final sent = (m['clientTime'] as num?)?.toInt() ?? 0;
        final server = (m['serverTime'] as num?)?.toInt() ?? 0;
        if (sent > 0 && server > 0) {
          final now = DateTime.now().millisecondsSinceEpoch;
          _clockOffsetMs = server - (sent + now) ~/ 2;
        }
        break;
      case 'error':
        lastError = m['message'] as String?;
        break;
      case 'removed':
        closedReason = 'You were removed from this Jam';
        break;
      case 'ended':
        closedReason = m['reason'] == 'expired' ? 'This Jam expired' : 'This Jam has ended';
        break;
    }
    notifyListeners();
  }

  void _onClosed() {
    _sub?.cancel();
    _sub = null;
    _pingTimer?.cancel();
    _channel = null;
    if (closedReason != null || jamId == null || _disposed) {
      connection = JamConnection.offline;
      notifyListeners();
      return;
    }
    _scheduleReconnect();
  }

  void _scheduleReconnect() {
    if (_disposed || jamId == null || closedReason != null) return;
    _attempt++;
    if (_attempt > 8) {
      connection = JamConnection.offline;
      lastError = 'Lost connection to the Jam';
      notifyListeners();
      return;
    }
    connection = JamConnection.reconnecting;
    notifyListeners();
    final delay = Duration(milliseconds: (500 * (1 << (_attempt - 1))).clamp(500, 15000));
    _reconnectTimer = Timer(delay, _connect);
  }

  /// Tries again after the reconnect attempts ran out.
  void reconnect() {
    _attempt = 0;
    lastError = null;
    _connect();
  }

  void _apply(Map<String, dynamic> snap) {
    jamId = snap['id'] as String;
    code = (snap['code'] as String?) ?? '';
    name = (snap['name'] as String?) ?? 'Jam';
    you = (snap['you'] as String?) ?? '';
    isAdmin = snap['role'] == 'admin';
    rev = (snap['rev'] as num?)?.toInt() ?? rev;
    final settings = snap['settings'] as Map<String, dynamic>? ?? const {};
    allowParticipantAdd = settings['allowParticipantAdd'] == true;
    JamParticipant p(Map<String, dynamic> j) =>
        JamParticipant(j['id'] as String, (j['name'] as String?) ?? '', j['role'] == 'admin', j['connected'] == true);
    participants = ((snap['participants'] as List?) ?? const []).cast<Map<String, dynamic>>().map(p).toList();
    removed = ((snap['removed'] as List?) ?? const []).cast<Map<String, dynamic>>().map(p).toList();
    final q = snap['queue'] as Map<String, dynamic>? ?? const {};
    queue = ((q['entries'] as List?) ?? const [])
        .cast<Map<String, dynamic>>()
        .map((e) => JamEntry(
              e['entryId'] as String,
              Track.fromJson(e['track'] as Map<String, dynamic>),
              (e['addedBy'] as String?) ?? '',
              (e['votes'] as num?)?.toInt() ?? 0,
            ))
        .toList();
    currentIndex = (q['current'] as num?)?.toInt() ?? -1;
    final np = snap['nowPlaying'] as Map<String, dynamic>? ?? const {};
    final t = np['track'];
    nowPlaying = t is Map<String, dynamic> ? Track.fromJson(t) : null;
    _applyPlayback(np);
  }

  void _applyPlayback(Map<String, dynamic> pb) {
    final entry = pb['entryId'] as String?;
    final wasPlaying = playing;
    final prevEntry = nowPlayingEntryId;
    final before = position;
    nowPlayingEntryId = entry;
    playing = pb['playing'] == true;
    _positionMs = (pb['positionMs'] as num?)?.toInt() ?? 0;
    _anchorServerMs = (pb['updatedAt'] as num?)?.toInt() ?? DateTime.now().millisecondsSinceEpoch + _clockOffsetMs;
    // Seeks count too, so everyone jumps to the same place.
    if (entry != prevEntry || playing != wasPlaying || (position - before).abs() > const Duration(seconds: 1)) {
      onPlaybackChanged?.call(this);
    }
  }

  String participantName(String id) {
    for (final p in participants) {
      if (p.id == id) return p.name;
    }
    return '';
  }

  void _teardown() {
    _reconnectTimer?.cancel();
    _pingTimer?.cancel();
    _sub?.cancel();
    _sub = null;
    _channel?.sink.close();
    _channel = null;
    _attempt = 0;
  }

  void _reset() {
    jamId = null;
    code = '';
    participants = [];
    removed = [];
    queue = [];
    nowPlaying = null;
    nowPlayingEntryId = null;
    playing = false;
    connection = JamConnection.offline;
    closedReason = null;
    notifyListeners();
  }

  /// Clears a Jam that ended or removed this device.
  void dismissClosed() {
    _teardown();
    _reset();
  }

  @override
  void dispose() {
    _disposed = true;
    _teardown();
    super.dispose();
  }
}
