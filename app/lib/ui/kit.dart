// The web panel's design system in Flutter: same tokens (styles.css), same components,
// so the app reads like the panel. Sizes are the panel's CSS pixels.
import 'dart:math' as math;

import 'package:flutter/cupertino.dart' show CupertinoPageTransitionsBuilder;
import 'package:flutter/material.dart';

import 'web_icon.dart';
import 'widgets.dart';

export 'web_icon.dart';

const kFont = 'GoogleSansFlex';
const kMono = 'monospace';

/// Status colors, fixed across seeds like --ok, --warn and --bad in the panel.
class Tones {
  Tones(this.dark);
  final bool dark;
  static Tones of(BuildContext c) =>
      Tones(Theme.of(c).brightness == Brightness.dark);
  Color get ok => dark ? const Color(0xFF8FD694) : const Color(0xFF2E7D32);
  Color get onOk => dark ? const Color(0xFF0B3A12) : Colors.white;
  Color get warn => dark ? const Color(0xFFF6C177) : const Color(0xFF8A5A00);
  Color get onWarn => dark ? const Color(0xFF3A2600) : Colors.white;
  Color get bad => dark ? const Color(0xFFF2B8B5) : const Color(0xFFB3261E);
  Color get onBad => dark ? const Color(0xFF601410) : Colors.white;
  Color get series1 => dark ? const Color(0xFF9478E8) : const Color(0xFF6750A4);
  Color get series2 => dark ? const Color(0xFF1E9E81) : const Color(0xFF008A73);
}

