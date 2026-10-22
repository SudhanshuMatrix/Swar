import 'dart:convert';

import 'package:hive_flutter/hive_flutter.dart';
import 'package:http/http.dart' as http;

import '../core/constants.dart';
import '../models/online_artist.dart';
import '../models/online_playlist.dart';
import '../models/track.dart';
import '../state/app_status.dart';

class StreamResult {
  /// Empty when the server only knows the song's YouTube video.
  final String url;
  final Track track;

  /// [url] is the catalog's 30-second preview, not the full song.
  final bool isPreview;
  StreamResult({required this.url, required this.track, this.isPreview = false});
}

/// An error from the Swar backend with its stable [code]
/// (see server/docs/API.md) and a message safe to show to the user.
class ApiException implements Exception {
  final int status;
  final String code;
  final String message;
  ApiException(this.status, this.code, this.message);

  @override
  String toString() => message;
}

class DeepSearchResult {
  final List<Track> tracks;
  final String notice;
  DeepSearchResult(this.tracks, this.notice);
}

class RadioStation {
  final String id;
  final String label;

  /// How the station was built, e.g. "provider_station_ranked" or
  /// "random_discovery". Shown so the app never over-claims.
  final String generation;
  final List<Track> tracks;
  RadioStation({required this.id, required this.label, required this.generation, required this.tracks});

  factory RadioStation.fromJson(Map<String, dynamic> j) => RadioStation(
        id: j['id'] as String,
        label: (j['label'] as String?) ?? 'Radio',
        generation: (j['generation'] as String?) ?? '',
        tracks: ((j['tracks'] as List?) ?? const []).cast<Map<String, dynamic>>().map(Track.fromJson).toList(),
      );
}

class DownloadEligibility {
  final bool eligible;
  final String reason;
  final String localId;

  /// The authorized source to download from, when eligible.
  final String? url;
  DownloadEligibility({required this.eligible, required this.reason, required this.localId, this.url});
}

/// Talks to the Swar Go backend (/server).
class ApiService {
  final String baseUrl;
  ApiService({String? baseUrl}) : baseUrl = baseUrl ?? ApiConfig.baseUrl;

  static const _deviceBox = 'swar_device';

  /// One client for every request, so connections to the server are kept
  /// open and reused instead of paying a new TLS handshake each time.
  static final http.Client _http = _StatusClient(http.Client());
  String? _token;

  // ---- Device identity ----

  /// Returns a device token, registering this install on first use.
  /// No account or personal data: the server only knows a random device id.
  Future<String> deviceToken() async {
    if (_token != null) return _token!;
    final box = await Hive.openBox(_deviceBox);
    final saved = box.get('token') as String?;
    final savedFor = box.get('server') as String?;
    if (saved != null && savedFor == baseUrl) {
      return _token = saved;
    }
    final res = await _http.post(Uri.parse('$baseUrl/v1/auth/device'));
    final data = _data(res) as Map<String, dynamic>;
    _token = data['token'] as String;
    await box.put('token', _token);
    await box.put('server', baseUrl);
    return _token!;
  }

  Future<void> _forgetToken() async {
    _token = null;
    final box = await Hive.openBox(_deviceBox);
    await box.delete('token');
  }

  /// Sends an authenticated /v1 request, re-registering once if the server
  /// no longer knows the token (e.g. its secret changed).
  Future<dynamic> _authed(String method, String path, {Object? body}) async {
    for (var attempt = 0; attempt < 2; attempt++) {
      final token = await deviceToken();
      final req = http.Request(method, Uri.parse('$baseUrl$path'))..headers['Authorization'] = 'Bearer $token';
      if (body != null) {
        req.headers['Content-Type'] = 'application/json';
        req.body = jsonEncode(body);
      }
      final res = await http.Response.fromStream(await _http.send(req));
      if (res.statusCode == 401 && attempt == 0) {
        await _forgetToken();
        continue;
      }
      return _data(res);
    }
  }

  Future<dynamic> _public(String method, String path, {Object? body}) async {
    final req = http.Request(method, Uri.parse('$baseUrl$path'));
    if (body != null) {
      req.headers['Content-Type'] = 'application/json';
      req.body = jsonEncode(body);
    }
    return _data(await http.Response.fromStream(await _http.send(req)));
  }

