import 'package:flutter/material.dart';

/// Swar's palette: a warm saffron accent on deep plum night tones, taken
/// from the orange of the app icon. Every screen reads colours from here,
/// so the look stays the same on phones, desktop and the web.
class SonivoColors {
  static const background = Color(0xFF0D0A14);
  static const surface = Color(0xFF15111F);
  static const surfaceHigh = Color(0xFF1E1830);
  static const surfaceHigher = Color(0xFF29213F);
  static const outline = Color(0xFF2E2645);

  static const accent = Color(0xFFFF7A45);
  static const accentSoft = Color(0xFFFFB08A);
  static const violet = Color(0xFF8E6CFF);
  static const rose = Color(0xFFFF4D8D);
  static const onAccent = Color(0xFF1A0E08);

  static const textPrimary = Color(0xFFF4F1FA);
  static const textSecondary = Color(0xFFA59FBA);
  static const textMuted = Color(0xFF6F6888);
  static const danger = Color(0xFFFF5C6C);

  /// Brand gradient for highlights such as the play button and banners.
  static const brandGradient = LinearGradient(
    colors: [Color(0xFFFF8A3D), Color(0xFFFF4D8D), Color(0xFF8E6CFF)],
    begin: Alignment.topLeft,
    end: Alignment.bottomRight,
  );

  /// Soft glow at the top of pages.
  static const pageGlow = LinearGradient(
    colors: [Color(0x332E1A4F), Color(0x000D0A14)],
    begin: Alignment.topCenter,
    end: Alignment.bottomCenter,
  );
}

/// Spacing and size scale shared by every layout.
class Gaps {
  static const xs = 4.0;
  static const sm = 8.0;
  static const md = 12.0;
  static const lg = 16.0;
  static const xl = 24.0;
  static const xxl = 32.0;

  static const radiusSm = 8.0;
  static const radius = 14.0;
  static const radiusLg = 20.0;

  /// Windows at least this wide get the desktop layout.
  static const desktopBreakpoint = 840.0;

  /// Widest a content column grows on big screens.
  static const contentMaxWidth = 1200.0;

  /// Side padding of page content: roomier on wide windows.
  static double pagePadding(BuildContext context) => MediaQuery.sizeOf(context).width >= desktopBreakpoint ? xxl : lg;
}

class SonivoTheme {
  static const fontFamily = 'Inter';

