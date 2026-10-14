import 'package:flutter/material.dart';
import 'package:provider/provider.dart';

import '../core/theme.dart';
import '../models/track.dart';
import '../state/library_provider.dart';
import '../state/player_provider.dart';
import '../widgets/track_tile.dart';

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
            style: TextButton.styleFrom(foregroundColor: Colors.redAccent),
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
        title: Text(title),
        actions: [
          if (!isLikedSongs && playlistId != null)
            IconButton(
              icon: const Icon(Icons.delete_outline),
              tooltip: 'Delete playlist',
              onPressed: () => _deletePlaylist(context),
            ),
        ],
      ),
      body: tracks.isEmpty
          ? Center(
              child: Column(
                mainAxisSize: MainAxisSize.min,
                children: [
                  Icon(
                    isLikedSongs ? Icons.favorite_border : Icons.music_note,
                    size: 64,
                    color: Colors.grey[700],
                  ),
                  const SizedBox(height: 16),
                  Text(
                    isLikedSongs ? 'No liked songs yet' : 'This playlist is empty',
                    style: const TextStyle(color: Colors.grey, fontSize: 18),
                  ),
                  const SizedBox(height: 8),
                  const Text(
                    'Search for songs and add them here',
                    style: TextStyle(color: Colors.grey, fontSize: 13),
                  ),
                ],
              ),
            )
          : CustomScrollView(
              slivers: [
                SliverToBoxAdapter(
                  child: Padding(
                    padding: const EdgeInsets.fromLTRB(16, 12, 16, 16),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          '${tracks.length} ${tracks.length == 1 ? "song" : "songs"} • ${_formatTotalDuration(tracks)}',
                          style: const TextStyle(color: Colors.grey, fontSize: 14),
                        ),
                        const SizedBox(height: 16),
                        Row(
                          children: [
                            ElevatedButton.icon(
                              onPressed: () => player.playQueue(tracks, 0),
                              icon: const Icon(Icons.play_arrow, color: Colors.black),
                              label: const Text('Play', style: TextStyle(color: Colors.black, fontWeight: FontWeight.bold)),
                              style: ElevatedButton.styleFrom(
                                backgroundColor: SonivoColors.accent,
                                shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(24)),
                                padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 12),
                              ),
                            ),
                            const SizedBox(width: 12),
                            OutlinedButton.icon(
                              onPressed: () {
                                if (tracks.isEmpty) return;
                                final shuffled = List<Track>.of(tracks)..shuffle();
                                player.playQueue(shuffled, 0);
                              },
                              icon: const Icon(Icons.shuffle, color: Colors.white),
                              label: const Text('Shuffle', style: TextStyle(color: Colors.white)),
                              style: OutlinedButton.styleFrom(
                                side: const BorderSide(color: Colors.grey),
                                shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(24)),
                                padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 12),
                              ),
                            ),
                          ],
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
                                child: Text('Remove from this playlist', style: TextStyle(color: Colors.redAccent)),
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
    );
  }
}
