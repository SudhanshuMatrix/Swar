import 'package:flutter/material.dart';
import 'package:provider/provider.dart';

import '../core/theme.dart';
import '../screens/now_playing_screen.dart';
import '../state/player_provider.dart';
import 'track_actions.dart';
import 'ui.dart';

/// The desktop and web player along the bottom of the window: the song on
/// the left, controls and progress in the middle, queue and volume on the
/// right.
class PlayerBar extends StatelessWidget {
  final VoidCallback onOpen;
  const PlayerBar({super.key, required this.onOpen});

  @override
  Widget build(BuildContext context) {
    final player = context.watch<PlayerProvider>();
    final track = player.currentTrack;
    if (track == null) return const SizedBox.shrink();

    final max = player.duration.inMilliseconds == 0 ? 1.0 : player.duration.inMilliseconds.toDouble();
    final value = player.position.inMilliseconds.clamp(0, max.toInt()).toDouble();

    return Container(
      height: 88,
      padding: const EdgeInsets.symmetric(horizontal: Gaps.lg),
      decoration: const BoxDecoration(
        color: SonivoColors.surface,
        border: Border(top: BorderSide(color: SonivoColors.outline)),
      ),
      child: Row(
        children: [
          // Song
          Expanded(
            flex: 3,
            child: InkWell(
              onTap: onOpen,
              borderRadius: BorderRadius.circular(Gaps.radius),
              child: Padding(
                padding: const EdgeInsets.all(Gaps.xs),
                child: Row(
                  children: [
                    Artwork(url: track.thumbnailUrl, size: 56),
                    const SizedBox(width: Gaps.md),
                    Flexible(
                      child: Column(
                        mainAxisAlignment: MainAxisAlignment.center,
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
                    LikeButton(track: track, size: 20),
                  ],
                ),
              ),
            ),
          ),
          // Controls
          Expanded(
            flex: 4,
            child: Column(
              mainAxisAlignment: MainAxisAlignment.center,
              children: [
                Row(
                  mainAxisAlignment: MainAxisAlignment.center,
                  children: [
                    IconButton(
                      iconSize: 20,
                      tooltip: 'Shuffle',
                      icon: Icon(Icons.shuffle_rounded,
                          color: player.shuffle ? SonivoColors.accent : SonivoColors.textSecondary),
                      onPressed: player.toggleShuffle,
                    ),
                    IconButton(
                      tooltip: 'Previous',
                      icon: const Icon(Icons.skip_previous_rounded),
                      onPressed: player.previous,
                    ),
                    const SizedBox(width: Gaps.xs),
                    GradientPlayButton(
                        size: 42, playing: player.playing, loading: player.loading, onPressed: player.playPause),
                    const SizedBox(width: Gaps.xs),
                    IconButton(
                      tooltip: 'Next',
                      icon: const Icon(Icons.skip_next_rounded),
                      onPressed: player.next,
                    ),
                    IconButton(
                      iconSize: 20,
                      tooltip: 'Repeat',
                      icon: Icon(
                        player.repeat == PlaybackRepeatMode.one ? Icons.repeat_one_rounded : Icons.repeat_rounded,
                        color:
                            player.repeat == PlaybackRepeatMode.off ? SonivoColors.textSecondary : SonivoColors.accent,
                      ),
                      onPressed: player.cycleRepeat,
                    ),
                  ],
                ),
                Row(
                  children: [
                    SizedBox(
                      width: 44,
                      child: Text(formatDuration(player.position),
                          textAlign: TextAlign.right,
                          style: const TextStyle(fontSize: 11, color: SonivoColors.textSecondary)),
                    ),
                    Expanded(
                      child: SliderTheme(
                        data: SliderTheme.of(context).copyWith(
                          trackHeight: 3,
                          thumbShape: const RoundSliderThumbShape(enabledThumbRadius: 5),
                        ),
                        child: SizedBox(
                          height: 20,
                          child: Slider(
                            value: value,
                            max: max,
                            onChanged: (v) => player.seek(Duration(milliseconds: v.toInt())),
                          ),
                        ),
                      ),
                    ),
                    SizedBox(
                      width: 44,
                      child: Text(formatDuration(player.duration),
                          style: const TextStyle(fontSize: 11, color: SonivoColors.textSecondary)),
                    ),
                  ],
                ),
              ],
            ),
          ),
          // Queue and volume
          Expanded(
            flex: 3,
            child: Row(
              mainAxisAlignment: MainAxisAlignment.end,
              children: [
                IconButton(
                  tooltip: 'Queue',
                  icon: const Icon(Icons.queue_music_rounded, size: 20),
                  onPressed: () => showQueueSheet(context),
                ),
                IconButton(
                  tooltip: player.volume == 0 ? 'Unmute' : 'Mute',
                  icon: Icon(
                    player.volume == 0
                        ? Icons.volume_off_rounded
                        : player.volume < 0.5
                            ? Icons.volume_down_rounded
                            : Icons.volume_up_rounded,
                    size: 20,
                  ),
                  onPressed: () => player.setVolume(player.volume == 0 ? 1 : 0),
                ),
                if (MediaQuery.sizeOf(context).width >= 1100)
                  SizedBox(
                    width: 120,
                    child: SliderTheme(
                      data: SliderTheme.of(context).copyWith(
                        trackHeight: 3,
                        thumbShape: const RoundSliderThumbShape(enabledThumbRadius: 5),
                      ),
                      child: Slider(value: player.volume, onChanged: player.setVolume),
                    ),
                  ),
                IconButton(
                  tooltip: 'Full screen player',
                  icon: const Icon(Icons.open_in_full_rounded, size: 18),
                  onPressed: onOpen,
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}
