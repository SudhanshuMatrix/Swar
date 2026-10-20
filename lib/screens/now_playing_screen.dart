import 'dart:ui';

import 'package:cached_network_image/cached_network_image.dart';
import 'package:flutter/material.dart';
import 'package:provider/provider.dart';

import '../core/theme.dart';
import '../state/player_provider.dart';
import '../widgets/track_actions.dart';
import '../widgets/ui.dart';

String formatDuration(Duration d) {
  final s = d.inSeconds.remainder(60).toString().padLeft(2, '0');
  return '${d.inMinutes}:$s';
}

/// The play queue as a sheet, used by the full player and the desktop
/// player bar.
void showQueueSheet(BuildContext context) {
  showModalBottomSheet<void>(
    context: context,
    isScrollControlled: true,
    useRootNavigator: true,
    constraints: const BoxConstraints(maxWidth: 640),
    builder: (_) => DraggableScrollableSheet(
      initialChildSize: 0.75,
      minChildSize: 0.4,
      maxChildSize: 0.95,
      expand: false,
      builder: (sheetContext, scrollController) {
        final player = sheetContext.watch<PlayerProvider>();
        final queue = player.queue;
        final currentIndex = player.queueIndex;

        return Column(
          children: [
            Padding(
              padding: const EdgeInsets.fromLTRB(Gaps.xl, 0, Gaps.md, Gaps.xs),
              child: Row(
                children: [
                  Expanded(child: Text('Up next', style: Theme.of(sheetContext).textTheme.titleLarge)),
                  TextButton(
                    onPressed: player.queue.length > 1 ? player.clearQueue : null,
                    child: const Text('Clear'),
                  ),
                ],
              ),
            ),
            SwitchListTile(
              contentPadding: const EdgeInsets.symmetric(horizontal: Gaps.xl),
              title: const Text('Autoplay similar songs', style: TextStyle(fontSize: 14)),
              value: player.autoPlayRelated,
              onChanged: (_) => player.toggleAutoPlayRelated(),
            ),
            const Divider(height: 1),
            Expanded(
              child: queue.isEmpty
                  ? const EmptyState(
                      icon: Icons.queue_music_rounded,
                      title: 'Queue is empty',
                      message: 'Songs you add to the queue show up here.',
                    )
                  : ListView.builder(
                      controller: scrollController,
                      padding: const EdgeInsets.symmetric(vertical: Gaps.sm, horizontal: Gaps.sm),
                      itemCount: queue.length,
                      itemBuilder: (context, i) {
                        final t = queue[i];
                        final isCurrent = i == currentIndex;
                        return ListTile(
                          leading: Artwork(url: t.thumbnailUrl, size: 44),
                          title: Text(
                            t.title,
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                            style: TextStyle(
                              color: isCurrent ? SonivoColors.accent : SonivoColors.textPrimary,
                              fontWeight: FontWeight.w600,
                            ),
                          ),
                          subtitle: Text(
                            t.artist,
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                            style: const TextStyle(color: SonivoColors.textSecondary, fontSize: 12),
                          ),
                          trailing: isCurrent
                              ? const Icon(Icons.graphic_eq_rounded, color: SonivoColors.accent)
                              : IconButton(
                                  tooltip: 'Remove',
                                  icon: const Icon(Icons.close_rounded, size: 18),
                                  onPressed: () => player.removeFromQueue(i),
                                ),
                          onTap: () => player.jumpToQueueIndex(i),
                        );
                      },
                    ),
            ),
          ],
        );
      },
    ),
  );
}

class NowPlayingScreen extends StatelessWidget {
  const NowPlayingScreen({super.key});

