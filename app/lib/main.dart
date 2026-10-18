import 'package:dynamic_color/dynamic_color.dart';
import 'package:flutter/material.dart';

import 'data/servers.dart';
import 'data/updater.dart';
import 'ui/add_server_screen.dart';
import 'ui/scope.dart';
import 'ui/shell.dart';

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();
  final state = await AppState.load();
  final updater = Updater();
  await updater.init();
  runApp(AppScope(state: state, updater: updater, child: const VpsAdminApp()));
}

class VpsAdminApp extends StatelessWidget {
  const VpsAdminApp({super.key});

  ThemeData _theme(ColorScheme scheme) => ThemeData(
        colorScheme: scheme,
        cardTheme: CardThemeData(
          elevation: 0,
          color: scheme.surfaceContainer,
          shape: const RoundedRectangleBorder(borderRadius: BorderRadius.all(Radius.circular(24))),
        ),
        navigationBarTheme: NavigationBarThemeData(
          labelTextStyle: WidgetStateProperty.resolveWith(
            (s) => TextStyle(fontSize: 12, fontWeight: s.contains(WidgetState.selected) ? FontWeight.w600 : FontWeight.w500),
          ),
        ),
        snackBarTheme: const SnackBarThemeData(behavior: SnackBarBehavior.floating),
      );

  @override
  Widget build(BuildContext context) {
    final state = AppScope.of(context);
    return DynamicColorBuilder(
      builder: (lightDynamic, darkDynamic) {
        // Material You: colors from the wallpaper on Android 12+, otherwise from the seed.
        final dyn = state.useDynamicColor && lightDynamic != null && darkDynamic != null;
        final light = dyn ? lightDynamic.harmonized() : ColorScheme.fromSeed(seedColor: state.seed.color);
        final dark = dyn ? darkDynamic.harmonized() : ColorScheme.fromSeed(seedColor: state.seed.color, brightness: Brightness.dark);
        return MaterialApp(
          title: 'VPS Admin',
          debugShowCheckedModeBanner: false,
          theme: _theme(light),
          darkTheme: _theme(dark),
          themeMode: state.themeMode,
          home: state.current == null ? const AddServerScreen(first: true) : const Shell(),
        );
      },
    );
  }
}
