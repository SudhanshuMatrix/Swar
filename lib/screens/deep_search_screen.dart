import 'package:flutter/material.dart';
import 'package:provider/provider.dart';

import '../core/errors.dart';
import '../core/theme.dart';
import '../models/track.dart';
import '../services/api_service.dart';
import '../state/player_provider.dart';
import '../widgets/track_tile.dart';

/// Results of a user-triggered Deep Search (yt-dlp on the server).
///
/// Deep Search only runs when the user taps the button: it is slower and
/// costs the server more than normal search.
class DeepSearchScreen extends StatefulWidget {
  final String query;
  const DeepSearchScreen({super.key, required this.query});

  @override
  State<DeepSearchScreen> createState() => _DeepSearchScreenState();
}

class _DeepSearchScreenState extends State<DeepSearchScreen> {
  late final TextEditingController _controller = TextEditingController(text: widget.query);
  List<Track> _results = [];
  String _notice = '';
  bool _loading = false;
  bool _searched = false;
  String? _error;

  @override
  void initState() {
    super.initState();
    if (widget.query.trim().isNotEmpty) _run();
  }

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  Future<void> _run() async {
    final q = _controller.text.trim();
    if (q.isEmpty || _loading) return;
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final res = await context.read<ApiService>().deepSearch(q);
      if (!mounted) return;
      setState(() {
        _results = res.tracks;
        _notice = res.notice;
        _searched = true;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() => _error = friendlyError(e));
      ErrorPopup.show(e, title: 'Deep Search failed', onRetry: _run);
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final player = context.read<PlayerProvider>();
    return Scaffold(
      appBar: AppBar(title: const Text('Deep Search')),
      body: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(16, 8, 16, 8),
            child: TextField(
              controller: _controller,
              onSubmitted: (_) => _run(),
              textInputAction: TextInputAction.search,
              decoration: InputDecoration(
                hintText: 'Song name, artist, or a YouTube link',
                prefixIcon: const Icon(Icons.travel_explore),
                suffixIcon: IconButton(icon: const Icon(Icons.arrow_forward), onPressed: _run),
                filled: true,
                fillColor: const Color(0xFF242424),
                border: OutlineInputBorder(borderRadius: BorderRadius.circular(10), borderSide: BorderSide.none),
              ),
            ),
          ),
          if (_loading) const LinearProgressIndicator(color: SonivoColors.accent),
          if (_loading)
            const Padding(
              padding: EdgeInsets.all(16),
              child: Text('Searching more widely. This can take a few seconds…',
                  style: TextStyle(color: SonivoColors.textSecondary)),
            ),
          if (_error != null)
            Padding(
              padding: const EdgeInsets.all(16),
              child: Text(_error!, style: const TextStyle(color: Colors.redAccent)),
            ),
          if (_notice.isNotEmpty && _results.isNotEmpty)
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 4),
              child: Text(_notice, style: const TextStyle(color: SonivoColors.textSecondary, fontSize: 12)),
            ),
          if (_searched && !_loading && _results.isEmpty && _error == null)
            const Padding(
              padding: EdgeInsets.all(16),
              child: Text('Nothing found. Try different words or paste a link.'),
            ),
          Expanded(
            child: ListView.builder(
              itemCount: _results.length,
              itemBuilder: (context, i) {
                final t = _results[i];
                return TrackTile(
                  track: t,
                  onTap: () {
                    if (!t.playable) {
                      ScaffoldMessenger.of(context).showSnackBar(const SnackBar(
                        content: Text('Found it, but this Swar server does not play Deep Search results.'),
                      ));
                      return;
                    }
                    player.playQueue(_results.where((r) => r.playable).toList(),
                        _results.where((r) => r.playable).toList().indexOf(t));
                  },
                  trailing: t.playable
                      ? const Icon(Icons.play_arrow, color: Colors.white70)
                      : const Tooltip(
                          message: 'Metadata only on this server',
                          child: Icon(Icons.info_outline, color: Colors.white38),
                        ),
                );
              },
            ),
          ),
        ],
      ),
    );
  }
}
