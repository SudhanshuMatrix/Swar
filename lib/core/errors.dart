import 'dart:async';

import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;

import '../services/api_service.dart';

/// A mistake in what the user typed; its message is shown as is.
class InputError implements Exception {
  final String message;
  const InputError(this.message);

  @override
  String toString() => message;
}

/// Turns any error into a short message a listener can act on, without
/// stack traces or raw exception text.
String friendlyError(Object error) {
  if (error is InputError) return error.message;
  if (error is ApiException) {
    switch (error.code) {
      case 'rate_limited':
        return 'Too many requests right now. Wait a few seconds and try again.';
      case 'not_found':
        return 'That song or playlist could not be found. It may have been removed.';
      case 'unavailable':
        return error.message.isNotEmpty ? error.message : 'This is not available on this server.';
      case 'upstream_timeout':
        return 'The music source took too long to answer. Try again in a moment.';
      case 'upstream_error':
        return 'The music source is having trouble right now. Try again in a moment.';
      case 'unauthorized':
        return 'Your session expired. Try again and Swar will sign this device in again.';
      case 'invalid_request':
        return error.message.isNotEmpty ? error.message : 'That request was not valid.';
    }
    if (error.status >= 500) return 'The Swar server is having trouble. Try again in a moment.';
    if (error.message.isNotEmpty) return error.message;
  }
  if (error is TimeoutException) {
    return 'The Swar server is taking too long to answer. If it was asleep, it can take up to a minute to wake up.';
  }
  final type = error.runtimeType.toString();
  if (error is http.ClientException || type.contains('SocketException') || type.contains('HandshakeException')) {
    return "Can't reach the Swar server. Check your internet connection and try again.";
  }
  if (error is FormatException) {
    return 'The server sent an unexpected answer. Try again in a moment.';
  }
  if (type.contains('PlayerException') || type.contains('PlatformException')) {
    return "This song couldn't be played on this device. Try another song.";
  }
  return 'Something went wrong. Please try again.';
}

/// Shows errors as a popup dialog from anywhere in the app.
class ErrorPopup {
  ErrorPopup._();

  /// Attached to the MaterialApp so popups work outside widgets too.
  static final navigatorKey = GlobalKey<NavigatorState>();

  static bool _open = false;

  /// Shows one popup at a time; errors arriving while one is open are
  /// dropped so a burst of failures doesn't stack dialogs.
  static Future<void> show(Object error, {String title = 'Something went wrong', VoidCallback? onRetry}) async {
    debugPrint('Swar error: $error');
    final context = navigatorKey.currentContext;
    if (context == null || _open) return;
    _open = true;
    try {
      await showDialog<void>(
        context: context,
        builder: (ctx) => AlertDialog(
          icon: const Icon(Icons.error_outline, color: Colors.redAccent, size: 36),
          title: Text(title, textAlign: TextAlign.center),
          content: Text(friendlyError(error), textAlign: TextAlign.center),
          actionsAlignment: MainAxisAlignment.center,
          actions: [
            TextButton(onPressed: () => Navigator.pop(ctx), child: const Text('OK')),
            if (onRetry != null)
              FilledButton(
                onPressed: () {
                  Navigator.pop(ctx);
                  onRetry();
                },
                child: const Text('Try again'),
              ),
          ],
        ),
      );
    } finally {
      _open = false;
    }
  }
}
