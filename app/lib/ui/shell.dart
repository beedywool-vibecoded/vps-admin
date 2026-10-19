import 'dart:typed_data';

import 'package:flutter/material.dart';

import '../data/servers.dart';
import 'add_server_screen.dart';
import 'apps_screen.dart';
import 'assistant_screen.dart';
import 'overview_screen.dart';
import 'scope.dart';
import 'services_screen.dart';
import 'settings_screen.dart';
import 'widgets.dart';

class Shell extends StatefulWidget {
  const Shell({super.key});

  @override
  State<Shell> createState() => _ShellState();
}

const _dests = [
  (Icons.dashboard_outlined, Icons.dashboard, 'Overview'),
  (Icons.view_in_ar_outlined, Icons.view_in_ar, 'Server'),
  (Icons.rocket_launch_outlined, Icons.rocket_launch, 'Apps'),
  (Icons.auto_awesome_outlined, Icons.auto_awesome, 'Assistant'),
  (Icons.settings_outlined, Icons.settings, 'Settings'),
];

class _ShellState extends State<Shell> with WidgetsBindingObserver {
  int _tab = 0;
  // The server-wide background chosen in the panel, per server.
  Uint8List? _bg;
  double _bgOpacity = 0.35;
  String? _bgFor;

  Future<void> _loadBackground() async {
    final state = AppScope.of(context);
    final id = state.currentId;
    if (id == null || id == _bgFor) return;
    _bgFor = id;
    try {
      final a = await state.api!.appearance();
      final bytes = a['background'] == true
          ? await state.api!.background()
          : null;
      if (mounted && state.currentId == id) {
        setState(() {
          _bg = bytes;
          _bgOpacity = (a['opacity'] as num?)?.toDouble() ?? 0.35;
        });
      }
    } catch (_) {
      if (mounted) setState(() => _bg = null);
    }
  }

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    _loadBackground();
  }

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    WidgetsBinding.instance.addPostFrameCallback(
      (_) => AppScope.updaterOf(context).check(AppScope.of(context).api),
    );
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    super.dispose();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState s) {
    if (s == AppLifecycleState.resumed) AppScope.updaterOf(context).resume();
  }

  void _switcher() {
    final state = AppScope.of(context);
    showModalBottomSheet(
      context: context,
      showDragHandle: true,
      builder: (c) => SafeArea(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            for (final s in state.servers)
              ListTile(
                leading: ShapeIcon(
                  M3Shape.cookie,
                  size: 36,
                  color: Theme.of(c).colorScheme.primaryContainer,
                ),
                title: Text(s.name),
                subtitle: Text(Uri.parse(s.url).host),
                trailing: s.id == state.currentId
                    ? const Icon(Icons.check)
                    : null,
                onTap: () {
                  state.select(s);
                  Navigator.pop(c);
                },
              ),
            ListTile(
              leading: const Icon(Icons.add),
              title: const Text('Add a server'),
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
    );
  }

  @override
  Widget build(BuildContext context) {
    final state = AppScope.of(context);
    final Server server = state.current!;
    final cs = Theme.of(context).colorScheme;
    // Keyed by server: switching rebuilds every tab against the new one.
    final pages = [
      OverviewScreen(
        key: ValueKey('o${server.id}'),
        onOpenTab: (i) => setState(() => _tab = i),
      ),
      ServicesScreen(key: ValueKey('s${server.id}')),
      AppsScreen(key: ValueKey('a${server.id}')),
      AssistantScreen(key: ValueKey('c${server.id}')),
      SettingsScreen(key: ValueKey('t${server.id}')),
    ];
    // M3 adaptive layout: a rail beside the content on wide screens (foldables unfolded,
    // tablets), the bottom bar on phones. Content never stretches past 1000 dp.
    final wide = MediaQuery.sizeOf(context).width >= 840;
    final content = Align(
      alignment: Alignment.topCenter,
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 1000),
        child: IndexedStack(index: _tab, children: pages),
      ),
    );
    final scaffold = Scaffold(
      backgroundColor: _bg == null ? null : Colors.transparent,
      appBar: AppBar(
        backgroundColor: _bg == null ? null : Colors.transparent,
        titleSpacing: 12,
        title: Material(
          color: cs.surfaceContainerHigh,
          shape: const StadiumBorder(),
          child: InkWell(
            customBorder: const StadiumBorder(),
            onTap: _switcher,
            child: Padding(
              padding: const EdgeInsets.fromLTRB(6, 6, 14, 6),
              child: Row(
                children: [
                  ShapeIcon(
                    M3Shape.cookie,
                    size: 32,
                    color: cs.primaryContainer,
                  ),
                  const SizedBox(width: 10),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        Text(
                          server.name,
                          style: const TextStyle(
                            fontSize: 15,
                            fontWeight: FontWeight.w500,
                          ),
                          overflow: TextOverflow.ellipsis,
                        ),
                        Text(
                          Uri.parse(server.url).host,
                          style: TextStyle(
                            fontSize: 12,
                            color: cs.onSurfaceVariant,
                          ),
                          overflow: TextOverflow.ellipsis,
                        ),
                      ],
                    ),
                  ),
                  const Icon(Icons.expand_more),
                ],
              ),
            ),
          ),
        ),
      ),
      body: wide
          ? Row(
              children: [
                NavigationRail(
                  selectedIndex: _tab,
                  onDestinationSelected: (i) => setState(() => _tab = i),
                  labelType: NavigationRailLabelType.all,
                  groupAlignment: -0.9,
                  destinations: [
                    for (final d in _dests)
                      NavigationRailDestination(
                        icon: Icon(d.$1),
                        selectedIcon: Icon(d.$2),
                        label: Text(d.$3),
                      ),
                  ],
                ),
                Expanded(child: content),
              ],
            )
          : content,
      bottomNavigationBar: wide
          ? null
          : NavigationBar(
              selectedIndex: _tab,
              onDestinationSelected: (i) => setState(() => _tab = i),
              destinations: [
                for (final d in _dests)
                  NavigationDestination(
                    icon: Icon(d.$1),
                    selectedIcon: Icon(d.$2),
                    label: d.$3,
                  ),
              ],
            ),
    );
    if (_bg == null) return scaffold;
    return ColoredBox(
      color: cs.surface,
      child: Stack(
        fit: StackFit.expand,
        children: [
          Opacity(
            opacity: _bgOpacity,
            child: Image.memory(_bg!, fit: BoxFit.cover, gaplessPlayback: true),
          ),
          scaffold,
        ],
      ),
    );
  }
}
