import 'package:flutter/material.dart';
import 'package:provider/provider.dart';

import '../core/theme.dart';
import '../models/track.dart';
import '../state/library_provider.dart';
import '../state/player_provider.dart';
import '../widgets/mini_player.dart';
import '../widgets/track_tile.dart';
import 'jam_screen.dart';
import 'library_screen.dart';
import 'now_playing_screen.dart';
import 'playlist_screen.dart';
import 'search_screen.dart';

class HomeShell extends StatefulWidget {
  const HomeShell({super.key});

  @override
  State<HomeShell> createState() => _HomeShellState();
}

class _HomeShellState extends State<HomeShell> {
  int _index = 0;

  void _openNowPlaying() {
    final track = context.read<PlayerProvider>().currentTrack;
    if (track == null) return;
    Navigator.of(context).push(MaterialPageRoute(builder: (_) => const NowPlayingScreen()));
  }

  void _switchToTab(int tabIndex) {
    setState(() => _index = tabIndex);
  }

  @override
  Widget build(BuildContext context) {
    final hasCurrentTrack = context.select<PlayerProvider, bool>((p) => p.currentTrack != null);
    final pages = [
      _HomeTab(onExploreTap: () => _switchToTab(1)),
      const SearchScreen(),
      const LibraryScreen(),
      const JamScreen(),
    ];

    return Scaffold(
      body: IndexedStack(index: _index, children: pages),
      bottomNavigationBar: SafeArea(
        top: false,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            if (hasCurrentTrack)
              GestureDetector(onTap: _openNowPlaying, child: const MiniPlayer()),
            BottomNavigationBar(
              type: BottomNavigationBarType.fixed,
              currentIndex: _index,
              onTap: (i) => setState(() => _index = i),
              items: const [
                BottomNavigationBarItem(icon: Icon(Icons.home_outlined), activeIcon: Icon(Icons.home), label: 'Home'),
                BottomNavigationBarItem(icon: Icon(Icons.search), label: 'Search'),
                BottomNavigationBarItem(
                  icon: Icon(Icons.library_music_outlined),
                  activeIcon: Icon(Icons.library_music),
                  label: 'Library',
                ),
                BottomNavigationBarItem(icon: Icon(Icons.groups_outlined), activeIcon: Icon(Icons.groups), label: 'Jam'),
              ],
            ),
          ],
        ),
      ),
    );
  }
}

class _HomeTab extends StatelessWidget {
  final VoidCallback onExploreTap;
  const _HomeTab({required this.onExploreTap});

  String _getGreeting() {
    final hour = DateTime.now().hour;
    if (hour < 12) return 'Good morning';
    if (hour < 18) return 'Good afternoon';
    return 'Good evening';
  }

