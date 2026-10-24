import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';

import '../core/app_icons.dart';
import '../core/theme.dart';
import '../services/app_settings.dart';
import '../services/ytdlp/device_ytdlp.dart';

/// Shows the first-launch yt-dlp setup, and the download popup when
/// updates have failed for two days. Everything else passes through.
///
/// - First launch: a full page downloads yt-dlp. If it fails the user can
///   retry or skip; skipping leaves Swar working (JioSaavn, downloads, the
///   server) and the background keeps trying.
/// - Updates run silently in the background. If they keep failing for
///   [DeviceYtDlp.forceAfter], a popup covers the app and downloads until
///   it succeeds, retrying by itself. After three failures in a row it also
///   offers "Not now" (until the next launch), so someone offline can still
///   play their downloaded songs.
class YtDlpGate extends StatefulWidget {
  final Widget child;
  const YtDlpGate({super.key, required this.child});

  @override
  State<YtDlpGate> createState() => _YtDlpGateState();
}

class _YtDlpGateState extends State<YtDlpGate> {
  final _yt = DeviceYtDlp.instance;
  bool _popupDismissed = false;
  int _popupFailures = 0;
  bool _popupRunning = false;
  Timer? _retry;

  @override
  void initState() {
    super.initState();
    _yt.addListener(_changed);
    _yt.setup.addListener(_changed);
    AppSettings.instance.addListener(_changed);
    WidgetsBinding.instance.addPostFrameCallback((_) => _changed());
  }

  @override
  void dispose() {
    _retry?.cancel();
    _yt.removeListener(_changed);
    _yt.setup.removeListener(_changed);
    AppSettings.instance.removeListener(_changed);
    super.dispose();
  }

  bool get _showPopup => _yt.mustDownloadNow && !_popupDismissed;

  void _changed() {
    if (!mounted) return;
    setState(() {});
    if (_showPopup && !_popupRunning) _popupDownload();
  }

  /// Downloads until it works, waiting longer after each failure.
  Future<void> _popupDownload() async {
    _retry?.cancel();
    _popupRunning = true;
    final ok = await _yt.install();
    _popupRunning = false;
    if (!mounted) return;
    if (ok) {
      _popupFailures = 0;
      setState(() {});
      return;
    }
    setState(() => _popupFailures++);
    if (_showPopup) {
      final wait = Duration(seconds: 10 * (1 << (_popupFailures - 1).clamp(0, 4)));
      _retry = Timer(wait, () {
        if (mounted && _showPopup && !_popupRunning) _popupDownload();
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    if (!DeviceYtDlp.supported) return widget.child;
    if (_yt.needsOnboarding) return const _OnboardingPage();
    return Stack(
      children: [
        widget.child,
        if (_showPopup)
          Positioned.fill(
            child: _DownloadPopup(
              failures: _popupFailures,
              onRetry: _popupRunning ? null : _popupDownload,
              onNotNow: _popupFailures >= 3
                  ? () {
                      _retry?.cancel();
                      setState(() => _popupDismissed = true);
                    }
                  : null,
            ),
          ),
      ],
    );
  }
}

String _sizeHint() {
  if (!kIsWeb && defaultTargetPlatform == TargetPlatform.android) return 'about 3 MB';
  return 'about 20 to 40 MB';
}

class _Progress extends StatelessWidget {
  final YtDlpSetup setup;
  const _Progress({required this.setup});

  @override
  Widget build(BuildContext context) {
    final p = setup.progress;
    return Column(
      children: [
        ClipRRect(
          borderRadius: BorderRadius.circular(4),
          child: LinearProgressIndicator(value: p, minHeight: 6, color: SonivoColors.accent),
        ),
        const SizedBox(height: 8),
        Text(
          p == null ? 'Downloading…' : 'Downloading… ${(p * 100).round()}%',
          style: const TextStyle(color: SonivoColors.textSecondary, fontSize: 13),
        ),
      ],
    );
  }
}

/// First launch: downloads yt-dlp before the app opens.
class _OnboardingPage extends StatefulWidget {
  const _OnboardingPage();

  @override
  State<_OnboardingPage> createState() => _OnboardingPageState();
}

class _OnboardingPageState extends State<_OnboardingPage> {
  final _yt = DeviceYtDlp.instance;

  @override
  void initState() {
    super.initState();
    _start();
  }

  Future<void> _start() async {
    if (await _yt.install()) await AppSettings.instance.setYtdlpOnboarded(true);
  }

  Future<void> _skip() async {
    // Counts as failing from now, so the popup comes after two days if the
    // background never manages it.
    if (AppSettings.instance.ytdlpFailingSince == null) {
      await AppSettings.instance.setYtdlpFailingSince(DateTime.now());
    }
    await AppSettings.instance.setYtdlpOnboarded(true);
  }

  @override
  Widget build(BuildContext context) {
    return Material(
      color: SonivoColors.background,
      child: SafeArea(
        child: Center(
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 420),
            child: Padding(
              padding: const EdgeInsets.all(24),
              child: ValueListenableBuilder<YtDlpSetup>(
                valueListenable: _yt.setup,
                builder: (context, s, _) {
                  final failed = s.phase == YtDlpPhase.failed || s.phase == YtDlpPhase.unsupported;
                  return Column(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      const Icon(PhosphorIconsRegular.globe, size: 56, color: SonivoColors.accent),
                      const SizedBox(height: 20),
                      Text('Getting Swar ready',
                          style: Theme.of(context).textTheme.headlineSmall, textAlign: TextAlign.center),
                      const SizedBox(height: 12),
                      Text(
                        'Swar downloads yt-dlp once (${_sizeHint()}). It lets Deep Search find and play songs '
                        'from YouTube on this device. It updates itself quietly in the background.',
                        textAlign: TextAlign.center,
                        style: const TextStyle(color: SonivoColors.textSecondary, height: 1.4),
                      ),
                      const SizedBox(height: 28),
                      if (!failed) _Progress(setup: s),
                      if (failed) ...[
                        Text(s.error ?? "Couldn't download yt-dlp.",
                            textAlign: TextAlign.center, style: const TextStyle(color: SonivoColors.danger)),
                        const SizedBox(height: 20),
                        SizedBox(
                          width: double.infinity,
                          child: FilledButton(onPressed: _start, child: const Text('Try again')),
                        ),
                        const SizedBox(height: 8),
                        SizedBox(
                          width: double.infinity,
                          child: TextButton(onPressed: _skip, child: const Text('Skip for now')),
                        ),
                        const SizedBox(height: 4),
                        const Text(
                          'Swar works without it, and keeps trying in the background.',
                          textAlign: TextAlign.center,
                          style: TextStyle(color: SonivoColors.textMuted, fontSize: 12),
                        ),
                      ],
                    ],
                  );
                },
              ),
            ),
          ),
        ),
      ),
    );
  }
}

