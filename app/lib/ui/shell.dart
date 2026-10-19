import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../data/servers.dart';
import 'add_server_screen.dart';
import 'kit.dart';
import 'live.dart';
import 'nav.dart';
import 'pages/apps.dart';
import 'pages/assistant.dart';
import 'pages/containers.dart';
import 'pages/overview.dart';
import 'pages/scripts.dart';
import 'pages/security.dart';
import 'pages/services.dart';
import 'pages/settings.dart';
import 'pages/updates.dart';
import 'scope.dart';
import 'widget_bar.dart';
import 'widgets.dart';

/// AppShell.vue: the panel's navigation. Phones get the bottom bar (four sections and
/// More), wide screens the rail with every section; very wide ones the widget bar.
class Shell extends StatefulWidget {
  const Shell({super.key});

  @override
  State<Shell> createState() => _ShellState();
}

const _pages = [
  ('overview', 'Overview', 'overview', true),
  ('containers', 'Containers', 'containers', true),
  ('services', 'Services', 'services', false),
  ('security', 'Security', 'security', false),
  ('apps', 'Apps', 'apps', true),
  ('assistant', 'Assistant', 'assistant', true),
  ('scripts', 'Scripts', 'terminal', false),
  ('updates', 'Updates', 'updates', false),
  ('settings', 'Settings', 'settings', false),
];

