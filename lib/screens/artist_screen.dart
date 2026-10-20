import 'package:cached_network_image/cached_network_image.dart';
import 'package:flutter/material.dart';
import 'package:provider/provider.dart';

import '../core/errors.dart';
import '../core/theme.dart';
import '../models/online_artist.dart';
import '../models/track.dart';
import '../services/api_service.dart';
import '../state/player_provider.dart';
import '../widgets/track_tile.dart';

/// A singer found in search: loads their top songs and plays them.
class ArtistScreen extends StatefulWidget {
  final OnlineArtist artist;
  const ArtistScreen({super.key, required this.artist});

  @override
  State<ArtistScreen> createState() => _ArtistScreenState();
}

class _ArtistScreenState extends State<ArtistScreen> {
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
      final p = await context.read<ApiService>().artist(widget.artist.id);
      if (!mounted) return;
      setState(() => _tracks = p.topSongs.where((t) => t.playable).toList());
    } catch (e) {
      if (!mounted) return;
      setState(() => _error = friendlyError(e));
      ErrorPopup.show(e, title: "Couldn't load this singer", onRetry: _load);
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final player = context.read<PlayerProvider>();
    final p = widget.artist;
    return Scaffold(
      appBar: AppBar(title: Text(p.name, maxLines: 1, overflow: TextOverflow.ellipsis)),
      body: ListView(
        children: [
          Padding(
            padding: const EdgeInsets.all(16),
            child: Row(
              children: [
                ClipRRect(
                  borderRadius: BorderRadius.circular(55),
                  child: p.imageUrl.isNotEmpty
                      ? CachedNetworkImage(imageUrl: p.imageUrl, width: 110, height: 110, fit: BoxFit.cover)
                      : Container(width: 110, height: 110, color: Colors.grey[850]),
                ),
                const SizedBox(width: 16),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(p.name, style: const TextStyle(fontSize: 20, fontWeight: FontWeight.bold)),
                      const SizedBox(height: 4),
                      Text(
                        _loading ? 'Loading…' : '${_tracks.length} songs',
                        style: const TextStyle(color: SonivoColors.textSecondary),
                      ),
                      const SizedBox(height: 8),
                      FilledButton.icon(
                        onPressed: _tracks.isEmpty ? null : () => player.playQueue(_tracks, 0),
                        icon: const Icon(Icons.play_arrow),
                        label: const Text('Play all'),
                      ),
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
                  Text(_error!, style: const TextStyle(color: Colors.redAccent)),
                  TextButton(onPressed: _load, child: const Text('Try again')),
                ],
              ),
            ),
          for (var i = 0; i < _tracks.length; i++)
            TrackTile(track: _tracks[i], onTap: () => player.playQueue(_tracks, i)),
        ],
      ),
    );
  }
}
