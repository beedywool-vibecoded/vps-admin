import 'package:flutter/widgets.dart';

import '../data/servers.dart';
import '../data/updater.dart';

class AppScope extends InheritedNotifier<AppState> {
  const AppScope({super.key, required AppState state, required this.updater, required super.child}) : super(notifier: state);
  final Updater updater;

  static AppState of(BuildContext context) => context.dependOnInheritedWidgetOfExactType<AppScope>()!.notifier!;
  static Updater updaterOf(BuildContext context) => context.dependOnInheritedWidgetOfExactType<AppScope>()!.updater;
}
