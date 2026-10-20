import 'package:cached_network_image/cached_network_image.dart';
import 'package:flutter/material.dart';

import '../core/app_icons.dart';
import '../core/theme.dart';

/// Rounded cover art with a branded placeholder while it loads or when a
/// track has no image.
class Artwork extends StatelessWidget {
  final String url;
  final double size;
  final double radius;
  final IconData icon;
  final bool circle;

  const Artwork({
    super.key,
    required this.url,
    this.size = 48,
    this.radius = Gaps.radiusSm,
    this.icon = PhosphorIconsRegular.musicNote,
    this.circle = false,
  });

  @override
  Widget build(BuildContext context) {
    final placeholder = _Placeholder(size: size, icon: icon);
    final image = url.isEmpty
        ? placeholder
        : CachedNetworkImage(
            imageUrl: url,
            width: size,
            height: size,
            fit: BoxFit.cover,
            fadeInDuration: const Duration(milliseconds: 180),
            placeholder: (_, __) => placeholder,
            errorWidget: (_, __, ___) => placeholder,
          );
    if (circle) return ClipOval(child: SizedBox(width: size, height: size, child: image));
    return ClipRRect(
      borderRadius: BorderRadius.circular(radius),
      child: SizedBox(width: size, height: size, child: image),
    );
  }
}

class _Placeholder extends StatelessWidget {
  final double size;
  final IconData icon;
  const _Placeholder({required this.size, required this.icon});

  @override
  Widget build(BuildContext context) {
    return Container(
      width: size,
      height: size,
      decoration: const BoxDecoration(
        gradient: LinearGradient(
          colors: [SonivoColors.surfaceHigher, SonivoColors.surfaceHigh],
          begin: Alignment.topLeft,
          end: Alignment.bottomRight,
        ),
      ),
      child: Icon(icon, color: SonivoColors.textMuted, size: size * 0.42),
    );
  }
}

/// Title row above a block of content, with an optional action on the right.
class SectionHeader extends StatelessWidget {
  final String title;
  final String? subtitle;
  final Widget? action;
  final EdgeInsetsGeometry? padding;

  const SectionHeader(this.title, {super.key, this.subtitle, this.action, this.padding});

  @override
  Widget build(BuildContext context) {
    final side = Gaps.pagePadding(context);
    return Padding(
      padding: padding ?? EdgeInsets.fromLTRB(side, Gaps.xl, side, Gaps.md),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.end,
        children: [
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(title, style: Theme.of(context).textTheme.titleLarge),
                if (subtitle != null) ...[
                  const SizedBox(height: 2),
                  Text(subtitle!, style: Theme.of(context).textTheme.bodySmall),
                ],
              ],
            ),
          ),
          if (action != null) action!,
        ],
      ),
    );
  }
}

/// The round play button with the brand gradient.
class GradientPlayButton extends StatelessWidget {
  final bool playing;
  final bool loading;
  final VoidCallback? onPressed;
  final double size;

  const GradientPlayButton({
    super.key,
    required this.playing,
    required this.onPressed,
    this.loading = false,
    this.size = 56,
  });

  @override
  Widget build(BuildContext context) {
    return Tooltip(
      message: playing ? 'Pause' : 'Play',
      child: Container(
        width: size,
        height: size,
        decoration: BoxDecoration(
          shape: BoxShape.circle,
          gradient: SonivoColors.brandGradient,
          boxShadow: [
            BoxShadow(color: SonivoColors.rose.withOpacity(0.35), blurRadius: size * 0.35, offset: const Offset(0, 4)),
          ],
        ),
        // A circular, transparent Material keeps the tap ripple round
        // without painting a square behind the button.
        child: Material(
          type: MaterialType.transparency,
          shape: const CircleBorder(),
          clipBehavior: Clip.antiAlias,
          child: InkWell(
            onTap: loading ? null : onPressed,
            child: Center(
              child: loading
                  ? SizedBox(
                      width: size * 0.4,
                      height: size * 0.4,
                      child: const CircularProgressIndicator(strokeWidth: 2.5, color: Colors.white),
                    )
                  : Icon(playing ? PhosphorIconsFill.pause : PhosphorIconsFill.play,
                      color: Colors.white, size: size * 0.55),
            ),
          ),
        ),
      ),
    );
  }
}

/// Small pill used for counts and labels.
class Pill extends StatelessWidget {
  final String text;
  final IconData? icon;
  const Pill(this.text, {super.key, this.icon});

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
      decoration: BoxDecoration(
        color: SonivoColors.surfaceHigher,
        borderRadius: BorderRadius.circular(999),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          if (icon != null) ...[Icon(icon, size: 14, color: SonivoColors.textSecondary), const SizedBox(width: 4)],
          Text(text,
              style: const TextStyle(fontSize: 12, color: SonivoColors.textSecondary, fontWeight: FontWeight.w600)),
        ],
      ),
    );
  }
}

/// A friendly empty state: icon, title and a hint.
class EmptyState extends StatelessWidget {
  final IconData icon;
  final String title;
  final String message;
  final Widget? action;

  const EmptyState({super.key, required this.icon, required this.title, required this.message, this.action});

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(Gaps.xxl),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Container(
              padding: const EdgeInsets.all(Gaps.lg),
              decoration:
                  BoxDecoration(color: SonivoColors.surfaceHigh, borderRadius: BorderRadius.circular(Gaps.radiusLg)),
              child: Icon(icon, size: 36, color: SonivoColors.accent),
            ),
            const SizedBox(height: Gaps.lg),
            Text(title, style: Theme.of(context).textTheme.titleMedium, textAlign: TextAlign.center),
            const SizedBox(height: Gaps.xs),
            Text(message, style: Theme.of(context).textTheme.bodySmall, textAlign: TextAlign.center),
            if (action != null) ...[const SizedBox(height: Gaps.lg), action!],
          ],
        ),
      ),
    );
  }
}

/// Wraps a page's scrollable content so it is padded the same way and stops
/// growing on very wide windows.
class PageWidth extends StatelessWidget {
  final Widget child;
  const PageWidth({super.key, required this.child});

  @override
  Widget build(BuildContext context) {
    return Align(
      alignment: Alignment.topCenter,
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: Gaps.contentMaxWidth),
        child: child,
      ),
    );
  }
}
