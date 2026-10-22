import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:hive_flutter/hive_flutter.dart';

import '../services/api_service.dart';

/// A message the server wants every listener to see, such as "update the
/// app" or "support us". Set by the admin API.
class ServerPopup {
  final int id;
  final String kind;
  final String title;
  final String message;
  final String buttonText;
  final String buttonUrl;
  final bool dismissible;

  const ServerPopup({
    required this.id,
    required this.kind,
    required this.title,
    required this.message,
    this.buttonText = '',
    this.buttonUrl = '',
    this.dismissible = true,
  });

  factory ServerPopup.fromJson(Map<String, dynamic> j) => ServerPopup(
        id: (j['id'] as num).toInt(),
        kind: j['kind'] as String? ?? 'info',
        title: j['title'] as String? ?? '',
        message: j['message'] as String? ?? '',
        buttonText: j['buttonText'] as String? ?? '',
        buttonUrl: j['buttonUrl'] as String? ?? '',
        dismissible: j['dismissible'] as bool? ?? true,
      );
}

/// Whether the server is in maintenance and which popup it wants shown.
///
/// Every server response carries X-Swar-Maintenance and X-Swar-Popup
/// headers; [observe] gets them from the shared HTTP client, and the full
/// details come from GET /v1/status only when something changed.
class AppStatus extends ChangeNotifier {
  AppStatus._();
  static final instance = AppStatus._();

  static const _box = 'swar_status';

  bool maintenance = false;
  String maintenanceMessage = '';
  ServerPopup? popup;

  /// Changes whenever a popup is activated or edited; empty when none.
  String popupKey = '';

  bool _fetching = false;
  bool _refetch = false;
  Timer? _poll;

  /// Called with the headers of every server response. Servers older than
  /// this feature send neither header and change nothing.
  void observe(String? maintenanceHeader, String? popupHeader) {
    if (maintenanceHeader == null && popupHeader == null) return;
    final on = maintenanceHeader == 'on';
    final key = (popupHeader == null || popupHeader == 'none') ? '' : popupHeader;
    if (on == maintenance && key == popupKey) return;
    if (!on && key.isEmpty) {
      // Nothing to fetch: just clear what was shown.
      _apply(false, maintenanceMessage, null, '');
      return;
    }
    refresh();
  }

  /// Fetches the full status (also used on start and by the Retry button).
  Future<void> refresh() async {
    if (_fetching) {
      _refetch = true;
      return;
    }
    _fetching = true;
    try {
      do {
        _refetch = false;
        final data = await ApiService().status();
        final m = (data['maintenance'] as Map?)?.cast<String, dynamic>() ?? const {};
        final p = data['popup'];
        _apply(
          m['enabled'] == true,
          m['message'] as String? ?? '',
          p is Map ? ServerPopup.fromJson(p.cast<String, dynamic>()) : null,
          data['popupKey'] as String? ?? '',
        );
      } while (_refetch);
    } catch (e) {
      debugPrint('Swar status check failed: $e');
    } finally {
      _fetching = false;
    }
  }

  void _apply(bool on, String message, ServerPopup? p, String key) {
    final changed = on != maintenance || message != maintenanceMessage || key != popupKey;
    maintenance = on;
    maintenanceMessage = message;
    popup = key.isEmpty ? null : p;
    popupKey = popup == null ? '' : key;
    // While in maintenance nothing else talks to the server, so check
    // back on our own to notice when it ends.
    if (on) {
      _poll ??= Timer.periodic(const Duration(seconds: 30), (_) => refresh());
    } else {
      _poll?.cancel();
      _poll = null;
    }
    if (changed) notifyListeners();
  }

  /// Whether the user already closed this version of the popup.
  static Future<bool> isDismissed(String key) async {
    final box = await Hive.openBox(_box);
    return box.get('dismissedPopup') == key;
  }

  static Future<void> dismiss(String key) async {
    final box = await Hive.openBox(_box);
    await box.put('dismissedPopup', key);
  }
}
