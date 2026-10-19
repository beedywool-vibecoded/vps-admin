import 'package:flutter/material.dart';

import '../data/api.dart';
import '../data/models.dart';
import 'scope.dart';
import 'widgets.dart';

Tone _tone(AppInfo a) {
  if (a.latest?.running ?? false) return Tone.warn;
  return switch (a.status) {
    'running' => Tone.ok,
    'degraded' || 'restarting' || 'missing' => Tone.bad,
    _ => Tone.idle,
  };
}

class AppsScreen extends StatelessWidget {
  const AppsScreen({super.key});

  @override
  Widget build(BuildContext context) {
    final api = AppScope.of(context).api!;
    return Loader<List<AppInfo>>(
      load: api.apps,
      refreshEvery: const Duration(seconds: 10),
      builder: (context, apps, _) {
        final cs = Theme.of(context).colorScheme;
        if (apps.isEmpty) {
          return ListView(
            padding: const EdgeInsets.all(32),
            children: [
              ShapeIcon(M3Shape.flower, size: 72, color: cs.primaryContainer),
              const SizedBox(height: 16),
              const Text(
                'No app yet. Connect GitHub and create apps from the web panel; they show up here.',
                textAlign: TextAlign.center,
              ),
            ],
          );
        }
        final shapes = M3Shape.values;
        return ListView.separated(
          padding: const EdgeInsets.all(16),
          itemCount: apps.length,
          separatorBuilder: (_, _) => const SizedBox(height: 8),
          itemBuilder: (c, i) {
            final a = apps[i];
            return Card(
              margin: EdgeInsets.zero,
              child: ListTile(
                contentPadding: const EdgeInsets.fromLTRB(12, 8, 12, 8),
                leading: ShapeIcon(
                  shapes[i % shapes.length],
                  size: 44,
                  color: i.isEven ? cs.primaryContainer : cs.secondaryContainer,
                ),
                title: Row(
                  children: [
                    StatusDot(_tone(a), label: a.status),
                    const SizedBox(width: 10),
                    Flexible(
                      child: Text(a.id, overflow: TextOverflow.ellipsis),
                    ),
                  ],
                ),
                subtitle: Text(
                  a.domain.isNotEmpty
                      ? a.domain
                      : a.dir.isNotEmpty
                      ? a.dir
                      : '${a.repo} · ${a.branch}',
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                ),
                trailing: const Icon(Icons.chevron_right),
                onTap: () => Navigator.push(
                  c,
                  MaterialPageRoute(builder: (_) => AppDetailScreen(id: a.id)),
                ),
              ),
            );
          },
        );
      },
    );
  }
}

class AppDetailScreen extends StatelessWidget {
  const AppDetailScreen({super.key, required this.id});
  final String id;

  static const _labels = {
    'queued': 'Queued',
    'building': 'Building',
    'starting': 'Starting',
    'live': 'Live',
    'failed': 'Failed',
    'superseded': 'Previous',
    'cancelled': 'Cancelled',
  };

