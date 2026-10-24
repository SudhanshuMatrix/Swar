/// Which queue entries to prepare while the current song plays. Pure, so
/// it is unit tested.
///
/// - The immediate next song comes first; a second one only when [count]
///   allows (not in data saver).
/// - With shuffle, only the song already picked to play next is known, so
///   only it is prepared.
/// - With repeat-all the queue wraps around; otherwise it stops at the end.
/// - The current song is never included, and neither is a song twice.
List<int> upcomingIndexes({
  required int length,
  required int current,
  required int count,
  int? shuffleNext,
  bool repeatAll = false,
}) {
  if (count <= 0 || length <= 1 || current < 0 || current >= length) return const [];
  if (shuffleNext != null) {
    return shuffleNext >= 0 && shuffleNext < length && shuffleNext != current ? [shuffleNext] : const [];
  }
  final out = <int>[];
  for (var step = 1; step < length && out.length < count; step++) {
    final i = current + step;
    if (i < length) {
      out.add(i);
    } else if (repeatAll) {
      final w = i % length;
      if (w != current && !out.contains(w)) out.add(w);
    } else {
      break;
    }
  }
  return out;
}
