import 'package:flutter/material.dart';

import '../core/theme.dart';
import '../state/player_provider.dart';

/// The Audio/Video segmented toggle on the Now Playing screen — same idea
/// as the tab switch in YT Music.
class AudioVideoSwitch extends StatelessWidget {
  final PlaybackMode mode;
  final ValueChanged<PlaybackMode> onChanged;

  const AudioVideoSwitch({super.key, required this.mode, required this.onChanged});

  @override
  Widget build(BuildContext context) {
    return Container(
      decoration: BoxDecoration(
        color: SonivoColors.surfaceHigh,
        borderRadius: BorderRadius.circular(20),
      ),
      padding: const EdgeInsets.all(4),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          _segment(context, 'Audio', PlaybackMode.audio, Icons.headphones),
          _segment(context, 'Video', PlaybackMode.video, Icons.smart_display),
        ],
      ),
    );
  }

  Widget _segment(BuildContext context, String label, PlaybackMode value, IconData icon) {
    final selected = mode == value;
    return GestureDetector(
      onTap: () => onChanged(value),
      child: AnimatedContainer(
        duration: const Duration(milliseconds: 200),
        padding: const EdgeInsets.symmetric(horizontal: 18, vertical: 8),
        decoration: BoxDecoration(
          color: selected ? SonivoColors.accent : Colors.transparent,
          borderRadius: BorderRadius.circular(16),
        ),
        child: Row(
          children: [
            Icon(icon, size: 16, color: selected ? Colors.black : Colors.white),
            const SizedBox(width: 6),
            Text(
              label,
              style: TextStyle(
                color: selected ? Colors.black : Colors.white,
                fontWeight: FontWeight.bold,
              ),
            ),
          ],
        ),
      ),
    );
  }
}