  static ThemeData get dark {
    const scheme = ColorScheme(
      brightness: Brightness.dark,
      primary: SonivoColors.accent,
      onPrimary: SonivoColors.onAccent,
      primaryContainer: Color(0xFF4A2414),
      onPrimaryContainer: SonivoColors.accentSoft,
      secondary: SonivoColors.violet,
      onSecondary: Colors.white,
      secondaryContainer: Color(0xFF2D2152),
      onSecondaryContainer: Color(0xFFD9CCFF),
      tertiary: SonivoColors.rose,
      onTertiary: Colors.white,
      error: SonivoColors.danger,
      onError: Colors.white,
      surface: SonivoColors.surface,
      onSurface: SonivoColors.textPrimary,
      onSurfaceVariant: SonivoColors.textSecondary,
      surfaceContainerLowest: SonivoColors.background,
      surfaceContainerLow: SonivoColors.surface,
      surfaceContainer: SonivoColors.surfaceHigh,
      surfaceContainerHigh: SonivoColors.surfaceHigh,
      surfaceContainerHighest: SonivoColors.surfaceHigher,
      outline: SonivoColors.outline,
      outlineVariant: SonivoColors.outline,
    );

    final base = ThemeData(
      brightness: Brightness.dark,
      useMaterial3: true,
      colorScheme: scheme,
      fontFamily: fontFamily,
      visualDensity: VisualDensity.standard,
    );

    final text = base.textTheme.apply(
      bodyColor: SonivoColors.textPrimary,
      displayColor: SonivoColors.textPrimary,
    );

    final shape = RoundedRectangleBorder(borderRadius: BorderRadius.circular(Gaps.radius));

    return base.copyWith(
      scaffoldBackgroundColor: SonivoColors.background,
      canvasColor: SonivoColors.background,
      primaryColor: SonivoColors.accent,
      dividerColor: SonivoColors.outline,
      splashFactory: InkSparkle.splashFactory,
      textTheme: text.copyWith(
        headlineLarge: text.headlineLarge?.copyWith(fontWeight: FontWeight.w800, letterSpacing: -0.8),
        headlineMedium: text.headlineMedium?.copyWith(fontWeight: FontWeight.w800, letterSpacing: -0.6),
        headlineSmall: text.headlineSmall?.copyWith(fontWeight: FontWeight.w800, letterSpacing: -0.4),
        titleLarge: text.titleLarge?.copyWith(fontWeight: FontWeight.w700, letterSpacing: -0.3),
        titleMedium: text.titleMedium?.copyWith(fontWeight: FontWeight.w600),
        bodyMedium: text.bodyMedium?.copyWith(color: SonivoColors.textPrimary),
        bodySmall: text.bodySmall?.copyWith(color: SonivoColors.textSecondary),
        labelLarge: text.labelLarge?.copyWith(fontWeight: FontWeight.w600),
      ),
      appBarTheme: const AppBarTheme(
        backgroundColor: Colors.transparent,
        surfaceTintColor: Colors.transparent,
        elevation: 0,
        scrolledUnderElevation: 0,
        centerTitle: false,
        titleTextStyle: TextStyle(
          fontFamily: fontFamily,
          fontSize: 20,
          fontWeight: FontWeight.w700,
          color: SonivoColors.textPrimary,
        ),
      ),
      navigationBarTheme: NavigationBarThemeData(
        backgroundColor: SonivoColors.surface,
        surfaceTintColor: Colors.transparent,
        indicatorColor: SonivoColors.accent.withOpacity(0.16),
        height: 64,
        labelBehavior: NavigationDestinationLabelBehavior.alwaysShow,
        iconTheme: WidgetStateProperty.resolveWith((s) => IconThemeData(
              color: s.contains(WidgetState.selected) ? SonivoColors.accent : SonivoColors.textSecondary,
            )),
        labelTextStyle: WidgetStateProperty.resolveWith((s) => TextStyle(
              fontFamily: fontFamily,
              fontSize: 12,
              fontWeight: s.contains(WidgetState.selected) ? FontWeight.w700 : FontWeight.w500,
              color: s.contains(WidgetState.selected) ? SonivoColors.textPrimary : SonivoColors.textSecondary,
            )),
      ),
      sliderTheme: SliderThemeData(
        activeTrackColor: SonivoColors.accent,
        thumbColor: SonivoColors.textPrimary,
        inactiveTrackColor: SonivoColors.surfaceHigher,
        overlayColor: SonivoColors.accent.withOpacity(0.12),
        trackHeight: 4,
        thumbShape: const RoundSliderThumbShape(enabledThumbRadius: 6),
        overlayShape: const RoundSliderOverlayShape(overlayRadius: 14),
      ),
      progressIndicatorTheme: const ProgressIndicatorThemeData(
        color: SonivoColors.accent,
        linearTrackColor: SonivoColors.surfaceHigher,
      ),
      listTileTheme: ListTileThemeData(
        iconColor: SonivoColors.textSecondary,
        textColor: SonivoColors.textPrimary,
        shape: shape,
        contentPadding: const EdgeInsets.symmetric(horizontal: Gaps.md),
      ),
      cardTheme: CardThemeData(
        color: SonivoColors.surfaceHigh,
        surfaceTintColor: Colors.transparent,
        elevation: 0,
        margin: EdgeInsets.zero,
        shape: shape,
      ),
      inputDecorationTheme: InputDecorationTheme(
        filled: true,
        fillColor: SonivoColors.surfaceHigh,
        hintStyle: const TextStyle(color: SonivoColors.textMuted, fontSize: 14),
        prefixIconColor: SonivoColors.textSecondary,
        suffixIconColor: SonivoColors.textSecondary,
        contentPadding: const EdgeInsets.symmetric(horizontal: Gaps.lg, vertical: 14),
        border: OutlineInputBorder(
          borderRadius: BorderRadius.circular(Gaps.radius),
          borderSide: const BorderSide(color: SonivoColors.outline),
        ),
        enabledBorder: OutlineInputBorder(
          borderRadius: BorderRadius.circular(Gaps.radius),
          borderSide: const BorderSide(color: SonivoColors.outline),
        ),
        focusedBorder: OutlineInputBorder(
          borderRadius: BorderRadius.circular(Gaps.radius),
          borderSide: const BorderSide(color: SonivoColors.accent, width: 1.5),
        ),
      ),
      filledButtonTheme: FilledButtonThemeData(
        style: FilledButton.styleFrom(
          backgroundColor: SonivoColors.accent,
          foregroundColor: SonivoColors.onAccent,
          textStyle: const TextStyle(fontFamily: fontFamily, fontWeight: FontWeight.w700),
          padding: const EdgeInsets.symmetric(horizontal: Gaps.xl, vertical: 14),
          shape: const StadiumBorder(),
        ),
      ),
      elevatedButtonTheme: ElevatedButtonThemeData(
        style: ElevatedButton.styleFrom(
          backgroundColor: SonivoColors.accent,
          foregroundColor: SonivoColors.onAccent,
          elevation: 0,
          textStyle: const TextStyle(fontFamily: fontFamily, fontWeight: FontWeight.w700),
          padding: const EdgeInsets.symmetric(horizontal: Gaps.xl, vertical: 14),
          shape: const StadiumBorder(),
        ),
      ),
      outlinedButtonTheme: OutlinedButtonThemeData(
        style: OutlinedButton.styleFrom(
          foregroundColor: SonivoColors.textPrimary,
          side: const BorderSide(color: SonivoColors.outline),
          shape: const StadiumBorder(),
          padding: const EdgeInsets.symmetric(horizontal: Gaps.xl, vertical: 14),
        ),
      ),
      textButtonTheme: TextButtonThemeData(
        style: TextButton.styleFrom(foregroundColor: SonivoColors.accent),
      ),
      iconButtonTheme: IconButtonThemeData(
        style: IconButton.styleFrom(foregroundColor: SonivoColors.textPrimary),
      ),
      chipTheme: ChipThemeData(
        backgroundColor: SonivoColors.surfaceHigh,
        selectedColor: SonivoColors.accent,
        side: const BorderSide(color: SonivoColors.outline),
        labelStyle: const TextStyle(fontFamily: fontFamily, fontWeight: FontWeight.w600, fontSize: 13),
        shape: const StadiumBorder(),
      ),
      switchTheme: SwitchThemeData(
        thumbColor: WidgetStateProperty.resolveWith(
            (s) => s.contains(WidgetState.selected) ? SonivoColors.onAccent : SonivoColors.textSecondary),
        trackColor: WidgetStateProperty.resolveWith(
            (s) => s.contains(WidgetState.selected) ? SonivoColors.accent : SonivoColors.surfaceHigher),
        trackOutlineColor: WidgetStateProperty.all(Colors.transparent),
      ),
      dialogTheme: DialogThemeData(
        backgroundColor: SonivoColors.surfaceHigh,
        surfaceTintColor: Colors.transparent,
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(Gaps.radiusLg)),
        titleTextStyle: const TextStyle(
          fontFamily: fontFamily,
          fontSize: 19,
          fontWeight: FontWeight.w700,
          color: SonivoColors.textPrimary,
        ),
      ),
      bottomSheetTheme: const BottomSheetThemeData(
        backgroundColor: SonivoColors.surface,
        surfaceTintColor: Colors.transparent,
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.vertical(top: Radius.circular(Gaps.radiusLg))),
        showDragHandle: true,
        dragHandleColor: SonivoColors.textMuted,
      ),
      popupMenuTheme: PopupMenuThemeData(
        color: SonivoColors.surfaceHigher,
        surfaceTintColor: Colors.transparent,
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(Gaps.radius)),
        textStyle: const TextStyle(fontFamily: fontFamily, fontSize: 14, color: SonivoColors.textPrimary),
      ),
      snackBarTheme: SnackBarThemeData(
        behavior: SnackBarBehavior.floating,
        backgroundColor: SonivoColors.surfaceHigher,
        contentTextStyle: const TextStyle(fontFamily: fontFamily, color: SonivoColors.textPrimary),
        actionTextColor: SonivoColors.accent,
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(Gaps.radius)),
      ),
      tooltipTheme: TooltipThemeData(
        decoration: BoxDecoration(
          color: SonivoColors.surfaceHigher,
          borderRadius: BorderRadius.circular(Gaps.radiusSm),
          border: Border.all(color: SonivoColors.outline),
        ),
        textStyle: const TextStyle(fontFamily: fontFamily, fontSize: 12, color: SonivoColors.textPrimary),
        waitDuration: const Duration(milliseconds: 400),
      ),
      scrollbarTheme: ScrollbarThemeData(
        thumbColor: WidgetStateProperty.all(SonivoColors.surfaceHigher),
        radius: const Radius.circular(8),
        thickness: WidgetStateProperty.all(8),
      ),
      tabBarTheme: const TabBarThemeData(
        labelColor: SonivoColors.textPrimary,
        unselectedLabelColor: SonivoColors.textSecondary,
        indicatorColor: SonivoColors.accent,
        dividerColor: Colors.transparent,
      ),
    );
  }
}
