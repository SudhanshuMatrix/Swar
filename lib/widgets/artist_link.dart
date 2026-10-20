import 'package:flutter/material.dart';
import 'package:provider/provider.dart';

import '../core/app_icons.dart';
import '../core/errors.dart';
import '../core/theme.dart';
import '../models/online_artist.dart';
import '../screens/artist_screen.dart';
import '../services/api_service.dart';

/// Opens pages inside the app shell (beside the sidebar and player) when
/// it is showing; set by the shell.
void Function(Widget page)? openInShell;

/// Splits "A, B feat. C" into the individual artist names.
List<String> splitArtists(String artists) => artists
    .split(RegExp(r'\s*(?:,|\bfeat\.|\bft\.)\s*', caseSensitive: false))
    .map((a) => a.trim())
    .where((a) => a.isNotEmpty)
    .toSet()
    .toList();

/// Finds the artist called [name] in the catalog and opens their page with
/// all their songs. Several names first ask which one.
Future<void> openArtistByName(BuildContext context, String artists, {bool closeCurrent = false}) async {
  final names = splitArtists(artists);
  if (names.isEmpty) return;
  var name = names.first;
  if (names.length > 1) {
    final picked = await showModalBottomSheet<String>(
      context: context,
      useRootNavigator: true,
      constraints: const BoxConstraints(maxWidth: 520),
      builder: (ctx) => SafeArea(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Padding(
              padding: const EdgeInsets.fromLTRB(Gaps.xl, 0, Gaps.xl, Gaps.sm),
              child: Align(
                alignment: Alignment.centerLeft,
                child: Text('Artists', style: Theme.of(ctx).textTheme.titleLarge),
              ),
            ),
            for (final n in names)
              ListTile(
                leading: const Icon(PhosphorIconsRegular.user),
                title: Text(n),
                onTap: () => Navigator.pop(ctx, n),
              ),
            const SizedBox(height: Gaps.md),
          ],
        ),
      ),
    );
    if (picked == null) return;
    name = picked;
  }
  if (!context.mounted) return;
  final api = context.read<ApiService>();
  final navigator = Navigator.of(context);
  try {
    final found = await api.searchArtists(name, limit: 5);
    final match = found.firstWhere(
      (a) => a.name.toLowerCase() == name.toLowerCase(),
      orElse: () => found.isNotEmpty ? found.first : OnlineArtist(id: '', name: name),
    );
    if (match.id.isEmpty) {
      ErrorPopup.show(InputError("Couldn't find $name in the catalog."), title: 'Artist not found');
      return;
    }
    final page = ArtistScreen(artist: match);
    if (closeCurrent) navigator.pop();
    final open = openInShell;
    if (open != null) {
      open(page);
    } else {
      navigator.push(MaterialPageRoute(builder: (_) => page));
    }
  } catch (e) {
    ErrorPopup.show(e, title: "Couldn't open $name");
  }
}

/// The artist line of a song, where each name opens that artist's page.
class ArtistLinks extends StatefulWidget {
  final String artists;
  final TextStyle? style;
  final bool closeCurrent;
  final int maxLines;

  const ArtistLinks(this.artists, {super.key, this.style, this.closeCurrent = false, this.maxLines = 1});

  @override
  State<ArtistLinks> createState() => _ArtistLinksState();
}

class _ArtistLinksState extends State<ArtistLinks> {
  bool _hover = false;

  @override
  Widget build(BuildContext context) {
    final base = widget.style ?? const TextStyle(color: SonivoColors.textSecondary);
    return MouseRegion(
      cursor: SystemMouseCursors.click,
      onEnter: (_) => setState(() => _hover = true),
      onExit: (_) => setState(() => _hover = false),
      child: GestureDetector(
        behavior: HitTestBehavior.opaque,
        onTap: () => openArtistByName(context, widget.artists, closeCurrent: widget.closeCurrent),
        child: Text(
          widget.artists.isEmpty ? 'Unknown artist' : widget.artists,
          maxLines: widget.maxLines,
          overflow: TextOverflow.ellipsis,
          style: base.copyWith(
            decoration: _hover ? TextDecoration.underline : null,
            decorationColor: base.color,
            color: _hover ? SonivoColors.textPrimary : base.color,
          ),
        ),
      ),
    );
  }
}
