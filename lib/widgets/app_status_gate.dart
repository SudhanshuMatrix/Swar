import 'dart:async';

import 'package:flutter/material.dart';
import 'package:provider/provider.dart';
import 'package:url_launcher/url_launcher.dart';

import '../core/app_icons.dart';
import '../core/errors.dart';
import '../core/theme.dart';
import '../state/app_status.dart';
import '../state/player_provider.dart';

/// Covers the app with a maintenance screen while the server is in
/// maintenance, and shows the server's active popup once per version.
class AppStatusGate extends StatefulWidget {
  final Widget child;
  const AppStatusGate({super.key, required this.child});

  @override
  State<AppStatusGate> createState() => _AppStatusGateState();
}

class _AppStatusGateState extends State<AppStatusGate> with WidgetsBindingObserver {
  final _status = AppStatus.instance;
  bool _maintenance = false;
  String _shownKey = '';
  NavigatorState? _dialogNav;
  Timer? _timer;

  @override
  void initState() {
    super.initState();
    _status.addListener(_onStatus);
    WidgetsBinding.instance.addObserver(this);
    _status.refresh();
    // Requests already report changes; this catches an app left idle.
    _timer = Timer.periodic(const Duration(minutes: 10), (_) => _status.refresh());
  }

  @override
  void dispose() {
    _timer?.cancel();
    WidgetsBinding.instance.removeObserver(this);
    _status.removeListener(_onStatus);
    super.dispose();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.resumed) _status.refresh();
  }

  void _onStatus() {
    if (_status.maintenance != _maintenance) {
      setState(() => _maintenance = _status.maintenance);
      if (_maintenance) {
        final player = context.read<PlayerProvider>();
        if (player.playing) player.playPause();
      }
    }
    _syncPopup();
  }

  Future<void> _syncPopup() async {
    final key = _status.maintenance ? '' : _status.popupKey;
    if (key == _shownKey) return;
    // A different popup (or none) is active now: close the old one first.
    _closeDialog();
    _shownKey = key;
    final popup = _status.popup;
    if (key.isEmpty || popup == null) return;
    if (popup.dismissible && await AppStatus.isDismissed(key)) return;
    final ctx = ErrorPopup.navigatorKey.currentContext;
    if (ctx == null || !ctx.mounted || !mounted || _shownKey != key) return;
    _dialogNav = Navigator.of(ctx);
    await showDialog<void>(
      context: ctx,
      barrierDismissible: popup.dismissible,
      builder: (_) => ServerPopupDialog(popup: popup, popupKey: key),
    );
    _dialogNav = null;
  }

  void _closeDialog() {
    final nav = _dialogNav;
    _dialogNav = null;
    if (nav != null && nav.mounted) nav.pop();
  }

  @override
  Widget build(BuildContext context) {
    return Stack(
      children: [
        widget.child,
        if (_maintenance)
          Positioned.fill(
            child: MaintenanceScreen(message: _status.maintenanceMessage, onRetry: _status.refresh),
          ),
      ],
    );
  }
}

class MaintenanceScreen extends StatefulWidget {
  final String message;
  final Future<void> Function() onRetry;
  const MaintenanceScreen({super.key, required this.message, required this.onRetry});

  @override
  State<MaintenanceScreen> createState() => _MaintenanceScreenState();
}

class _MaintenanceScreenState extends State<MaintenanceScreen> {
  bool _checking = false;

  Future<void> _retry() async {
    setState(() => _checking = true);
    await widget.onRetry();
    if (mounted) setState(() => _checking = false);
  }