/// The panel's theme: Google Sans Flex, 28 px cards on surface-container, pill buttons.
ThemeData webTheme(ColorScheme cs) {
  final base = ThemeData(colorScheme: cs, useMaterial3: true);
  final text = base.textTheme
      .apply(
        fontFamily: kFont,
        bodyColor: cs.onSurface,
        displayColor: cs.onSurface,
      )
      .copyWith(
        headlineMedium: TextStyle(
          fontFamily: kFont,
          fontSize: 30,
          fontWeight: FontWeight.w500,
          color: cs.onSurface,
        ),
        titleLarge: TextStyle(
          fontFamily: kFont,
          fontSize: 22,
          fontWeight: FontWeight.w500,
          color: cs.onSurface,
        ),
        titleMedium: TextStyle(
          fontFamily: kFont,
          fontSize: 20,
          fontWeight: FontWeight.w500,
          color: cs.onSurface,
        ),
        titleSmall: TextStyle(
          fontFamily: kFont,
          fontSize: 16,
          fontWeight: FontWeight.w500,
          color: cs.onSurface,
        ),
        bodyLarge: TextStyle(
          fontFamily: kFont,
          fontSize: 16,
          height: 1.45,
          color: cs.onSurface,
        ),
        bodyMedium: TextStyle(
          fontFamily: kFont,
          fontSize: 15,
          height: 1.45,
          color: cs.onSurface,
        ),
        bodySmall: TextStyle(
          fontFamily: kFont,
          fontSize: 13,
          height: 1.4,
          color: cs.onSurfaceVariant,
        ),
        labelLarge: const TextStyle(
          fontFamily: kFont,
          fontSize: 14,
          fontWeight: FontWeight.w600,
        ),
      );
  const pill = StadiumBorder();
  const btnText = TextStyle(
    fontFamily: kFont,
    fontSize: 14,
    fontWeight: FontWeight.w600,
  );
  final btnSize = WidgetStateProperty.all(const Size(0, 40));
  final btnPad = WidgetStateProperty.all(
    const EdgeInsets.symmetric(horizontal: 20),
  );
  OutlineInputBorder field(Color c, [double w = 1]) => OutlineInputBorder(
    borderRadius: BorderRadius.circular(12),
    borderSide: BorderSide(color: c, width: w),
  );
  return base.copyWith(
    textTheme: text,
    primaryTextTheme: text,
    // Pushed screens rise in like the panel's pages; iOS keeps its edge-swipe transition.
    pageTransitionsTheme: const PageTransitionsTheme(
      builders: {
        TargetPlatform.android: _FadeRiseTransitions(),
        TargetPlatform.fuchsia: _FadeRiseTransitions(),
        TargetPlatform.linux: _FadeRiseTransitions(),
        TargetPlatform.windows: _FadeRiseTransitions(),
        TargetPlatform.iOS: CupertinoPageTransitionsBuilder(),
        TargetPlatform.macOS: CupertinoPageTransitionsBuilder(),
      },
    ),
    scaffoldBackgroundColor: cs.surface,
    canvasColor: cs.surface,
    dividerColor: cs.surfaceContainerHigh,
    dividerTheme: DividerThemeData(
      color: cs.surfaceContainerHigh,
      thickness: 1,
      space: 1,
    ),
    cardTheme: CardThemeData(
      elevation: 0,
      margin: EdgeInsets.zero,
      color: cs.surfaceContainer,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.all(Radius.circular(28)),
      ),
    ),
    filledButtonTheme: FilledButtonThemeData(
      style: ButtonStyle(
        shape: WidgetStateProperty.all(pill),
        minimumSize: btnSize,
        padding: btnPad,
        textStyle: WidgetStateProperty.all(btnText),
      ),
    ),
    outlinedButtonTheme: OutlinedButtonThemeData(
      style: ButtonStyle(
        shape: WidgetStateProperty.all(pill),
        minimumSize: btnSize,
        padding: btnPad,
        textStyle: WidgetStateProperty.all(btnText),
        side: WidgetStateProperty.all(BorderSide(color: cs.outline)),
      ),
    ),
    textButtonTheme: TextButtonThemeData(
      style: ButtonStyle(
        shape: WidgetStateProperty.all(pill),
        minimumSize: btnSize,
        padding: WidgetStateProperty.all(
          const EdgeInsets.symmetric(horizontal: 12),
        ),
        textStyle: WidgetStateProperty.all(btnText),
      ),
    ),
    iconButtonTheme: IconButtonThemeData(
      style: ButtonStyle(
        foregroundColor: WidgetStateProperty.all(cs.onSurfaceVariant),
        minimumSize: WidgetStateProperty.all(const Size(40, 40)),
      ),
    ),
    inputDecorationTheme: InputDecorationTheme(
      border: field(cs.outline),
      enabledBorder: field(cs.outline),
      focusedBorder: field(cs.primary, 2),
      errorBorder: field(cs.error),
      focusedErrorBorder: field(cs.error, 2),
      contentPadding: const EdgeInsets.symmetric(horizontal: 16, vertical: 16),
      hintStyle: TextStyle(color: cs.onSurfaceVariant),
      labelStyle: TextStyle(color: cs.onSurfaceVariant),
    ),
    dialogTheme: DialogThemeData(
      backgroundColor: cs.surfaceContainerHigh,
      surfaceTintColor: Colors.transparent,
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(28)),
      titleTextStyle: text.titleLarge,
    ),
    bottomSheetTheme: BottomSheetThemeData(
      backgroundColor: cs.surfaceContainerLow,
      surfaceTintColor: Colors.transparent,
      showDragHandle: true,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(28)),
      ),
    ),
    snackBarTheme: SnackBarThemeData(
      behavior: SnackBarBehavior.floating,
      backgroundColor: cs.onSurface,
      contentTextStyle: TextStyle(fontFamily: kFont, color: cs.surface),
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
    ),
    popupMenuTheme: PopupMenuThemeData(
      color: cs.surfaceContainerHighest,
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(16)),
    ),
    navigationBarTheme: NavigationBarThemeData(
      backgroundColor: cs.surfaceContainer,
      indicatorColor: cs.secondaryContainer,
      height: 80,
      labelTextStyle: WidgetStateProperty.resolveWith(
        (s) => TextStyle(
          fontFamily: kFont,
          fontSize: 12,
          fontWeight: FontWeight.w500,
          color: s.contains(WidgetState.selected)
              ? cs.onSurface
              : cs.onSurfaceVariant,
        ),
      ),
    ),
    navigationRailTheme: NavigationRailThemeData(
      backgroundColor: Colors.transparent,
      indicatorColor: cs.secondaryContainer,
      labelType: NavigationRailLabelType.all,
      selectedLabelTextStyle: TextStyle(
        fontFamily: kFont,
        fontSize: 12,
        fontWeight: FontWeight.w500,
        color: cs.onSurface,
      ),
      unselectedLabelTextStyle: TextStyle(
        fontFamily: kFont,
        fontSize: 12,
        fontWeight: FontWeight.w500,
        color: cs.onSurfaceVariant,
      ),
    ),
    appBarTheme: AppBarTheme(
      backgroundColor: cs.surface,
      surfaceTintColor: Colors.transparent,
      scrolledUnderElevation: 0,
      titleTextStyle: text.titleLarge,
    ),
    switchTheme: base.switchTheme,
    checkboxTheme: CheckboxThemeData(
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(4)),
    ),
    tooltipTheme: TooltipThemeData(
      decoration: BoxDecoration(
        color: cs.inverseSurface,
        borderRadius: BorderRadius.circular(8),
      ),
      textStyle: TextStyle(
        fontFamily: kFont,
        fontSize: 12,
        color: cs.onInverseSurface,
      ),
    ),
  );
}

