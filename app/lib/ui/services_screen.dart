import 'package:flutter/material.dart';

import '../data/models.dart';
import 'scope.dart';
import 'scripts_tab.dart';
import 'widgets.dart';

class ServicesScreen extends StatelessWidget {
  const ServicesScreen({super.key});

  @override
  Widget build(BuildContext context) => DefaultTabController(
    length: 3,
    child: Column(
      children: [
        const TabBar(
          tabs: [
            Tab(text: 'Containers'),
            Tab(text: 'Services'),
            Tab(text: 'Scripts'),
          ],
        ),
        Expanded(
          child: TabBarView(
            children: [_Containers(), _Systemd(), ScriptsTab()],
          ),
        ),
      ],
    ),
  );
}

Future<void> _actions(
  BuildContext context, {
  required String title,
  required String subtitle,
  required bool running,
  required Future<void> Function(String) act,
  required Future<String> Function() logs,
}) async {
  final picked = await showModalBottomSheet<String>(
    context: context,
    showDragHandle: true,
    builder: (c) => SafeArea(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(16, 0, 16, 16),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(title, style: Theme.of(c).textTheme.titleLarge),
            Text(subtitle, style: Theme.of(c).textTheme.bodySmall),
            const SizedBox(height: 16),
            Row(
              children: [
                if (running)
                  Expanded(
                    child: FilledButton.tonal(
                      onPressed: () => Navigator.pop(c, 'restart'),
                      style: FilledButton.styleFrom(
                        minimumSize: const Size.fromHeight(56),
                      ),
                      child: const Text('Restart'),
                    ),
                  ),
                if (running) const SizedBox(width: 8),
                if (running)
                  Expanded(
                    child: FilledButton.tonal(
                      onPressed: () => Navigator.pop(c, 'stop'),
                      style: FilledButton.styleFrom(
                        minimumSize: const Size.fromHeight(56),
                      ),
                      child: const Text('Stop'),
                    ),
                  ),
                if (!running)
                  Expanded(
                    child: FilledButton.tonal(
                      onPressed: () => Navigator.pop(c, 'start'),
                      style: FilledButton.styleFrom(
                        minimumSize: const Size.fromHeight(56),
                      ),
                      child: const Text('Start'),
                    ),
                  ),
                const SizedBox(width: 8),
                Expanded(
                  child: FilledButton(
                    onPressed: () => Navigator.pop(c, 'logs'),
                    style: FilledButton.styleFrom(
                      minimumSize: const Size.fromHeight(56),
                    ),
                    child: const Text('Live logs'),
                  ),
                ),
              ],
            ),
          ],
        ),
      ),
    ),
  );
  if (picked == null || !context.mounted) return;
  if (picked == 'logs') {
    Navigator.push(
      context,
      MaterialPageRoute(
        builder: (_) => LogsPage(title: '$title logs', load: logs),
      ),
    );
    return;
  }
  if (picked != 'start' &&
      !await confirmAction(
        context,
        title: '${picked == 'stop' ? 'Stop' : 'Restart'} $title?',
        body: picked == 'stop'
            ? 'It stays stopped until started again.'
            : 'It is unavailable for a moment.',
        action: picked == 'stop' ? 'Stop' : 'Restart',
        danger: picked == 'stop',
      )) {
    return;
  }
  try {
    await act(picked);
    if (context.mounted) showInfo(context, '$title: $picked done');
  } catch (e) {
    if (context.mounted) showError(context, e);
  }
}

class _Systemd extends StatefulWidget {
  @override
  State<_Systemd> createState() => _SystemdState();
}

class _SystemdState extends State<_Systemd> {
  String filter = 'running';
  final _key = GlobalKey();

  @override
  Widget build(BuildContext context) {
    final api = AppScope.of(context).api!;
    return Loader<List<Service>>(
      key: _key,
      load: api.services,
      builder: (context, all, reload) {
        bool match(Service s, String f) =>
            f == 'all' ||
            (f == 'running' && s.active == 'active') ||
            (f == 'failed' && s.active == 'failed');
        final shown = all.where((s) => match(s, filter)).toList();
        return ListView(
          padding: const EdgeInsets.only(bottom: 16),
          children: [
            Padding(
              padding: const EdgeInsets.all(12),
              child: SegmentedButton<String>(
                segments: [
                  for (final f in ['running', 'failed', 'all'])
                    ButtonSegment(
                      value: f,
                      label: Text(
                        '${f[0].toUpperCase()}${f.substring(1)} · ${all.where((s) => match(s, f)).length}',
                      ),
                    ),
                ],
                selected: {filter},
                onSelectionChanged: (s) => setState(() => filter = s.first),
              ),
            ),
            for (final s in shown)
              ListTile(
                leading: StatusDot(
                  s.active == 'failed'
                      ? Tone.bad
                      : s.active == 'active'
                      ? Tone.ok
                      : Tone.idle,
                  label: s.active,
                ),
                title: Text(s.name),
                subtitle: Text(
                  s.description,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                ),
                trailing: const Icon(Icons.more_vert),
                onTap: () => _actions(
                  context,
                  title: s.name,
                  subtitle: '${s.active} · ${s.sub}',
                  running: s.active == 'active',
                  act: (a) async {
                    await api.serviceAction(s.name, a);
                    await reload();
                  },
                  logs: () => api.serviceLogs(s.name),
                ),
              ),
          ],
        );
      },
    );
  }
}

