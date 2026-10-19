import 'package:dynamic_color/dynamic_color.dart';
import 'package:flutter/material.dart';

import 'data/servers.dart';
import 'data/updater.dart';
import 'ui/add_server_screen.dart';
import 'ui/scope.dart';
import 'ui/shell.dart';

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();
  // Never stay on the splash screen: if stored settings cannot be read, start empty
  // and say so, instead of failing before the first frame.
  AppState state;
  Object? startError;
  try {
    state = await AppState.load();
  } catch (e) {
    startError = e;
    state = await AppState.empty();
  }
  final updater = Updater();
  try {
    await updater.init();
  } catch (_) {}
  runApp(
    AppScope(
      state: state,
      updater: updater,
      child: VpsAdminApp(startError: startError),
    ),
  );
}

class VpsAdminApp extends StatelessWidget {
  const VpsAdminApp({super.key, this.startError});
  final Object? startError;

  ThemeData _theme(ColorScheme scheme) => ThemeData(
    colorScheme: scheme,
    cardTheme: CardThemeData(
      elevation: 0,
      color: scheme.surfaceContainer,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.all(Radius.circular(24)),
      ),
    ),
    navigationBarTheme: NavigationBarThemeData(
      labelTextStyle: WidgetStateProperty.resolveWith(
        (s) => TextStyle(
          fontSize: 12,
          fontWeight: s.contains(WidgetState.selected)
              ? FontWeight.w600
              : FontWeight.w500,
        ),
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
        final dyn =
            state.useDynamicColor &&
            lightDynamic != null &&
            darkDynamic != null;
        final light = dyn
            ? lightDynamic.harmonized()
            : ColorScheme.fromSeed(seedColor: state.seed.color);
        final dark = dyn
            ? darkDynamic.harmonized()
            : ColorScheme.fromSeed(
                seedColor: state.seed.color,
                brightness: Brightness.dark,
              );
        return MaterialApp(
          title: 'VPS Admin',
          debugShowCheckedModeBanner: false,
          theme: _theme(light),
          darkTheme: _theme(dark),
          themeMode: state.themeMode,
          home: startError != null
              ? _StartError(error: startError!)
              : state.current == null
              ? const AddServerScreen(first: true)
              : const Shell(),
        );
      },
    );
  }
}

class _StartError extends StatelessWidget {
  const _StartError({required this.error});
  final Object error;

  @override
  Widget build(BuildContext context) => Scaffold(
    body: SafeArea(
      child: Padding(
        padding: const EdgeInsets.all(24),
        child: Column(
          mainAxisAlignment: MainAxisAlignment.center,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Icon(
              Icons.error_outline,
              size: 48,
              color: Theme.of(context).colorScheme.error,
            ),
            const SizedBox(height: 16),
            Text(
              'The saved servers could not be read',
              style: Theme.of(context).textTheme.titleLarge,
            ),
            const SizedBox(height: 8),
            SelectableText(
              '$error',
              style: const TextStyle(fontFamily: 'monospace', fontSize: 12),
            ),
            const SizedBox(height: 16),
            FilledButton(
              onPressed: () => Navigator.of(context).pushReplacement(
                MaterialPageRoute(
                  builder: (_) => const AddServerScreen(first: true),
                ),
              ),
              child: const Text('Add a server'),
            ),
          ],
        ),
      ),
    ),
  );
}
