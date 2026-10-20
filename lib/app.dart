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
      theme: SonivoTheme.dark,
      darkTheme: SonivoTheme.dark,
      themeMode: ThemeMode.dark,
      home: const HomeShell(),
    );
  }
}