/// Icons chosen for container groups in the panel, by name.
const _groupIcons = <String, IconData>{
  'database': Icons.storage,
  'game': Icons.sports_esports_outlined,
  'music': Icons.music_note_outlined,
  'film': Icons.movie_outlined,
  'chat': Icons.chat_bubble_outline,
  'cloud': Icons.cloud_outlined,
  'mail': Icons.mail_outline,
  'cart': Icons.shopping_cart_outlined,
  'code': Icons.code,
  'robot': Icons.smart_toy_outlined,
  'chart': Icons.bar_chart,
  'camera': Icons.photo_camera_outlined,
  'book': Icons.menu_book_outlined,
  'home': Icons.home_outlined,
  'bolt': Icons.bolt,
  'heart': Icons.favorite_border,
  'star': Icons.star_border,
  'leaf': Icons.eco_outlined,
  'paw': Icons.pets,
  'coffee': Icons.coffee_outlined,
  'image': Icons.image_outlined,
  'video': Icons.videocam_outlined,
  'bell': Icons.notifications_none,
  'cpu': Icons.memory,
  'wrench': Icons.build_outlined,
  'users': Icons.group_outlined,
  'calendar': Icons.calendar_today_outlined,
  'map': Icons.map_outlined,
  'server': Icons.dns_outlined,
  'shop': Icons.storefront_outlined,
  'globe': Icons.public,
  'folder': Icons.folder_outlined,
  'terminal': Icons.terminal,
  'lock': Icons.lock_outline,
  'key': Icons.key,
  'phone': Icons.smartphone,
  'apps': Icons.rocket_launch_outlined,
  'containers': Icons.inventory_2_outlined,
  'security': Icons.shield_outlined,
  'github': Icons.hub_outlined,
};

class _Containers extends StatelessWidget {
  @override
  Widget build(BuildContext context) {
    final api = AppScope.of(context).api!;
    return Loader<(List<DockerContainer>, Map<String, dynamic>)>(
      load: () async {
        final list = await api.containers();
        var icons = <String, dynamic>{};
        try {
          icons =
              (await api.appearance())['groupIcons'] as Map<String, dynamic>? ??
              {};
        } catch (_) {}
        return (list, icons);
      },
      refreshEvery: const Duration(seconds: 15),
      builder: (context, data, reload) {
        final (list, icons) = data;
        final cs = Theme.of(context).colorScheme;
        final groups = <String, List<DockerContainer>>{};
        for (final c in list) {
          (groups[c.project ?? 'Standalone'] ??= []).add(c);
        }
        final shapes = M3Shape.values;
        var i = 0;
        return ListView(
          padding: const EdgeInsets.fromLTRB(12, 12, 12, 16),
          children: [
            for (final g in groups.entries)
              Builder(
                builder: (context) {
                  final n = i++;
                  final tint = n.isEven
                      ? cs.primaryContainer
                      : cs.secondaryContainer;
                  final ink = n.isEven
                      ? cs.onPrimaryContainer
                      : cs.onSecondaryContainer;
                  final icon = _groupIcons[icons[g.key]];
                  return Card(
                    margin: const EdgeInsets.only(bottom: 8),
                    clipBehavior: Clip.antiAlias,
                    child: ExpansionTile(
                      // Collapsed by default; the state survives refreshes.
                      key: PageStorageKey('group:${g.key}'),
                      shape: const Border(),
                      collapsedShape: const Border(),
                      leading: icon != null
                          ? Container(
                              width: 36,
                              height: 36,
                              decoration: BoxDecoration(
                                color: tint,
                                borderRadius: BorderRadius.circular(11),
                              ),
                              child: Icon(icon, size: 20, color: ink),
                            )
                          : ShapeIcon(
                              shapes[n % shapes.length],
                              size: 36,
                              color: tint,
                            ),
                      title: Text(g.key),
                      subtitle: g.value.first.workDir == null
                          ? null
                          : Text(
                              g.value.first.workDir!,
                              maxLines: 1,
                              overflow: TextOverflow.ellipsis,
                              style: Theme.of(context).textTheme.bodySmall
                                  ?.copyWith(
                                    fontFamily: 'monospace',
                                    color: cs.onSurfaceVariant,
                                  ),
                            ),
                      trailing: Row(
                        mainAxisSize: MainAxisSize.min,
                        children: [
                          for (final c in g.value.take(6))
                            Padding(
                              padding: const EdgeInsets.only(left: 3),
                              child: StatusDot(
                                c.bad
                                    ? Tone.bad
                                    : c.state == 'running'
                                    ? Tone.ok
                                    : Tone.idle,
                                label: c.state,
                              ),
                            ),
                          const SizedBox(width: 8),
                          Text('${g.value.length}'),
                        ],
                      ),
                      children: [
                        for (final c in g.value)
                          ListTile(
                            leading: StatusDot(
                              c.bad
                                  ? Tone.bad
                                  : c.state == 'running'
                                  ? Tone.ok
                                  : Tone.idle,
                              label: c.health ?? c.state,
                            ),
                            title: Text(c.name),
                            subtitle: Text(
                              c.status,
                              maxLines: 1,
                              overflow: TextOverflow.ellipsis,
                            ),
                            trailing: c.exposed
                                ? const Tooltip(
                                    message: 'Published on every interface',
                                    child: Icon(Icons.public, size: 18),
                                  )
                                : null,
                            onTap: () => _actions(
                              context,
                              title: c.name,
                              subtitle: c.image,
                              running: c.state == 'running',
                              act: (a) async {
                                await api.containerAction(c.id, a);
                                await reload();
                              },
                              logs: () => api.containerLogs(c.id),
                            ),
                          ),
                      ],
                    ),
                  );
                },
              ),
          ],
        );
      },
    );
  }
}
