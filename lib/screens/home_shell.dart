import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:provider/provider.dart';

import '../core/app_icons.dart';
import '../core/genres.dart';
import '../core/theme.dart';
import '../models/track.dart';
import '../state/library_provider.dart';
import '../state/player_provider.dart';
import '../widgets/artist_link.dart';
import '../widgets/genre_card.dart';
import '../widgets/mini_player.dart';
import '../widgets/player_bar.dart';
import '../widgets/track_actions.dart';
import '../widgets/track_tile.dart';
import '../widgets/ui.dart';
import 'jam_screen.dart';
import 'library_screen.dart';
import 'now_playing_screen.dart';
import 'playlist_screen.dart';
import 'search_screen.dart';

class _Tab {
  final String label;
  final IconData icon;
  final IconData selectedIcon;
  const _Tab(this.label, this.icon, this.selectedIcon);
}

const _tabs = [
  _Tab('Home', PhosphorIconsRegular.house, PhosphorIconsFill.house),
  _Tab('Search', PhosphorIconsRegular.magnifyingGlass, PhosphorIconsBold.magnifyingGlass),
  _Tab('Library', PhosphorIconsRegular.books, PhosphorIconsFill.books),
  _Tab('Jam', PhosphorIconsRegular.usersThree, PhosphorIconsFill.usersThree),
];

/// True while the keyboard focus is in a text field, so shortcuts such as
/// Space never steal keys the user is typing.
bool isTyping() {
  final ctx = FocusManager.instance.primaryFocus?.context;
  if (ctx == null) return false;
  return ctx.widget is EditableText || ctx.findAncestorWidgetOfExactType<EditableText>() != null;
}

class HomeShell extends StatefulWidget {
  const HomeShell({super.key});

  @override
  State<HomeShell> createState() => _HomeShellState();
}

class _HomeShellState extends State<HomeShell> {
  int _index = 0;

  // Each tab keeps its own page stack, so opening an artist or playlist
  // keeps the sidebar, tabs and player in place.
  final _navigators = List.generate(_tabs.length, (_) => GlobalKey<NavigatorState>());

  @override
  void initState() {
    super.initState();
    openInShell = (page) => _navigators[_index].currentState?.push(MaterialPageRoute(builder: (_) => page));
  }

  @override
  void dispose() {
    openInShell = null;
    super.dispose();
  }

  void _openNowPlaying() {
    if (context.read<PlayerProvider>().currentTrack == null) return;
    Navigator.of(context, rootNavigator: true).push(PageRouteBuilder(
      transitionDuration: const Duration(milliseconds: 280),
      pageBuilder: (_, __, ___) => const NowPlayingScreen(),
      transitionsBuilder: (_, anim, __, child) => SlideTransition(
        position: Tween(begin: const Offset(0, 1), end: Offset.zero)
            .animate(CurvedAnimation(parent: anim, curve: Curves.easeOutCubic)),
        child: child,
      ),
    ));
  }

  void _selectTab(int i) {
    if (i == _index) {
      // Tapping the open tab goes back to its first page.
      _navigators[i].currentState?.popUntil((r) => r.isFirst);
    }
    setState(() => _index = i);
  }

  void _searchGenre(Genre g) {
    _navigators[1].currentState?.popUntil((r) => r.isFirst);
    setState(() => _index = 1);
    SearchScreen.requests.value = null;
    SearchScreen.requests.value = g.query;
  }

  void _openPlaylist(Widget page) {
    final nav = _navigators[2].currentState;
    setState(() => _index = 2);
    nav?.popUntil((r) => r.isFirst);
    nav?.push(MaterialPageRoute(builder: (_) => page));
  }

