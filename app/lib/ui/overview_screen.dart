import 'package:flutter/material.dart';

import '../data/models.dart';
import 'scope.dart';
import 'security_screen.dart';
import 'widgets.dart';

class _Data {
  _Data(
    this.host,
    this.snap,
    this.security,
    this.update,
    this.containers,
    this.failed,
  );
  final Host host;
  final Snapshot snap;
  final Security? security;
  final UpdateStatus? update;
  final List<DockerContainer>? containers;
  final List<Service> failed;
}

class OverviewScreen extends StatelessWidget {
  const OverviewScreen({super.key, required this.onOpenTab});
  final void Function(int tab) onOpenTab;

  @override
  Widget build(BuildContext context) {
    final api = AppScope.of(context).api!;
    return Loader<_Data>(
      refreshEvery: const Duration(seconds: 10),
      load: () async {
        final sys = await api.system();
        Future<T?> soft<T>(Future<T> f) =>
            f.then<T?>((v) => v).catchError((_) => null);
        final r = await Future.wait([
          soft(api.security()),
          soft(api.update()),
          soft(api.containers()),
          soft(api.services()),
        ]);
        final svcs = r[3] as List<Service>?;
        return _Data(
          sys.host,
          sys.snapshot,
          r[0] as Security?,
          r[1] as UpdateStatus?,
          r[2] as List<DockerContainer>?,
          svcs?.where((s) => s.active == 'failed').toList() ?? [],
        );
      },
      builder: (context, d, reload) {
        final t = Theme.of(context);
        final cs = t.colorScheme;
        final running =
            d.containers?.where((c) => c.state == 'running').length ?? 0;
        final bad = d.containers?.where((c) => c.bad).toList() ?? [];
        final toFix =
            d.security?.checks
                .where((c) => c.status == 'warn' || c.status == 'fail')
                .length ??
            0;
        final alert = d.failed.isNotEmpty
            ? '${d.failed.length} failed: ${d.failed.map((s) => s.name).join(', ')}'
            : bad.isNotEmpty
            ? 'Container trouble: ${bad.map((c) => c.name).join(', ')}'
            : null;
        final root = d.snap.root;
        return ListView(
          padding: const EdgeInsets.fromLTRB(16, 4, 16, 24),
          children: [
            Padding(
              padding: const EdgeInsets.fromLTRB(4, 4, 4, 12),
              child: Text(
                '${d.host.os} · up ${duration(d.snap.uptime)}',
                style: t.textTheme.bodyMedium?.copyWith(
                  color: cs.onSurfaceVariant,
                ),
              ),
            ),
            if (alert != null)
              Card(
                color: cs.errorContainer,
                margin: const EdgeInsets.only(bottom: 12),
                child: ListTile(
                  leading: Icon(
                    Icons.warning_amber_rounded,
                    color: cs.onErrorContainer,
                  ),
                  title: Text(
                    alert,
                    style: TextStyle(color: cs.onErrorContainer),
                  ),
                  onTap: () => onOpenTab(1),
                ),
              ),
            // Fixed card height; the column count follows the width (2 on a phone, 4 unfolded).
            GridView(
              shrinkWrap: true,
              physics: const NeverScrollableScrollPhysics(),
              gridDelegate: const SliverGridDelegateWithMaxCrossAxisExtent(
                maxCrossAxisExtent: 230,
                mainAxisExtent: 156,
                mainAxisSpacing: 10,
                crossAxisSpacing: 10,
              ),
              children: [
                MetricCard(
                  label: 'CPU · ${d.host.cores} cores',
                  value: '${d.snap.cpu.round()}%',
                  percent: d.snap.cpu,
                  sub: 'Load ${d.snap.load[0].toStringAsFixed(2)}',
                  shape: M3Shape.cookie,
                  tint: cs.primaryContainer,
                ),
                MetricCard(
                  label: 'Memory',
                  value: '${d.snap.memPercent.round()}%',
                  percent: d.snap.memPercent,
                  sub: '${bytes(d.snap.memUsed)} / ${bytes(d.snap.memTotal)}',
                  shape: M3Shape.clover,
                  tint: cs.secondaryContainer,
                ),
                MetricCard(
                  label: 'Disk ${root?.mount ?? ''}',
                  value: root == null ? 'n/a' : '${root.percent.round()}%',
                  percent: root?.percent ?? 0,
                  sub: root == null
                      ? ''
                      : '${bytes(root.used)} / ${bytes(root.total)}',
                  shape: M3Shape.sunny,
                  tint: cs.tertiaryContainer,
                ),
                MetricCard(
                  label: 'Containers',
                  value: d.containers == null ? 'n/a' : '$running',
                  percent: d.containers?.isEmpty ?? true
                      ? 0
                      : 100 * running / d.containers!.length,
                  sub: d.containers == null
                      ? 'Docker not reachable'
                      : '${d.containers!.length} in total',
                  shape: M3Shape.flower,
                  tint: cs.primaryContainer,
                ),
              ],
            ),
            const SizedBox(height: 10),
            Card(
              margin: EdgeInsets.zero,
              child: ListTile(
                contentPadding: const EdgeInsets.fromLTRB(12, 8, 16, 8),
                leading: ShapeIcon(
                  M3Shape.sunny,
                  size: 56,
                  color: cs.tertiaryContainer,
                  child: Text(
                    d.security == null
                        ? '…'
                        : '${(d.security!.score / 10).round()}/10',
                    style: TextStyle(
                      color: cs.onTertiaryContainer,
                      fontWeight: FontWeight.w600,
                    ),
                  ),
                ),
                title: const Text('Security'),
                subtitle: Text(
                  d.security == null
                      ? 'Checking…'
                      : toFix == 0
                      ? 'Nothing to fix'
                      : '$toFix points to fix',
                ),
                trailing: const Icon(Icons.chevron_right),
                onTap: () => Navigator.push(
                  context,
                  MaterialPageRoute(builder: (_) => const SecurityScreen()),
                ),
              ),
            ),
            const SizedBox(height: 10),
            if (d.update != null)
              Card(
                margin: EdgeInsets.zero,
                child: ListTile(
                  contentPadding: const EdgeInsets.fromLTRB(12, 8, 16, 8),
                  leading: ShapeIcon(
                    M3Shape.clover,
                    size: 44,
                    color: cs.secondaryContainer,
                  ),
                  title: Text('Agent ${d.update!.current}'),
                  subtitle: Text(
                    d.update!.available
                        ? '${d.update!.latest} is available'
                        : d.update!.error != null
                        ? 'Update check failed: ${d.update!.error}'
                        : 'Up to date · auto-update ${d.update!.auto ? 'on' : 'off'}',
                  ),
                  trailing: d.update!.available
                      ? FilledButton(
                          onPressed: () async {
                            final api = AppScope.of(context).api!;
                            if (!await confirmAction(
                              context,
                              title: 'Install ${d.update!.latest}?',
                              body:
                                  'The agent checks the signature, swaps its binary and restarts. It rolls back by itself if the new version fails.',
                              action: 'Install',
                            )) {
                              return;
                            }
                            try {
                              await api.applyUpdate();
                              if (context.mounted) {
                                showInfo(
                                  context,
                                  'Installing, the server reconnects in a few seconds',
                                );
                              }
                            } catch (e) {
                              if (context.mounted) showError(context, e);
                            }
                          },
                          child: const Text('Install'),
                        )
                      : null,
                ),
              ),
          ],
        );
      },
    );
  }
}