  /// Unwraps the {success, data} envelope or throws [ApiException].
  dynamic _data(http.Response res) {
    Map<String, dynamic>? body;
    try {
      body = jsonDecode(res.body) as Map<String, dynamic>;
    } catch (_) {}
    if (res.statusCode >= 200 && res.statusCode < 300 && body != null && body['success'] == true) {
      return body['data'];
    }
    final err = body?['error'];
    final code = err is Map ? (err['code'] as String? ?? 'error') : (body?['code'] as String? ?? 'error');
    final msg = (body?['message'] as String?) ?? (err is String ? err : 'Request failed (${res.statusCode})');
    throw ApiException(res.statusCode, code, msg);
  }

  /// Maintenance mode and the active popup (see [AppStatus]).
  Future<Map<String, dynamic>> status() async {
    return (await _public('GET', '/v1/status') as Map).cast<String, dynamic>();
  }

  // ---- Catalog (legacy routes, unchanged contract) ----

  Future<List<Track>> search(String query, {int limit = 50}) async {
    final uri = Uri.parse('$baseUrl/search').replace(queryParameters: {
      'q': query,
      'limit': '$limit',
    });
    final res = await _http.get(uri);
    if (res.statusCode != 200) {
      throw _legacyError(res, 'Search failed');
    }
    final data = jsonDecode(res.body) as Map<String, dynamic>;
    final results = (data['results'] as List).cast<Map<String, dynamic>>();
    return results.map(Track.fromJson).toList();
  }

  ApiException _legacyError(http.Response res, String fallback) {
    try {
      final body = jsonDecode(res.body) as Map<String, dynamic>;
      return ApiException(res.statusCode, (body['code'] as String?) ?? 'error', (body['error'] as String?) ?? fallback);
    } catch (_) {
      return ApiException(res.statusCode, 'error', '$fallback (${res.statusCode})');
    }
  }

  Future<StreamResult> resolveAudio(String trackId) async {
    final res = await _http.get(Uri.parse('$baseUrl/track/${Uri.encodeComponent(trackId)}'));
    if (res.statusCode != 200) {
      throw _legacyError(res, 'Could not resolve audio');
    }
    final data = jsonDecode(res.body) as Map<String, dynamic>;
    var url = (data['audio_url'] as String?) ?? '';
    // Deep Search audio is relayed by the server and comes back as a path.
    if (url.startsWith('/')) url = '$baseUrl$url';
    return StreamResult(url: url, track: Track.fromJson(data), isPreview: data['preview'] == true);
  }

  /// Why the server's audio relay won't serve [url] (a Deep Search song),
  /// or null when it does. The player itself only reports "can't play".
  Future<ApiException?> relayProblem(String url) async {
    if (!url.contains('/v1/playback/')) return null;
    try {
      final res = await _http.get(Uri.parse(url), headers: {'Range': 'bytes=0-1'}).timeout(const Duration(seconds: 45));
      if (res.statusCode < 400) return null;
      _data(res);
    } on ApiException catch (e) {
      return e;
    } catch (_) {}
    return null;
  }

  Future<List<Track>> getRelated(String trackId) async {
    final res = await _http.get(Uri.parse('$baseUrl/related/${Uri.encodeComponent(trackId)}'));
    if (res.statusCode != 200) {
      throw _legacyError(res, 'Could not fetch related tracks');
    }
    final data = jsonDecode(res.body) as Map<String, dynamic>;
    final results = (data['results'] as List).cast<Map<String, dynamic>>();
    return results.map(Track.fromJson).toList();
  }

  /// Ranked next songs for [seedId]. [session] is what played before it this
  /// session (newest first) and [skipped] what the listener skipped, so the
  /// server can follow the session's mood. Falls back to the simpler
  /// related list if the server is older.
  Future<List<Track>> recommendNext(String seedId,
      {List<String> session = const [],
      List<String> skipped = const [],
      List<String> exclude = const [],
      int limit = 10}) async {
    try {
      final data = await _authed('POST', '/v1/recommendations/next', body: {
        'seedTrackId': seedId,
        'session': session.take(50).toList(),
        'skipped': skipped.take(200).toList(),
        'exclude': exclude.take(500).toList(),
        'limit': limit,
      }) as Map<String, dynamic>;
      return ((data['results'] as List?) ?? const [])
          .whereType<Map<String, dynamic>>()
          .map((r) => Track.fromJson(r['track'] as Map<String, dynamic>))
          .toList();
    } on ApiException catch (e) {
      if (e.code == 'invalid_request' || e.status == 404) return getRelated(seedId);
      rethrow;
    }
  }