class _ShellState extends State<Shell> with WidgetsBindingObserver {
  String page = 'overview';
  LiveSession? live;
  String? liveFor;
  bool barOpen = true;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    WidgetsBinding.instance.addPostFrameCallback(
      (_) => AppScope.updaterOf(context).check(AppScope.of(context).api),
    );
    SharedPreferences.getInstance()
        .then((p) {
          if (mounted) {
            setState(() => barOpen = p.getBool('widgets.open') ?? true);
          }
        })
        .catchError((_) {});
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    live?.dispose();
    super.dispose();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState s) {
    if (s == AppLifecycleState.resumed) AppScope.updaterOf(context).resume();
  }

  // One live session per server; switching servers starts a fresh one.
  LiveSession _session(AppState state) {
    if (liveFor != state.currentId || live == null) {
      live?.dispose();
      live = LiveSession(state.api!);
      liveFor = state.currentId;
    }
    return live!;
  }

  void _go(String p) => setState(() => page = p);

  void _setBar(bool v) {
    setState(() => barOpen = v);
    SharedPreferences.getInstance()
        .then((p) => p.setBool('widgets.open', v))
        .catchError((_) => false);
  }

  void _switcher(AppState state) => showModalBottomSheet(
    context: context,
    builder: (c) => SafeArea(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(12, 0, 12, 12),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            for (final s in state.servers)
              _SheetItem(
                icon: 'globe',
                label: s.name,
                sub: Uri.parse(s.url).host,
                active: s.id == state.currentId,
                onTap: () {
                  state.select(s);
                  Navigator.pop(c);
                },
              ),
            _SheetItem(
              icon: 'plus',
              label: 'Add a server',
              onTap: () {
                Navigator.pop(c);
                Navigator.push(
                  context,
                  MaterialPageRoute(builder: (_) => const AddServerScreen()),
                );
              },
            ),
          ],
        ),
      ),
    ),
  );

  void _more() => showModalBottomSheet(
    context: context,
    builder: (c) => SafeArea(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(12, 0, 12, 12),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            for (final p in _pages.where((p) => !p.$4))
              _SheetItem(
                icon: p.$3,
                label: p.$2,
                active: page == p.$1,
                onTap: () {
                  Navigator.pop(c);
                  _go(p.$1);
                },
              ),
          ],
        ),
      ),
    ),
  );

  Widget _page() => switch (page) {
    'containers' => const ContainersPage(),
    'services' => const ServicesPage(),
    'security' => const SecurityPage(),
    'apps' => const AppsPage(),
    'assistant' => const AssistantPage(),
    'scripts' => const ScriptsPage(),
    'updates' => const UpdatesPage(),
    'settings' => const SettingsPage(),
    _ => const OverviewPage(),
  };

  @override
  Widget build(BuildContext context) {
    final state = AppScope.of(context);
    final server = state.current!;
    final session = _session(state);
    final cs = Theme.of(context).colorScheme;
    final w = MediaQuery.sizeOf(context).width;
    final wide = w >= 840;
    final bar = w >= 1500 && barOpen;
    final mobile = _pages.where((p) => p.$4).toList();

    return Live(
      session: session,
      child: Nav(
        onGo: _go,
        bar: bar,
        child: ListenableBuilder(
          listenable: session,
          builder: (context, _) {
            final topbar = Padding(
              padding: EdgeInsets.fromLTRB(16, 12, 16, wide ? 12 : 4),
              child: Row(
                children: [
                  if (wide) ...[
                    const Text(
                      'VPS ADMIN',
                      style: TextStyle(
                        fontSize: 30,
                        fontWeight: FontWeight.w500,
                        letterSpacing: 4.2,
                      ),
                    ),
                    const SizedBox(width: 20),
                  ],
                  Flexible(
                    child: Material(
                      color: Colors.transparent,
                      shape: StadiumBorder(
                        side: BorderSide(color: cs.outlineVariant),
                      ),
                      child: InkWell(
                        customBorder: const StadiumBorder(),
                        onTap: () => _switcher(state),
                        child: Container(
                          height: 40,
                          padding: const EdgeInsets.fromLTRB(14, 0, 12, 0),
                          child: Row(
                            mainAxisSize: MainAxisSize.min,
                            children: [
                              Container(
                                width: 8,
                                height: 8,
                                decoration: BoxDecoration(
                                  shape: BoxShape.circle,
                                  color: session.error == null
                                      ? Tones.of(context).ok
                                      : cs.error,
                                ),
                              ),
                              const SizedBox(width: 8),
                              Flexible(
                                child: Text(
                                  session.host?.hostname ?? server.name,
                                  overflow: TextOverflow.ellipsis,
                                  style: const TextStyle(fontSize: 14),
                                ),
                              ),
                              const SizedBox(width: 6),
                              WIcon(
                                'chevron',
                                size: 18,
                                color: cs.onSurfaceVariant,
                              ),
                            ],
                          ),
                        ),
                      ),
                    ),
                  ),
                  if (wide &&
                      session.host != null &&
                      session.snapshot != null) ...[
                    const SizedBox(width: 12),
                    WChip(
                      '${session.host!.os} · up ${duration(session.snapshot!.uptime)}',
                    ),
                  ],
                  if (w >= 1500 && !barOpen) ...[
                    const Spacer(),
                    WButton(
                      'Widgets',
                      icon: 'chart',
                      kind: BtnKind.tonal,
                      onPressed: () => _setBar(true),
                    ),
                  ],
                ],
              ),
            );
            final content = Column(
              children: [
                topbar,
                Expanded(
                  child: Align(
                    alignment: Alignment.topCenter,
                    child: ConstrainedBox(
                      constraints: const BoxConstraints(maxWidth: 1340),
                      child: KeyedSubtree(
                        key: ValueKey('${state.currentId}/$page'),
                        child: _page(),
                      ),
                    ),
                  ),
                ),
              ],
            );
            final body = SafeArea(
              bottom: false,
              child: wide
                  ? Row(
                      crossAxisAlignment: CrossAxisAlignment.stretch,
                      children: [
                        _Rail(page: page, onGo: _go),
                        VerticalDivider(
                          width: 1,
                          color: cs.surfaceContainerHigh,
                        ),
                        Expanded(child: content),
                        if (bar) ...[
                          VerticalDivider(
                            width: 1,
                            color: cs.surfaceContainerHigh,
                          ),
                          SizedBox(
                            width: 300,
                            child: WidgetBar(onClose: () => _setBar(false)),
                          ),
                        ],
                      ],
                    )
                  : content,
            );
            final mobileIndex = mobile.indexWhere((p) => p.$1 == page);
            final scaffold = Scaffold(
              backgroundColor: session.background == null
                  ? cs.surface
                  : Colors.transparent,
              body: body,
              bottomNavigationBar: wide
                  ? null
                  : NavigationBar(
                      selectedIndex: mobileIndex < 0
                          ? mobile.length
                          : mobileIndex,
                      onDestinationSelected: (i) =>
                          i == mobile.length ? _more() : _go(mobile[i].$1),
                      destinations: [
                        for (final p in mobile)
                          NavigationDestination(
                            icon: WIcon(p.$3, color: cs.onSurfaceVariant),
                            selectedIcon: WIcon(
                              p.$3,
                              color: cs.onSecondaryContainer,
                            ),
                            label: p.$2,
                          ),
                        NavigationDestination(
                          icon: WIcon('more', color: cs.onSurfaceVariant),
                          selectedIcon: WIcon(
                            'more',
                            color: cs.onSecondaryContainer,
                          ),
                          label: 'More',
                        ),
                      ],
                    ),
            );
            if (session.background == null) return scaffold;
            return ColoredBox(
              color: cs.surface,
              child: Stack(
                fit: StackFit.expand,
                children: [
                  Opacity(
                    opacity: session.opacity,
                    child: Image.memory(
                      session.background!,
                      fit: BoxFit.cover,
                      gaplessPlayback: true,
                    ),
                  ),
                  scaffold,
                ],
              ),
            );
          },
        ),
      ),
    );
  }
}

