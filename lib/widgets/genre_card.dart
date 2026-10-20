import 'package:flutter/material.dart';

import '../core/genres.dart';
import '../core/theme.dart';

/// A Discover card: the genre's cover art with its name on top.
class GenreCard extends StatefulWidget {
  final Genre genre;
  final VoidCallback onTap;
  const GenreCard({super.key, required this.genre, required this.onTap});

  @override
  State<GenreCard> createState() => _GenreCardState();
}

class _GenreCardState extends State<GenreCard> {
  bool _hover = false;

  @override
  Widget build(BuildContext context) {
    return MouseRegion(
      cursor: SystemMouseCursors.click,
      onEnter: (_) => setState(() => _hover = true),
      onExit: (_) => setState(() => _hover = false),
      child: AnimatedScale(
        scale: _hover ? 1.03 : 1,
        duration: const Duration(milliseconds: 160),
        child: ClipRRect(
          borderRadius: BorderRadius.circular(Gaps.radius),
          child: Material(
            color: SonivoColors.surfaceHigh,
            child: InkWell(
              onTap: widget.onTap,
              child: Stack(
                fit: StackFit.expand,
                children: [
                  Image.asset(widget.genre.cover, fit: BoxFit.cover),
                  const DecoratedBox(
                    decoration: BoxDecoration(
                      gradient: LinearGradient(
                        colors: [Color(0x00000000), Color(0x99000000)],
                        begin: Alignment.center,
                        end: Alignment.bottomCenter,
                      ),
                    ),
                  ),
                  Positioned(
                    left: Gaps.md,
                    right: Gaps.md,
                    bottom: Gaps.md,
                    child: Text(
                      widget.genre.title,
                      maxLines: 2,
                      overflow: TextOverflow.ellipsis,
                      style: const TextStyle(
                        color: Colors.white,
                        fontSize: 17,
                        fontWeight: FontWeight.w800,
                        letterSpacing: -0.3,
                        shadows: [Shadow(color: Color(0x80000000), blurRadius: 8)],
                      ),
                    ),
                  ),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }
}
