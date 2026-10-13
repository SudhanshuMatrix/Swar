import 'package:cached_network_image/cached_network_image.dart';
import 'package:flutter/material.dart';
import 'package:provider/provider.dart';

import '../core/theme.dart';
import '../models/track.dart';
import '../state/player_provider.dart';

class TrackTile extends StatelessWidget {
  final Track track;
  final VoidCallback onTap;
  final Widget? trailing;

  const TrackTile({super.key, required this.track, required this.onTap, this.trailing});

  @override
  Widget build(BuildContext context) {
    final isCurrent = context.select<PlayerProvider, bool>((p) => p.currentTrack?.id == track.id);

    return ListTile(
      leading: ClipRRect(
        borderRadius: BorderRadius.circular(4),
        child: track.thumbnailUrl.isNotEmpty
            ? CachedNetworkImage(
                imageUrl: track.thumbnailUrl,
                width: 48,
                height: 48,
                fit: BoxFit.cover,
                errorWidget: (_, __, ___) => Container(width: 48, height: 48, color: Colors.grey[800]),
                placeholder: (_, __) => Container(width: 48, height: 48, color: Colors.grey[800]),
              )
            : Container(width: 48, height: 48, color: Colors.grey[800]),
      ),
      title: Text(
        track.title,
        maxLines: 1,
        overflow: TextOverflow.ellipsis,
        style: TextStyle(
          color: isCurrent ? SonivoColors.accent : Colors.white,
          fontWeight: isCurrent ? FontWeight.bold : FontWeight.normal,
        ),
      ),
      subtitle: Text(
        track.artist,
        maxLines: 1,
        overflow: TextOverflow.ellipsis,
        style: const TextStyle(color: Colors.grey),
      ),
      trailing: trailing,
      onTap: onTap,
    );
  }
}
