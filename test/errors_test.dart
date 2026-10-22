import 'dart:async';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:sonivo/core/errors.dart';
import 'package:sonivo/services/api_service.dart';
import 'package:sonivo/services/youtube_audio.dart';

void main() {
  test('network failures read as a connection problem', () {
    expect(friendlyError(http.ClientException('Connection refused')), contains("Can't reach the Swar server"));
  });

  test('server error codes map to plain messages', () {
    expect(friendlyError(ApiException(429, 'rate_limited', 'Too many requests')), contains('Too many requests'));
    expect(friendlyError(ApiException(404, 'not_found', '')), contains('could not be found'));
    expect(friendlyError(ApiException(503, 'error', '')), contains('having trouble'));
  });

  test('server messages for disabled features are passed through', () {
    expect(friendlyError(ApiException(403, 'unavailable', 'Downloads are not enabled on this server')),
        'Downloads are not enabled on this server');
  });

  test('timeouts mention the server waking up', () {
    expect(friendlyError(TimeoutException('slow')), contains('wake up'));
  });

  test('unknown errors never show raw exception text', () {
    expect(friendlyError(StateError('Bad state: internal detail')), isNot(contains('internal detail')));
  });

  test('Deep Search relay failures say YouTube refused the song', () {
    final m = friendlyError(ApiException(502, 'upstream', 'the audio source refused the request'));
    expect(m, contains('YouTube'));
    expect(m, contains('refused the request'));
  });

  test('a YouTube block on the server is named, not blamed on the song', () {
    expect(friendlyError(ApiException(503, 'rate_limited', 'YouTube is blocking requests from this server right now')),
        startsWith('YouTube is blocking'));
  });

  test('a song YouTube refused on this device shows the reason as is', () {
    expect(
        friendlyError(const YouTubeAudioException('This YouTube video is private.')), 'This YouTube video is private.');
  });
}
