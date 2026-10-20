import 'package:flutter/material.dart';
import 'package:provider/provider.dart';

import '../core/app_icons.dart';
import '../core/errors.dart';
import '../core/genres.dart';
import '../core/theme.dart';
import '../models/online_artist.dart';
import '../models/online_playlist.dart';
import '../models/track.dart';
import '../services/api_service.dart';
import '../state/library_provider.dart';
import '../state/player_provider.dart';
import '../widgets/genre_card.dart';
import '../widgets/track_actions.dart';
import '../widgets/track_tile.dart';
import '../widgets/ui.dart';
import 'artist_screen.dart';
import 'deep_search_screen.dart';
import 'online_playlist_screen.dart';

class SearchScreen extends StatefulWidget {
  const SearchScreen({super.key});

  /// Other screens set this to run a search, such as a Discover card.
  static final requests = ValueNotifier<String?>(null);

  @override
  State<SearchScreen> createState() => _SearchScreenState();
}

class _SearchScreenState extends State<SearchScreen> {
  final _controller = TextEditingController();
  List<Track> _results = [];
  List<OnlinePlaylist> _playlists = [];
  List<OnlineArtist> _artists = [];

  bool get _hasExtras => _playlists.isNotEmpty || _artists.isNotEmpty;
  bool _loading = false;
  bool _searched = false;
  String? _error;

  @override
  void initState() {
    super.initState();
    SearchScreen.requests.addListener(_onRequest);
    WidgetsBinding.instance.addPostFrameCallback((_) => _onRequest());
  }

  void _onRequest() {
    final q = SearchScreen.requests.value;
    if (q == null || !mounted) return;
    SearchScreen.requests.value = null;
    _controller.text = q;
    _search(q);
  }

  @override
  void dispose() {
    SearchScreen.requests.removeListener(_onRequest);
    _controller.dispose();
    super.dispose();
  }