/// The panel's rail: the logo, then every section as a pill and a label.
class _Rail extends StatelessWidget {
  const _Rail({required this.page, required this.onGo});
  final String page;
  final ValueChanged<String> onGo;

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    return SizedBox(
      width: 96,
      child: SingleChildScrollView(
        padding: const EdgeInsets.symmetric(vertical: 20),
        child: Column(
          children: [
            ShapeIcon(
              M3Shape.cookie,
              size: 48,
              color: cs.primaryContainer,
              child: WIcon('services', size: 22, color: cs.onPrimaryContainer),
            ),
            const SizedBox(height: 18),
            for (final p in _pages)
              InkWell(
                onTap: () => onGo(p.$1),
                borderRadius: BorderRadius.circular(16),
                child: Padding(
                  padding: const EdgeInsets.symmetric(vertical: 4),
                  child: Column(
                    children: [
                      AnimatedContainer(
                        duration: const Duration(milliseconds: 200),
                        width: 56,
                        height: 32,
                        decoration: BoxDecoration(
                          color: page == p.$1
                              ? cs.secondaryContainer
                              : Colors.transparent,
                          borderRadius: BorderRadius.circular(16),
                        ),
                        child: Center(
                          child: WIcon(
                            p.$3,
                            color: page == p.$1
                                ? cs.onSecondaryContainer
                                : cs.onSurfaceVariant,
                          ),
                        ),
                      ),
                      const SizedBox(height: 4),
                      Text(
                        p.$2,
                        style: TextStyle(
                          fontSize: 12,
                          fontWeight: FontWeight.w500,
                          color: page == p.$1
                              ? cs.onSurface
                              : cs.onSurfaceVariant,
                        ),
                      ),
                      const SizedBox(height: 6),
                    ],
                  ),
                ),
              ),
          ],
        ),
      ),
    );
  }
}

class _SheetItem extends StatelessWidget {
  const _SheetItem({
    required this.icon,
    required this.label,
    this.sub,
    this.active = false,
    required this.onTap,
  });
  final String icon, label;
  final String? sub;
  final bool active;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    final fg = active ? cs.onSecondaryContainer : cs.onSurface;
    return Material(
      color: active ? cs.secondaryContainer : Colors.transparent,
      borderRadius: BorderRadius.circular(28),
      child: InkWell(
        borderRadius: BorderRadius.circular(28),
        onTap: onTap,
        child: Container(
          constraints: const BoxConstraints(minHeight: 56),
          padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
          child: Row(
            children: [
              WIcon(icon, size: 22, color: fg),
              const SizedBox(width: 16),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    Text(
                      label,
                      style: TextStyle(
                        fontSize: 16,
                        fontWeight: FontWeight.w500,
                        color: fg,
                      ),
                    ),
                    if (sub != null)
                      Text(
                        sub!,
                        style: TextStyle(
                          fontSize: 13,
                          color: active ? fg : cs.onSurfaceVariant,
                        ),
                      ),
                  ],
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

/// Hidden widgets of the bar, remembered on this phone.
Future<Set<String>> readHiddenWidgets() async {
  try {
    final p = await SharedPreferences.getInstance();
    return (jsonDecode(p.getString('widgets.hidden') ?? '[]') as List)
        .cast<String>()
        .toSet();
  } catch (_) {
    return {};
  }
}

Future<void> saveHiddenWidgets(Set<String> h) async {
  try {
    final p = await SharedPreferences.getInstance();
    await p.setString('widgets.hidden', jsonEncode(h.toList()));
  } catch (_) {}
}
