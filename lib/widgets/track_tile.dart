import 'package:flutter/material.dart';
import 'package:provider/provider.dart';

import '../core/app_icons.dart';
import '../core/errors.dart';
import '../core/theme.dart';
import '../models/track.dart';
import '../services/download_service.dart';
import '../state/player_provider.dart';
import 'ui.dart';

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

  String _fmt(Duration d) {
    if (d == Duration.zero) return '';
    final s = d.inSeconds.remainder(60).toString().padLeft(2, '0');
    return '${d.inMinutes}:$s';
  }

  @override
  Widget build(BuildContext context) {
    final isCurrent = context.select<PlayerProvider, bool>((p) => p.currentTrack?.id == track.id);
    final wide = MediaQuery.sizeOf(context).width >= Gaps.desktopBreakpoint;
    final side = Gaps.pagePadding(context) - Gaps.sm;

    return Padding(
      padding: EdgeInsets.symmetric(horizontal: side, vertical: 1),
      child: Material(
        color: isCurrent ? SonivoColors.accent.withOpacity(0.08) : Colors.transparent,
        borderRadius: BorderRadius.circular(Gaps.radius),
        child: InkWell(
          onTap: onTap,
          borderRadius: BorderRadius.circular(Gaps.radius),
          hoverColor: SonivoColors.surfaceHigh,
          child: Padding(
            padding: const EdgeInsets.symmetric(horizontal: Gaps.sm, vertical: 6),
            child: Row(
              children: [
                Stack(
                  alignment: Alignment.center,
                  children: [
                    Artwork(url: track.thumbnailUrl, size: 48),
                    if (isCurrent)
                      Container(
                        width: 48,
                        height: 48,
                        decoration: BoxDecoration(
                          color: Colors.black.withOpacity(0.45),
                          borderRadius: BorderRadius.circular(Gaps.radiusSm),
                        ),
                        child: const Icon(PhosphorIconsRegular.waveform, color: SonivoColors.accent),
                      ),
                  ],
                ),
                const SizedBox(width: Gaps.md),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        track.title,
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: TextStyle(
                          fontSize: 15,
                          color: isCurrent ? SonivoColors.accent : SonivoColors.textPrimary,
                          fontWeight: FontWeight.w600,
                        ),
                      ),
                      const SizedBox(height: 2),
                      Text(
                        track.artist.isEmpty ? 'Unknown artist' : track.artist,
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: const TextStyle(
                            fontSize: 13,
                            color: SonivoColors.textSecondary,
                            fontFeatures: [FontFeature.tabularFigures()]),
                      ),
                    ],
                  ),
                ),
                if (wide && track.album.isNotEmpty)
                  Expanded(
                    child: Padding(
                      padding: const EdgeInsets.only(left: Gaps.md),
                      child: Text(
                        track.album,
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: const TextStyle(
                            fontSize: 13,
                            color: SonivoColors.textSecondary,
                            fontFeatures: [FontFeature.tabularFigures()]),
                      ),
                    ),
                  ),
                if (wide)
                  SizedBox(
                    width: 56,
                    child: Text(
                      _fmt(track.duration),
                      textAlign: TextAlign.right,
                      style: const TextStyle(
                          fontSize: 13, color: SonivoColors.textMuted, fontFeatures: [FontFeature.tabularFigures()]),
                    ),
                  ),
                if (showDownload) DownloadButton(track: track),
                if (trailing != null) trailing!,
              ],
            ),
          ),
        ),
      ),
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
          child: Icon(PhosphorIconsFill.checkCircle, color: SonivoColors.accent, size: 22),
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
          icon: const Icon(PhosphorIconsRegular.downloadSimple, color: SonivoColors.textSecondary),
          onPressed: () => _run(context, () => downloads.resume(track.id)),
        );
      default:
        return IconButton(
          tooltip: 'Download',
          icon: const Icon(PhosphorIconsRegular.downloadSimple, color: SonivoColors.textSecondary),
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
