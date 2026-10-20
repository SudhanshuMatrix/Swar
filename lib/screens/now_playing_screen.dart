import 'package:cached_network_image/cached_network_image.dart';
import 'package:flutter/material.dart';
import 'package:provider/provider.dart';

import '../core/theme.dart';
import '../models/track.dart';
import '../state/library_provider.dart';
import '../state/player_provider.dart';

class NowPlayingScreen extends StatelessWidget {
  const NowPlayingScreen({super.key});

  String _fmt(Duration d) {
    final m = d.inMinutes.remainder(60).toString();
    final s = d.inSeconds.remainder(60).toString().padLeft(2, '0');
    return '$m:$s';
  }

  Future<void> _addToPlaylistDialog(BuildContext context, Track track) async {
    final library = context.read<LibraryProvider>();
    if (library.playlists.isEmpty) {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('No playlists yet. Create one in the Library tab.')),
      );
      return;
    }
    final playlistId = await showDialog<String>(
      context: context,
      builder: (ctx) => SimpleDialog(
        title: const Text('Add to playlist'),
        children: library.playlists
            .map((p) => SimpleDialogOption(
                  onPressed: () => Navigator.pop(ctx, p.id),
                  child: Text(p.name),
                ))
            .toList(),
      ),
    );
    if (playlistId != null) {
      await library.addToPlaylist(playlistId, track);
      if (context.mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(content: Text('Added to playlist')),
        );
      }
    }
  }

  void _openQueueSheet(BuildContext context) {
    showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      backgroundColor: SonivoColors.surface,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(20)),
      ),
      builder: (ctx) => DraggableScrollableSheet(
        initialChildSize: 0.75,
        minChildSize: 0.4,
        maxChildSize: 0.95,
        expand: false,
        builder: (_, scrollController) {
          final player = context.watch<PlayerProvider>();
          final queue = player.queue;
          final currentIndex = player.queueIndex;

          return Column(
            children: [
              Container(
                margin: const EdgeInsets.only(top: 8, bottom: 4),
                width: 40,
                height: 4,
                decoration: BoxDecoration(
                  color: Colors.grey[600],
                  borderRadius: BorderRadius.circular(2),
                ),
              ),
              Padding(
                padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
                child: Row(
                  mainAxisAlignment: MainAxisAlignment.spaceBetween,
                  children: [
                    const Text('Play Queue', style: TextStyle(fontSize: 18, fontWeight: FontWeight.bold)),
                    Row(
                      children: [
                        TextButton(
                          onPressed: player.queue.length > 1 ? player.clearQueue : null,
                          child: const Text('Clear Queue', style: TextStyle(color: Colors.grey, fontSize: 13)),
                        ),
                      ],
                    ),
                  ],
                ),
              ),
              Padding(
                padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 4),
                child: Row(
                  mainAxisAlignment: MainAxisAlignment.spaceBetween,
                  children: [
                    const Text('Autoplay similar tracks', style: TextStyle(fontSize: 13, color: Colors.grey)),
                    Switch(
                      value: player.autoPlayRelated,
                      activeColor: SonivoColors.accent,
                      onChanged: (_) => player.toggleAutoPlayRelated(),
                    ),
                  ],
                ),
              ),
              const Divider(),
              Expanded(
                child: queue.isEmpty
                    ? const Center(child: Text('Queue is empty', style: TextStyle(color: Colors.grey)))
                    : ListView.builder(
                        controller: scrollController,
                        itemCount: queue.length,
                        itemBuilder: (context, i) {
                          final t = queue[i];
                          final isCurrent = i == currentIndex;

                          return ListTile(
                            leading: ClipRRect(
                              borderRadius: BorderRadius.circular(4),
                              child: t.thumbnailUrl.isNotEmpty
                                  ? CachedNetworkImage(
                                      imageUrl: t.thumbnailUrl,
                                      width: 44,
                                      height: 44,
                                      fit: BoxFit.cover,
                                    )
                                  : Container(width: 44, height: 44, color: Colors.grey[800]),
                            ),
                            title: Text(
                              t.title,
                              maxLines: 1,
                              overflow: TextOverflow.ellipsis,
                              style: TextStyle(
                                color: isCurrent ? SonivoColors.accent : Colors.white,
                                fontWeight: isCurrent ? FontWeight.bold : FontWeight.normal,
                              ),
                            ),
                            subtitle: Text(
                              t.artist,
                              maxLines: 1,
                              overflow: TextOverflow.ellipsis,
                              style: const TextStyle(color: Colors.grey, fontSize: 12),
                            ),
                            trailing: isCurrent
                                ? const Icon(Icons.volume_up, color: SonivoColors.accent)
                                : IconButton(
                                    icon: const Icon(Icons.close, size: 18, color: Colors.grey),
                                    onPressed: () => player.removeFromQueue(i),
                                  ),
                            onTap: () => player.jumpToQueueIndex(i),
                          );
                        },
                      ),
              ),
            ],
          );
        },
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final player = context.watch<PlayerProvider>();
    final library = context.watch<LibraryProvider>();
    final track = player.currentTrack;

    if (track == null) {
      return const Scaffold(body: Center(child: Text('Nothing playing')));
    }

    final liked = library.isLiked(track.id);

    return Scaffold(
      appBar: AppBar(
        leading: IconButton(
          icon: const Icon(Icons.keyboard_arrow_down),
          tooltip: 'Minimize',
          onPressed: () => Navigator.of(context).maybePop(),
        ),
        title: const Text('Now Playing', style: TextStyle(fontSize: 14, fontWeight: FontWeight.w600)),
        centerTitle: true,
        actions: [
          IconButton(
            icon: const Icon(Icons.playlist_add),
            tooltip: 'Add to playlist',
            onPressed: () => _addToPlaylistDialog(context, track),
          ),
          IconButton(
            icon: const Icon(Icons.queue_music),
            tooltip: 'Queue',
            onPressed: () => _openQueueSheet(context),
          ),
        ],
      ),
      body: SafeArea(
        child: Padding(
          padding: const EdgeInsets.symmetric(horizontal: 24, vertical: 8),
          child: Column(
            children: [
              if (player.errorMessage != null)
                Padding(
                  padding: const EdgeInsets.only(bottom: 8),
                  child: Text(
                    player.errorMessage!,
                    style: const TextStyle(color: Colors.redAccent, fontSize: 12),
                    textAlign: TextAlign.center,
                  ),
                ),
              Expanded(
                child: Center(
                  child: AspectRatio(
                    aspectRatio: 1,
                    child: Container(
                      decoration: BoxDecoration(
                        borderRadius: BorderRadius.circular(12),
                        boxShadow: [
                          BoxShadow(
                            color: Colors.black.withOpacity(0.5),
                            blurRadius: 16,
                            offset: const Offset(0, 8),
                          ),
                        ],
                      ),
                      child: ClipRRect(
                        borderRadius: BorderRadius.circular(12),
                        child: _buildArt(player, track),
                      ),
                    ),
                  ),
                ),
              ),
              const SizedBox(height: 20),
              Row(
                children: [
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          track.title,
                          style: const TextStyle(fontSize: 20, fontWeight: FontWeight.bold),
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                        ),
                        const SizedBox(height: 4),
                        Text(
                          track.artist,
                          style: const TextStyle(fontSize: 15, color: SonivoColors.textSecondary),
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                        ),
                      ],
                    ),
                  ),
                  IconButton(
                    iconSize: 28,
                    icon: Icon(
                      liked ? Icons.favorite : Icons.favorite_border,
                      color: liked ? SonivoColors.accent : Colors.grey,
                    ),
                    onPressed: () => library.toggleLiked(track),
                  ),
                ],
              ),
              const SizedBox(height: 12),
              SliderTheme(
                data: SliderTheme.of(context).copyWith(
                  trackHeight: 3,
                  thumbShape: const RoundSliderThumbShape(enabledThumbRadius: 6),
                ),
                child: Slider(
                  value: player.duration.inMilliseconds == 0
                      ? 0
                      : player.position.inMilliseconds
                          .clamp(0, player.duration.inMilliseconds)
                          .toDouble(),
                  max: player.duration.inMilliseconds == 0 ? 1 : player.duration.inMilliseconds.toDouble(),
                  onChanged: (v) => player.seek(Duration(milliseconds: v.toInt())),
                ),
              ),
              Row(
                mainAxisAlignment: MainAxisAlignment.spaceBetween,
                children: [
                  Text(_fmt(player.position), style: const TextStyle(color: Colors.grey, fontSize: 12)),
                  Text(_fmt(player.duration), style: const TextStyle(color: Colors.grey, fontSize: 12)),
                ],
              ),
              const SizedBox(height: 8),
              Row(
                mainAxisAlignment: MainAxisAlignment.spaceBetween,
                children: [
                  IconButton(
                    icon: Icon(Icons.shuffle, color: player.shuffle ? SonivoColors.accent : Colors.grey),
                    tooltip: 'Shuffle',
                    onPressed: player.toggleShuffle,
                  ),
                  IconButton(
                    icon: const Icon(Icons.skip_previous, size: 36),
                    tooltip: 'Previous',
                    onPressed: player.previous,
                  ),
                  IconButton(
                    iconSize: 64,
                    icon: player.loading
                        ? const SizedBox(width: 44, height: 44, child: CircularProgressIndicator(strokeWidth: 3))
                        : Icon(
                            player.playing ? Icons.pause_circle_filled : Icons.play_circle_filled,
                            color: Colors.white,
                          ),
                    onPressed: player.loading ? null : player.playPause,
                  ),
                  IconButton(
                    icon: const Icon(Icons.skip_next, size: 36),
                    tooltip: 'Next',
                    onPressed: player.next,
                  ),
                  IconButton(
                    icon: Icon(
                      player.repeat == PlaybackRepeatMode.one ? Icons.repeat_one : Icons.repeat,
                      color: player.repeat == PlaybackRepeatMode.off ? Colors.grey : SonivoColors.accent,
                    ),
                    tooltip: 'Repeat',
                    onPressed: player.cycleRepeat,
                  ),
                ],
              ),
              const SizedBox(height: 8),
            ],
          ),
        ),
      ),
    );
  }

  Widget _buildArt(PlayerProvider player, Track track) {
    if (track.thumbnailUrl.isNotEmpty) {
      return CachedNetworkImage(
        imageUrl: track.thumbnailUrl,
        fit: BoxFit.cover,
        errorWidget: (_, __, ___) => Container(color: Colors.grey[850]),
      );
    }
    return Container(color: Colors.grey[850]);
  }
}
