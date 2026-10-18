import 'package:flutter/material.dart';

import '../data/models.dart';
import 'scope.dart';
import 'widgets.dart';

Tone _tone(AppInfo a) {
  if (a.latest?.running ?? false) return Tone.warn;
  return switch (a.status) { 'running' => Tone.ok, 'degraded' || 'restarting' || 'missing' => Tone.bad, _ => Tone.idle };
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
          return ListView(padding: const EdgeInsets.all(32), children: [
            ShapeIcon(M3Shape.flower, size: 72, color: cs.primaryContainer),
            const SizedBox(height: 16),
            const Text('No app yet. Connect GitHub and create apps from the web panel; they show up here.', textAlign: TextAlign.center),
          ]);
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
                leading: ShapeIcon(shapes[i % shapes.length], size: 44, color: i.isEven ? cs.primaryContainer : cs.secondaryContainer),
                title: Row(children: [StatusDot(_tone(a), label: a.status), const SizedBox(width: 10), Flexible(child: Text(a.id, overflow: TextOverflow.ellipsis))]),
                subtitle: Text(a.domain.isNotEmpty ? a.domain : '${a.repo} · ${a.branch}', maxLines: 1, overflow: TextOverflow.ellipsis),
                trailing: const Icon(Icons.chevron_right),
                onTap: () => Navigator.push(c, MaterialPageRoute(builder: (_) => AppDetailScreen(id: a.id))),
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

  static const _labels = {'queued': 'Queued', 'building': 'Building', 'starting': 'Starting', 'live': 'Live', 'failed': 'Failed', 'superseded': 'Previous', 'cancelled': 'Cancelled'};

  @override
  Widget build(BuildContext context) {
    final api = AppScope.of(context).api!;
    return Scaffold(
      appBar: AppBar(title: Text(id)),
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

          return ListView(padding: const EdgeInsets.all(16), children: [
            Row(children: [StatusDot(_tone(a), label: a.status), const SizedBox(width: 10), Text(busy ? 'Deploying' : a.status, style: t.textTheme.titleMedium)]),
            const SizedBox(height: 4),
            Text('${a.repo} · ${a.branch}${a.domain.isNotEmpty ? ' · ${a.domain}' : ''}', style: t.textTheme.bodySmall),
            const SizedBox(height: 16),
            Row(children: [
              Expanded(child: OutlinedButton(onPressed: a.live == null ? null : () => run(() => api.restartApp(id), 'Restarted'), child: const Text('Restart'))),
              const SizedBox(width: 8),
              Expanded(child: FilledButton(onPressed: busy ? null : () => run(() => api.deploy(id), 'Deployment started'), child: const Text('Redeploy'))),
            ]),
            const SizedBox(height: 16),
            Text('Deployments', style: t.textTheme.titleMedium),
            const SizedBox(height: 8),
            Card(
              margin: EdgeInsets.zero,
              child: Column(children: [
                for (final dep in d.deployments)
                  ListTile(
                    leading: StatusDot(switch (dep.status) { 'live' => Tone.ok, 'failed' => Tone.bad, 'superseded' || 'cancelled' => Tone.idle, _ => Tone.warn }, label: dep.status),
                    title: Text(dep.message.isEmpty ? dep.trigger : dep.message, maxLines: 1, overflow: TextOverflow.ellipsis),
                    subtitle: Text('${_labels[dep.status] ?? dep.status} · ${dep.short} · ${ago(dep.createdAt)}'),
                    trailing: dep.status == 'superseded'
                        ? TextButton(
                            onPressed: busy
                                ? null
                                : () async {
                                    if (await confirmAction(context, title: 'Roll back to ${dep.short}?', body: '"${dep.message}" goes live again.', action: 'Roll back')) {
                                      run(() => api.rollback(id, dep.id), 'Rollback started');
                                    }
                                  },
                            child: const Text('Roll back'))
                        : null,
                    onTap: () => Navigator.push(context, MaterialPageRoute(builder: (_) => LogsPage(title: 'Build ${dep.short}', load: () => api.deploymentLog(id, dep.id)))),
                  ),
              ]),
            ),
          ]);
        },
      ),
    );
  }
}