// ---------------------------------------------------------------------------------
// Layout

/// A page: scrolls, 16 px gaps between sections, like .page.
class WPage extends StatelessWidget {
  const WPage({super.key, required this.children, this.onRefresh});
  final List<Widget> children;
  final Future<void> Function()? onRefresh;

  @override
  Widget build(BuildContext context) {
    final list = ListView.separated(
      padding: const EdgeInsets.fromLTRB(16, 8, 16, 32),
      itemCount: children.length,
      separatorBuilder: (_, _) => const SizedBox(height: 16),
      itemBuilder: (_, i) => children[i],
    );
    return onRefresh == null
        ? list
        : RefreshIndicator(onRefresh: onRefresh!, child: list);
  }
}

/// The 30 px page heading with its actions, like .page-title.
class PageTitle extends StatelessWidget {
  const PageTitle(this.title, {super.key, this.actions = const [], this.sub});
  final String title;
  final String? sub;
  final List<Widget> actions;

  @override
  Widget build(BuildContext context) => Wrap(
    spacing: 12,
    runSpacing: 12,
    crossAxisAlignment: WrapCrossAlignment.center,
    alignment: WrapAlignment.spaceBetween,
    children: [
      Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        mainAxisSize: MainAxisSize.min,
        children: [
          Text(title, style: Theme.of(context).textTheme.headlineMedium),
          if (sub != null)
            Text(sub!, style: Theme.of(context).textTheme.bodySmall),
        ],
      ),
      if (actions.isNotEmpty)
        Wrap(spacing: 8, runSpacing: 8, children: actions),
    ],
  );
}

/// .card: 28 px corners, surface-container, 20 x 22 padding.
class WCard extends StatelessWidget {
  const WCard({
    super.key,
    required this.child,
    this.padding = const EdgeInsets.fromLTRB(22, 20, 22, 20),
    this.color,
    this.onTap,
    this.radius = 28,
  });
  final Widget child;
  final EdgeInsets padding;
  final Color? color;
  final VoidCallback? onTap;
  final double radius;

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    final shape = RoundedRectangleBorder(
      borderRadius: BorderRadius.circular(radius),
    );
    return Material(
      color: color ?? cs.surfaceContainer,
      shape: shape,
      clipBehavior: Clip.antiAlias,
      child: onTap == null
          ? Padding(padding: padding, child: child)
          : InkWell(
              onTap: onTap,
              customBorder: shape,
              child: Padding(padding: padding, child: child),
            ),
    );
  }
}

/// .card-head: an optional shape, the 20 px title, then chips and buttons.
class CardHead extends StatelessWidget {
  const CardHead(
    this.title, {
    super.key,
    this.leading,
    this.trailing = const [],
    this.sub,
  });
  final String title;
  final String? sub;
  final Widget? leading;
  final List<Widget> trailing;

  @override
  Widget build(BuildContext context) => Padding(
    padding: const EdgeInsets.only(bottom: 12),
    child: Row(
      children: [
        if (leading != null) ...[leading!, const SizedBox(width: 12)],
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(title, style: Theme.of(context).textTheme.titleMedium),
              if (sub != null)
                Text(sub!, style: Theme.of(context).textTheme.bodySmall),
            ],
          ),
        ),
        for (final w in trailing) ...[const SizedBox(width: 8), w],
      ],
    ),
  );
}

