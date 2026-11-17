import 'dart:async';
import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import '../lib/models/track.dart';
import '../lib/services/catalog_search.dart';
import '../lib/services/direct_catalog.dart';

void main() {
  test('typeahead and submit coalesce and reuse the same result', () async {
    var now = DateTime(2026);
    var requests = 0;
    final response = Completer<({CatalogResults results, bool partial})>();
    final search = CatalogSearch((query, limit) {
      requests++;
      expect(query.toLowerCase(), 'night ride');
      expect(limit, 30);
      return response.future;
    }, clock: () => now);
    final typeahead = search.search(' Night  Ride ', 30);
    final submitted = search.search('night ride', 30);
    const track = Track(
      id: 'yt:abcdefghijk',
      title: 'Night Ride Home',
      artist: 'Joni Mitchell',
      thumbnailUrl: '',
      duration: Duration(seconds: 190),
    );
    response.complete((
      results: (songs: [track], artists: [], playlists: []),
      partial: false,
    ));
    expect((await typeahead).songs, [track]);
    expect((await submitted).songs, [track]);
    expect(search.suggestions('night ride', 30).songs, [track]);
    expect(search.suggestions('night ride home', 30).songs, [track]);
    expect(search.suggestions('night ride live', 30).songs, isEmpty);
    expect(requests, 1);
    now = now.add(const Duration(minutes: 5));
    expect(search.cached('night ride', 30), isNull);
  });

  test('native search normalizes and merges provider results', () async {
    final calls = <String>{};
    final client = MockClient((request) async {
      Object response;
      if (request.url.host == 'music.youtube.com') {
        calls.add('youtube');
        expect(request.method, 'POST');
        expect((jsonDecode(request.body) as Map)['query'], 'night & day');
        response = {
          'contents': [
            {
              'musicResponsiveListItemRenderer': {
                'playlistItemData': {'videoId': 'abcdefghijk'},
                'flexColumns': [
                  column(['Night Ride']),
                  column(['Joni Mitchell', '•', '3:10']),
                ],
              },
            },
          ],
        };
      } else {
        expect(request.url.host, 'www.jiosaavn.com');
        expect(request.url.queryParameters['q'], 'night & day');
        final call = request.url.queryParameters['__call']!;
        calls.add(call);
        response = {
          'results': switch (call) {
            'search.getResults' => [
                {
                  'id': 's1',
                  'title': 'Night &amp; Day',
                  'image': 'http://example.com/150x150.jpg',
                  'more_info': {
                    'encrypted_media_url': 'media-reference',
                    'primary_artists': 'Test Artist',
                    'duration': '191',
                    'rights': {'cacheable': false},
                  },
                },
              ],
            'search.getArtistResults' => [
                {'id': 42, 'name': 'Test Artist'},
              ],
            'search.getPlaylistResults' => [
                {'id': 'p1', 'title': 'Night Mix', 'list_count': '12'},
              ],
            _ => throw StateError('Unexpected provider call: $call'),
          },
        };
      }
      return http.Response(jsonEncode(response), 200);
    });
    addTearDown(client.close);
    final direct = DirectCatalog(client);
    final result = await direct.search('night & day', 30, (
      saavnMedia: true,
      saavnDownloads: true,
      youtubeMusic: true,
    ));
    expect(calls, hasLength(4));
    expect(result.partial, isFalse);
    expect(result.results.songs.map((track) => track.id), [
      'saavn:s1',
      'yt:abcdefghijk',
    ]);
    final first = result.results.songs.first;
    expect(first.title, 'Night & Day');
    expect(first.thumbnailUrl, 'https://example.com/500x500.jpg');
    expect(first.downloadable, isFalse);
    expect(result.results.songs.last.duration.inSeconds, 190);
    expect(result.results.artists.single.id, 'saavn:42');
    expect(result.results.playlists.single.songCount, 12);
  });

  test('provider failure keeps successful categories and can trigger fallback', () async {
    final calls = <String>[];
    final client = MockClient((request) async {
      final call = request.url.queryParameters['__call']!;
      calls.add(call);
      if (call == 'search.getPlaylistResults') {
        return http.Response('upstream error', 503);
      }
      return http.Response('{"results":[{"id":"42","name":"Artist"}]}', 200);
    });
    addTearDown(client.close);
    final result = await DirectCatalog(client).search('artist', 30, (
      saavnMedia: false,
      saavnDownloads: false,
      youtubeMusic: false,
    ));
    expect(calls, unorderedEquals([
      'search.getArtistResults',
      'search.getPlaylistResults',
    ]));
    expect(result.partial, isTrue);
    expect(result.failed, isFalse);
    expect(result.results.artists.single.name, 'Artist');
    expect(result.results.songs, isEmpty);
  });
}

Map<String, Object> column(List<String> values) => {
      'musicResponsiveListItemFlexColumnRenderer': {
        'text': {
          'runs': values.map((text) => {'text': text}).toList(),
        },
      },
    };