  Future<void> _addToPlaylistDialog(BuildContext context, Track track) async {
    final library = context.read<LibraryProvider>();
    if (library.playlists.isEmpty) {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('No playlists yet. Create one in Library first.')),
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

  @override
  Widget build(BuildContext context) {
    final library = context.watch<LibraryProvider>();
    final player = context.read<PlayerProvider>();
    final isBrandNew = library.recent.isEmpty && library.liked.isEmpty && library.playlists.isEmpty;

    return SafeArea(
      child: CustomScrollView(
        slivers: [
          SliverAppBar(
            title: Text(
              _getGreeting(),
              style: const TextStyle(fontSize: 22, fontWeight: FontWeight.bold),
            ),
            floating: true,
            actions: [
              IconButton(
                icon: const Icon(Icons.search),
                tooltip: 'Search',
                onPressed: onExploreTap,
              ),
            ],
          ),
          if (library.playlists.isNotEmpty) ...[
            SliverToBoxAdapter(
              child: Padding(
                padding: const EdgeInsets.fromLTRB(16, 8, 16, 8),
                child: GridView.builder(
                  shrinkWrap: true,
                  physics: const NeverScrollableScrollPhysics(),
                  itemCount: library.playlists.take(6).length,
                  gridDelegate: const SliverGridDelegateWithFixedCrossAxisCount(
                    crossAxisCount: 2,
                    crossAxisSpacing: 8,
                    mainAxisSpacing: 8,
                    childAspectRatio: 3.2,
                  ),
                  itemBuilder: (context, i) {
                    final p = library.playlists[i];
                    return InkWell(
                      onTap: () => Navigator.of(context).push(MaterialPageRoute(
                        builder: (_) => PlaylistScreen(title: p.name, tracks: p.tracks, playlistId: p.id),
                      )),
                      borderRadius: BorderRadius.circular(6),
                      child: Container(
                        decoration: BoxDecoration(
                          color: const Color(0xFF282828),
                          borderRadius: BorderRadius.circular(6),
                        ),
                        padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 6),
                        child: Row(
                          children: [
                            const Icon(Icons.queue_music, color: SonivoColors.accent, size: 22),
                            const SizedBox(width: 8),
                            Expanded(
                              child: Text(
                                p.name,
                                maxLines: 1,
                                overflow: TextOverflow.ellipsis,
                                style: const TextStyle(fontWeight: FontWeight.bold, fontSize: 13),
                              ),
                            ),
                          ],
                        ),
                      ),
                    );
                  },
                ),
              ),
            ),
          ],
          if (isBrandNew)
            SliverToBoxAdapter(
              child: Padding(
                padding: const EdgeInsets.all(24),
                child: Container(
                  padding: const EdgeInsets.all(20),
                  decoration: BoxDecoration(
                    color: SonivoColors.surface,
                    borderRadius: BorderRadius.circular(12),
                    border: Border.all(color: Colors.white12),
                  ),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      const Text(
                        'Welcome to Sonivo! 🎵',
                        style: TextStyle(fontSize: 20, fontWeight: FontWeight.bold),
                      ),
                      const SizedBox(height: 8),
                      const Text(
                        'Stream any song or music video directly with background audio support and offline playlists.',
                        style: TextStyle(color: SonivoColors.textSecondary, fontSize: 14),
                      ),
                      const SizedBox(height: 16),
                      ElevatedButton.icon(
                        onPressed: onExploreTap,
                        icon: const Icon(Icons.search, color: Colors.black),
                        label: const Text('Discover Music', style: TextStyle(color: Colors.black, fontWeight: FontWeight.bold)),
                        style: ElevatedButton.styleFrom(
                          backgroundColor: SonivoColors.accent,
                          shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(20)),
                        ),
                      ),
                    ],
                  ),
                ),
              ),
            ),
          if (library.recent.isNotEmpty) ...[
            const _SectionHeader('Recently played'),
            SliverList(
              delegate: SliverChildBuilderDelegate(
                (context, i) {
                  final t = library.recent[i];
                  return TrackTile(
                    track: t,
                    onTap: () => player.playQueue(library.recent, i),
                    trailing: Row(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        _LikeButton(track: t),
                        PopupMenuButton<String>(
                          icon: const Icon(Icons.more_vert, size: 20),
                          onSelected: (action) {
                            if (action == 'play_next') player.playNext(t);
                            if (action == 'queue') player.addToQueue(t);
                            if (action == 'playlist') _addToPlaylistDialog(context, t);
                          },
                          itemBuilder: (_) => const [
                            PopupMenuItem(value: 'play_next', child: Text('Play next')),
                            PopupMenuItem(value: 'queue', child: Text('Add to queue')),
                            PopupMenuItem(value: 'playlist', child: Text('Add to playlist')),
                          ],
                        ),
                      ],
                    ),
                  );
                },
                childCount: library.recent.length,
              ),
            ),
          ],
          if (library.liked.isNotEmpty) ...[
            const _SectionHeader('Liked Songs'),
            SliverList(
              delegate: SliverChildBuilderDelegate(
                (context, i) {
                  final t = library.liked[i];
                  return TrackTile(
                    track: t,
                    onTap: () => player.playQueue(library.liked, i),
                    trailing: Row(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        _LikeButton(track: t),
                        PopupMenuButton<String>(
                          icon: const Icon(Icons.more_vert, size: 20),
                          onSelected: (action) {
                            if (action == 'play_next') player.playNext(t);
                            if (action == 'queue') player.addToQueue(t);
                            if (action == 'playlist') _addToPlaylistDialog(context, t);
                          },
                          itemBuilder: (_) => const [
                            PopupMenuItem(value: 'play_next', child: Text('Play next')),
                            PopupMenuItem(value: 'queue', child: Text('Add to queue')),
                            PopupMenuItem(value: 'playlist', child: Text('Add to playlist')),
                          ],
                        ),
                      ],
                    ),
                  );
                },
                childCount: library.liked.length,
              ),
            ),
          ],
          const SliverToBoxAdapter(child: SizedBox(height: 32)),
        ],
      ),
    );
  }
}

class _SectionHeader extends StatelessWidget {
  final String title;
  const _SectionHeader(this.title);

  @override
  Widget build(BuildContext context) {
    return SliverToBoxAdapter(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(16, 16, 16, 8),
        child: Text(title, style: const TextStyle(fontSize: 19, fontWeight: FontWeight.bold)),
      ),
    );
  }
}

class _LikeButton extends StatelessWidget {
  final Track track;
  const _LikeButton({required this.track});

  @override
  Widget build(BuildContext context) {
    final library = context.watch<LibraryProvider>();
    final liked = library.isLiked(track.id);
    return IconButton(
      icon: Icon(liked ? Icons.favorite : Icons.favorite_border, color: liked ? SonivoColors.accent : Colors.grey),
      onPressed: () => library.toggleLiked(track),
    );
  }
}
