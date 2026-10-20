import 'package:flutter/material.dart';
import 'package:provider/provider.dart';

import '../core/app_icons.dart';
import '../core/errors.dart';
import '../core/theme.dart';
import '../models/track.dart';
import '../services/api_service.dart';
import '../services/jam_service.dart';
import '../state/library_provider.dart';
import 'ui.dart';

/// Adds [track] to the Jam, or explains why it can't be added right now.
void addToJam(BuildContext context, Track track) {
  final jam = context.read<JamController>();
  final messenger = ScaffoldMessenger.maybeOf(context);
  if (!jam.inJam) {
    messenger?.showSnackBar(const SnackBar(content: Text('Start or join a Jam first, from the Jam tab.')));
    return;
  }
  if (!(jam.isAdmin || jam.allowParticipantAdd)) {
    messenger?.showSnackBar(const SnackBar(content: Text('Only the host can add songs in this Jam.')));
    return;
  }
  jam.addTrack(track);
  messenger?.showSnackBar(SnackBar(content: Text('Added "${track.title}" to the Jam')));
}

/// A sheet to search for songs and add them to the Jam queue.
Future<void> showJamSongPicker(BuildContext context) {
  return showModalBottomSheet<void>(
    context: context,
    isScrollControlled: true,
    useRootNavigator: true,
    constraints: const BoxConstraints(maxWidth: 680),
    builder: (_) => const FractionallySizedBox(heightFactor: 0.88, child: _JamSongPicker()),
  );
}

class _JamSongPicker extends StatefulWidget {
  const _JamSongPicker();

  @override
  State<_JamSongPicker> createState() => _JamSongPickerState();
}

class _JamSongPickerState extends State<_JamSongPicker> {
  final _controller = TextEditingController();
  final _added = <String>{};
  List<Track> _results = [];
  bool _loading = false;
  String? _error;

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  Future<void> _search(String q) async {
    q = q.trim();
    if (q.isEmpty) return;
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final res = await context.read<ApiService>().search(q);
      if (!mounted) return;
      setState(() => _results = res.where((t) => t.playable).toList());
    } catch (e) {
      if (mounted) setState(() => _error = friendlyError(e));
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  void _add(Track t) {
    context.read<JamController>().addTrack(t);
    setState(() => _added.add(t.id));
  }

  @override
  Widget build(BuildContext context) {
    // Before searching, offer the listener's own liked and recent songs.
    final library = context.watch<LibraryProvider>();
    final suggestions = {...library.recent, ...library.liked}.toList();
    final list = _results.isNotEmpty || _controller.text.trim().isNotEmpty ? _results : suggestions;

    return Column(
      children: [
        Padding(
          padding: const EdgeInsets.fromLTRB(Gaps.xl, 0, Gaps.xl, Gaps.md),
          child: Row(
            children: [
              Expanded(child: Text('Add songs to the Jam', style: Theme.of(context).textTheme.titleLarge)),
              IconButton(
                tooltip: 'Done',
                icon: const Icon(PhosphorIconsRegular.check),
                onPressed: () => Navigator.of(context).pop(),
              ),
            ],
          ),
        ),
        Padding(
          padding: const EdgeInsets.symmetric(horizontal: Gaps.xl),
          child: TextField(
            controller: _controller,
            autofocus: true,
            textInputAction: TextInputAction.search,
            onSubmitted: _search,
            decoration: InputDecoration(
              hintText: 'Search songs',
              prefixIcon: const Icon(PhosphorIconsRegular.magnifyingGlass),
              suffixIcon: IconButton(
                tooltip: 'Search',
                icon: const Icon(PhosphorIconsRegular.arrowRight),
                onPressed: () => _search(_controller.text),
              ),
            ),
          ),
        ),
        const SizedBox(height: Gaps.sm),
        if (_loading)
          const Padding(padding: EdgeInsets.symmetric(horizontal: Gaps.xl), child: LinearProgressIndicator()),
        if (_error != null)
          Padding(
            padding: const EdgeInsets.all(Gaps.lg),
            child: Text(_error!, style: const TextStyle(color: SonivoColors.danger)),
          ),
        if (list.isNotEmpty && list == suggestions)
          const Padding(
            padding: EdgeInsets.fromLTRB(Gaps.xl, Gaps.sm, Gaps.xl, Gaps.xs),
            child: Align(
              alignment: Alignment.centerLeft,
              child: Text('From your library', style: TextStyle(color: SonivoColors.textSecondary, fontSize: 13)),
            ),
          ),
        Expanded(
          child: list.isEmpty && !_loading
              ? const EmptyState(
                  icon: PhosphorIconsRegular.musicNotesPlus,
                  title: 'Find something to play',
                  message: 'Search for a song and tap + to add it to everyone\'s queue.',
                )
              : ListView.builder(
                  padding: const EdgeInsets.symmetric(horizontal: Gaps.sm, vertical: Gaps.xs),
                  itemCount: list.length,
                  itemBuilder: (context, i) {
                    final t = list[i];
                    final added = _added.contains(t.id);
                    return ListTile(
                      leading: Artwork(url: t.thumbnailUrl, size: 44),
                      title: Text(t.title, maxLines: 1, overflow: TextOverflow.ellipsis),
                      subtitle: Text(t.artist,
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: const TextStyle(color: SonivoColors.textSecondary)),
                      trailing: added
                          ? const Icon(PhosphorIconsFill.checkCircle, color: SonivoColors.accent)
                          : IconButton.filledTonal(
                              tooltip: 'Add to Jam',
                              icon: const Icon(PhosphorIconsRegular.plus),
                              onPressed: () => _add(t),
                            ),
                      onTap: added ? null : () => _add(t),
                    );
                  },
                ),
        ),
      ],
    );
  }
}