/// .row: separated by a 1 px surface-container-high line, except the first.
class WRow extends StatelessWidget {
  const WRow({
    super.key,
    required this.children,
    this.first = false,
    this.onTap,
    this.padding = const EdgeInsets.symmetric(horizontal: 6, vertical: 12),
    this.align = CrossAxisAlignment.center,
  });
  final List<Widget> children;
  final bool first;
  final VoidCallback? onTap;
  final EdgeInsets padding;
  final CrossAxisAlignment align;

  @override
  Widget build(BuildContext context) {
    final row = Container(
      padding: padding,
      decoration: BoxDecoration(
        border: first
            ? null
            : Border(
                top: BorderSide(
                  color: Theme.of(context).colorScheme.surfaceContainerHigh,
                ),
              ),
      ),
      child: Row(crossAxisAlignment: align, children: _gap(children, 14)),
    );
    return onTap == null ? row : InkWell(onTap: onTap, child: row);
  }
}

List<Widget> _gap(List<Widget> c, double g) => [
  for (var i = 0; i < c.length; i++) ...[if (i > 0) SizedBox(width: g), c[i]],
];

/// Muted text (on-surface-variant), 13 px by default like .muted.small.
class Muted extends StatelessWidget {
  const Muted(
    this.text, {
    super.key,
    this.size = 13,
    this.maxLines,
    this.mono = false,
  });
  final String text;
  final double size;
  final int? maxLines;
  final bool mono;

  @override
  Widget build(BuildContext context) => Text(
    text,
    maxLines: maxLines,
    overflow: maxLines == null ? null : TextOverflow.ellipsis,
    style: TextStyle(
      fontFamily: mono ? kMono : kFont,
      fontSize: size,
      color: Theme.of(context).colorScheme.onSurfaceVariant,
    ),
  );
}

// ---------------------------------------------------------------------------------
// Small parts

/// .chip and .chip-outline.
class WChip extends StatelessWidget {
  const WChip(
    this.label, {
    super.key,
    this.outline = false,
    this.icon,
    this.color,
    this.textColor,
  });
  final String label;
  final bool outline;
  final String? icon;
  final Color? color, textColor;

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    final fg =
        textColor ?? (outline ? cs.onSurfaceVariant : cs.onSecondaryContainer);
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
      decoration: BoxDecoration(
        color: outline ? null : (color ?? cs.secondaryContainer),
        borderRadius: BorderRadius.circular(8),
        border: outline ? Border.all(color: cs.outlineVariant) : null,
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          if (icon != null) ...[
            WIcon(icon!, size: 14, color: fg),
            const SizedBox(width: 6),
          ],
          Text(
            label,
            style: TextStyle(
              fontFamily: kFont,
              fontSize: 12,
              fontWeight: outline ? FontWeight.w500 : FontWeight.w600,
              color: fg,
            ),
          ),
        ],
      ),
    );
  }
}

/// The round check mark of the security lists: ✓, ! or ?.
class Mark extends StatelessWidget {
  const Mark(this.status, {super.key, this.size = 22});
  final String status; // ok | warn | fail | unknown
  final double size;

  @override
  Widget build(BuildContext context) {
    final t = Tones.of(context);
    final cs = Theme.of(context).colorScheme;
    final (bg, fg) = switch (status) {
      'ok' => (t.ok, t.onOk),
      'warn' => (t.warn, t.onWarn),
      'fail' => (t.bad, t.onBad),
      _ => (cs.surfaceContainerHighest, cs.onSurfaceVariant),
    };
    return Container(
      width: size,
      height: size,
      alignment: Alignment.center,
      decoration: BoxDecoration(color: bg, shape: BoxShape.circle),
      child: Text(
        status == 'ok'
            ? '✓'
            : status == 'unknown'
            ? '?'
            : '!',
        style: TextStyle(
          fontSize: size * .55,
          fontWeight: FontWeight.w700,
          color: fg,
        ),
      ),
    );
  }
}