  /// Tells the server a song was played or skipped; it uses this to rank
  /// future recommendations. Best effort.
  Future<void> recordPlay(String trackId,
      {required DateTime startedAt, required Duration played, required bool skipped}) async {
    await _authed('POST', '/v1/library/history', body: {
      'events': [
        {
          'trackId': trackId,
          'playedAt': startedAt.millisecondsSinceEpoch,
          'msPlayed': played.inMilliseconds,
          'skipped': skipped,
        }
      ],
    });
  }

  // ---- Playlists from the catalog ----

  Future<List<OnlinePlaylist>> searchPlaylists(String query, {int limit = 10}) async {
    final q = Uri(queryParameters: {'q': query, 'type': 'playlists', 'limit': '$limit'}).query;
    final data = await _public('GET', '/v1/search?$q') as Map<String, dynamic>;
    final page = data['playlists'] as Map<String, dynamic>?;
    return ((page?['results'] as List?) ?? const [])
        .whereType<Map<String, dynamic>>()
        .map(OnlinePlaylist.fromJson)
        .toList();
  }

  Future<List<OnlineArtist>> searchArtists(String query, {int limit = 10}) async {
    final q = Uri(queryParameters: {'q': query, 'type': 'artists', 'limit': '$limit'}).query;
    final data = await _public('GET', '/v1/search?$q') as Map<String, dynamic>;
    final page = data['artists'] as Map<String, dynamic>?;
    return rankArtists(((page?['results'] as List?) ?? const [])
        .whereType<Map<String, dynamic>>()
        .map(OnlineArtist.fromJson)
        .toList());
  }

  /// A few songs and singers for [query], for suggestions while typing. One
  /// small request, which also warms the server's cache for the full search.
  Future<({List<Track> songs, List<OnlineArtist> artists})> suggest(String query) async {
    final q = Uri(queryParameters: {'q': query, 'type': 'all', 'limit': '5'}).query;
    final data = await _public('GET', '/v1/search?$q') as Map<String, dynamic>;
    List<Map<String, dynamic>> results(String key) =>
        (((data[key] as Map<String, dynamic>?)?['results'] as List?) ?? const [])
            .whereType<Map<String, dynamic>>()
            .toList();
    return (
      songs: results('songs').map(Track.fromJson).where((t) => t.playable).toList(),
      artists: results('artists').map(OnlineArtist.fromJson).toList(),
    );
  }

  /// The artist's profile with their top songs.
  Future<OnlineArtist> artist(String id) async {
    final data = await _public('GET', '/v1/artists/${Uri.encodeComponent(id)}');
    return OnlineArtist.fromJson(data as Map<String, dynamic>);
  }

  /// One page of an artist's songs, most popular first, with the total the
  /// catalog reports for the artist.
  Future<({List<Track> tracks, int total})> artistSongs(String id, {int page = 0, int songCount = 50}) async {
    final data = await _public('GET', '/v1/artists/${Uri.encodeComponent(id)}/songs?page=$page&songCount=$songCount')
        as Map<String, dynamic>;
    final tracks =
        ((data['results'] as List?) ?? const []).whereType<Map<String, dynamic>>().map(Track.fromJson).toList();
    return (tracks: tracks, total: (data['total'] as num?)?.toInt() ?? 0);
  }

  Future<OnlinePlaylist> playlist(String id, {int limit = 100}) async {
    final data = await _public('GET', '/v1/playlists/${Uri.encodeComponent(id)}?limit=$limit');
    return OnlinePlaylist.fromJson(data as Map<String, dynamic>);
  }

  // ---- Deep Search ----

  /// Runs only when the user asks for it: the server starts yt-dlp for
  /// each call, so it is rate limited more strictly than normal search.
  Future<DeepSearchResult> deepSearch(String query, {int limit = 10}) async {
    final data =
        await _public('POST', '/v1/search/deep', body: {'query': query, 'limit': limit}) as Map<String, dynamic>;
    final results = ((data['results'] as List?) ?? const []).cast<Map<String, dynamic>>();
    return DeepSearchResult(results.map(Track.fromJson).toList(), (data['notice'] as String?) ?? '');
  }

  // ---- Playback ----

