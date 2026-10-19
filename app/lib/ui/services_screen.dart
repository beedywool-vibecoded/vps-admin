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

class _Containers extends StatelessWidget {
  @override
  Widget build(BuildContext context) {
    final api = AppScope.of(context).api!;
    return Loader<List<DockerContainer>>(
      load: api.containers,
      refreshEvery: const Duration(seconds: 15),
      builder: (context, list, reload) {
        final groups = <String, List<DockerContainer>>{};
        for (final c in list) {
          (groups[c.project ?? 'Standalone'] ??= []).add(c);
        }
        return ListView(
          padding: const EdgeInsets.fromLTRB(12, 12, 12, 16),
          children: [
            for (final g in groups.entries) ...[
              Padding(
                padding: const EdgeInsets.fromLTRB(8, 8, 8, 6),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(g.key, style: Theme.of(context).textTheme.titleMedium),
                    if (g.value.first.workDir != null)
                      Text(
                        g.value.first.workDir!,
                        style: Theme.of(context).textTheme.bodySmall?.copyWith(
                          fontFamily: 'monospace',
                          color: Theme.of(context).colorScheme.onSurfaceVariant,
                        ),
                      ),
                  ],
                ),
              ),
              Card(
                margin: const EdgeInsets.only(bottom: 8),
                child: Column(
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
              ),
            ],
          ],
        );
      },
    );
  }
}
