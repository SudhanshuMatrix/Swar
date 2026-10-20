import 'package:cached_network_image/cached_network_image.dart';
import 'package:flutter/material.dart';
import 'package:provider/provider.dart';

import '../core/errors.dart';
import '../core/theme.dart';
import '../models/track.dart';
import '../services/download_service.dart';
import '../state/player_provider.dart';

class TrackTile extends StatelessWidget {
  final Track track;
  final VoidCallback onTap;
  final Widget? trailing;

  /// Shows a download button before [trailing].
  final bool showDownload;

  const TrackTile({
    super.key,
    required this.track,
    required this.onTap,
    this.trailing,
    this.showDownload = true,
  });

  @override
  Widget build(BuildContext context) {
    final isCurrent = context.select<PlayerProvider, bool>((p) => p.currentTrack?.id == track.id);

    return ListTile(
      leading: ClipRRect(
        borderRadius: BorderRadius.circular(4),
        child: track.thumbnailUrl.isNotEmpty
            ? CachedNetworkImage(
                imageUrl: track.thumbnailUrl,
                width: 48,
                height: 48,
                fit: BoxFit.cover,
                errorWidget: (_, __, ___) => Container(width: 48, height: 48, color: Colors.grey[800]),
                placeholder: (_, __) => Container(width: 48, height: 48, color: Colors.grey[800]),
              )
            : Container(width: 48, height: 48, color: Colors.grey[800]),
      ),
      title: Text(
        track.title,
        maxLines: 1,
        overflow: TextOverflow.ellipsis,
        style: TextStyle(
          color: isCurrent ? SonivoColors.accent : Colors.white,
          fontWeight: isCurrent ? FontWeight.bold : FontWeight.normal,
        ),
      ),
      subtitle: Text(
        track.artist,
        maxLines: 1,
        overflow: TextOverflow.ellipsis,
        style: const TextStyle(color: Colors.grey),
      ),
      trailing: showDownload
          ? Row(
              mainAxisSize: MainAxisSize.min,
              children: [DownloadButton(track: track), if (trailing != null) trailing!],
            )
          : trailing,
      onTap: onTap,
    );
  }
}

/// Download state for one song: tap to download, shows progress while it
/// runs and a check mark once the song is on the device.
class DownloadButton extends StatelessWidget {
  final Track track;
  const DownloadButton({super.key, required this.track});

  @override
  Widget build(BuildContext context) {
    final status = context.select<DownloadService, DownloadStatus?>((d) => d.statusOf(track.id));
    final progress = context.select<DownloadService, double>((d) => d.progressOf(track.id));
    final downloads = context.read<DownloadService>();
    if (!downloads.supported) return const SizedBox.shrink();

    switch (status) {
      case DownloadStatus.completed:
        return const Padding(
          padding: EdgeInsets.all(12),
          child: Icon(Icons.download_done, color: SonivoColors.accent, size: 22),
        );
      case DownloadStatus.queued:
      case DownloadStatus.downloading:
      case DownloadStatus.verifying:
        return IconButton(
          tooltip: 'Pause download',
          onPressed: () => downloads.pause(track.id),
          icon: SizedBox(
            width: 20,
            height: 20,
            child: CircularProgressIndicator(
              strokeWidth: 2.5,
              value: progress > 0 ? progress : null,
              color: SonivoColors.accent,
            ),
          ),
        );
      case DownloadStatus.paused:
        return IconButton(
          tooltip: 'Resume download',
          icon: const Icon(Icons.play_for_work, color: Colors.white70),
          onPressed: () => _run(context, () => downloads.resume(track.id)),
        );
      default:
        return IconButton(
          tooltip: 'Download',
          icon: const Icon(Icons.download_outlined, color: Colors.white70),
          onPressed: () => _run(context, () => downloads.download(track)),
        );
    }
  }

  Future<void> _run(BuildContext context, Future<void> Function() action) async {
    try {
      await action();
    } catch (e) {
      ErrorPopup.show(e, title: 'Couldn\'t download "${track.title}"');
    }
  }
}
