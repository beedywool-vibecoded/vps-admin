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

class _ShellState extends State<Shell> with WidgetsBindingObserver {
  int _tab = 0;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    WidgetsBinding.instance.addPostFrameCallback((_) => AppScope.updaterOf(context).check(AppScope.of(context).api));
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
        child: Column(mainAxisSize: MainAxisSize.min, children: [
          for (final s in state.servers)
            ListTile(
              leading: ShapeIcon(M3Shape.cookie, size: 36, color: Theme.of(c).colorScheme.primaryContainer),
              title: Text(s.name),
              subtitle: Text(Uri.parse(s.url).host),
              trailing: s.id == state.currentId ? const Icon(Icons.check) : null,
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
              Navigator.push(context, MaterialPageRoute(builder: (_) => const AddServerScreen()));
            },
          ),
        ]),
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
      OverviewScreen(key: ValueKey('o${server.id}'), onOpenTab: (i) => setState(() => _tab = i)),
      AppsScreen(key: ValueKey('a${server.id}')),
      AssistantScreen(key: ValueKey('c${server.id}')),
      ServicesScreen(key: ValueKey('s${server.id}')),
      SettingsScreen(key: ValueKey('t${server.id}')),
    ];
    return Scaffold(
      appBar: AppBar(
        titleSpacing: 12,
        title: Material(
          color: cs.surfaceContainerHigh,
          shape: const StadiumBorder(),
          child: InkWell(
            customBorder: const StadiumBorder(),
            onTap: _switcher,
            child: Padding(
              padding: const EdgeInsets.fromLTRB(6, 6, 14, 6),
              child: Row(children: [
                ShapeIcon(M3Shape.cookie, size: 32, color: cs.primaryContainer),
                const SizedBox(width: 10),
                Expanded(
                  child: Column(crossAxisAlignment: CrossAxisAlignment.start, mainAxisSize: MainAxisSize.min, children: [
                    Text(server.name, style: const TextStyle(fontSize: 15, fontWeight: FontWeight.w500), overflow: TextOverflow.ellipsis),
                    Text(Uri.parse(server.url).host, style: TextStyle(fontSize: 12, color: cs.onSurfaceVariant), overflow: TextOverflow.ellipsis),
                  ]),
                ),
                const Icon(Icons.expand_more),
              ]),
            ),
          ),
        ),
      ),
      body: IndexedStack(index: _tab, children: pages),
      bottomNavigationBar: NavigationBar(
        selectedIndex: _tab,
        onDestinationSelected: (i) => setState(() => _tab = i),
        destinations: const [
          NavigationDestination(icon: Icon(Icons.dashboard_outlined), selectedIcon: Icon(Icons.dashboard), label: 'Overview'),
          NavigationDestination(icon: Icon(Icons.rocket_launch_outlined), selectedIcon: Icon(Icons.rocket_launch), label: 'Apps'),
          NavigationDestination(icon: Icon(Icons.auto_awesome_outlined), selectedIcon: Icon(Icons.auto_awesome), label: 'Assistant'),
          NavigationDestination(icon: Icon(Icons.view_list_outlined), selectedIcon: Icon(Icons.view_list), label: 'Services'),
          NavigationDestination(icon: Icon(Icons.settings_outlined), selectedIcon: Icon(Icons.settings), label: 'Settings'),
        ],
      ),
    );
  }
}