  Future<void> _search(String query) async {
    final trimmed = query.trim();
    if (trimmed.isEmpty) {
      setState(() {
        _results = [];
        _playlists = [];
        _artists = [];
      });
      return;
    }
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final api = context.read<ApiService>();
      // Singers and playlists are a bonus: if those searches fail, songs
      // still show.
      final playlistsFuture = api.searchPlaylists(trimmed).catchError((_) => <OnlinePlaylist>[]);
      final artistsFuture = api.searchArtists(trimmed, limit: 8).catchError((_) => <OnlineArtist>[]);
      context.read<LibraryProvider>().addSearch(trimmed);
      final results = await api.search(trimmed);
      final playlists = await playlistsFuture;
      final artists = await artistsFuture;
      if (!mounted) return;
      setState(() {
        _results = results;
        _playlists = playlists;
        _artists = artists;
        _searched = true;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() => _error = friendlyError(e));
      ErrorPopup.show(e, title: 'Search failed', onRetry: () => _search(trimmed));
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  void _openDeepSearch() {
    Navigator.of(context).push(MaterialPageRoute(
      builder: (_) => DeepSearchScreen(query: _controller.text.trim()),
    ));
  }

  void _selectGenre(Genre genre) {
    _controller.text = genre.query;
    _search(genre.query);
  }

  @override
  Widget build(BuildContext context) {
    final player = context.read<PlayerProvider>();
    final side = Gaps.pagePadding(context);

    return SafeArea(
      bottom: false,
      child: PageWidth(
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Padding(
              padding: EdgeInsets.fromLTRB(side, Gaps.xl, side, Gaps.md),
              child: Text('Search', style: Theme.of(context).textTheme.headlineMedium),
            ),
            Padding(
              padding: EdgeInsets.fromLTRB(side, 0, side, Gaps.sm),
              child: TextField(
                controller: _controller,
                onSubmitted: _search,
                textInputAction: TextInputAction.search,
                style: const TextStyle(fontSize: 15),
                decoration: InputDecoration(
                  hintText: 'Songs, singers or playlists',
                  prefixIcon: const Icon(PhosphorIconsRegular.magnifyingGlass),
                  suffixIcon: _controller.text.isNotEmpty
                      ? IconButton(
                          tooltip: 'Clear',
                          icon: const Icon(PhosphorIconsRegular.x),
                          onPressed: () {
                            _controller.clear();
                            setState(() {
                              _results = [];
                              _playlists = [];
                              _artists = [];
                              _searched = false;
                              _error = null;
                            });
                          },
                        )
                      : null,
                ),
                onChanged: (text) {
                  if (text.isEmpty && (_results.isNotEmpty || _hasExtras)) {
                    setState(() {
                      _results = [];
                      _playlists = [];
                      _artists = [];
                    });
                  } else {
                    // Keeps the clear button in step with the text.
                    setState(() {});
                  }
                },
              ),
            ),
            if (_loading)
              Padding(padding: EdgeInsets.symmetric(horizontal: side), child: const LinearProgressIndicator()),
            if (_error != null)
              Padding(
                padding: EdgeInsets.fromLTRB(side, Gaps.sm, side, 0),
                child: Container(
                  padding: const EdgeInsets.all(Gaps.md),
                  decoration: BoxDecoration(
                    color: SonivoColors.danger.withOpacity(0.12),
                    borderRadius: BorderRadius.circular(Gaps.radius),
                    border: Border.all(color: SonivoColors.danger.withOpacity(0.3)),
                  ),
                  child: Row(
                    children: [
                      const Icon(PhosphorIconsRegular.warningCircle, color: SonivoColors.danger),
                      const SizedBox(width: Gaps.sm),
                      Expanded(child: Text(_error!, style: const TextStyle(color: SonivoColors.danger, fontSize: 13))),
                    ],
                  ),
                ),
              ),
            if (_searched && _results.isEmpty && !_hasExtras && !_loading)
              Padding(
                padding: EdgeInsets.fromLTRB(side, Gaps.sm, side, 0),
                child: Text('No results for "${_controller.text.trim()}".',
                    style: const TextStyle(color: SonivoColors.textSecondary)),
              ),
            if (_searched && !_loading) _DeepSearchPrompt(onTap: _openDeepSearch),
            Expanded(
              child: _results.isEmpty && !_hasExtras && !_loading
                  ? _buildBrowseGenres()
                  : ListView.builder(
                      padding: const EdgeInsets.only(bottom: Gaps.xl),
                      itemCount: _results.length + (_hasExtras ? 1 : 0),
                      itemBuilder: (context, index) {
                        if (_hasExtras && index == 0) {
                          return Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              if (_artists.isNotEmpty) _ArtistRow(artists: _artists),
                              if (_playlists.isNotEmpty) _PlaylistRow(playlists: _playlists),
                              if (_results.isNotEmpty) const SectionHeader('Songs'),
                            ],
                          );
                        }
                        final i = _hasExtras ? index - 1 : index;
                        final t = _results[i];
                        return TrackTile(
                          track: t,
                          onTap: () => player.playQueue(_results, i),
                          trailing: TrackMenu(track: t),
                        );
                      },
                    ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _buildBrowseGenres() {
    final side = Gaps.pagePadding(context);
    return CustomScrollView(
      slivers: [
        if (context.watch<LibraryProvider>().searches.isNotEmpty) _recentSearches(side),
        const SliverToBoxAdapter(child: SectionHeader('Discover', subtitle: 'Pick a mood or genre')),
        SliverPadding(
          padding: EdgeInsets.fromLTRB(side, 0, side, Gaps.xl),
          sliver: SliverGrid(
            gridDelegate: const SliverGridDelegateWithMaxCrossAxisExtent(
              maxCrossAxisExtent: 220,
              crossAxisSpacing: Gaps.md,
              mainAxisSpacing: Gaps.md,
              childAspectRatio: 1,
            ),
            delegate: SliverChildBuilderDelegate(
              (context, i) => GenreCard(genre: genres[i], onTap: () => _selectGenre(genres[i])),
              childCount: genres.length,
            ),
          ),
        ),
      ],
    );
  }

  Widget _recentSearches(double side) {
    final library = context.watch<LibraryProvider>();
    return SliverToBoxAdapter(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          SectionHeader(
            'Recent searches',
            action: TextButton(onPressed: library.clearSearches, child: const Text('Clear')),
          ),
          Padding(
            padding: EdgeInsets.symmetric(horizontal: side),
            child: Wrap(
              spacing: Gaps.sm,
              runSpacing: Gaps.sm,
              children: [
                for (final q in library.searches)
                  InputChip(
                    avatar: const Icon(PhosphorIconsRegular.clockCounterClockwise, size: 16),
                    label: Text(q),
                    onPressed: () {
                      _controller.text = q;
                      _search(q);
                    },
                    deleteIcon: const Icon(PhosphorIconsRegular.x, size: 14),
                    deleteButtonTooltipMessage: 'Remove',
                    onDeleted: () => library.removeSearch(q),
                  ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

class _DeepSearchPrompt extends StatelessWidget {
  final VoidCallback onTap;
  const _DeepSearchPrompt({required this.onTap});

  @override
  Widget build(BuildContext context) {
    final side = Gaps.pagePadding(context);
    return Padding(
      padding: EdgeInsets.fromLTRB(side - Gaps.sm, Gaps.xs, side, 0),
      child: TextButton.icon(
        onPressed: onTap,
        icon: const Icon(PhosphorIconsRegular.globe, size: 18),
        label: const Text("Didn't find your song? Try Deep Search"),
      ),
    );
  }
}

/// Horizontal row of matching playlists above the song results.
class _PlaylistRow extends StatelessWidget {
  final List<OnlinePlaylist> playlists;
  const _PlaylistRow({required this.playlists});

  @override
  Widget build(BuildContext context) {
    final side = Gaps.pagePadding(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const SectionHeader('Playlists'),
        SizedBox(
          height: 200,
          child: ListView.separated(
            scrollDirection: Axis.horizontal,
            padding: EdgeInsets.symmetric(horizontal: side),
            itemCount: playlists.length,
            separatorBuilder: (_, __) => const SizedBox(width: Gaps.lg),
            itemBuilder: (context, i) {
              final p = playlists[i];
              return InkWell(
                borderRadius: BorderRadius.circular(Gaps.radius),
                onTap: () => Navigator.of(context).push(MaterialPageRoute(
                  builder: (_) => OnlinePlaylistScreen(playlist: p),
                )),
                child: SizedBox(
                  width: 140,
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Artwork(url: p.imageUrl, size: 140, radius: Gaps.radius, icon: PhosphorIconsRegular.playlist),
                      const SizedBox(height: Gaps.sm),
                      Text(p.name,
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: const TextStyle(fontWeight: FontWeight.w700)),
                      if (p.songCount > 0)
                        Text('${p.songCount} songs',
                            style: const TextStyle(color: SonivoColors.textSecondary, fontSize: 12)),
                    ],
                  ),
                ),
              );
            },
          ),
        ),
      ],
    );
  }
}

/// Round singer photos above the results, biggest catalogs first; tap one
/// for their top songs.
class _ArtistRow extends StatelessWidget {
  final List<OnlineArtist> artists;
  const _ArtistRow({required this.artists});

  @override
  Widget build(BuildContext context) {
    final side = Gaps.pagePadding(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const SectionHeader('Singers'),
        SizedBox(
          height: 160,
          child: ListView.separated(
            scrollDirection: Axis.horizontal,
            padding: EdgeInsets.symmetric(horizontal: side),
            itemCount: artists.length,
            separatorBuilder: (_, __) => const SizedBox(width: Gaps.lg),
            itemBuilder: (context, i) {
              final a = artists[i];
              return InkWell(
                borderRadius: BorderRadius.circular(Gaps.radius),
                onTap: () => Navigator.of(context).push(MaterialPageRoute(builder: (_) => ArtistScreen(artist: a))),
                child: SizedBox(
                  width: 104,
                  child: Column(
                    children: [
                      Artwork(url: a.imageUrl, size: 100, circle: true, icon: PhosphorIconsRegular.user),
                      const SizedBox(height: Gaps.sm),
                      Text(a.name,
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          textAlign: TextAlign.center,
                          style: const TextStyle(fontWeight: FontWeight.w600)),
                      if (a.songCount != null)
                        Text('${a.songCount} songs',
                            style: const TextStyle(color: SonivoColors.textSecondary, fontSize: 12)),
                    ],
                  ),
                ),
              );
            },
          ),
        ),
      ],
    );
  }
}