/// .icon-btn with a panel icon.
class WIconButton extends StatelessWidget {
  const WIconButton(
    this.icon, {
    super.key,
    required this.tooltip,
    this.onPressed,
    this.size = 20,
    this.color,
  });
  final String icon, tooltip;
  final VoidCallback? onPressed;
  final double size;
  final Color? color;

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    final c = color ?? cs.onSurfaceVariant;
    return IconButton(
      tooltip: tooltip,
      onPressed: onPressed,
      // Icon-only buttons read as buttons: the outline keeps them visible on a card.
      style: IconButton.styleFrom(side: BorderSide(color: cs.outlineVariant)),
      icon: WIcon(
        icon,
        size: size,
        color: onPressed == null ? c.withValues(alpha: .38) : c,
      ),
    );
  }
}

/// A button with a panel icon before its label: filled, tonal, outlined or text.
enum BtnKind { filled, tonal, outlined, text, danger }

class WButton extends StatelessWidget {
  const WButton(
    this.label, {
    super.key,
    this.icon,
    this.onPressed,
    this.kind = BtnKind.filled,
    this.large = false,
    this.busy = false,
  });
  final String label;
  final String? icon;
  final VoidCallback? onPressed;
  final BtnKind kind;
  final bool large;

  /// Keeps the label and puts Pelote's small loader in front while the action runs.
  final bool busy;

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    final fg = switch (kind) {
      BtnKind.filled => cs.onPrimary,
      BtnKind.tonal => cs.onSecondaryContainer,
      BtnKind.danger => cs.onError,
      _ => cs.primary,
    };
    final child = Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        if (busy) ...[
          SizedBox.square(
            dimension: 18,
            child: PeloteLoader(size: 18, color: fg, label: label),
          ),
          const SizedBox(width: 8),
        ] else if (icon != null) ...[
          WIcon(
            icon!,
            size: 18,
            color: onPressed == null ? cs.onSurface.withValues(alpha: .38) : fg,
          ),
          const SizedBox(width: 8),
        ],
        Text(label),
      ],
    );
    final style = large
        ? ButtonStyle(
            minimumSize: WidgetStateProperty.all(const Size(0, 48)),
            padding: WidgetStateProperty.all(
              const EdgeInsets.symmetric(horizontal: 28),
            ),
            textStyle: WidgetStateProperty.all(
              const TextStyle(
                fontFamily: kFont,
                fontSize: 15,
                fontWeight: FontWeight.w600,
              ),
            ),
          )
        : null;
    return switch (kind) {
      BtnKind.filled => FilledButton(
        onPressed: onPressed,
        style: style,
        child: child,
      ),
      BtnKind.tonal => FilledButton.tonal(
        onPressed: onPressed,
        style: style,
        child: child,
      ),
      BtnKind.danger => FilledButton(
        onPressed: onPressed,
        style: (style ?? const ButtonStyle()).copyWith(
          backgroundColor: WidgetStateProperty.all(cs.error),
          foregroundColor: WidgetStateProperty.all(cs.onError),
        ),
        child: child,
      ),
      BtnKind.outlined => OutlinedButton(
        onPressed: onPressed,
        style: style,
        child: child,
      ),
      BtnKind.text => TextButton(
        onPressed: onPressed,
        style: style,
        child: child,
      ),
    };
  }
}

/// .field.search: the filled 56 px pill.
class SearchField extends StatelessWidget {
  const SearchField({
    super.key,
    required this.hint,
    required this.onChanged,
    this.controller,
  });
  final String hint;
  final ValueChanged<String> onChanged;
  final TextEditingController? controller;

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    final pill = OutlineInputBorder(
      borderRadius: BorderRadius.circular(28),
      borderSide: BorderSide.none,
    );
    return SizedBox(
      height: 56,
      child: TextField(
        controller: controller,
        onChanged: onChanged,
        decoration: InputDecoration(
          hintText: hint,
          filled: true,
          fillColor: cs.surfaceContainerHighest,
          prefixIcon: Padding(
            padding: const EdgeInsets.only(left: 20, right: 10),
            child: WIcon('search', size: 20, color: cs.onSurfaceVariant),
          ),
          prefixIconConstraints: const BoxConstraints(),
          border: pill,
          enabledBorder: pill,
          focusedBorder: OutlineInputBorder(
            borderRadius: BorderRadius.circular(28),
            borderSide: BorderSide(color: cs.primary, width: 2),
          ),
          contentPadding: const EdgeInsets.symmetric(vertical: 18),
        ),
      ),
    );
  }
}

