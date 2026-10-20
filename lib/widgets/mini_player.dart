import 'package:flutter/material.dart';
import 'package:provider/provider.dart';

import '../core/app_icons.dart';
import '../core/theme.dart';
import '../state/player_provider.dart';
import 'ui.dart';

/// Floating player card above the bottom navigation on phones. Tap it to
/// open the full player.
class MiniPlayer extends StatelessWidget {
  final VoidCallback onOpen;
  const MiniPlayer({super.key, required this.onOpen});

  @override
  Widget build(BuildContext context) {
    final player = context.watch<PlayerProvider>();
    final track = player.currentTrack;
    if (track == null) return const SizedBox.shrink();

    final progress =
        player.duration.inMilliseconds == 0 ? 0.0 : player.position.inMilliseconds / player.duration.inMilliseconds;

    return Padding(
      padding: const EdgeInsets.fromLTRB(Gaps.sm, 0, Gaps.sm, Gaps.sm),
      child: Material(
        color: SonivoColors.surfaceHigher,
        borderRadius: BorderRadius.circular(Gaps.radius),
        clipBehavior: Clip.antiAlias,
        child: InkWell(
          onTap: onOpen,
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Padding(
                padding: const EdgeInsets.fromLTRB(Gaps.sm, Gaps.sm, Gaps.xs, Gaps.sm),
                child: Row(
                  children: [
                    Hero(tag: 'now-playing-art', child: Artwork(url: track.thumbnailUrl, size: 44)),
                    const SizedBox(width: Gaps.md),
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(track.title,
                              maxLines: 1,
                              overflow: TextOverflow.ellipsis,
                              style: const TextStyle(fontWeight: FontWeight.w700, fontSize: 14)),
                          const SizedBox(height: 2),
                          Text(track.artist,
                              maxLines: 1,
                              overflow: TextOverflow.ellipsis,
                              style: const TextStyle(color: SonivoColors.textSecondary, fontSize: 12)),
                        ],
                      ),
                    ),
                    GradientPlayButton(
                      size: 40,
                      playing: player.playing,
                      loading: player.loading,
                      onPressed: player.playPause,
                    ),
                    IconButton(
                      tooltip: 'Next',
                      icon: const Icon(PhosphorIconsFill.skipForward),
                      onPressed: player.next,
                    ),
                  ],
                ),
              ),
              LinearProgressIndicator(
                value: progress.clamp(0, 1).toDouble(),
                minHeight: 2,
                color: SonivoColors.accent,
                backgroundColor: SonivoColors.outline,
              ),
            ],
          ),
        ),
      ),
    );
  }
}