  KeyEventResult _onKey(FocusNode node, KeyEvent event) {
    if (event is! KeyDownEvent || isTyping()) return KeyEventResult.ignored;
    final player = context.read<PlayerProvider>();
    final key = event.logicalKey;
    final mod = HardwareKeyboard.instance.isControlPressed || HardwareKeyboard.instance.isMetaPressed;
    if (key == LogicalKeyboardKey.space || key == LogicalKeyboardKey.mediaPlayPause) {
      if (player.currentTrack == null) return KeyEventResult.ignored;
      player.playPause();
      return KeyEventResult.handled;
    }
    if (key == LogicalKeyboardKey.mediaTrackNext || (mod && key == LogicalKeyboardKey.arrowRight)) {
      player.next();
      return KeyEventResult.handled;
    }
    if (key == LogicalKeyboardKey.mediaTrackPrevious || (mod && key == LogicalKeyboardKey.arrowLeft)) {
      player.previous();
      return KeyEventResult.handled;
    }
    if (mod && key == LogicalKeyboardKey.keyF) {
      _selectTab(1);
      return KeyEventResult.handled;
    }
    return KeyEventResult.ignored;
  }

  Widget _tabNavigator(int i) {
    final Widget root = switch (i) {
      0 => _HomeTab(onExploreTap: () => _selectTab(1), onGenre: _searchGenre),
      1 => const SearchScreen(),
      2 => const LibraryScreen(),
      _ => const JamScreen(),
    };
    return HeroControllerScope.none(
      child: Navigator(
        key: _navigators[i],
        onGenerateRoute: (_) => MaterialPageRoute(
          builder: (_) => Scaffold(backgroundColor: Colors.transparent, body: root),
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final hasCurrentTrack = context.select<PlayerProvider, bool>((p) => p.currentTrack != null);
    final wide = MediaQuery.sizeOf(context).width >= Gaps.desktopBreakpoint;
    final body = IndexedStack(index: _index, children: List.generate(_tabs.length, _tabNavigator));

    final Widget layout = wide
        ? Scaffold(
            body: Column(
              children: [
                Expanded(
                  child: Row(
                    children: [
                      _Sidebar(index: _index, onSelect: _selectTab, onOpenPlaylist: _openPlaylist),
                      Expanded(
                        child: DecoratedBox(
                          decoration: const BoxDecoration(gradient: SonivoColors.pageGlow),
                          child: body,
                        ),
                      ),
                    ],
                  ),
                ),
                if (hasCurrentTrack) PlayerBar(onOpen: _openNowPlaying),
              ],
            ),
          )
        : Scaffold(
            body: DecoratedBox(
              decoration: const BoxDecoration(gradient: SonivoColors.pageGlow),
              child: body,
            ),
            bottomNavigationBar: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                if (hasCurrentTrack) MiniPlayer(onOpen: _openNowPlaying),
                NavigationBar(
                  selectedIndex: _index,
                  onDestinationSelected: _selectTab,
                  destinations: [
                    for (final t in _tabs)
                      NavigationDestination(icon: Icon(t.icon), selectedIcon: Icon(t.selectedIcon), label: t.label),
                  ],
                ),
              ],
            ),
          );

    // Back goes back inside the open tab before leaving the app.
    return PopScope(
      canPop: false,
      onPopInvokedWithResult: (didPop, _) {
        if (didPop) return;
        final nav = _navigators[_index].currentState;
        if (nav != null && nav.canPop()) {
          nav.pop();
        } else if (_index != 0) {
          setState(() => _index = 0);
        } else {
          SystemNavigator.pop();
        }
      },
      child: Focus(autofocus: true, onKeyEvent: _onKey, child: layout),
    );
  }
}

class _Sidebar extends StatelessWidget {
  final int index;
  final ValueChanged<int> onSelect;
  final void Function(Widget page) onOpenPlaylist;
  const _Sidebar({required this.index, required this.onSelect, required this.onOpenPlaylist});

