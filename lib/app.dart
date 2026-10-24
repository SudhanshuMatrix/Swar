import 'dart:ui' show PointerDeviceKind;

import 'package:flutter/material.dart';

import 'core/errors.dart';
import 'core/theme.dart';
import 'screens/home_shell.dart';
import 'widgets/app_status_gate.dart';
import 'widgets/ytdlp_gate.dart';

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
      builder: (context, child) => AppStatusGate(child: YtDlpGate(child: child ?? const SizedBox.shrink())),
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
