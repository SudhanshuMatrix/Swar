import 'dart:ui' show PointerDeviceKind;

import 'package:flutter/material.dart';

import 'core/errors.dart';
import 'core/theme.dart';
import 'screens/home_shell.dart';

class SonivoApp extends StatelessWidget {
  const SonivoApp({super.key});

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'Swar',
      navigatorKey: ErrorPopup.navigatorKey,
      debugShowCheckedModeBanner: false,
      scrollBehavior: const _SwarScrollBehavior(),
      theme: SonivoTheme.dark,
      darkTheme: SonivoTheme.dark,
      themeMode: ThemeMode.dark,
      home: const HomeShell(),
    );
  }
}

/// Lets mouse and trackpad users drag sideways rows (Discover, singers,
/// playlists) the same way touch users swipe them.
class _SwarScrollBehavior extends MaterialScrollBehavior {
  const _SwarScrollBehavior();

  @override
  Set<PointerDeviceKind> get dragDevices => {
        PointerDeviceKind.touch,
        PointerDeviceKind.mouse,
        PointerDeviceKind.trackpad,
        PointerDeviceKind.stylus,
      };
}
