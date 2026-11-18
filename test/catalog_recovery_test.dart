import 'dart:async';
import 'dart:convert';

import 'package:flutter/foundation.dart' show kIsWeb;
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:sonivo/core/constants.dart';
import 'package:sonivo/services/api_service.dart';

const _query = 'krsna';
final _hosted = Uri.parse(ApiConfig.hostedBaseUrl);

ApiService _api(
  Future<http.Response> Function(http.Request) respond, {
  String baseUrl = ApiConfig.hostedBaseUrl,
}) {
  final client = MockClient(respond);
  addTearDown(client.close);
  return ApiService(baseUrl: baseUrl, client: client);
}

http.Response _json(Object value, [int status = 200]) => http.Response(
      jsonEncode(value),
      status,
      headers: {'content-type': 'application/json; charset=utf-8'},
    );

http.Response _providerResponse(http.Request request) {
  expectSync(request.headers, isNot(contains('authorization')));
  if (request.url.host == 'music.youtube.com') {
    expectSync((jsonDecode(request.body) as Map)['query'], _query);
    return _json({'contents': []});
  }
  expectSync(request.url.host, 'www.jiosaavn.com');
  expectSync(request.url.queryParameters['q'], _query);
  return _json({
    'results': [
      if (request.url.queryParameters['__call'] == 'search.getResults')
        {
          'id': 'boomsong',
          'title': 'Boom Shaka',
          'more_info': {
            'primary_artists': r'KR$NA',
            'encrypted_media_url': 'provider-media-reference',
            'rights': {'cacheable': true},
          },
        },
    ],
  });
}

void main() {
  test(
    'native search recovers when startup status and search cannot connect',
    () async {
      final offline = http.ClientException('Connection refused');
      final paths = <String>[];
      final api = _api((request) async {
        if (request.url.host == _hosted.host) {
          paths.add(request.url.path);
          throw offline;
        }
        return _providerResponse(request);
      });

      await expectLater(api.status(), throwsA(same(offline)));
      final song = (await api.searchAll(_query)).songs.single;

      expect(paths, ['/v1/status', '/v1/search']);
      expect(song.id, 'saavn:boomsong');
      expect(song.artist, r'KR$NA');
      expect(song.downloadable, isFalse);
    },
    skip: kIsWeb,
  );

  testWidgets('a stalled Swar request yields to native catalog search', (
    tester,
  ) async {
    final stalled = Completer<http.Response>();
    var providerReached = false;
    final api = _api((request) async {
      if (request.url.host == _hosted.host) return stalled.future;
      providerReached = true;
      return _providerResponse(request);
    });

    final search = api.searchAll(_query);
    await tester.pump(const Duration(seconds: 15));

    expect(providerReached, isTrue);
    expect((await search).songs.single.id, 'saavn:boomsong');
    stalled.complete(http.Response('', 504));
  }, skip: kIsWeb);

  test('a gateway failure recovers with native provider results', () async {
    final api = _api((request) async {
      if (request.url.host == _hosted.host) {
        return http.Response('Bad Gateway', 502);
      }
      return _providerResponse(request);
    });

    expect((await api.searchAll(_query)).songs.single.id, 'saavn:boomsong');
  }, skip: kIsWeb);

  test(
    'status can opt out of provider fallback while search is pending',
    () async {
      final received = Completer<void>();
      final response = Completer<http.Response>();
      final api = _api((request) async {
        expect(request.url.host, _hosted.host);
        if (request.url.path == '/v1/status') {
          return _json({'success': true, 'data': {}});
        }
        received.complete();
        return response.future;
      });

      final search = api.searchAll(_query);
      await received.future;
      await api.status();
      response.complete(http.Response('Bad Gateway', 502));
      await expectLater(
        search,
        throwsA(isA<ApiException>().having((e) => e.status, 'status', 502)),
      );
    },
  );

  test(
    'an unreachable custom server does not send queries to providers',
    () async {
      final offline = http.ClientException('Connection refused');
      final api = _api((request) async {
        expect(request.url.host, 'custom.example');
        throw offline;
      }, baseUrl: 'https://custom.example');

      await expectLater(api.searchAll(_query), throwsA(same(offline)));
    },
  );

  test('an empty partial recovery preserves the connection error', () async {
    final offline = http.ClientException('Connection refused');
    var providerReached = false;
    final api = _api((request) async {
      if (request.url.host == _hosted.host) throw offline;
      providerReached = true;
      if (request.url.queryParameters['__call'] == 'search.getArtistResults') {
        return _json({'results': []});
      }
      return http.Response('', 503);
    });

    await expectLater(api.searchAll(_query), throwsA(same(offline)));
    expect(providerReached, isTrue);
    expect(api.cachedSearchAll(_query), isNull);
  }, skip: kIsWeb);

  for (final (status, code) in [(503, 'maintenance'), (429, 'rate_limited')]) {
    test('search respects the server response for $code', () async {
      final api = _api((request) async {
        expect(request.url.host, _hosted.host);
        return _json({
          'success': false,
          'error': {'code': code},
        }, status);
      });

      await expectLater(
        api.searchAll(_query),
        throwsA(isA<ApiException>().having((e) => e.code, 'code', code)),
      );
    });
  }
}
