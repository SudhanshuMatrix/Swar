import 'package:flutter/widgets.dart';
import 'package:provider/provider.dart';

import '../state/player_provider.dart';

/// Rebuilds [builder] as the song plays without redrawing the rest of the
/// screen, so progress bars stay cheap on slow phones.
class PlaybackPosition extends StatelessWidget {
  final Widget Function(BuildContext context, Duration position) builder;
  const PlaybackPosition({super.key, required this.builder});

  @override
  Widget build(BuildContext context) {
    return RepaintBoundary(
      child: ValueListenableBuilder<Duration>(
        valueListenable: context.read<PlayerProvider>().progress,
        builder: (context, position, _) => builder(context, position),
      ),
    );
  }
}
