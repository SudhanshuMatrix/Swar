import 'package:flutter/material.dart';
import 'package:provider/provider.dart';

import '../core/theme.dart';
import '../models/track.dart';
import '../services/api_service.dart';
import '../services/download_service.dart';
import '../services/jam_service.dart';
import '../state/library_provider.dart';
import '../state/player_provider.dart';
import '../widgets/track_tile.dart';
import 'deep_search_screen.dart';

class SearchScreen extends StatefulWidget {
  const SearchScreen({super.key});

  @override
  State<SearchScreen> createState() => _SearchScreenState();
}

class _SearchScreenState extends State<SearchScreen> {
  final _controller = TextEditingController();
  List<Track> _results = [];
  bool _loading = false;
  bool _searched = false;
  String? _error;

  static const _genres = [
    _GenreItem('Top Hits', Icons.trending_up, Color(0xFFE91E63)),
    _GenreItem('Pop', Icons.music_note, Color(0xFF9C27B0)),
    _GenreItem('Rock', Icons.electric_bolt, Color(0xFFFF5722)),
    _GenreItem('Hip-Hop', Icons.speaker, Color(0xFFFF9800)),
    _GenreItem('Lofi & Chill', Icons.nightlight_round, Color(0xFF3F51B5)),
    _GenreItem('Indie', Icons.nature_people, Color(0xFF009688)),
    _GenreItem('Electronic', Icons.graphic_eq, Color(0xFF2196F3)),
    _GenreItem('Jazz & Blues', Icons.wine_bar, Color(0xFF795548)),
    _GenreItem('Acoustic', Icons.forest, Color(0xFF4CAF50)),
    _GenreItem('Workout', Icons.fitness_center, Color(0xFFF44336)),
    _GenreItem('Focus & Study', Icons.auto_stories, Color(0xFF607D8B)),
    _GenreItem('Gaming', Icons.sports_esports, Color(0xFF673AB7)),
  ];

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  Future<void> _search(String query) async {
    final trimmed = query.trim();
    if (trimmed.isEmpty) {
      setState(() => _results = []);
      return;
    }
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final api = context.read<ApiService>();
      final results = await api.search(trimmed);
      if (!mounted) return;
      setState(() {
        _results = results;
        _searched = true;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() => _error = 'Search failed. Make sure the Swar backend is running ($e)');
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  Future<void> _addToPlaylistDialog(Track track) async {
    final library = context.read<LibraryProvider>();
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
                  child: Text(p.name),
                ))
            .toList(),
      ),
    );
    if (playlistId != null) {
      await library.addToPlaylist(playlistId, track);
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(content: Text('Added to playlist')),
        );
      }
    }
  }

  void _openDeepSearch() {
    Navigator.of(context).push(MaterialPageRoute(
      builder: (_) => DeepSearchScreen(query: _controller.text.trim()),
    ));
  }

  Future<void> _download(Track t) async {
    final messenger = ScaffoldMessenger.of(context);
    try {
      await context.read<DownloadService>().download(t);
      messenger.showSnackBar(SnackBar(content: Text('Downloading "${t.title}"')));
    } catch (e) {
      messenger.showSnackBar(SnackBar(content: Text('$e')));
    }
  }

  void _selectGenre(String genre) {
    _controller.text = genre;
    _search(genre);
  }

  @override
  Widget build(BuildContext context) {
    final player = context.read<PlayerProvider>();
    final library = context.watch<LibraryProvider>();

    return SafeArea(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(16, 12, 16, 8),
            child: TextField(
              controller: _controller,
              onSubmitted: _search,
              textInputAction: TextInputAction.search,
              decoration: InputDecoration(
                hintText: 'Search songs, artists or albums',
                hintStyle: const TextStyle(color: SonivoColors.textSecondary, fontSize: 14),
                prefixIcon: const Icon(Icons.search, color: Colors.white70),
                filled: true,
                fillColor: const Color(0xFF242424),
                border: OutlineInputBorder(borderRadius: BorderRadius.circular(10), borderSide: BorderSide.none),
                contentPadding: const EdgeInsets.symmetric(horizontal: 16, vertical: 12),
                suffixIcon: _controller.text.isNotEmpty
                    ? IconButton(
                        icon: const Icon(Icons.clear, color: Colors.white70),
                        onPressed: () {
                          _controller.clear();
                          setState(() {
                            _results = [];
                            _searched = false;
                            _error = null;
                          });
                        },
                      )
                    : null,
              ),
              onChanged: (text) {
                if (text.isEmpty && _results.isNotEmpty) {
                  setState(() => _results = []);
                }
              },
            ),
          ),
          if (_loading) const LinearProgressIndicator(color: SonivoColors.accent),
          if (_error != null)
            Padding(
              padding: const EdgeInsets.all(16),
              child: Container(
                padding: const EdgeInsets.all(12),
                decoration: BoxDecoration(
                  color: Colors.red.withOpacity(0.15),
                  borderRadius: BorderRadius.circular(8),
                  border: Border.all(color: Colors.redAccent.withOpacity(0.3)),
                ),
                child: Row(
                  children: [
                    const Icon(Icons.error_outline, color: Colors.redAccent),
                    const SizedBox(width: 8),
                    Expanded(child: Text(_error!, style: const TextStyle(color: Colors.redAccent, fontSize: 13))),
                  ],
                ),
              ),
            ),
          if (_searched && _results.isEmpty && !_loading)
            Padding(
              padding: const EdgeInsets.fromLTRB(16, 8, 16, 0),
              child: Text('No results for "${_controller.text.trim()}".',
                  style: const TextStyle(color: SonivoColors.textSecondary)),
            ),
          if (_searched && !_loading) _DeepSearchPrompt(onTap: _openDeepSearch),
          Expanded(
            child: _results.isEmpty && !_loading
                ? _buildBrowseGenres()
                : ListView.builder(
                    itemCount: _results.length,
                    itemBuilder: (context, i) {
                      final t = _results[i];
                      final jam = context.read<JamController>();
                      final isLiked = library.isLiked(t.id);
                      return TrackTile(
                        track: t,
                        onTap: () => player.playQueue(_results, i),
                        trailing: PopupMenuButton<String>(
                          icon: const Icon(Icons.more_vert),
                          onSelected: (value) {
                            if (value == 'play_next') player.playNext(t);
                            if (value == 'queue') player.addToQueue(t);
                            if (value == 'like') library.toggleLiked(t);
                            if (value == 'playlist') _addToPlaylistDialog(t);
                            if (value == 'radio') player.startRadio(t);
                            if (value == 'download') _download(t);
                            if (value == 'jam') jam.addTrack(t);
                          },
                          itemBuilder: (_) => [
                            const PopupMenuItem(value: 'play_next', child: Text('Play next')),
                            const PopupMenuItem(value: 'queue', child: Text('Add to queue')),
                            const PopupMenuItem(value: 'playlist', child: Text('Add to playlist')),
                            PopupMenuItem(value: 'like', child: Text(isLiked ? 'Unlike' : 'Like')),
                            const PopupMenuItem(value: 'radio', child: Text('Start radio')),
                            if (t.downloadable) const PopupMenuItem(value: 'download', child: Text('Download')),
                            if (jam.inJam && (jam.isAdmin || jam.allowParticipantAdd))
                              const PopupMenuItem(value: 'jam', child: Text('Add to Jam')),
                          ],
                        ),
                      );
                    },
                  ),
          ),
        ],
      ),
    );
  }

  Widget _buildBrowseGenres() {
    return SingleChildScrollView(
      padding: const EdgeInsets.fromLTRB(16, 8, 16, 24),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          const Text(
            'Browse all',
            style: TextStyle(fontSize: 18, fontWeight: FontWeight.bold),
          ),
          const SizedBox(height: 12),
          GridView.builder(
            shrinkWrap: true,
            physics: const NeverScrollableScrollPhysics(),
            itemCount: _genres.length,
            gridDelegate: const SliverGridDelegateWithFixedCrossAxisCount(
              crossAxisCount: 2,
              crossAxisSpacing: 12,
              mainAxisSpacing: 12,
              childAspectRatio: 1.8,
            ),
            itemBuilder: (context, i) {
              final g = _genres[i];
              return InkWell(
                onTap: () => _selectGenre(g.title),
                borderRadius: BorderRadius.circular(8),
                child: Container(
                  decoration: BoxDecoration(
                    color: g.color.withOpacity(0.85),
                    borderRadius: BorderRadius.circular(8),
                  ),
                  padding: const EdgeInsets.all(12),
                  child: Stack(
                    children: [
                      Text(
                        g.title,
                        style: const TextStyle(
                          color: Colors.white,
                          fontSize: 16,
                          fontWeight: FontWeight.bold,
                        ),
                      ),
                      Positioned(
                        right: -4,
                        bottom: -4,
                        child: Transform.rotate(
                          angle: 0.3,
                          child: Icon(g.icon, size: 40, color: Colors.white.withOpacity(0.35)),
                        ),
                      ),
                    ],
                  ),
                ),
              );
            },
          ),
        ],
      ),
    );
  }
}

class _GenreItem {
  final String title;
  final IconData icon;
  final Color color;
  const _GenreItem(this.title, this.icon, this.color);
}

class _DeepSearchPrompt extends StatelessWidget {
  final VoidCallback onTap;
  const _DeepSearchPrompt({required this.onTap});

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.fromLTRB(8, 0, 8, 0),
      child: TextButton.icon(
        onPressed: onTap,
        icon: const Icon(Icons.travel_explore, size: 18),
        label: const Text("Didn't find your song? Try Deep Search."),
        style: TextButton.styleFrom(foregroundColor: SonivoColors.accent),
      ),
    );
  }
}