  @override
  Widget build(BuildContext context) {
    final text = Theme.of(context).textTheme;
    return Material(
      color: SonivoColors.background,
      child: DecoratedBox(
        decoration: const BoxDecoration(gradient: SonivoColors.pageGlow),
        child: SafeArea(
          child: Center(
            child: SingleChildScrollView(
              padding: const EdgeInsets.all(Gaps.xl),
              child: ConstrainedBox(
                constraints: const BoxConstraints(maxWidth: 420),
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    Container(
                      padding: const EdgeInsets.all(Gaps.lg),
                      decoration: const BoxDecoration(shape: BoxShape.circle, gradient: SonivoColors.brandGradient),
                      child: const Icon(PhosphorIconsRegular.hardDrives, color: Colors.white, size: 36),
                    ),
                    const SizedBox(height: Gaps.xl),
                    Text('Under maintenance', style: text.headlineSmall, textAlign: TextAlign.center),
                    const SizedBox(height: Gaps.md),
                    Text(
                      widget.message.isNotEmpty
                          ? widget.message
                          : 'Swar is down for maintenance. Please check back soon.',
                      style: text.bodyLarge?.copyWith(color: SonivoColors.textSecondary),
                      textAlign: TextAlign.center,
                    ),
                    const SizedBox(height: Gaps.xl),
                    FilledButton.icon(
                      onPressed: _checking ? null : _retry,
                      icon: _checking
                          ? const SizedBox(width: 16, height: 16, child: CircularProgressIndicator(strokeWidth: 2))
                          : const Icon(PhosphorIconsRegular.arrowClockwise),
                      label: Text(_checking ? 'Checking' : 'Try again'),
                    ),
                    const SizedBox(height: Gaps.md),
                    Text(
                      'Swar will open again by itself when it is back.',
                      style: text.bodySmall?.copyWith(color: SonivoColors.textMuted),
                      textAlign: TextAlign.center,
                    ),
                  ],
                ),
              ),
            ),
          ),
        ),
      ),
    );
  }
}

class ServerPopupDialog extends StatelessWidget {
  final ServerPopup popup;
  final String popupKey;
  const ServerPopupDialog({super.key, required this.popup, required this.popupKey});

  IconData get _icon => switch (popup.kind) {
        'update' => PhosphorIconsRegular.downloadSimple,
        'support' => PhosphorIconsRegular.heart,
        _ => PhosphorIconsRegular.info,
      };

  void _close(BuildContext context) {
    AppStatus.dismiss(popupKey);
    Navigator.pop(context);
  }

  @override
  Widget build(BuildContext context) {
    final text = Theme.of(context).textTheme;
    final url = Uri.tryParse(popup.buttonUrl);
    final hasLink = popup.buttonUrl.isNotEmpty && url != null;
    const buttonShape = StadiumBorder();
    const buttonSize = Size.fromHeight(48);
    return PopScope(
      canPop: popup.dismissible,
      onPopInvokedWithResult: (didPop, _) {
        if (didPop) AppStatus.dismiss(popupKey);
      },
      child: Dialog(
        backgroundColor: SonivoColors.surfaceHigh,
        insetPadding: const EdgeInsets.symmetric(horizontal: Gaps.xl, vertical: Gaps.xxl),
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(Gaps.radiusLg),
          side: const BorderSide(color: SonivoColors.outline),
        ),
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 400),
          child: Padding(
            padding: const EdgeInsets.fromLTRB(Gaps.xl, Gaps.xxl, Gaps.xl, Gaps.xl),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                Center(
                  child: Container(
                    width: 64,
                    height: 64,
                    decoration: const BoxDecoration(shape: BoxShape.circle, gradient: SonivoColors.brandGradient),
                    child: Icon(_icon, color: Colors.white, size: 30),
                  ),
                ),
                const SizedBox(height: Gaps.lg),
                Text(popup.title, style: text.titleLarge, textAlign: TextAlign.center),
                const SizedBox(height: Gaps.sm),
                Flexible(
                  child: SingleChildScrollView(
                    child: Text(
                      popup.message,
                      style: text.bodyMedium?.copyWith(color: SonivoColors.textSecondary, height: 1.45),
                      textAlign: TextAlign.center,
                    ),
                  ),
                ),
                if (hasLink || popup.dismissible) const SizedBox(height: Gaps.xl),
                if (hasLink)
                  FilledButton(
                    style: FilledButton.styleFrom(minimumSize: buttonSize, shape: buttonShape),
                    onPressed: () {
                      launchUrl(url, mode: LaunchMode.externalApplication);
                      if (popup.dismissible) _close(context);
                    },
                    child: Text(popup.buttonText.isNotEmpty ? popup.buttonText : 'Open'),
                  ),
                if (hasLink && popup.dismissible) const SizedBox(height: Gaps.sm),
                if (popup.dismissible && hasLink)
                  TextButton(
                    style: TextButton.styleFrom(minimumSize: buttonSize, shape: buttonShape),
                    onPressed: () => _close(context),
                    child: const Text('Not now'),
                  ),
                if (popup.dismissible && !hasLink)
                  FilledButton(
                    style: FilledButton.styleFrom(minimumSize: buttonSize, shape: buttonShape),
                    onPressed: () => _close(context),
                    child: const Text('OK'),
                  ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}
