import 'package:flutter/material.dart';
import 'package:provider/provider.dart';

import '../core/errors.dart';
import '../core/theme.dart';
import '../services/download_service.dart';
import '../state/player_provider.dart';
import '../widgets/track_tile.dart';

String formatBytes(int bytes) {
  if (bytes < 1024) return '$bytes B';
  if (bytes < 1024 * 1024) return '${(bytes / 1024).toStringAsFixed(0)} KB';
  if (bytes < 1024 * 1024 * 1024) return '${(bytes / (1024 * 1024)).toStringAsFixed(1)} MB';
  return '${(bytes / (1024 * 1024 * 1024)).toStringAsFixed(2)} GB';
}

/// Offline downloads: progress, pause/resume, cancel, retry and storage.
class DownloadsScreen extends StatelessWidget {
  const DownloadsScreen({super.key});

  @override
  Widget build(BuildContext context) {
    final downloads = context.watch<DownloadService>();
    final player = context.read<PlayerProvider>();
    final items = downloads.items;
    final ready = items.where((i) => i.status == DownloadStatus.completed).map((i) => i.track).toList();

    return Scaffold(
      appBar: AppBar(
        title: const Text('Offline downloads'),
        actions: [
          if (ready.isNotEmpty)
            IconButton(
              tooltip: 'Play all offline',
              icon: const Icon(Icons.play_circle_fill, color: SonivoColors.accent),
              onPressed: () => player.playQueue(ready, 0),
            ),
        ],
      ),
      body: !downloads.supported
          ? const Center(child: Text('Offline downloads are available in the mobile and desktop apps.'))
          : Column(
              children: [
                ListTile(
                  leading: const Icon(Icons.sd_storage_outlined),
                  title: Text('${downloads.completedCount} songs offline'),
                  subtitle: Text('${formatBytes(downloads.bytesUsed)} used on this device'),
                ),
                const Divider(height: 1),
                Expanded(
                  child: items.isEmpty
                      ? const Center(
                          child: Padding(
                            padding: EdgeInsets.all(24),
                            child: Text(
                              'Nothing downloaded yet.\nUse "Download" in a song\'s menu where your server allows it.',
                              textAlign: TextAlign.center,
                              style: TextStyle(color: SonivoColors.textSecondary),
                            ),
                          ),
                        )
                      : ListView.builder(
                          itemCount: items.length,
                          itemBuilder: (context, i) => _DownloadTile(item: items[i]),
                        ),
                ),
              ],
            ),
    );
  }
}

class _DownloadTile extends StatelessWidget {
  final DownloadItem item;
  const _DownloadTile({required this.item});

  String _statusText() {
    switch (item.status) {
      case DownloadStatus.queued:
        return 'Waiting…';
      case DownloadStatus.downloading:
        return item.bytesTotal > 0
            ? '${(item.progress * 100).toStringAsFixed(0)}% · ${formatBytes(item.bytesDone)} of ${formatBytes(item.bytesTotal)}'
            : formatBytes(item.bytesDone);
      case DownloadStatus.paused:
        return 'Paused · ${formatBytes(item.bytesDone)}';
      case DownloadStatus.verifying:
        return 'Verifying…';
      case DownloadStatus.completed:
        return 'Offline · ${formatBytes(item.bytesTotal)}';
      case DownloadStatus.failed:
        return item.error.isEmpty ? 'Failed' : item.error;
      case DownloadStatus.canceled:
        return item.error.isEmpty ? 'Canceled' : item.error;
    }
  }

  Future<void> _run(BuildContext context, Future<void> Function() action) async {
    try {
      await action();
    } catch (e) {
      ErrorPopup.show(e, title: 'Download problem');
    }
  }

  @override
  Widget build(BuildContext context) {
    final downloads = context.read<DownloadService>();
    final player = context.read<PlayerProvider>();
    final id = item.track.id;
    final active = item.status == DownloadStatus.downloading || item.status == DownloadStatus.queued;

    return Column(
      children: [
        TrackTile(
          track: item.track,
          showDownload: false,
          onTap: () {
            if (item.status == DownloadStatus.completed) player.playSingle(item.track);
          },
          trailing: Row(
            mainAxisSize: MainAxisSize.min,
            children: [
              if (active)
                IconButton(tooltip: 'Pause', icon: const Icon(Icons.pause), onPressed: () => downloads.pause(id)),
              if (item.status == DownloadStatus.paused)
                IconButton(
                  tooltip: 'Resume',
                  icon: const Icon(Icons.play_arrow),
                  onPressed: () => _run(context, () => downloads.resume(id)),
                ),
              if (item.status == DownloadStatus.failed || item.status == DownloadStatus.canceled)
                IconButton(
                  tooltip: 'Retry',
                  icon: const Icon(Icons.refresh),
                  onPressed: () => _run(context, () => downloads.download(item.track)),
                ),
              if (active || item.status == DownloadStatus.paused)
                IconButton(tooltip: 'Cancel', icon: const Icon(Icons.close), onPressed: () => downloads.cancel(id)),
              if (!active && item.status != DownloadStatus.paused)
                IconButton(
                  tooltip: 'Delete',
                  icon: const Icon(Icons.delete_outline),
                  onPressed: () => downloads.remove(id),
                ),
            ],
          ),
        ),
        Padding(
          padding: const EdgeInsets.fromLTRB(80, 0, 16, 8),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              if (item.status == DownloadStatus.downloading || item.status == DownloadStatus.paused)
                LinearProgressIndicator(
                  value: item.bytesTotal > 0 ? item.progress : null,
                  color: SonivoColors.accent,
                  backgroundColor: SonivoColors.outline,
                ),
              const SizedBox(height: 4),
              Text(
                _statusText(),
                style: TextStyle(
                  fontSize: 12,
                  color: item.status == DownloadStatus.failed ? SonivoColors.danger : SonivoColors.textSecondary,
                ),
              ),
            ],
          ),
        ),
      ],
    );
  }
}
