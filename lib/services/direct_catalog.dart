import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';

import 'package:http/http.dart' as http;

import '../models/online_artist.dart';
import '../models/online_playlist.dart';
import '../models/track.dart';
import 'catalog_search.dart';

typedef DirectCatalogOptions = ({
  bool saavnMedia,
  bool saavnDownloads,
  bool youtubeMusic,
});

/// Queries the same public provider catalogs as the Go search route.
class DirectCatalog {
  DirectCatalog(this._http);

  final http.Client _http;
  static const _budget = Duration(seconds: 2);
  static const _maxBytes = 4 << 20;
  static const _userAgent =
      'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 '
      '(KHTML, like Gecko) Chrome/130.0 Safari/537.36';
  static final _videoId = RegExp(r'^[A-Za-z0-9_-]{11}$');
  static final _duration = RegExp(r'^(\d+:)?\d{1,2}:\d{2}$');
  static final _saavnSize = RegExp(r'150x150|50x50');
  static final _youtubeSize = RegExp(r'=w\d+-h\d+[^/]*$');
  static final _entity = RegExp(r'&(?:#\d+|#[xX][\da-fA-F]+|[A-Za-z]+);');
  static final _wordBreak = RegExp(r'[^\p{L}\p{Nd}]+', unicode: true);

  Future<({CatalogResults results, bool partial, bool failed})> search(
    String query,
    int limit,
    DirectCatalogOptions options,
  ) async {
    final saavn = options.saavnMedia
        ? _try(() async => _unique(
              (await _saavn('search.getResults', query, limit))
                  .map((item) => _saavnTrack(item, options.saavnDownloads))
                  .whereType<Track>(),
              (track) => track.id,
            ))
        : Future<List<Track>?>.value(const []);
    final youtube = options.youtubeMusic
        ? _try(() => _youtube(query, limit))
        : Future<List<Track>?>.value(const []);
    final artists = _try(() async => rankArtists(_unique(
          (await _saavn('search.getArtistResults', query, limit))
              .map(_saavnArtist)
              .whereType<OnlineArtist>(),
          (artist) => artist.id,
        )));
    final playlists = _try(() async => _unique(
          (await _saavn('search.getPlaylistResults', query, limit))
              .map(_saavnPlaylist)
              .whereType<OnlinePlaylist>(),
          (playlist) => playlist.id,
        ));

    final primary = await saavn;
    final secondary = await youtube;
    final singers = await artists;
    final mixes = await playlists;
    return (
      results: (
        songs: _merge(primary ?? const [], secondary ?? const [], limit),
        artists: singers ?? const [],
        playlists: mixes ?? const [],
      ),
      partial: primary == null ||
          secondary == null ||
          singers == null ||
          mixes == null,
      failed: (!options.saavnMedia || primary == null) &&
          (!options.youtubeMusic || secondary == null) &&
          singers == null &&
          mixes == null,
    );
  }

  Future<T?> _try<T>(Future<T> Function() request) async {
    try {
      return await request();
    } on Exception {
      return null;
    }
  }

  Future<Object?> _json(String method, Uri uri, {Object? body}) async {
    final abort = Completer<void>();
    final req = http.AbortableRequest(method, uri, abortTrigger: abort.future)
      ..followRedirects = false
      ..headers['Accept'] = 'application/json'
      ..headers['User-Agent'] = _userAgent;
    if (body != null) {
      req.headers['Content-Type'] = 'application/json';
      req.headers['Origin'] = 'https://music.youtube.com';
      req.body = jsonEncode(body);
    }
    Future<Object?> load() async {
      final resp = await _http.send(req);
      if (resp.statusCode != 200) {
        await resp.stream.listen(null).cancel();
        throw const FormatException('Catalog request failed');
      }
      final bytes = BytesBuilder(copy: false);
      await for (final chunk in resp.stream) {
        if (bytes.length + chunk.length > _maxBytes) {
          throw const FormatException('Catalog response too large');
        }
        bytes.add(chunk);
      }
      return jsonDecode(utf8.decode(bytes.takeBytes()));
    }

    return load().timeout(_budget, onTimeout: () {
      if (!abort.isCompleted) abort.complete();
      throw TimeoutException('Catalog request timed out', _budget);
    });
  }

  Future<List<Map>> _saavn(String call, String query, int limit) async {
    final data = await _json('GET', Uri.https('www.jiosaavn.com', '/api.php', {
      '__call': call,
      '_format': 'json',
      '_marker': '0',
      'api_version': '4',
      'ctx': 'web6dot0',
      'q': query,
      'p': '1',
      'n': '$limit',
    }));
    final results = _get(data, ['results']);
    if (results is! List) {
      throw const FormatException('Invalid JioSaavn search response');
    }
    return results.whereType<Map>().take(limit).toList();
  }