  /// Warms the server's cache for the next one or two tracks.
  Future<void> prefetch(List<String> trackIds, {bool dataSaver = false}) async {
    if (trackIds.isEmpty) return;
    await _public('POST', '/v1/playback/prefetch',
        body: {'trackIds': trackIds.take(2).toList(), 'dataSaver': dataSaver});
  }

  // ---- Radio ----

  Future<RadioStation> startRadio(String kind, String seed, {int count = 15}) async {
    final data = await _authed('POST', '/v1/radio', body: {'kind': kind, 'seed': seed, 'count': count});
    return RadioStation.fromJson(data as Map<String, dynamic>);
  }

  Future<List<Track>> radioNext(String stationId, {int count = 10}) async {
    final data = await _authed('POST', '/v1/radio/$stationId/next', body: {'count': count});
    return RadioStation.fromJson(data as Map<String, dynamic>).tracks;
  }

  // ---- Downloads ----

  Future<DownloadEligibility> downloadEligibility(String trackId) async {
    final data =
        await _public('GET', '/v1/downloads/eligibility/${Uri.encodeComponent(trackId)}') as Map<String, dynamic>;
    String? url;
    final d = data['descriptor'];
    if (d is Map && d['sources'] is List) {
      final audio = (d['sources'] as List).whereType<Map>().where((s) => s['kind'] == 'audio').toList();
      // Highest quality the source offers for the offline copy.
      audio.sort((a, b) => ((b['bitrate'] as num?) ?? 0).compareTo((a['bitrate'] as num?) ?? 0));
      if (audio.isNotEmpty) url = audio.first['url'] as String?;
    }
    return DownloadEligibility(
      eligible: data['eligible'] == true && url != null,
      reason: (data['reason'] as String?) ?? '',
      localId: (data['localId'] as String?) ?? trackId,
      url: url,
    );
  }

  /// Reports download status so the server-side record stays in step.
  Future<void> reportDownload(String trackId, String status,
      {int bytesTotal = 0, int bytesDone = 0, String sha256 = '', String error = ''}) async {
    await _authed('PUT', '/v1/downloads/${Uri.encodeComponent(trackId)}', body: {
      'status': status,
      'bytesTotal': bytesTotal,
      'bytesDone': bytesDone,
      'sha256': sha256,
      'error': error,
    });
  }

  Future<void> removeDownloadRecord(String trackId) async {
    await _authed('DELETE', '/v1/downloads/${Uri.encodeComponent(trackId)}');
  }

  // ---- Swar Jam ----

  Future<Map<String, dynamic>> createJam(String displayName,
      {bool allowParticipantAdd = true, bool allowVoting = false}) async {
    return await _authed('POST', '/v1/jams', body: {
      'displayName': displayName,
      'settings': {'allowParticipantAdd': allowParticipantAdd, 'allowVoting': allowVoting},
    }) as Map<String, dynamic>;
  }

  Future<Map<String, dynamic>> joinJam(String code, String displayName) async {
    return await _authed('POST', '/v1/jams/join', body: {'code': code, 'displayName': displayName})
        as Map<String, dynamic>;
  }

  Future<void> leaveJam(String jamId) => _authed('POST', '/v1/jams/$jamId/leave');

  Future<void> endJam(String jamId) => _authed('POST', '/v1/jams/$jamId/end');

  Future<void> removeJamParticipant(String jamId, String participantId) =>
      _authed('DELETE', '/v1/jams/$jamId/participants/$participantId');

  /// WebSocket address for a Jam. The token goes in the query because
  /// WebSocket clients cannot always set headers; the server never logs it.
  Future<Uri> jamSocketUri(String jamId) async {
    final token = await deviceToken();
    final base = Uri.parse(baseUrl);
    return base.replace(
      scheme: base.scheme == 'https' ? 'wss' : 'ws',
      path: '/v1/jams/$jamId/ws',
      queryParameters: {'access_token': token},
    );
  }
}

/// Passes the maintenance and popup headers of every response to
/// [AppStatus], so any request (a search, a song) notices a change.
class _StatusClient extends http.BaseClient {
  final http.Client _inner;
  _StatusClient(this._inner);

  @override
  Future<http.StreamedResponse> send(http.BaseRequest request) async {
    final res = await _inner.send(request);
    AppStatus.instance.observe(res.headers['x-swar-maintenance'], res.headers['x-swar-popup']);
    return res;
  }
}