  @override
  Widget build(BuildContext context) {
    final api = AppScope.of(context).api!;
    return Scaffold(
      appBar: AppBar(
        title: Text(id),
        actions: [
          IconButton(
            tooltip: 'Settings',
            icon: const Icon(Icons.tune),
            onPressed: () => Navigator.push(
              context,
              MaterialPageRoute(builder: (_) => AppSettingsScreen(id: id)),
            ),
          ),
        ],
      ),
      body: Loader(
        load: () => api.app(id),
        refreshEvery: const Duration(seconds: 4),
        builder: (context, d, reload) {
          final t = Theme.of(context);
          final a = d.app;
          final busy = d.deployments.any((x) => x.running);
          Future<void> run(Future<void> Function() f, String done) async {
            try {
              await f();
              await reload();
              if (context.mounted) showInfo(context, done);
            } catch (e) {
              if (context.mounted) showError(context, e);
            }
          }

          return ListView(
            padding: const EdgeInsets.all(16),
            children: [
              Row(
                children: [
                  StatusDot(_tone(a), label: a.status),
                  const SizedBox(width: 10),
                  Text(
                    busy ? 'Deploying' : a.status,
                    style: t.textTheme.titleMedium,
                  ),
                ],
              ),
              const SizedBox(height: 4),
              Text(
                '${a.repo} · ${a.branch}${a.domain.isNotEmpty ? ' · ${a.domain}' : ''}',
                style: t.textTheme.bodySmall,
              ),
              if (a.dir.isNotEmpty)
                Padding(
                  padding: const EdgeInsets.only(top: 4),
                  child: Row(
                    children: [
                      const Icon(Icons.folder_outlined, size: 16),
                      const SizedBox(width: 6),
                      Expanded(
                        child: Text(
                          'Updates ${a.dir} on push',
                          style: t.textTheme.bodySmall,
                        ),
                      ),
                    ],
                  ),
                ),
              const SizedBox(height: 16),
              Row(
                children: [
                  Expanded(
                    child: OutlinedButton(
                      onPressed: a.live == null
                          ? null
                          : () => run(() => api.restartApp(id), 'Restarted'),
                      child: const Text('Restart'),
                    ),
                  ),
                  const SizedBox(width: 8),
                  Expanded(
                    child: FilledButton(
                      onPressed: busy
                          ? null
                          : () =>
                                run(() => api.deploy(id), 'Deployment started'),
                      child: const Text('Redeploy'),
                    ),
                  ),
                ],
              ),
              const SizedBox(height: 8),
              TextButton.icon(
                icon: const Icon(Icons.sync),
                label: const Text('Check for updates'),
                onPressed: () async {
                  try {
                    final c = await api.checkApp(id);
                    if (!context.mounted) return;
                    if (c.upToDate) {
                      showInfo(context, 'Up to date with ${a.branch}');
                      return;
                    }
                    final short = c.latestSha.length > 7
                        ? c.latestSha.substring(0, 7)
                        : c.latestSha;
                    if (await confirmAction(
                      context,
                      title: 'New commit on ${a.branch}',
                      body: '$short ${c.latestMessage}',
                      action: 'Update now',
                    )) {
                      run(() => api.deploy(id), 'Update started');
                    }
                  } catch (e) {
                    if (context.mounted) showError(context, e);
                  }
                },
              ),
              const SizedBox(height: 8),
              Text('Deployments', style: t.textTheme.titleMedium),
              const SizedBox(height: 8),
              Card(
                margin: EdgeInsets.zero,
                child: Column(
                  children: [
                    for (final dep in d.deployments)
                      ListTile(
                        leading: StatusDot(switch (dep.status) {
                          'live' => Tone.ok,
                          'failed' => Tone.bad,
                          'superseded' || 'cancelled' => Tone.idle,
                          _ => Tone.warn,
                        }, label: dep.status),
                        title: Text(
                          dep.message.isEmpty ? dep.trigger : dep.message,
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                        ),
                        subtitle: Text(
                          '${_labels[dep.status] ?? dep.status} · ${dep.short} · ${ago(dep.createdAt)}',
                        ),
                        trailing: dep.status == 'superseded'
                            ? TextButton(
                                onPressed: busy
                                    ? null
                                    : () async {
                                        if (await confirmAction(
                                          context,
                                          title: 'Roll back to ${dep.short}?',
                                          body:
                                              '"${dep.message}" goes live again.',
                                          action: 'Roll back',
                                        )) {
                                          run(
                                            () => api.rollback(id, dep.id),
                                            'Rollback started',
                                          );
                                        }
                                      },
                                child: const Text('Roll back'),
                              )
                            : null,
                        onTap: () => Navigator.push(
                          context,
                          MaterialPageRoute(
                            builder: (_) => LogsPage(
                              title: 'Build ${dep.short}',
                              load: () => api.deploymentLog(id, dep.id),
                            ),
                          ),
                        ),
                      ),
                  ],
                ),
              ),
            ],
          );
        },
      ),
    );
  }
}

/// Branch, automatic deploys and custom build commands of an app, as in the panel.
class AppSettingsScreen extends StatefulWidget {
  const AppSettingsScreen({super.key, required this.id});
  final String id;

