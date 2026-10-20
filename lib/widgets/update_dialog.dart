import 'package:flutter/material.dart';
import 'package:url_launcher/url_launcher.dart';

import '../core/app_icons.dart';
import '../core/theme.dart';
import '../services/update_service.dart';

/// Checks for a newer release once, quietly, a few seconds after launch.
Future<void> checkForUpdatesOnStart(BuildContext context) async {
  await Future<void>.delayed(const Duration(seconds: 4));
  try {
    final info = await UpdateService().check();
    if (info == null || await UpdateService.isSkipped(info.version)) return;
    if (context.mounted) await showUpdateDialog(context, info);
  } catch (_) {
    // Offline or rate limited: try again next launch.
  }
}

/// The "Check for updates" button: always answers, even when up to date.
Future<void> checkForUpdatesNow(BuildContext context) async {
  final messenger = ScaffoldMessenger.maybeOf(context);
  try {
    final info = await UpdateService().check();
    if (!context.mounted) return;
    if (info == null) {
      final current = await UpdateService.currentVersion();
      messenger?.showSnackBar(SnackBar(content: Text('You have the latest version ($current).')));
      return;
    }
    await showUpdateDialog(context, info);
  } catch (_) {
    messenger?.showSnackBar(const SnackBar(content: Text("Couldn't check for updates. Try again later.")));
  }
}

/// Turns the release's Markdown changelog into plain readable lines.
String readableNotes(String markdown) {
  return markdown
      .split('\n')
      .map((l) => l.trimRight())
      .map((l) => l.replaceFirst(RegExp(r'^#{1,6}\s*'), ''))
      .map((l) => l.replaceFirst(RegExp(r'^\s*[-*]\s+'), '•  '))
      .map((l) => l.replaceAll('**', '').replaceAll('`', ''))
      .join('\n')
      .replaceAll(RegExp(r'\n{3,}'), '\n\n')
      .trim();
}

Future<void> showUpdateDialog(BuildContext context, UpdateInfo info) {
  return showDialog<void>(
    context: context,
    builder: (ctx) => AlertDialog(
      icon: Container(
        padding: const EdgeInsets.all(Gaps.md),
        decoration: const BoxDecoration(shape: BoxShape.circle, gradient: SonivoColors.brandGradient),
        child: const Icon(PhosphorIconsRegular.downloadSimple, color: Colors.white),
      ),
      title: Text('Swar ${info.version} is available'),
      content: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 460, maxHeight: 320),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('You have ${info.current}.', style: Theme.of(ctx).textTheme.bodySmall),
            if (info.notes.isNotEmpty) ...[
              const SizedBox(height: Gaps.md),
              Text("What's new", style: Theme.of(ctx).textTheme.titleSmall),
              const SizedBox(height: Gaps.xs),
              Flexible(
                child: SingleChildScrollView(
                  child: Text(readableNotes(info.notes), style: Theme.of(ctx).textTheme.bodyMedium),
                ),
              ),
            ],
          ],
        ),
      ),
      actions: [
        TextButton(
          onPressed: () {
            UpdateService.skip(info.version);
            Navigator.pop(ctx);
          },
          child: const Text('Skip this version'),
        ),
        TextButton(onPressed: () => Navigator.pop(ctx), child: const Text('Later')),
        FilledButton(
          onPressed: () {
            Navigator.pop(ctx);
            launchUrl(Uri.parse(info.downloadUrl), mode: LaunchMode.externalApplication);
          },
          child: const Text('Download'),
        ),
      ],
    ),
  );
}

/// Shows the running version with a button to check for a newer one.
class AboutTile extends StatefulWidget {
  const AboutTile({super.key});

  @override
  State<AboutTile> createState() => _AboutTileState();
}

class _AboutTileState extends State<AboutTile> {
  String _version = '';
  bool _checking = false;

  @override
  void initState() {
    super.initState();
    UpdateService.currentVersion().then((v) {
      if (mounted) setState(() => _version = v.toString());
    }).catchError((_) {});
  }

  @override
  Widget build(BuildContext context) {
    return ListTile(
      leading: const Icon(PhosphorIconsRegular.info),
      title: const Text('About Swar'),
      subtitle: Text(_version.isEmpty ? 'Version' : 'Version $_version'),
      trailing: _checking
          ? const SizedBox(width: 20, height: 20, child: CircularProgressIndicator(strokeWidth: 2))
          : TextButton(
              onPressed: () async {
                setState(() => _checking = true);
                await checkForUpdatesNow(context);
                if (mounted) setState(() => _checking = false);
              },
              child: const Text('Check for updates'),
            ),
    );
  }
}