  @override
  Widget build(BuildContext context) {
    final library = context.watch<LibraryProvider>();
    return Container(
      width: 256,
      decoration: const BoxDecoration(
        color: SonivoColors.surface,
        border: Border(right: BorderSide(color: SonivoColors.outline)),
      ),
      child: SafeArea(
        right: false,
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Padding(
              padding: const EdgeInsets.fromLTRB(Gaps.xl, Gaps.xl, Gaps.xl, Gaps.xl),
              child: Row(
                children: [
                  ClipOval(child: Image.asset('assets/brand/logo.png', width: 34, height: 34)),
                  const SizedBox(width: Gaps.md),
                  ShaderMask(
                    shaderCallback: (r) => SonivoColors.brandGradient.createShader(r),
                    child: const Text('Swar',
                        style: TextStyle(
                            fontSize: 24, fontWeight: FontWeight.w800, letterSpacing: -0.8, color: Colors.white)),
                  ),
                ],
              ),
            ),
            for (var i = 0; i < _tabs.length; i++)
              _SidebarItem(
                icon: index == i ? _tabs[i].selectedIcon : _tabs[i].icon,
                label: _tabs[i].label,
                selected: index == i,
                onTap: () => onSelect(i),
              ),
            const Padding(
              padding: EdgeInsets.fromLTRB(Gaps.xl, Gaps.xl, Gaps.xl, Gaps.sm),
              child: Text('YOUR MUSIC',
                  style: TextStyle(
                      fontSize: 11, letterSpacing: 1.4, fontWeight: FontWeight.w700, color: SonivoColors.textMuted)),
            ),
            _SidebarItem(
              icon: PhosphorIconsFill.heart,
              iconColor: SonivoColors.rose,
              label: 'Liked Songs',
              trailing: library.liked.isEmpty ? null : '${library.liked.length}',
              onTap: () => onOpenPlaylist(PlaylistScreen(title: 'Liked Songs', tracks: library.liked)),
            ),
            Expanded(
              child: ListView(
                padding: const EdgeInsets.only(bottom: Gaps.lg),
                children: [
                  for (final p in library.playlists)
                    _SidebarItem(
                      icon: PhosphorIconsRegular.playlist,
                      label: p.name,
                      trailing: p.tracks.isEmpty ? null : '${p.tracks.length}',
                      onTap: () => onOpenPlaylist(PlaylistScreen(title: p.name, tracks: p.tracks, playlistId: p.id)),
                    ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _SidebarItem extends StatelessWidget {
  final IconData icon;
  final Color? iconColor;
  final String label;
  final String? trailing;
  final bool selected;
  final VoidCallback onTap;

  const _SidebarItem({
    required this.icon,
    required this.label,
    required this.onTap,
    this.iconColor,
    this.trailing,
    this.selected = false,
  });

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: Gaps.md, vertical: 2),
      child: Material(
        color: selected ? SonivoColors.accent.withOpacity(0.12) : Colors.transparent,
        borderRadius: BorderRadius.circular(Gaps.radius),
        child: InkWell(
          onTap: onTap,
          borderRadius: BorderRadius.circular(Gaps.radius),
          hoverColor: SonivoColors.surfaceHigh,
          child: Padding(
            padding: const EdgeInsets.symmetric(horizontal: Gaps.md, vertical: 11),
            child: Row(
              children: [
                Icon(icon, size: 22, color: iconColor ?? (selected ? SonivoColors.accent : SonivoColors.textSecondary)),
                const SizedBox(width: Gaps.md),
                Expanded(
                  child: Text(
                    label,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(
                      fontSize: 14,
                      fontWeight: selected ? FontWeight.w700 : FontWeight.w500,
                      color: selected ? SonivoColors.textPrimary : SonivoColors.textSecondary,
                    ),
                  ),
                ),
                if (trailing != null)
                  Text(trailing!, style: const TextStyle(fontSize: 12, color: SonivoColors.textMuted)),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

class _HomeTab extends StatelessWidget {
  final VoidCallback onExploreTap;
  final ValueChanged<Genre> onGenre;
  const _HomeTab({required this.onExploreTap, required this.onGenre});

  String _greeting() {
    final hour = DateTime.now().hour;
    if (hour < 12) return 'Good morning';
    if (hour < 18) return 'Good afternoon';
    return 'Good evening';
  }

  @override
  Widget build(BuildContext context) {
    final library = context.watch<LibraryProvider>();
    final player = context.read<PlayerProvider>();
    final isBrandNew = library.recent.isEmpty && library.liked.isEmpty && library.playlists.isEmpty;
    final side = Gaps.pagePadding(context);
    final width = MediaQuery.sizeOf(context).width;

    return SafeArea(
      bottom: false,
      child: PageWidth(
        child: CustomScrollView(
          slivers: [
            SliverToBoxAdapter(
              child: Padding(
                padding: EdgeInsets.fromLTRB(side, Gaps.xl, side, Gaps.sm),
                child: Row(
                  children: [
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(_greeting(), style: Theme.of(context).textTheme.headlineMedium),
                          const SizedBox(height: Gaps.xs),
                          const Text('What do you want to hear today?',
                              style: TextStyle(color: SonivoColors.textSecondary, fontSize: 15)),
                        ],
                      ),
                    ),
                    IconButton.filledTonal(
                      icon: const Icon(PhosphorIconsRegular.magnifyingGlass),
                      tooltip: 'Search',
                      onPressed: onExploreTap,
                    ),
                  ],
                ),
              ),
            ),
            if (isBrandNew) SliverToBoxAdapter(child: _WelcomeBanner(onExplore: onExploreTap)),
            if (library.playlists.isNotEmpty || library.liked.isNotEmpty)
              SliverPadding(
                padding: EdgeInsets.fromLTRB(side, Gaps.lg, side, 0),
                sliver: SliverGrid(
                  gridDelegate: SliverGridDelegateWithFixedCrossAxisCount(
                    crossAxisCount: width >= 1100 ? 3 : 2,
                    crossAxisSpacing: Gaps.sm,
                    mainAxisSpacing: Gaps.sm,
                    mainAxisExtent: 60,
                  ),
                  delegate: SliverChildListDelegate([
                    if (library.liked.isNotEmpty)
                      _QuickTile(
                        title: 'Liked Songs',
                        icon: PhosphorIconsFill.heart,
                        gradient: const LinearGradient(colors: [SonivoColors.rose, SonivoColors.violet]),
                        onTap: () => Navigator.of(context).push(MaterialPageRoute(
                            builder: (_) => PlaylistScreen(title: 'Liked Songs', tracks: library.liked))),
                      ),
                    for (final p in library.playlists.take(5))
                      _QuickTile(
                        title: p.name,
                        icon: PhosphorIconsRegular.playlist,
                        onTap: () => Navigator.of(context).push(MaterialPageRoute(
                          builder: (_) => PlaylistScreen(title: p.name, tracks: p.tracks, playlistId: p.id),
                        )),
                      ),
                  ]),
                ),
              ),
            if (library.recent.isNotEmpty) ...[
              const SliverToBoxAdapter(child: SectionHeader('Jump back in', subtitle: 'Songs you played recently')),
              SliverToBoxAdapter(
                child: SizedBox(
                  height: 214,
                  child: ListView.separated(
                    scrollDirection: Axis.horizontal,
                    padding: EdgeInsets.symmetric(horizontal: side),
                    itemCount: library.recent.length.clamp(0, 15),
                    separatorBuilder: (_, __) => const SizedBox(width: Gaps.lg),
                    itemBuilder: (context, i) => _CoverCard(
                      track: library.recent[i],
                      onTap: () => player.playQueue(library.recent, i),
                    ),
                  ),
                ),
              ),
            ],
            SliverToBoxAdapter(
              child: SectionHeader(
                'Discover',
                subtitle: 'Moods and genres to explore',
                action: TextButton(onPressed: onExploreTap, child: const Text('Browse all')),
              ),
            ),
            SliverToBoxAdapter(
              child: SizedBox(
                height: 168,
                child: ListView.separated(
                  scrollDirection: Axis.horizontal,
                  padding: EdgeInsets.symmetric(horizontal: side),
                  itemCount: genres.length,
                  separatorBuilder: (_, __) => const SizedBox(width: Gaps.md),
                  itemBuilder: (context, i) => SizedBox(
                    width: 168,
                    child: GenreCard(genre: genres[i], onTap: () => onGenre(genres[i])),
                  ),
                ),
              ),
            ),
            if (library.liked.isNotEmpty) ...[
              SliverToBoxAdapter(
                child: SectionHeader(
                  'Liked Songs',
                  subtitle: '${library.liked.length} songs',
                  action: TextButton.icon(
                    onPressed: () => player.playQueue(library.liked, 0),
                    icon: const Icon(PhosphorIconsFill.play),
                    label: const Text('Play all'),
                  ),
                ),
              ),
              SliverList(
                delegate: SliverChildBuilderDelegate(
                  (context, i) {
                    final t = library.liked[i];
                    return TrackTile(
                      track: t,
                      onTap: () => player.playQueue(library.liked, i),
                      trailing: Row(
                        mainAxisSize: MainAxisSize.min,
                        children: [LikeButton(track: t), TrackMenu(track: t, showLike: false)],
                      ),
                    );
                  },
                  childCount: library.liked.length,
                ),
              ),
            ],
            const SliverToBoxAdapter(child: SizedBox(height: Gaps.xxl)),
          ],
        ),
      ),
    );
  }
}

class _WelcomeBanner extends StatelessWidget {
  final VoidCallback onExplore;
  const _WelcomeBanner({required this.onExplore});

  @override
  Widget build(BuildContext context) {
    final side = Gaps.pagePadding(context);
    return Padding(
      padding: EdgeInsets.fromLTRB(side, Gaps.lg, side, 0),
      child: Container(
        padding: const EdgeInsets.all(Gaps.xl),
        decoration: BoxDecoration(
          gradient: SonivoColors.brandGradient,
          borderRadius: BorderRadius.circular(Gaps.radiusLg),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Text('Welcome to Swar',
                style: TextStyle(fontSize: 26, fontWeight: FontWeight.w800, letterSpacing: -0.6, color: Colors.white)),
            const SizedBox(height: Gaps.sm),
            const Text(
              'Stream any song, save it for offline, and listen together with Jam.',
              style: TextStyle(color: Color(0xE6FFFFFF), fontSize: 15),
            ),
            const SizedBox(height: Gaps.lg),
            FilledButton.icon(
              style: FilledButton.styleFrom(backgroundColor: Colors.white, foregroundColor: SonivoColors.onAccent),
              onPressed: onExplore,
              icon: const Icon(PhosphorIconsRegular.compass),
              label: const Text('Start exploring'),
            ),
          ],
        ),
      ),
    );
  }
}

class _QuickTile extends StatelessWidget {
  final String title;
  final IconData icon;
  final Gradient? gradient;
  final VoidCallback onTap;
  const _QuickTile({required this.title, required this.icon, required this.onTap, this.gradient});

  @override
  Widget build(BuildContext context) {
    return Material(
      color: SonivoColors.surfaceHigh,
      borderRadius: BorderRadius.circular(Gaps.radius),
      clipBehavior: Clip.antiAlias,
      child: InkWell(
        onTap: onTap,
        hoverColor: SonivoColors.surfaceHigher,
        child: Row(
          children: [
            Container(
              width: 60,
              height: 60,
              decoration: BoxDecoration(
                gradient: gradient ??
                    const LinearGradient(
                      colors: [SonivoColors.surfaceHigher, SonivoColors.outline],
                      begin: Alignment.topLeft,
                      end: Alignment.bottomRight,
                    ),
              ),
              child: Icon(icon, color: gradient == null ? SonivoColors.accent : Colors.white),
            ),
            const SizedBox(width: Gaps.md),
            Expanded(
              child: Text(title,
                  maxLines: 2,
                  overflow: TextOverflow.ellipsis,
                  style: const TextStyle(fontWeight: FontWeight.w700, fontSize: 14)),
            ),
          ],
        ),
      ),
    );
  }
}

class _CoverCard extends StatelessWidget {
  final Track track;
  final VoidCallback onTap;
  const _CoverCard({required this.track, required this.onTap});

  @override
  Widget build(BuildContext context) {
    return SizedBox(
      width: 150,
      child: InkWell(
        onTap: onTap,
        borderRadius: BorderRadius.circular(Gaps.radius),
        child: Padding(
          padding: const EdgeInsets.all(2),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Artwork(url: track.thumbnailUrl, size: 146, radius: Gaps.radius),
              const SizedBox(height: Gaps.sm),
              Text(track.title,
                  maxLines: 1, overflow: TextOverflow.ellipsis, style: const TextStyle(fontWeight: FontWeight.w700)),
              const SizedBox(height: 2),
              Text(track.artist,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: const TextStyle(color: SonivoColors.textSecondary, fontSize: 12)),
            ],
          ),
        ),
      ),
    );
  }
}
