import 'package:flutter/material.dart';
import 'package:provider/provider.dart';

import '../models/track.dart';
import '../state/player_provider.dart';
import 'ui.dart';

/// The big play button on a playlist or singer page. It plays [tracks] from
/// the top, and while they are what's playing it pauses and resumes them.
class PlayAllButton extends StatelessWidget {
  final List<Track> tracks;

  /// Starts the list another way, e.g. skipping songs that can't play.
  final VoidCallback? onPlay;
  final double size;

  const PlayAllButton({super.key, required this.tracks, this.onPlay, this.size = 48});

  @override
  Widget build(BuildContext context) {
    final player = context.read<PlayerProvider>();
    final active = context.select<PlayerProvider, bool>((p) => p.isQueueOf(tracks));
    final playing = context.select<PlayerProvider, bool>((p) => active && p.playing);
    final loading = context.select<PlayerProvider, bool>((p) => active && p.loading);
    return GradientPlayButton(
      size: size,
      playing: playing,
      loading: loading,
      onPressed: tracks.isEmpty
          ? null
          : active
              ? player.playPause
              : (onPlay ?? () => player.playQueue(tracks, 0)),
    );
  }
}