/// Covers the app until yt-dlp is downloaded, after two days of failed
/// updates.
class _DownloadPopup extends StatelessWidget {
  final int failures;
  final VoidCallback? onRetry;
  final VoidCallback? onNotNow;
  const _DownloadPopup({required this.failures, this.onRetry, this.onNotNow});

  @override
  Widget build(BuildContext context) {
    return Material(
      color: Colors.black54,
      child: Center(
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 400),
          child: Card(
            margin: const EdgeInsets.all(24),
            child: Padding(
              padding: const EdgeInsets.all(24),
              child: ValueListenableBuilder<YtDlpSetup>(
                valueListenable: DeviceYtDlp.instance.setup,
                builder: (context, s, _) {
                  final downloading = s.phase == YtDlpPhase.downloading;
                  return Column(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      const Icon(PhosphorIconsRegular.globe, size: 40, color: SonivoColors.accent),
                      const SizedBox(height: 16),
                      Text('Downloading yt-dlp',
                          style: Theme.of(context).textTheme.titleLarge, textAlign: TextAlign.center),
                      const SizedBox(height: 10),
                      const Text(
                        "Swar couldn't update yt-dlp for two days, so it's downloading it now. "
                        'This stays open until the download finishes.',
                        textAlign: TextAlign.center,
                        style: TextStyle(color: SonivoColors.textSecondary, height: 1.4),
                      ),
                      const SizedBox(height: 24),
                      if (downloading) _Progress(setup: s),
                      if (!downloading && failures > 0) ...[
                        Text(s.error ?? "Couldn't download yt-dlp.",
                            textAlign: TextAlign.center, style: const TextStyle(color: SonivoColors.danger)),
                        const SizedBox(height: 4),
                        const Text('Trying again shortly…',
                            style: TextStyle(color: SonivoColors.textMuted, fontSize: 12)),
                        const SizedBox(height: 16),
                        SizedBox(
                          width: double.infinity,
                          child: FilledButton(onPressed: onRetry, child: const Text('Try again now')),
                        ),
                        if (onNotNow != null)
                          SizedBox(
                            width: double.infinity,
                            child: TextButton(onPressed: onNotNow, child: const Text('Not now')),
                          ),
                      ],
                    ],
                  );
                },
              ),
            ),
          ),
        ),
      ),
    );
  }
}