/// .filter: the services filter chips with a check when selected.
class FilterChipRow<T> extends StatelessWidget {
  const FilterChipRow({
    super.key,
    required this.options,
    required this.value,
    required this.onChanged,
  });
  final List<(T, String)> options;
  final T value;
  final ValueChanged<T> onChanged;

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    return Wrap(
      spacing: 8,
      runSpacing: 8,
      children: [
        for (final (v, label) in options)
          Material(
            color: v == value ? cs.secondaryContainer : Colors.transparent,
            shape: RoundedRectangleBorder(
              borderRadius: BorderRadius.circular(10),
              side: BorderSide(
                color: v == value ? cs.secondaryContainer : cs.outlineVariant,
              ),
            ),
            child: InkWell(
              borderRadius: BorderRadius.circular(10),
              onTap: () => onChanged(v),
              child: Container(
                height: 36,
                padding: const EdgeInsets.symmetric(horizontal: 16),
                child: Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    if (v == value) ...[
                      WIcon('check', size: 18, color: cs.onSecondaryContainer),
                      const SizedBox(width: 6),
                    ],
                    Text(
                      label,
                      style: TextStyle(
                        fontFamily: kFont,
                        fontSize: 14,
                        fontWeight: FontWeight.w500,
                        color: v == value
                            ? cs.onSecondaryContainer
                            : cs.onSurfaceVariant,
                      ),
                    ),
                  ],
                ),
              ),
            ),
          ),
      ],
    );
  }
}

/// .seg: the outlined segmented control (theme, time range).
class Seg<T> extends StatelessWidget {
  const Seg({
    super.key,
    required this.options,
    required this.value,
    required this.onChanged,
  });
  final List<(T, String)> options;
  final T value;
  final ValueChanged<T> onChanged;

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    return Container(
      height: 40,
      decoration: BoxDecoration(
        borderRadius: BorderRadius.circular(20),
        border: Border.all(color: cs.outline),
      ),
      clipBehavior: Clip.antiAlias,
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          for (var i = 0; i < options.length; i++)
            Container(
              decoration: BoxDecoration(
                color: options[i].$1 == value ? cs.secondaryContainer : null,
                border: i == 0
                    ? null
                    : Border(left: BorderSide(color: cs.outline)),
              ),
              child: InkWell(
                onTap: () => onChanged(options[i].$1),
                child: Padding(
                  padding: const EdgeInsets.symmetric(horizontal: 16),
                  child: Center(
                    child: Text(
                      options[i].$2,
                      style: TextStyle(
                        fontFamily: kFont,
                        fontSize: 14,
                        fontWeight: FontWeight.w500,
                        color: options[i].$1 == value
                            ? cs.onSecondaryContainer
                            : cs.onSurface,
                      ),
                    ),
                  ),
                ),
              ),
            ),
        ],
      ),
    );
  }
}

/// A tonal tile inside a card (.tile): shape, title, muted line, action.
class Tile extends StatelessWidget {
  const Tile({
    super.key,
    required this.shape,
    required this.tint,
    required this.title,
    this.sub,
    this.trailing,
  });
  final M3Shape shape;
  final Color tint;
  final String title;
  final String? sub;
  final Widget? trailing;

  @override
  Widget build(BuildContext context) => Container(
    padding: const EdgeInsets.all(14),
    decoration: BoxDecoration(
      color: Theme.of(context).colorScheme.surfaceContainerHigh,
      borderRadius: BorderRadius.circular(20),
    ),
    child: Row(
      children: [
        ShapeIcon(shape, size: 40, color: tint),
        const SizedBox(width: 14),
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(title, style: Theme.of(context).textTheme.titleSmall),
              if (sub != null) Muted(sub!),
            ],
          ),
        ),
        if (trailing != null) ...[const SizedBox(width: 8), trailing!],
      ],
    ),
  );
}

