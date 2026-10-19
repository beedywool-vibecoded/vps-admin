import 'package:flutter/widgets.dart';

/// Lets pages switch the main section (like RouterLink in the panel) and know whether
/// the widget bar is showing the resource cards.
class Nav extends InheritedWidget {
  const Nav({
    super.key,
    required this.onGo,
    required this.bar,
    required super.child,
  });
  final void Function(String page) onGo;
  final bool bar;

  static Nav _of(BuildContext c) =>
      c.dependOnInheritedWidgetOfExactType<Nav>()!;
  static void go(BuildContext c, String page) =>
      c.getInheritedWidgetOfExactType<Nav>()!.onGo(page);
  static bool barShown(BuildContext c) => _of(c).bar;

  @override
  bool updateShouldNotify(Nav old) => old.bar != bar;
}