  @override
  Widget build(BuildContext context) {
    final player = context.watch<PlayerProvider>();
    final track = player.currentTrack;

    if (track == null) {
      return const Scaffold(
        body: EmptyState(icon: Icons.music_off_rounded, title: 'Nothing playing', message: 'Pick a song to start.'),
      );
    }

    final wide = MediaQuery.sizeOf(context).width >= Gaps.desktopBreakpoint;

    final art = Hero(
      tag: 'now-playing-art',
      child: Container(
        decoration: BoxDecoration(
          borderRadius: BorderRadius.circular(Gaps.radiusLg),
          boxShadow: const [BoxShadow(color: Color(0x99000000), blurRadius: 40, offset: Offset(0, 18))],
        ),
        child: ClipRRect(
          borderRadius: BorderRadius.circular(Gaps.radiusLg),
          child: AspectRatio(
            aspectRatio: 1,
            child: track.thumbnailUrl.isEmpty
                ? const Artwork(url: '', size: 600)
                : CachedNetworkImage(
                    imageUrl: track.thumbnailUrl,
                    fit: BoxFit.cover,
                    errorWidget: (_, __, ___) => const Artwork(url: '', size: 600),
                  ),
          ),
        ),
      ),
    );

    final controls = _Controls(player: player);

    return Scaffold(
      extendBodyBehindAppBar: true,
      appBar: AppBar(
        leading: IconButton(
          icon: const Icon(Icons.keyboard_arrow_down_rounded, size: 30),
          tooltip: 'Close',
          onPressed: () => Navigator.of(context).maybePop(),
        ),
        title: Column(
          children: [
            const Text('NOW PLAYING',
                style: TextStyle(fontSize: 11, letterSpacing: 1.6, color: SonivoColors.textSecondary)),
            if (player.radioLabel != null)
              Text(player.radioLabel!, style: const TextStyle(fontSize: 14, fontWeight: FontWeight.w700)),
          ],
        ),
        centerTitle: true,
        actions: [
          IconButton(
            icon: const Icon(Icons.playlist_add_rounded),
            tooltip: 'Add to playlist',
            onPressed: () => showAddToPlaylist(context, track),
          ),
          IconButton(
            icon: const Icon(Icons.queue_music_rounded),
            tooltip: 'Queue',
            onPressed: () => showQueueSheet(context),
          ),
          const SizedBox(width: Gaps.sm),
        ],
      ),
      body: Stack(
        fit: StackFit.expand,
        children: [
          // Blurred cover behind everything, tinted with the page colour.
          if (track.thumbnailUrl.isNotEmpty)
            ImageFiltered(
              imageFilter: ImageFilter.blur(sigmaX: 60, sigmaY: 60),
              child: CachedNetworkImage(imageUrl: track.thumbnailUrl, fit: BoxFit.cover),
            ),
          Container(
            decoration: BoxDecoration(
              gradient: LinearGradient(
                begin: Alignment.topCenter,
                end: Alignment.bottomCenter,
                colors: [
                  SonivoColors.background.withOpacity(0.55),
                  SonivoColors.background.withOpacity(0.92),
                  SonivoColors.background,
                ],
              ),
            ),
          ),
          SafeArea(
            child: Padding(
              padding: const EdgeInsets.fromLTRB(Gaps.xl, Gaps.sm, Gaps.xl, Gaps.lg),
              child: wide
                  ? Row(
                      children: [
                        Expanded(
                            child: Center(
                                child: ConstrainedBox(constraints: const BoxConstraints(maxWidth: 480), child: art))),
                        const SizedBox(width: Gaps.xxl * 1.5),
                        Expanded(
                            child: Center(
                                child:
                                    ConstrainedBox(constraints: const BoxConstraints(maxWidth: 520), child: controls))),
                      ],
                    )
                  : Column(
                      children: [
                        Expanded(child: Center(child: art)),
                        const SizedBox(height: Gaps.xl),
                        controls,
                      ],
                    ),
            ),
          ),
        ],
      ),
    );
  }
}

class _Controls extends StatelessWidget {
  final PlayerProvider player;
  const _Controls({required this.player});

  @override
  Widget build(BuildContext context) {
    final track = player.currentTrack!;
    final max = player.duration.inMilliseconds == 0 ? 1.0 : player.duration.inMilliseconds.toDouble();
    final value = player.position.inMilliseconds.clamp(0, max.toInt()).toDouble();

    return Column(
      mainAxisSize: MainAxisSize.min,
      children: [
        if (player.errorMessage != null)
          Padding(
            padding: const EdgeInsets.only(bottom: Gaps.md),
            child: Text(
              player.errorMessage!,
              style: const TextStyle(color: SonivoColors.danger, fontSize: 12),
              textAlign: TextAlign.center,
            ),
          ),
        Row(
          children: [
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(track.title,
                      style: Theme.of(context).textTheme.headlineSmall, maxLines: 2, overflow: TextOverflow.ellipsis),
                  const SizedBox(height: Gaps.xs),
                  Text(track.artist,
                      style: const TextStyle(fontSize: 15, color: SonivoColors.textSecondary),
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis),
                ],
              ),
            ),
            LikeButton(track: track, size: 28),
          ],
        ),
        const SizedBox(height: Gaps.md),
        Slider(value: value, max: max, onChanged: (v) => player.seek(Duration(milliseconds: v.toInt()))),
        Padding(
          padding: const EdgeInsets.symmetric(horizontal: Gaps.lg),
          child: Row(
            mainAxisAlignment: MainAxisAlignment.spaceBetween,
            children: [
              Text(formatDuration(player.position),
                  style: const TextStyle(color: SonivoColors.textSecondary, fontSize: 12)),
              Text(formatDuration(player.duration),
                  style: const TextStyle(color: SonivoColors.textSecondary, fontSize: 12)),
            ],
          ),
        ),
        const SizedBox(height: Gaps.md),
        Row(
          mainAxisAlignment: MainAxisAlignment.spaceBetween,
          children: [
            IconButton(
              icon:
                  Icon(Icons.shuffle_rounded, color: player.shuffle ? SonivoColors.accent : SonivoColors.textSecondary),
              tooltip: 'Shuffle',
              onPressed: player.toggleShuffle,
            ),
            IconButton(
              icon: const Icon(Icons.skip_previous_rounded, size: 40),
              tooltip: 'Previous',
              onPressed: player.previous,
            ),
            GradientPlayButton(size: 72, playing: player.playing, loading: player.loading, onPressed: player.playPause),
            IconButton(
              icon: const Icon(Icons.skip_next_rounded, size: 40),
              tooltip: 'Next',
              onPressed: player.next,
            ),
            IconButton(
              icon: Icon(
                player.repeat == PlaybackRepeatMode.one ? Icons.repeat_one_rounded : Icons.repeat_rounded,
                color: player.repeat == PlaybackRepeatMode.off ? SonivoColors.textSecondary : SonivoColors.accent,
              ),
              tooltip: 'Repeat',
              onPressed: player.cycleRepeat,
            ),
          ],
        ),
      ],
    );
  }
}
