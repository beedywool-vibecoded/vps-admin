import 'package:flutter/material.dart';

import '../data/servers.dart';
import 'add_server_screen.dart';
import 'scope.dart';
import 'widgets.dart';

class SettingsScreen extends StatelessWidget {
  const SettingsScreen({super.key});

  Future<void> _reboot(BuildContext context, AppState state) async {
    final server = state.current!;
    final typed = TextEditingController();
    final ok = await showDialog<bool>(
      context: context,
      builder: (c) => StatefulBuilder(
        builder: (c, set) => AlertDialog(
          title: const Text('Restart the server?'),
          content: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              const Text(
                'Every site, container and service stops for about a minute.',
              ),
              const SizedBox(height: 12),
              TextField(
                controller: typed,
                onChanged: (_) => set(() {}),
                decoration: InputDecoration(
                  labelText: 'Type ${server.name} to confirm',
                  filled: true,
                ),
              ),
            ],
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(c, false),
              child: const Text('Cancel'),
            ),
            FilledButton(
              style: FilledButton.styleFrom(
                backgroundColor: Theme.of(c).colorScheme.error,
                foregroundColor: Theme.of(c).colorScheme.onError,
              ),
              onPressed: typed.text.trim() == server.name
                  ? () => Navigator.pop(c, true)
                  : null,
              child: const Text('Restart'),
            ),
          ],
        ),
      ),
    );
    if (ok != true || !context.mounted) return;
    try {
      await state.api!.reboot(typed.text.trim());
      if (context.mounted) {
        showInfo(context, 'Rebooting: the server is back in about a minute');
      }
    } catch (e) {
      if (context.mounted) showError(context, e);
    }
  }

  @override
  Widget build(BuildContext context) {
    final state = AppScope.of(context);
    final updater = AppScope.updaterOf(context);
    final t = Theme.of(context);
    final cs = t.colorScheme;
    Widget header(String s) => Padding(
      padding: const EdgeInsets.fromLTRB(20, 16, 20, 4),
      child: Text(
        s,
        style: t.textTheme.titleSmall?.copyWith(color: cs.primary),
      ),
    );

    return ListView(
      padding: const EdgeInsets.only(bottom: 24),
      children: [
        header('Servers'),
        for (final s in state.servers)
          ListTile(
            leading: ShapeIcon(
              M3Shape.cookie,
              size: 40,
              color: s.id == state.currentId
                  ? cs.primaryContainer
                  : cs.surfaceContainerHighest,
            ),
            title: Text(s.name),
            subtitle: Text(Uri.parse(s.url).host),
            selected: s.id == state.currentId,
            onTap: () => state.select(s),
            trailing: IconButton(
              tooltip: 'Remove',
              icon: const Icon(Icons.delete_outline),
              onPressed: () async {
                if (await confirmAction(
                  context,
                  title: 'Remove ${s.name}?',
                  body:
                      'The phone forgets this server. Revoke it in the panel (Settings, Devices) to cut its access too.',
                  action: 'Remove',
                  danger: true,
                )) {
                  state.remove(s);
                }
              },
            ),
          ),
        ListTile(
          leading: const Icon(Icons.add),
          title: const Text('Add a server'),
          onTap: () => Navigator.push(
            context,
            MaterialPageRoute(builder: (_) => const AddServerScreen()),
          ),
        ),
        header('This server'),
        ListTile(
          leading: Icon(Icons.restart_alt, color: cs.error),
          title: const Text('Restart the server'),
          subtitle: const Text('Everything stops for about a minute'),
          onTap: () => _reboot(context, state),
        ),
        header('Appearance'),
        SwitchListTile(
          title: const Text('Dynamic color'),
          subtitle: const Text('Material You: colors from your wallpaper'),
          value: state.useDynamicColor,
          onChanged: (v) => state.setTheme(dynamic: v),
        ),
        Padding(
          padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
          child: SegmentedButton<ThemeMode>(
            segments: const [
              ButtonSegment(value: ThemeMode.system, label: Text('System')),
              ButtonSegment(value: ThemeMode.dark, label: Text('Dark')),
              ButtonSegment(value: ThemeMode.light, label: Text('Light')),
            ],
            selected: {state.themeMode},
            onSelectionChanged: (s) => state.setTheme(mode: s.first),
          ),
        ),
        if (!state.useDynamicColor)
          Padding(
            padding: const EdgeInsets.fromLTRB(16, 8, 16, 8),
            child: Wrap(
              spacing: 12,
              children: [
                for (final s in Seed.values)
                  Semantics(
                    label: s.label,
                    selected: state.seed == s,
                    button: true,
                    child: GestureDetector(
                      onTap: () => state.setTheme(seed: s),
                      child: Container(
                        width: 44,
                        height: 44,
                        decoration: BoxDecoration(
                          color: s.color,
                          shape: BoxShape.circle,
                          border: state.seed == s
                              ? Border.all(color: cs.onSurface, width: 3)
                              : null,
                        ),
                      ),
                    ),
                  ),
              ],
            ),
          ),
        header('App'),
        ListenableBuilder(
          listenable: updater,
          builder: (c, _) => ListTile(
            leading: const Icon(Icons.system_update),
            title: Text('VPS Admin ${updater.currentLabel}'),
            subtitle: Text(
              updater.progress != null
                  ? 'Downloading ${(updater.progress! * 100).round()}%'
                  : updater.error ??
                        (updater.available != null
                            ? '${updater.available!.versionName} is available'
                            : 'Up to date (checked on the current server)'),
            ),
            trailing: updater.available != null && updater.progress == null
                ? FilledButton(
                    onPressed: () => updater.install(state.api!),
                    child: const Text('Update'),
                  )
                : IconButton(
                    tooltip: 'Check now',
                    icon: const Icon(Icons.refresh),
                    onPressed: () => updater.check(state.api),
                  ),
          ),
        ),
      ],
    );
  }
}
