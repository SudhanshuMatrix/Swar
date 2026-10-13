import 'package:flutter/material.dart';

class SonivoColors {
  static const background = Color(0xFF121212);
  static const surface = Color(0xFF1E1E1E);
  static const surfaceHigh = Color(0xFF282828);
  static const accent = Color(0xFF1DB954);
  static const textSecondary = Color(0xFFB3B3B3);
}

class SonivoTheme {
  static ThemeData get dark {
    final base = ThemeData(
      brightness: Brightness.dark,
      useMaterial3: true,
      colorScheme: ColorScheme.fromSeed(
        seedColor: SonivoColors.accent,
        brightness: Brightness.dark,
        surface: SonivoColors.surface,
      ),
    );
    return base.copyWith(
      scaffoldBackgroundColor: SonivoColors.background,
      primaryColor: SonivoColors.accent,
      appBarTheme: const AppBarTheme(
        backgroundColor: SonivoColors.background,
        elevation: 0,
      ),
      bottomNavigationBarTheme: const BottomNavigationBarThemeData(
        backgroundColor: SonivoColors.surface,
        selectedItemColor: SonivoColors.accent,
        unselectedItemColor: SonivoColors.textSecondary,
      ),
      sliderTheme: const SliderThemeData(
        activeTrackColor: SonivoColors.accent,
        thumbColor: Colors.white,
        inactiveTrackColor: SonivoColors.surfaceHigh,
        trackHeight: 3,
      ),
      listTileTheme: const ListTileThemeData(
        iconColor: SonivoColors.textSecondary,
      ),
      dialogBackgroundColor: SonivoColors.surfaceHigh,
    );
  }
}
