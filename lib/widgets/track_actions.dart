import 'package:flutter/material.dart';
import 'package:provider/provider.dart';

import '../models/track.dart';
import '../services/jam_service.dart';
import '../state/library_provider.dart';
import '../state/player_provider.dart';

/// Lets the listener pick one of their playlists for [track].
Future<void> showAddToPlaylist(BuildContext context, Track track) async {
  final library = context.read<LibraryProvider>();
  final messenger = ScaffoldMessenger.maybeOf(context);
  if (library.playlists.isEmpty) {
    await showDialog<void>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('No playlists yet'),
        content: const Text('Create a playlist from the Library tab first.'),
        actions: [TextButton(onPressed: () => Navigator.pop(ctx), child: const Text('OK'))],
      ),
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
                padding: const EdgeInsets.symmetric(horizontal: 24, vertical: 12),
                child: Row(children: [
                  const Icon(Icons.queue_music_rounded, size: 20),
                  const SizedBox(width: 12),
                  Expanded(child: Text(p.name)),
                ]),
              ))
          .toList(),
    ),
  );
  if (playlistId != null) {
    await library.addToPlaylist(playlistId, track);
    messenger?.showSnackBar(const SnackBar(content: Text('Added to playlist')));
  }
}

/// The "more" menu shown next to a song.
class TrackMenu extends StatelessWidget {
  final Track track;
  final bool showLike;
  final bool showRadio;
  const TrackMenu({super.key, required this.track, this.showLike = true, this.showRadio = true});

  @override
  Widget build(BuildContext context) {
    final player = context.read<PlayerProvider>();
    final library = context.watch<LibraryProvider>();
    final jam = context.watch<JamController>();
    final liked = library.isLiked(track.id);
    return PopupMenuButton<String>(
      tooltip: 'More',
      icon: const Icon(Icons.more_horiz_rounded),
      onSelected: (value) {
        switch (value) {
          case 'play_next':
            player.playNext(track);
          case 'queue':
            player.addToQueue(track);
          case 'playlist':
            showAddToPlaylist(context, track);
          case 'like':
            library.toggleLiked(track);
          case 'radio':
            player.startRadio(track);
          case 'jam':
            jam.addTrack(track);
        }
      },
      itemBuilder: (_) => [
        _item('play_next', Icons.playlist_play_rounded, 'Play next'),
        _item('queue', Icons.queue_rounded, 'Add to queue'),
        _item('playlist', Icons.playlist_add_rounded, 'Add to playlist'),
        if (showLike)
          _item('like', liked ? Icons.favorite_rounded : Icons.favorite_border_rounded, liked ? 'Unlike' : 'Like'),
        if (showRadio) _item('radio', Icons.radio_rounded, 'Start radio'),
        if (jam.inJam && (jam.isAdmin || jam.allowParticipantAdd)) _item('jam', Icons.groups_rounded, 'Add to Jam'),
      ],
    );
  }

  PopupMenuItem<String> _item(String value, IconData icon, String label) => PopupMenuItem(
        value: value,
        child: Row(children: [Icon(icon, size: 20), const SizedBox(width: 12), Text(label)]),
      );
}

/// Heart button that likes or unlikes [track].
class LikeButton extends StatelessWidget {
  final Track track;
  final double size;
  const LikeButton({super.key, required this.track, this.size = 22});

  @override
  Widget build(BuildContext context) {
    final library = context.watch<LibraryProvider>();
    final liked = library.isLiked(track.id);
    return IconButton(
      tooltip: liked ? 'Unlike' : 'Like',
      iconSize: size,
      icon: Icon(liked ? Icons.favorite_rounded : Icons.favorite_border_rounded,
          color: liked ? Theme.of(context).colorScheme.tertiary : null),
      onPressed: () => library.toggleLiked(track),
    );
  }
}
