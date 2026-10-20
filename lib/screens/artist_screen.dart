import 'package:flutter/material.dart';
import 'package:provider/provider.dart';

import '../core/app_icons.dart';
import '../core/errors.dart';
import '../core/theme.dart';
import '../models/online_artist.dart';
import '../models/track.dart';
import '../services/api_service.dart';
import '../state/player_provider.dart';
import '../widgets/track_actions.dart';
import '../widgets/track_tile.dart';
import '../widgets/ui.dart';

/// A singer's page with all of their songs.
class ArtistScreen extends StatefulWidget {
  final OnlineArtist artist;
  const ArtistScreen({super.key, required this.artist});

  @override
  State<ArtistScreen> createState() => _ArtistScreenState();
}

class _ArtistScreenState extends State<ArtistScreen> {
  List<Track> _tracks = [];
  int _total = 0;
  String _imageUrl = '';
  bool _loading = true;
  bool _loadingMore = false;
  String? _error;
  int _generation = 0;

  @override
  void initState() {
    super.initState();
    _imageUrl = widget.artist.imageUrl;
    _load();
  }

  @override
  void dispose() {
    _generation++;
    super.dispose();
  }

  /// Loads every song the catalog lists for the artist, page by page, so the
  /// count matches what search showed.
  Future<void> _load() async {
    final gen = ++_generation;
    final api = context.read<ApiService>();
    setState(() {
      _loading = true;
      _error = null;
      _tracks = [];
    });
    try {
      final first = await api.artistSongs(widget.artist.id);
      if (!mounted || gen != _generation) return;
      final seen = <String>{};
      void add(List<Track> page) => _tracks.addAll(page.where((t) => seen.add(t.id)));
      setState(() {
        add(first.tracks);
        _total = first.total;
        _loading = false;
        _loadingMore = true;
      });
      if (_imageUrl.isEmpty) {
        api.artist(widget.artist.id).then((p) {
          if (mounted && p.imageUrl.isNotEmpty) setState(() => _imageUrl = p.imageUrl);
        }).catchError((_) {});
      }
      var emptyPages = 0;
      for (var page = 1; page < 40 && (_total == 0 || _tracks.length < _total); page++) {
        final next = await api.artistSongs(widget.artist.id, page: page);
        if (!mounted || gen != _generation) return;
        final before = _tracks.length;
        setState(() => add(next.tracks));
        // Stop when the catalog keeps returning nothing new.
        emptyPages = _tracks.length == before ? emptyPages + 1 : 0;
        if (next.tracks.isEmpty || emptyPages >= 2) break;
      }
    } catch (e) {
      if (!mounted || gen != _generation) return;
      if (_tracks.isEmpty) {
        setState(() => _error = friendlyError(e));
        ErrorPopup.show(e, title: "Couldn't load this singer", onRetry: _load);
      }
    } finally {
      if (mounted && gen == _generation) {
        setState(() {
          _loading = false;
          _loadingMore = false;
        });
      }
    }
  }

  List<Track> get _playable => _tracks.where((t) => t.playable).toList();

  void _play(int index, {bool shuffle = false}) {
    final list = _playable;
    if (list.isEmpty) return;
    if (shuffle) list.shuffle();
    final start = shuffle ? 0 : list.indexOf(_tracks[index]).clamp(0, list.length - 1);
    context.read<PlayerProvider>().playQueue(list, start);
  }

  @override
  Widget build(BuildContext context) {
    final p = widget.artist;
    final side = Gaps.pagePadding(context);
    final count =
        _loadingMore && _total > _tracks.length ? '${_tracks.length} of $_total songs' : '${_tracks.length} songs';

    return Scaffold(
      appBar: AppBar(),
      body: PageWidth(
        child: CustomScrollView(
          slivers: [
            SliverToBoxAdapter(
              child: Padding(
                padding: EdgeInsets.fromLTRB(side, 0, side, Gaps.lg),
                child: Row(
                  children: [
                    Artwork(url: _imageUrl, size: 148, circle: true, icon: PhosphorIconsRegular.user),
                    const SizedBox(width: Gaps.xl),
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text('ARTIST',
                              style: Theme.of(context)
                                  .textTheme
                                  .labelSmall
                                  ?.copyWith(letterSpacing: 1.6, color: SonivoColors.textSecondary)),
                          const SizedBox(height: Gaps.xs),
                          Text(p.name,
                              maxLines: 2,
                              overflow: TextOverflow.ellipsis,
                              style: Theme.of(context).textTheme.headlineMedium),
                          const SizedBox(height: Gaps.xs),
                          Text(_loading ? 'Loading songs…' : count, style: Theme.of(context).textTheme.bodySmall),
                          const SizedBox(height: Gaps.md),
                          Row(
                            children: [
                              GradientPlayButton(
                                  size: 48, playing: false, onPressed: _tracks.isEmpty ? null : () => _play(0)),
                              const SizedBox(width: Gaps.sm),
                              IconButton.filledTonal(
                                tooltip: 'Shuffle',
                                icon: const Icon(PhosphorIconsRegular.shuffle),
                                onPressed: _tracks.isEmpty ? null : () => _play(0, shuffle: true),
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
            if (_loading)
              SliverToBoxAdapter(
                  child:
                      Padding(padding: EdgeInsets.symmetric(horizontal: side), child: const LinearProgressIndicator())),
            if (_error != null)
              SliverToBoxAdapter(
                child: EmptyState(
                  icon: PhosphorIconsRegular.cloudSlash,
                  title: "Couldn't load songs",
                  message: _error!,
                  action: FilledButton(onPressed: _load, child: const Text('Try again')),
                ),
              ),
            SliverList(
              delegate: SliverChildBuilderDelegate(
                (context, i) {
                  final t = _tracks[i];
                  return TrackTile(
                    track: t,
                    onTap: () => t.playable ? _play(i) : null,
                    trailing: TrackMenu(track: t),
                  );
                },
                childCount: _tracks.length,
              ),
            ),
            if (_loadingMore)
              const SliverToBoxAdapter(
                child: Padding(
                  padding: EdgeInsets.all(Gaps.lg),
                  child: Center(
                      child: SizedBox(width: 22, height: 22, child: CircularProgressIndicator(strokeWidth: 2.5))),
                ),
              ),
            const SliverToBoxAdapter(child: SizedBox(height: Gaps.xxl)),
          ],
        ),
      ),
    );
  }
}