  static Track? _saavnTrack(Map item, bool downloads) {
    final id = _str(item['id']);
    final info = _object(item['more_info']);
    if (id.isEmpty || _str(info['encrypted_media_url']).isEmpty) return null;
    final artists = _object(info['artistMap']);
    final featured = [
      ..._list(artists['primary_artists']),
      ..._list(artists['featured_artists']),
    ];
    final mapped = featured.isEmpty ? _list(artists['artists']) : featured;
    final names = mapped
        .map((artist) => _text(_get(artist, ['name'])))
        .where((name) => name.isNotEmpty)
        .join(', ');
    final artist = [
      names,
      _text(info['primary_artists']),
      _text(info['singers']),
      _text(item['subtitle']),
    ].firstWhere((name) => name.isNotEmpty, orElse: () => '');
    final cacheable = _str(_object(info['rights'])['cacheable']).toLowerCase();
    return Track(
      id: 'saavn:$id',
      provider: 'saavn',
      title: _text(item['title']),
      artist: artist,
      album: _text(info['album']),
      thumbnailUrl: _saavnImage(item['image']),
      duration: Duration(seconds: _int(info['duration'])),
      downloadable: downloads &&
          (cacheable.isEmpty || const {'1', 'true', 'yes'}.contains(cacheable)),
    );
  }

  static OnlineArtist? _saavnArtist(Map item) {
    final id = _str(item['id']);
    if (id.isEmpty) return null;
    return OnlineArtist(
      id: 'saavn:$id',
      name: _text(item['name']),
      imageUrl: _saavnImage(item['image']),
    );
  }

  static OnlinePlaylist? _saavnPlaylist(Map item) {
    final id = _str(item['id']);
    if (id.isEmpty) return null;
    final info = _object(item['more_info']);
    final songCount = [
      _int(item['list_count']),
      _int(info['song_count']),
      _list(item['list']).length,
    ].firstWhere((count) => count > 0, orElse: () => 0);
    return OnlinePlaylist(
      id: 'saavn:$id',
      name: _text(item['title']),
      imageUrl: _saavnImage(item['image']),
      songCount: songCount,
    );
  }

  Future<List<Track>> _youtube(String query, int limit) async {
    final data = await _json(
      'POST',
      Uri.https('music.youtube.com', '/youtubei/v1/search', {'prettyPrint': 'false'}),
      body: {
        'context': {
          'client': {
            'clientName': 'WEB_REMIX',
            'clientVersion': '1.20241023.01.00',
            'hl': 'en',
            'gl': 'IN',
          },
        },
        'query': query,
        'params': 'EgWKAQIIAWoMEA4QChADEAQQCRAF',
      },
    );
    if (data is! Map || !data.containsKey('contents')) {
      throw const FormatException('Invalid YouTube Music search response');
    }
    return _unique(_youtubeRows(data).map(_youtubeTrack).whereType<Track>(),
        (track) => track.id).take(limit).toList();
  }

  static Iterable<Map> _youtubeRows(Object? node) sync* {
    if (node is Map) {
      final row = node['musicResponsiveListItemRenderer'];
      if (row is Map) {
        yield row;
      } else {
        for (final child in node.values) {
          yield* _youtubeRows(child);
        }
      }
    } else if (node is List) {
      for (final child in node) {
        yield* _youtubeRows(child);
      }
    }
  }

  static List _runs(Object? column) => _list(_get(column, [
        'musicResponsiveListItemFlexColumnRenderer',
        'text',
        'runs',
      ]));