/// The large status banner of the overview (error container).
class AlertBanner extends StatelessWidget {
  const AlertBanner({
    super.key,
    required this.text,
    this.action,
    this.onAction,
  });
  final String text;
  final String? action;
  final VoidCallback? onAction;

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 16),
      decoration: BoxDecoration(
        color: cs.errorContainer,
        borderRadius: BorderRadius.circular(20),
      ),
      child: Wrap(
        spacing: 16,
        runSpacing: 12,
        crossAxisAlignment: WrapCrossAlignment.center,
        children: [
          Row(
            mainAxisSize: MainAxisSize.min,
            children: [
              WIcon('alert', size: 24, color: cs.onErrorContainer),
              const SizedBox(width: 16),
              ConstrainedBox(
                constraints: BoxConstraints(
                  maxWidth: MediaQuery.sizeOf(context).width - 120,
                ),
                child: Text(text, style: TextStyle(color: cs.onErrorContainer)),
              ),
            ],
          ),
          if (action != null)
            FilledButton(
              onPressed: onAction,
              style: FilledButton.styleFrom(
                backgroundColor: cs.error,
                foregroundColor: cs.onError,
              ),
              child: Text(action!),
            ),
        ],
      ),
    );
  }
}

// ---------------------------------------------------------------------------------
// Dialogs and sheets

/// M3Dialog: title, close button, content and actions on surface-container-high.
Future<T?> showWDialog<T>(
  BuildContext context, {
  required String title,
  required Widget Function(BuildContext) body,
  List<Widget> Function(BuildContext)? actions,
  List<Widget> header = const [],
  bool wide = false,
}) => showDialog<T>(
  context: context,
  barrierColor: Colors.black54,
  builder: (c) {
    final w = MediaQuery.sizeOf(c).width;
    return Dialog(
      insetPadding: const EdgeInsets.all(16),
      child: ConstrainedBox(
        constraints: BoxConstraints(
          maxWidth: wide ? math.min(1100, w - 32) : math.min(480, w - 32),
        ),
        child: Padding(
          padding: const EdgeInsets.fromLTRB(24, 20, 24, 24),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Row(
                children: [
                  Expanded(
                    child: Text(title, style: Theme.of(c).textTheme.titleLarge),
                  ),
                  ...header,
                  WIconButton(
                    'close',
                    tooltip: 'Close',
                    onPressed: () => Navigator.pop(c),
                    size: 22,
                  ),
                ],
              ),
              const SizedBox(height: 16),
              Flexible(child: body(c)),
              if (actions != null) ...[
                const SizedBox(height: 16),
                Wrap(
                  alignment: WrapAlignment.end,
                  spacing: 8,
                  runSpacing: 8,
                  children: actions(c),
                ),
              ],
            ],
          ),
        ),
      ),
    );
  },
);

/// The panel's confirm dialog (ConfirmDialog.vue).
Future<bool> wConfirm(
  BuildContext context, {
  required String title,
  required String body,
  required String action,
  bool danger = false,
}) async =>
    await showWDialog<bool>(
      context,
      title: title,
      body: (_) => Muted(body, size: 15),
      actions: (c) => [
        WButton(
          'Cancel',
          kind: BtnKind.text,
          onPressed: () => Navigator.pop(c, false),
        ),
        WButton(
          action,
          kind: danger ? BtnKind.danger : BtnKind.filled,
          onPressed: () => Navigator.pop(c, true),
        ),
      ],
    ) ??
    false;

// ---------------------------------------------------------------------------------
// Metrics

/// MetricCard.vue: a spinning decorative shape, the label, a 48 px value, the wavy
/// progress and a muted line. compact is the widget bar size.
class WMetricCard extends StatefulWidget {
  const WMetricCard({
    super.key,
    required this.label,
    required this.value,
    this.percent,
    required this.sub,
    required this.shape,
    required this.tint,
    this.compact = false,
    this.onTap,
  });
  final String label, value, sub;
  final double? percent;
  final M3Shape shape;
  final Color tint;
  final bool compact;
  final VoidCallback? onTap;

  @override
  State<WMetricCard> createState() => _WMetricCardState();
}

