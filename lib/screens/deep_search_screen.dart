import 'package:flutter/material.dart';
import 'package:provider/provider.dart';

import '../core/app_icons.dart';
import '../core/errors.dart';
import '../core/theme.dart';
import '../models/track.dart';
import '../services/deep_search_service.dart';
import '../services/ytdlp/device_ytdlp.dart';
import '../state/player_provider.dart';
import '../widgets/track_actions.dart';
import '../widgets/track_tile.dart';

/// Results of a user-triggered Deep Search: yt-dlp on this device for
/// YouTube, plus the server's search of the other music catalogs.
///
/// Deep Search only runs when the user asks for it: it is slower than
/// normal search. Repeated searches come from the cache.
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

  /// The search the screen shows; older answers are ignored.
  int _searchId = 0;

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
    if (q.isEmpty) return;
    final id = ++_searchId;
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final hit = await context.read<DeepSearchService>().search(q, onRefreshed: (res) {
        // A background refresh of what is on screen.
        if (mounted && id == _searchId) setState(() => _show(res.tracks, res.notice));
      });
      if (!mounted || id != _searchId) return;
      setState(() => _show(hit.value.tracks, hit.value.notice));
    } catch (e) {
      if (!mounted || id != _searchId) return;
      setState(() => _error = friendlyError(e));
      ErrorPopup.show(e, title: 'Deep Search failed', onRetry: _run);
    } finally {
      if (mounted && id == _searchId) setState(() => _loading = false);
    }
  }

  void _show(List<Track> tracks, String notice) {
    _results = tracks;
    _notice = notice;
    _searched = true;
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
                prefixIcon: const Icon(PhosphorIconsRegular.globe),
                suffixIcon: IconButton(icon: const Icon(PhosphorIconsRegular.arrowRight), onPressed: _run),
                filled: true,
                border: OutlineInputBorder(borderRadius: BorderRadius.circular(10), borderSide: BorderSide.none),
              ),
            ),
          ),
          if (DeviceYtDlp.supported && !context.watch<DeviceYtDlp>().installed)
            const Padding(
              padding: EdgeInsets.symmetric(horizontal: 16, vertical: 4),
              child: Text(
                "yt-dlp isn't on this device yet, so YouTube is searched by the server, which YouTube sometimes blocks.",
                style: TextStyle(color: SonivoColors.textMuted, fontSize: 12),
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
              child: Text(_error!, style: const TextStyle(color: SonivoColors.danger)),
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
                      ? TrackMenu(track: t, showRadio: false)
                      : const Tooltip(
                          message: 'Metadata only on this server',
                          child: Icon(PhosphorIconsRegular.info, color: SonivoColors.textMuted),
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