  static Track? _youtubeTrack(Map item) {
    final columns = _list(item['flexColumns']);
    if (columns.isEmpty) return null;
    final titleRuns = _runs(columns.first);
    if (titleRuns.isEmpty) return null;
    final title = _str(_get(titleRuns.first, ['text']));
    var id = _str(_get(item, ['playlistItemData', 'videoId']));
    if (id.isEmpty) {
      id = _str(_get(titleRuns.first, ['navigationEndpoint', 'watchEndpoint', 'videoId']));
    }
    if (!_videoId.hasMatch(id) || title.isEmpty) return null;

    final artists = <String>[];
    var fallbackArtist = '';
    var album = '';
    var seconds = 0;
    for (final run in columns.skip(1).expand(_runs)) {
      final text = _str(_get(run, ['text'])).trim();
      if (text.isEmpty || text == '•' || text == '&') continue;
      final page = _get(run, [
        'navigationEndpoint',
        'browseEndpoint',
        'browseEndpointContextSupportedConfigs',
        'browseEndpointContextMusicConfig',
        'pageType',
      ]);
      if (page == 'MUSIC_PAGE_TYPE_ARTIST' || page == 'MUSIC_PAGE_TYPE_USER_CHANNEL') {
        artists.add(text);
      } else if (page == 'MUSIC_PAGE_TYPE_ALBUM') {
        album = text;
      } else if (_duration.hasMatch(text)) {
        seconds = text.split(':').fold(0, (sum, part) => sum * 60 + _int(part));
      } else if (fallbackArtist.isEmpty && text != 'Song') {
        fallbackArtist = text;
      }
    }
    final thumbs = _list(_get(item, [
      'thumbnail',
      'musicThumbnailRenderer',
      'thumbnail',
      'thumbnails',
    ]));
    final image = thumbs.isEmpty ? '' : _str(_get(thumbs.last, ['url']));
    return Track(
      id: 'yt:$id',
      provider: 'yt',
      youtubeId: id,
      title: title,
      artist: artists.isEmpty ? fallbackArtist : artists.join(', '),
      album: album,
      thumbnailUrl: image.replaceFirst(_youtubeSize, '=w544-h544-l90-rj'),
      duration: Duration(seconds: seconds),
    );
  }

  static List<Track> _merge(List<Track> saavn, List<Track> youtube, int limit) {
    final out = <Track>[];
    final titles = <(String, String)>{};
    final ids = <String>{};
    final durations = <(String, String), List<int>>{};
    void add(Track track) {
      if (out.length == limit || !ids.add(track.id)) return;
      final artist = track.artist.split(',').first.trim();
      if (!titles.add((track.title.trim().toLowerCase(), artist.toLowerCase()))) return;
      final fp = (
        track.title.toLowerCase().replaceAll(_wordBreak, ''),
        track.artist.toLowerCase().replaceAll(_wordBreak, ''),
      );
      final seconds = track.duration.inSeconds;
      if (seconds > 0 && fp.$1.isNotEmpty && fp.$2.isNotEmpty) {
        final seen = durations.putIfAbsent(fp, () => []);
        if (seen.any((other) => (other - seconds).abs() <= 2)) return;
        seen.add(seconds);
      }
      out.add(track);
    }

    for (var i = 0; out.length < limit && (i < saavn.length || i < youtube.length); i++) {
      if (i < saavn.length) add(saavn[i]);
      if (i < youtube.length) add(youtube[i]);
    }
    return out;
  }

  static List<T> _unique<T>(Iterable<T> items, String Function(T) id) {
    final seen = <String>{};
    return items.where((item) => seen.add(id(item))).toList();
  }

  static Object? _get(Object? value, List<String> path) {
    for (final key in path) {
      if (value is! Map) return null;
      value = value[key];
    }
    return value;
  }

  static List _list(Object? value) => value is List ? value : const [];

  static Map _object(Object? value) => value is Map ? value : const {};

  static String _str(Object? value) {
    if (value is String) return value.trim();
    return value is num || value is bool ? value.toString() : '';
  }

  static int _int(Object? value) => int.tryParse(_str(value)) ?? 0;

  static String _saavnImage(Object? value) {
    final uri = _str(value).replaceFirst(RegExp(r'^http:'), 'https:');
    return uri.startsWith('https:') ? uri.replaceAll(_saavnSize, '500x500') : '';
  }

  static String _text(Object? value) =>
      _str(value).replaceAllMapped(_entity, (match) {
        final entity = match[0]!;
        const named = {
          '&amp;': '&', '&lt;': '<', '&gt;': '>', '&quot;': '"',
          '&apos;': "'", '&nbsp;': '\u00a0', '&rsquo;': '’',
          '&lsquo;': '‘', '&ldquo;': '“', '&rdquo;': '”',
          '&ndash;': '–', '&mdash;': '—', '&hellip;': '…',
        };
        if (!entity.startsWith('&#')) return named[entity] ?? entity;
        final number = entity.substring(2, entity.length - 1);
        final hex = number.startsWith('x') || number.startsWith('X');
        final scalar = int.tryParse(hex ? number.substring(1) : number, radix: hex ? 16 : 10);
        return scalar == null || scalar > 0x10ffff || (scalar >= 0xd800 && scalar <= 0xdfff)
            ? entity
            : String.fromCharCode(scalar);
      });
}
