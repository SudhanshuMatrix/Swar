import 'package:flutter/material.dart';
import 'package:provider/provider.dart';

import '../core/theme.dart';
import '../models/track.dart';
import '../state/library_provider.dart';
import '../state/player_provider.dart';
import '../widgets/track_tile.dart';
import '../widgets/ui.dart';

class PlaylistScreen extends StatelessWidget {
  final String title;
  final List<Track>? initialTracks;
  final String? playlistId; // null for built-in "Liked Songs"

  const PlaylistScreen({
    super.key,
    required this.title,
    List<Track>? tracks,
    this.playlistId,
  }) : initialTracks = tracks;

  String _formatTotalDuration(List<Track> tracks) {
    final totalSeconds = tracks.fold<int>(0, (sum, t) => sum + t.duration.inSeconds);
    final hours = totalSeconds ~/ 3600;
    final minutes = (totalSeconds % 3600) ~/ 60;
    if (hours > 0) {
      return '$hours hr $minutes min';
    }
    return '$minutes min';
  }

  Future<void> _addToAnotherPlaylistDialog(BuildContext context, Track track) async {
    final library = context.read<LibraryProvider>();
    final available = library.playlists.where((p) => p.id != playlistId).toList();
    if (available.isEmpty) {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('No other playlists available')),
      );
      return;
    }
    final targetId = await showDialog<String>(
      context: context,
      builder: (ctx) => SimpleDialog(
        title: const Text('Add to playlist'),
        children: available
            .map((p) => SimpleDialogOption(
                  onPressed: () => Navigator.pop(ctx, p.id),
                  child: Text(p.name),
                ))
            .toList(),
      ),
    );
    if (targetId != null) {
      await library.addToPlaylist(targetId, track);
      if (context.mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(content: Text('Added to playlist')),
        );
      }
    }
  }

  Future<void> _deletePlaylist(BuildContext context) async {
    if (playlistId == null) return;
    final confirm = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Delete playlist?'),
        content: Text('Are you sure you want to delete "$title"?'),
        actions: [
          TextButton(onPressed: () => Navigator.pop(ctx, false), child: const Text('Cancel')),
          TextButton(
            onPressed: () => Navigator.pop(ctx, true),
            style: TextButton.styleFrom(foregroundColor: SonivoColors.danger),
            child: const Text('Delete'),
          ),
        ],
      ),
    );
    if (confirm == true && context.mounted) {
      await context.read<LibraryProvider>().deletePlaylist(playlistId!);
      if (context.mounted) Navigator.of(context).pop();
    }
  }

  @override
  Widget build(BuildContext context) {
    final player = context.read<PlayerProvider>();
    final library = context.watch<LibraryProvider>();

    final tracks = playlistId == null
        ? library.liked
        : (library.playlists.where((p) => p.id == playlistId).firstOrNull?.tracks ?? initialTracks ?? const []);

    final isLikedSongs = playlistId == null;

    return Scaffold(
      appBar: AppBar(
        actions: [
          if (!isLikedSongs && playlistId != null)
            IconButton(
              icon: const Icon(Icons.delete_outline_rounded),
              tooltip: 'Delete playlist',
              onPressed: () => _deletePlaylist(context),
            ),
        ],
      ),
      body: tracks.isEmpty
          ? EmptyState(
              icon: isLikedSongs ? Icons.favorite_border_rounded : Icons.music_note_rounded,
              title: isLikedSongs ? 'No liked songs yet' : '$title is empty',
              message: 'Search for songs and add them here.',
            )
          : PageWidth(
              child: CustomScrollView(
                slivers: [
                  SliverToBoxAdapter(
                    child: Padding(
                      padding: EdgeInsets.fromLTRB(Gaps.pagePadding(context), 0, Gaps.pagePadding(context), Gaps.lg),
                      child: Row(
                        crossAxisAlignment: CrossAxisAlignment.end,
                        children: [
                          Container(
                            width: 132,
                            height: 132,
                            decoration: BoxDecoration(
                              borderRadius: BorderRadius.circular(Gaps.radiusLg),
                              gradient: isLikedSongs
                                  ? const LinearGradient(colors: [SonivoColors.rose, SonivoColors.violet])
                                  : SonivoColors.brandGradient,
                            ),
                            child: Icon(isLikedSongs ? Icons.favorite_rounded : Icons.queue_music_rounded,
                                size: 56, color: Colors.white),
                          ),
                          const SizedBox(width: Gaps.lg),
                          Expanded(
                            child: Column(
                              crossAxisAlignment: CrossAxisAlignment.start,
                              children: [
                                Text(isLikedSongs ? 'COLLECTION' : 'PLAYLIST',
                                    style: const TextStyle(
                                        fontSize: 11, letterSpacing: 1.4, color: SonivoColors.textSecondary)),
                                const SizedBox(height: Gaps.xs),
                                Text(title,
                                    maxLines: 2,
                                    overflow: TextOverflow.ellipsis,
                                    style: Theme.of(context).textTheme.headlineMedium),
                                const SizedBox(height: Gaps.xs),
                                Text(
                                  '${tracks.length} ${tracks.length == 1 ? "song" : "songs"} • ${_formatTotalDuration(tracks)}',
                                  style: const TextStyle(color: SonivoColors.textSecondary, fontSize: 14),
                                ),
                                const SizedBox(height: Gaps.md),
                                Row(
                                  children: [
                                    GradientPlayButton(
                                      size: 48,
                                      playing: false,
                                      onPressed: () => player.playQueue(tracks, 0),
                                    ),
                                    const SizedBox(width: Gaps.sm),
                                    IconButton.filledTonal(
                                      tooltip: 'Shuffle',
                                      icon: const Icon(Icons.shuffle_rounded),
                                      onPressed: () {
                                        final shuffled = List<Track>.of(tracks)..shuffle();
                                        player.playQueue(shuffled, 0);
                                      },
                                    ),
                                  ],
                                ),
                              ],
                            ),
                          ),
                        ],
                      ),
                    ),
                  ),
                  SliverList(
                    delegate: SliverChildBuilderDelegate(
                      (context, i) {
                        final t = tracks[i];
                        final isLiked = library.isLiked(t.id);
                        return TrackTile(
                          track: t,
                          onTap: () => player.playQueue(tracks, i),
                          trailing: PopupMenuButton<String>(
                            icon: const Icon(Icons.more_vert),
                            onSelected: (action) {
                              if (action == 'play_next') player.playNext(t);
                              if (action == 'queue') player.addToQueue(t);
                              if (action == 'like') library.toggleLiked(t);
                              if (action == 'add_playlist') _addToAnotherPlaylistDialog(context, t);
                              if (action == 'remove' && playlistId != null) {
                                library.removeFromPlaylist(playlistId!, t.id);
                              }
                            },
                            itemBuilder: (_) => [
                              const PopupMenuItem(value: 'play_next', child: Text('Play next')),
                              const PopupMenuItem(value: 'queue', child: Text('Add to queue')),
                              PopupMenuItem(value: 'like', child: Text(isLiked ? 'Unlike' : 'Like')),
                              const PopupMenuItem(value: 'add_playlist', child: Text('Add to other playlist')),
                              if (playlistId != null)
                                const PopupMenuItem(
                                  value: 'remove',
                                  child:
                                      Text('Remove from this playlist', style: TextStyle(color: SonivoColors.danger)),
                                ),
                            ],
                          ),
                        );
                      },
                      childCount: tracks.length,
                    ),
                  ),
                  const SliverToBoxAdapter(child: SizedBox(height: 32)),
                ],
              ),
            ),
    );
  }
}
