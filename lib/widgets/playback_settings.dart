import 'package:flutter/material.dart';
import 'package:provider/provider.dart';

import '../core/app_icons.dart';
import '../core/theme.dart';
import '../services/app_settings.dart';
import '../services/ytdlp/device_ytdlp.dart';

/// Library settings for next-song preparation and yt-dlp on this device.
class PlaybackSettingsTiles extends StatelessWidget {
  const PlaybackSettingsTiles({super.key});

  @override
  Widget build(BuildContext context) {
    final settings = context.watch<AppSettings>();
    const secondary = TextStyle(color: SonivoColors.textSecondary);
    return Column(
      children: [
        SwitchListTile(
          title: const Text('Prepare the next song', style: TextStyle(fontWeight: FontWeight.w600)),
          subtitle: const Text('Gets the next song ready while this one plays, so it starts at once.', style: secondary),
          value: settings.prefetchEnabled,
          onChanged: settings.setPrefetchEnabled,
        ),
        SwitchListTile(
          title: const Text('Data saver', style: TextStyle(fontWeight: FontWeight.w600)),
          subtitle: const Text('Prepares one song ahead instead of two, and skips cover art.', style: secondary),
          value: settings.dataSaver,
          onChanged: settings.prefetchEnabled ? settings.setDataSaver : null,
        ),
        if (DeviceYtDlp.supported) const _YtDlpTile(),
      ],
    );
  }
}

class _YtDlpTile extends StatelessWidget {
  const _YtDlpTile();

  String _ago(DateTime? t) {
    if (t == null) return 'never';
    final d = DateTime.now().difference(t);
    if (d.inMinutes < 2) return 'just now';
    if (d.inHours < 1) return '${d.inMinutes} minutes ago';
    if (d.inDays < 1) return '${d.inHours} hours ago';
    return '${d.inDays} days ago';
  }

  @override
  Widget build(BuildContext context) {
    final yt = context.watch<DeviceYtDlp>();
    return ValueListenableBuilder<YtDlpSetup>(
      valueListenable: yt.setup,
      builder: (context, s, _) {
        final busy = s.phase == YtDlpPhase.downloading;
        final subtitle = busy
            ? 'Downloading${s.progress == null ? '…' : ' ${(s.progress! * 100).round()}%'}'
            : yt.installed
                ? 'Version ${yt.version ?? '?'} · checked ${_ago(yt.lastCheck)}. Updates itself in the background.'
                : (s.error ?? 'Not downloaded yet. Deep Search uses the server meanwhile.');
        return ListTile(
          leading: const CircleAvatar(
            backgroundColor: SonivoColors.surfaceHigh,
            child: Icon(PhosphorIconsRegular.globe, color: SonivoColors.accent),
          ),
          title: const Text('yt-dlp on this device', style: TextStyle(fontWeight: FontWeight.w600)),
          subtitle: Text(subtitle, style: const TextStyle(color: SonivoColors.textSecondary)),
          trailing: busy
              ? const SizedBox(width: 20, height: 20, child: CircularProgressIndicator(strokeWidth: 2))
              : TextButton(
                  onPressed: () async {
                    final ok = await yt.install();
                    if (!context.mounted) return;
                    ScaffoldMessenger.of(context).showSnackBar(SnackBar(
                      content: Text(ok ? 'yt-dlp ${yt.version ?? ''} is up to date.' : (yt.setup.value.error ?? 'Update failed.')),
                    ));
                  },
                  child: Text(yt.installed ? 'Update now' : 'Download'),
                ),
        );
      },
    );
  }
}
