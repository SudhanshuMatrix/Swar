import 'dart:async';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:sonivo/core/errors.dart';
import 'package:sonivo/services/api_service.dart';

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
}
