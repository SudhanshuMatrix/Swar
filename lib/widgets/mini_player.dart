import 'package:cached_network_image/cached_network_image.dart';
import 'package:flutter/material.dart';
import 'package:provider/provider.dart';

import '../state/player_provider.dart';

/// Persistent bottom bar, Spotify-style — tap it to open the full player.
class MiniPlayer extends StatelessWidget {
  const MiniPlayer({super.key});

  @override
  Widget build(BuildContext context) {
    final player = context.watch<PlayerProvider>();
    final track = player.currentTrack;
    if (track == null) return const SizedBox.shrink();

    final progress = player.duration.inMilliseconds == 0
        ? 0.0
        : player.position.inMilliseconds / player.duration.inMilliseconds;

    return Material(
      color: const Color(0xFF282828),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          LinearProgressIndicator(
            value: progress.clamp(0, 1),
            minHeight: 2.5,
            color: const Color(0xFF1DB954),
            backgroundColor: Colors.grey[800],
          ),
          ListTile(
            dense: true,
            leading: ClipRRect(
              borderRadius: BorderRadius.circular(4),
              child: track.thumbnailUrl.isNotEmpty
                  ? CachedNetworkImage(imageUrl: track.thumbnailUrl, width: 40, height: 40, fit: BoxFit.cover)
                  : Container(width: 40, height: 40, color: Colors.grey[800]),
            ),
            title: Text(track.title, maxLines: 1, overflow: TextOverflow.ellipsis),
            subtitle: Text(track.artist, maxLines: 1, overflow: TextOverflow.ellipsis),
            trailing: Row(
              mainAxisSize: MainAxisSize.min,
              children: [
                IconButton(
                  icon: player.loading
                      ? const SizedBox(
                          width: 20,
                          height: 20,
                          child: CircularProgressIndicator(strokeWidth: 2),
                        )
                      : Icon(player.playing ? Icons.pause : Icons.play_arrow),
                  onPressed: player.loading ? null : player.playPause,
                ),
                IconButton(icon: const Icon(Icons.skip_next), onPressed: player.next),
              ],
            ),
          ),
        ],
      ),
    );
  }
}
