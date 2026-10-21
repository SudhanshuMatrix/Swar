import 'package:flutter/material.dart';
import 'package:provider/provider.dart';

import '../core/app_icons.dart';
import '../core/errors.dart';
import '../core/theme.dart';
import '../models/online_playlist.dart';
import '../models/track.dart';
import '../services/api_service.dart';
import '../state/player_provider.dart';
import '../widgets/play_all_button.dart';
import '../widgets/track_actions.dart';
import '../widgets/track_tile.dart';
import '../widgets/ui.dart';

/// A public playlist found in search: loads its songs and plays them.
class OnlinePlaylistScreen extends StatefulWidget {
  final OnlinePlaylist playlist;
  const OnlinePlaylistScreen({super.key, required this.playlist});

  @override
  State<OnlinePlaylistScreen> createState() => _OnlinePlaylistScreenState();
}

class _OnlinePlaylistScreenState extends State<OnlinePlaylistScreen> {
  List<Track> _tracks = [];
  bool _loading = true;
  String? _error;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final p = await context.read<ApiService>().playlist(widget.playlist.id);
      if (!mounted) return;
      setState(() => _tracks = p.tracks.where((t) => t.playable).toList());
    } catch (e) {
      if (!mounted) return;
      setState(() => _error = friendlyError(e));
      ErrorPopup.show(e, title: "Couldn't open this playlist", onRetry: _load);
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final player = context.read<PlayerProvider>();
    final p = widget.playlist;
    return Scaffold(
      appBar: AppBar(title: Text(p.name, maxLines: 1, overflow: TextOverflow.ellipsis)),
      body: PageWidth(
        child: ListView(
          children: [
            Padding(
              padding: EdgeInsets.fromLTRB(Gaps.pagePadding(context), Gaps.sm, Gaps.pagePadding(context), Gaps.lg),
              child: Row(
                children: [
                  Artwork(url: p.imageUrl, size: 140, radius: Gaps.radiusLg, icon: PhosphorIconsRegular.playlist),
                  const SizedBox(width: 16),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(p.name, style: Theme.of(context).textTheme.headlineSmall),
                        const SizedBox(height: 4),
                        Text(
                          _loading ? 'Loading…' : '${_tracks.length} songs',
                          style: const TextStyle(color: SonivoColors.textSecondary),
                        ),
                        const SizedBox(height: 8),
                        PlayAllButton(tracks: _tracks, size: 44),
                      ],
                    ),
                  ),
                ],
              ),
            ),
            if (_loading) const LinearProgressIndicator(color: SonivoColors.accent),
            if (_error != null)
              Padding(
                padding: const EdgeInsets.all(16),
                child: Column(
                  children: [
                    Text(_error!, style: const TextStyle(color: SonivoColors.danger)),
                    TextButton(onPressed: _load, child: const Text('Try again')),
                  ],
                ),
              ),
            for (var i = 0; i < _tracks.length; i++)
              TrackTile(
                  track: _tracks[i], onTap: () => player.playQueue(_tracks, i), trailing: TrackMenu(track: _tracks[i])),
          ],
        ),
      ),
    );
  }
}
