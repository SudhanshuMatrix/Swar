import 'package:flutter/material.dart';
import 'package:provider/provider.dart';

import '../core/app_icons.dart';
import '../core/theme.dart';
import '../state/player_provider.dart';

/// "12 min left", for a sleep timer ending at [at].
String sleepLeft(DateTime at) {
  final left = at.difference(DateTime.now());
  if (left.inMinutes >= 60) return '${left.inHours} h ${left.inMinutes.remainder(60)} min left';
  return '${left.inMinutes.clamp(1, 59)} min left';
}

/// A moon button that opens the sleep timer and glows while one is set.
class SleepTimerButton extends StatelessWidget {
  final double size;
  const SleepTimerButton({super.key, this.size = 22});

  @override
  Widget build(BuildContext context) {
    final player = context.watch<PlayerProvider>();
    final at = player.sleepAt;
    final tooltip = at != null
        ? 'Sleep timer: ${sleepLeft(at)}'
        : player.sleepAfterTrack
            ? 'Sleep timer: after this song'
            : 'Sleep timer';
    return IconButton(
      tooltip: tooltip,
      icon: Icon(
        player.sleepTimerActive ? PhosphorIconsFill.moon : PhosphorIconsRegular.moon,
        size: size,
        color: player.sleepTimerActive ? SonivoColors.accent : null,
      ),
      onPressed: () => showSleepTimerSheet(context),
    );
  }
}

Future<void> showSleepTimerSheet(BuildContext context) {
  return showModalBottomSheet<void>(
    context: context,
    useRootNavigator: true,
    constraints: const BoxConstraints(maxWidth: 520),
    builder: (_) => const _SleepTimerSheet(),
  );
}

class _SleepTimerSheet extends StatelessWidget {
  const _SleepTimerSheet();

  static const _choices = [15, 30, 45, 60, 90];

  @override
  Widget build(BuildContext context) {
    final player = context.watch<PlayerProvider>();
    final theme = Theme.of(context);

    void pick(VoidCallback action, String message) {
      action();
      Navigator.of(context).pop();
      ScaffoldMessenger.maybeOf(context)?.showSnackBar(SnackBar(content: Text(message)));
    }

    final status = player.sleepAt != null
        ? 'Music pauses in ${sleepLeft(player.sleepAt!).replaceAll(' left', '')}.'
        : player.sleepAfterTrack
            ? 'Music pauses when this song ends.'
            : 'Pause the music after a while.';

    return SafeArea(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(Gaps.xl, 0, Gaps.xl, Gaps.xl),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Container(
                  width: 44,
                  height: 44,
                  decoration: BoxDecoration(
                    gradient: SonivoColors.brandGradient,
                    borderRadius: BorderRadius.circular(14),
                  ),
                  child: const Icon(PhosphorIconsFill.moon, color: Colors.white),
                ),
                const SizedBox(width: Gaps.md),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text('Sleep timer', style: theme.textTheme.titleLarge),
                      Text(status, style: theme.textTheme.bodySmall),
                    ],
                  ),
                ),
              ],
            ),
            const SizedBox(height: Gaps.xl),
            Wrap(
              spacing: Gaps.sm,
              runSpacing: Gaps.sm,
              children: [
                for (final m in _choices)
                  ChoiceChip(
                    label: Text(m < 60 ? '$m min' : (m == 60 ? '1 hour' : '1.5 hours')),
                    selected: false,
                    onSelected: (_) => pick(() => player.setSleepTimer(Duration(minutes: m)),
                        'Sleep timer set for ${m < 60 ? '$m minutes' : (m == 60 ? '1 hour' : '1.5 hours')}'),
                  ),
                ChoiceChip(
                  label: const Text('End of this song'),
                  selected: player.sleepAfterTrack,
                  onSelected: (_) => pick(player.setSleepAfterTrack, 'Music will pause when this song ends'),
                ),
              ],
            ),
            if (player.sleepTimerActive) ...[
              const SizedBox(height: Gaps.lg),
              SizedBox(
                width: double.infinity,
                child: OutlinedButton.icon(
                  onPressed: () => pick(() => player.setSleepTimer(null), 'Sleep timer off'),
                  icon: const Icon(PhosphorIconsRegular.x, size: 18),
                  label: const Text('Turn off'),
                ),
              ),
            ],
          ],
        ),
      ),
    );
  }
}
