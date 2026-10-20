import 'package:flutter/material.dart';
import 'package:provider/provider.dart';

import '../core/app_icons.dart';
import '../core/theme.dart';
import '../models/playlist.dart';
import '../services/download_service.dart';
import '../state/library_provider.dart';
import '../state/player_provider.dart';
import '../widgets/ui.dart';
import '../widgets/update_dialog.dart';
import 'downloads_screen.dart';
import 'playlist_screen.dart';

class LibraryScreen extends StatelessWidget {
  const LibraryScreen({super.key});

  Future<void> _createPlaylist(BuildContext context) async {
    final controller = TextEditingController();
    final name = await showDialog<String>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('New playlist'),
        content: TextField(
          controller: controller,
          autofocus: true,
          decoration: const InputDecoration(hintText: 'Playlist name'),
        ),
        actions: [
          TextButton(onPressed: () => Navigator.pop(ctx), child: const Text('Cancel')),
          TextButton(onPressed: () => Navigator.pop(ctx, controller.text), child: const Text('Create')),
        ],
      ),
    );
    if (name != null && name.trim().isNotEmpty) {
      if (context.mounted) {
        await context.read<LibraryProvider>().createPlaylist(name.trim());
      }
    }
  }

  Future<void> _renamePlaylist(BuildContext context, Playlist playlist) async {
    final controller = TextEditingController(text: playlist.name);
    final newName = await showDialog<String>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Rename playlist'),
        content: TextField(
          controller: controller,
          autofocus: true,
          decoration: const InputDecoration(hintText: 'New playlist name'),
        ),
        actions: [
          TextButton(onPressed: () => Navigator.pop(ctx), child: const Text('Cancel')),
          TextButton(onPressed: () => Navigator.pop(ctx, controller.text), child: const Text('Save')),
        ],
      ),
    );
    if (newName != null && newName.trim().isNotEmpty && newName.trim() != playlist.name) {
      if (context.mounted) {
        await context.read<LibraryProvider>().renamePlaylist(playlist.id, newName.trim());
      }
    }
  }

  Future<void> _deletePlaylist(BuildContext context, Playlist playlist) async {
    final confirm = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Delete playlist?'),
        content: Text('Are you sure you want to delete "${playlist.name}"?'),
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
      await context.read<LibraryProvider>().deletePlaylist(playlist.id);
    }
  }

  @override
  Widget build(BuildContext context) {
    final library = context.watch<LibraryProvider>();
    final player = context.read<PlayerProvider>();

    return SafeArea(
      bottom: false,
      child: PageWidth(
        child: Column(
          children: [
            Padding(
              padding: EdgeInsets.fromLTRB(Gaps.pagePadding(context), Gaps.xl, Gaps.pagePadding(context), Gaps.sm),
              child: Row(
                children: [
                  Expanded(
                    child: Text('Your Library', style: Theme.of(context).textTheme.headlineMedium),
                  ),
                  IconButton(
                    icon: const Icon(PhosphorIconsRegular.plus, size: 28),
                    tooltip: 'Create Playlist',
                    onPressed: () => _createPlaylist(context),
                  ),
                ],
              ),
            ),
            Expanded(
              child: ListView(
                children: [
                  ListTile(
                    leading: const CircleAvatar(
                      backgroundColor: SonivoColors.accent,
                      child: Icon(PhosphorIconsFill.heart, color: SonivoColors.onAccent),
                    ),
                    title: const Text('Liked Songs', style: TextStyle(fontWeight: FontWeight.w600)),
                    subtitle: Text(
                      '${library.liked.length} ${library.liked.length == 1 ? "song" : "songs"}',
                      style: const TextStyle(color: SonivoColors.textSecondary),
                    ),
                    onTap: () => Navigator.of(context).push(MaterialPageRoute(
                      builder: (_) => PlaylistScreen(title: 'Liked Songs', tracks: library.liked),
                    )),
                  ),
                  ListTile(
                    leading: const CircleAvatar(
                      backgroundColor: SonivoColors.surfaceHigh,
                      child: Icon(PhosphorIconsRegular.fire, color: SonivoColors.accent),
                    ),
                    title: const Text('Most played', style: TextStyle(fontWeight: FontWeight.w600)),
                    subtitle: Text(
                      library.mostPlayed.isEmpty
                          ? 'Songs you play again and again'
                          : '${library.mostPlayed.length} ${library.mostPlayed.length == 1 ? "song" : "songs"}',
                      style: const TextStyle(color: SonivoColors.textSecondary),
                    ),
                    onTap: () => Navigator.of(context).push(MaterialPageRoute(
                      builder: (_) => PlaylistScreen(title: 'Most played', tracks: library.mostPlayed),
                    )),
                  ),
                  ListTile(
                    leading: const CircleAvatar(
                      backgroundColor: SonivoColors.surfaceHigh,
                      child: Icon(PhosphorIconsRegular.clockCounterClockwise, color: SonivoColors.accent),
                    ),
                    title: const Text('Recently played', style: TextStyle(fontWeight: FontWeight.w600)),
                    subtitle: Text(
                      '${library.recent.length} ${library.recent.length == 1 ? "song" : "songs"}',
                      style: const TextStyle(color: SonivoColors.textSecondary),
                    ),
                    onTap: () => Navigator.of(context).push(MaterialPageRoute(
                      builder: (_) => PlaylistScreen(title: 'Recently played', tracks: library.recent),
                    )),
                  ),
                  ListTile(
                    leading: const CircleAvatar(
                      backgroundColor: SonivoColors.surfaceHigh,
                      child: Icon(PhosphorIconsFill.checkCircle, color: SonivoColors.accent),
                    ),
                    title: const Text('Offline downloads', style: TextStyle(fontWeight: FontWeight.w600)),
                    subtitle: Text(
                      '${context.watch<DownloadService>().completedCount} songs on this device',
                      style: const TextStyle(color: SonivoColors.textSecondary),
                    ),
                    onTap: () => Navigator.of(context).push(MaterialPageRoute(builder: (_) => const DownloadsScreen())),
                  ),
                  const Divider(indent: 16, endIndent: 16),
                  ...library.playlists.map(
                    (p) => ListTile(
                      leading: const CircleAvatar(
                        backgroundColor: SonivoColors.surfaceHigh,
                        child: Icon(PhosphorIconsRegular.playlist, color: SonivoColors.textSecondary),
                      ),
                      title: Text(p.name, style: const TextStyle(fontWeight: FontWeight.w600)),
                      subtitle: Text(
                        '${p.tracks.length} ${p.tracks.length == 1 ? "song" : "songs"}',
                        style: const TextStyle(color: SonivoColors.textSecondary),
                      ),
                      onTap: () => Navigator.of(context).push(MaterialPageRoute(
                        builder: (_) => PlaylistScreen(title: p.name, tracks: p.tracks, playlistId: p.id),
                      )),
                      trailing: PopupMenuButton<String>(
                        icon: const Icon(PhosphorIconsRegular.dotsThreeVertical, size: 20),
                        onSelected: (action) {
                          if (action == 'play' && p.tracks.isNotEmpty) {
                            player.playQueue(p.tracks, 0);
                          }
                          if (action == 'rename') _renamePlaylist(context, p);
                          if (action == 'delete') _deletePlaylist(context, p);
                        },
                        itemBuilder: (_) => [
                          if (p.tracks.isNotEmpty) const PopupMenuItem(value: 'play', child: Text('Play')),
                          const PopupMenuItem(value: 'rename', child: Text('Rename')),
                          const PopupMenuItem(
                            value: 'delete',
                            child: Text('Delete', style: TextStyle(color: SonivoColors.danger)),
                          ),
                        ],
                      ),
                    ),
                  ),
                  if (library.playlists.isEmpty)
                    const Padding(
                      padding: EdgeInsets.all(32),
                      child: Center(
                        child: Text(
                          'No playlists yet.\nTap + at the top to create your first playlist.',
                          textAlign: TextAlign.center,
                          style: TextStyle(color: SonivoColors.textSecondary, height: 1.5),
                        ),
                      ),
                    ),
                  const Divider(indent: 16, endIndent: 16),
                  const AboutTile(),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}