  @override
  State<AppSettingsScreen> createState() => _AppSettingsScreenState();
}

class _AppSettingsScreenState extends State<AppSettingsScreen> {
  static const _fields = {
    'branch': 'Branch',
    'installCmd': 'Install command',
    'buildCmd': 'Build command',
    'startCmd': 'Start command',
    'dockerfile': 'Dockerfile',
    'composeFile': 'Compose file',
  };
  Map<String, dynamic>? _app;
  final _ctl = <String, TextEditingController>{};
  bool _saving = false;
  String? _error;

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    if (_app == null && _error == null) _load();
  }

  Future<void> _load() async {
    try {
      final a = await AppScope.of(context).api!.appRaw(widget.id);
      setState(() {
        _app = a;
        for (final k in _fields.keys) {
          _ctl[k] = TextEditingController(text: a[k] as String? ?? '');
        }
      });
    } catch (e) {
      setState(() => _error = e is ApiException ? e.message : e.toString());
    }
  }

  @override
  void dispose() {
    for (final c in _ctl.values) {
      c.dispose();
    }
    super.dispose();
  }

  Future<void> _save() async {
    setState(() => _saving = true);
    try {
      final a = {
        ..._app!,
        for (final k in _fields.keys) k: _ctl[k]!.text.trim(),
      };
      await AppScope.of(context).api!.saveApp(widget.id, a);
      if (mounted) {
        showInfo(context, 'Saved, it applies at the next deployment');
        Navigator.pop(context);
      }
    } catch (e) {
      if (mounted) showError(context, e);
    } finally {
      if (mounted) setState(() => _saving = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final a = _app;
    final build = a?['build'] as String? ?? '';
    final linked = (a?['dir'] as String? ?? '').isNotEmpty;
    bool shown(String k) => switch (k) {
      'installCmd' ||
      'buildCmd' => build == 'node' || build == 'static' || build == 'auto',
      'startCmd' => build == 'node' || build == 'auto',
      'dockerfile' => build == 'dockerfile' || build == 'auto',
      'composeFile' => build == 'compose' || build == 'auto',
      _ => true,
    };
    return Scaffold(
      appBar: AppBar(title: Text('${widget.id} settings')),
      body: a == null
          ? Center(
              child: _error != null
                  ? Text(_error!)
                  : const CircularProgressIndicator(),
            )
          : ListView(
              padding: const EdgeInsets.all(16),
              children: [
                if (linked)
                  Padding(
                    padding: const EdgeInsets.only(bottom: 12),
                    child: CmdText('Updates the folder `${a['dir']}` on push.'),
                  ),
                for (final k in _fields.keys)
                  if (shown(k) &&
                      !(linked && k != 'branch' && k != 'composeFile'))
                    Padding(
                      padding: const EdgeInsets.only(bottom: 12),
                      child: TextField(
                        controller: _ctl[k],
                        autocorrect: false,
                        style: k == 'branch'
                            ? null
                            : const TextStyle(fontFamily: 'monospace'),
                        decoration: InputDecoration(
                          labelText: _fields[k],
                          helperText: k == 'startCmd'
                              ? 'Runs with sh -c, PORT=3000 is set. Empty: npm start'
                              : k == 'branch'
                              ? null
                              : 'Empty keeps the default',
                          border: const OutlineInputBorder(),
                        ),
                      ),
                    ),
                SwitchListTile(
                  contentPadding: EdgeInsets.zero,
                  title: Text(
                    linked ? 'Update on every push' : 'Deploy on every push',
                  ),
                  value: a['autoDeploy'] as bool? ?? false,
                  onChanged: (v) => setState(() => a['autoDeploy'] = v),
                ),
                const SizedBox(height: 12),
                FilledButton(
                  onPressed: _saving ? null : _save,
                  style: FilledButton.styleFrom(
                    minimumSize: const Size.fromHeight(48),
                  ),
                  child: Text(_saving ? 'Saving…' : 'Save'),
                ),
              ],
            ),
    );
  }
}