class _WMetricCardState extends State<WMetricCard>
    with SingleTickerProviderStateMixin {
  late final _spin = AnimationController(
    vsync: this,
    duration: const Duration(seconds: 40),
  )..repeat();

  @override
  void dispose() {
    _spin.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    final c = widget.compact;
    final deco = c ? 96.0 : 120.0;
    return WCard(
      radius: c ? 24 : 28,
      padding: c
          ? const EdgeInsets.fromLTRB(18, 16, 18, 16)
          : const EdgeInsets.fromLTRB(22, 20, 22, 20),
      onTap: widget.onTap,
      child: Stack(
        clipBehavior: Clip.none,
        children: [
          Positioned(
            right: c ? -44 : -52,
            top: c ? -46 : -54,
            child: Opacity(
              opacity: .55,
              child: RotationTransition(
                turns: _spin,
                child: ShapeIcon(widget.shape, size: deco, color: widget.tint),
              ),
            ),
          ),
          Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                widget.label,
                style: TextStyle(
                  fontFamily: kFont,
                  fontSize: c ? 14 : 15,
                  fontWeight: FontWeight.w500,
                  color: cs.onSurfaceVariant,
                ),
              ),
              SizedBox(height: c ? 8 : 10),
              Text(
                widget.value,
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                style: TextStyle(
                  fontFamily: kFont,
                  fontSize: c ? 34 : 48,
                  height: 1,
                  color: cs.onSurface,
                ),
              ),
              if (widget.percent != null) ...[
                SizedBox(height: c ? 8 : 10),
                WaveProgress(widget.percent! / 100),
              ],
              SizedBox(height: c ? 8 : 10),
              Text(
                widget.sub,
                maxLines: 2,
                overflow: TextOverflow.ellipsis,
                style: TextStyle(
                  fontFamily: kFont,
                  fontSize: c ? 12 : 13,
                  color: cs.onSurfaceVariant,
                ),
              ),
            ],
          ),
        ],
      ),
    );
  }
}

/// The badge of a container group: its chosen icon in a tonal square, or a shape.
class GroupBadge extends StatelessWidget {
  const GroupBadge({
    super.key,
    required this.icon,
    required this.index,
    this.size = 36,
  });
  final String? icon;
  final int index;
  final double size;

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    final tint = index.isOdd ? cs.secondaryContainer : cs.primaryContainer;
    final ink = index.isOdd ? cs.onSecondaryContainer : cs.onPrimaryContainer;
    if (icon == null || icon!.isEmpty) {
      return ShapeIcon(
        M3Shape.values[index % M3Shape.values.length],
        size: size,
        color: tint,
      );
    }
    return Container(
      width: size,
      height: size,
      alignment: Alignment.center,
      decoration: BoxDecoration(
        color: tint,
        borderRadius: BorderRadius.circular(size * .3),
      ),
      child: WIcon(icon!, size: size * .55, color: ink),
    );
  }
}

/// Icons offered for container groups, as in the panel's picker.
const groupIconChoices = [
  'database',
  'game',
  'music',
  'film',
  'chat',
  'cloud',
  'mail',
  'cart',
  'code',
  'robot',
  'chart',
  'camera',
  'book',
  'home',
  'bolt',
  'heart',
  'star',
  'leaf',
  'paw',
  'coffee',
  'image',
  'video',
  'bell',
  'cpu',
  'wrench',
  'users',
  'calendar',
  'map',
  'server',
  'shop',
  'globe',
  'folder',
  'terminal',
  'lock',
  'key',
  'phone',
  'apps',
  'containers',
  'security',
  'github',
];

/// A pushed page fades in and rises a little, like the panel's page transition.
class _FadeRiseTransitions extends PageTransitionsBuilder {
  const _FadeRiseTransitions();

  @override
  Widget buildTransitions<T>(
    PageRoute<T> route,
    BuildContext context,
    Animation<double> animation,
    Animation<double> secondaryAnimation,
    Widget child,
  ) {
    if (MediaQuery.disableAnimationsOf(context)) return child;
    final curved = CurvedAnimation(
      parent: animation,
      curve: Curves.easeOutCubic,
      reverseCurve: Curves.easeIn,
    );
    return FadeTransition(
      opacity: curved,
      child: SlideTransition(
        position: Tween<Offset>(
          begin: const Offset(0, 0.02),
          end: Offset.zero,
        ).animate(curved),
        child: child,
      ),
    );
  }
}
