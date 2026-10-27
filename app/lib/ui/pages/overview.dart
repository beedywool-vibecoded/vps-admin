import 'dart:async';

import 'package:flutter/material.dart';

import '../../data/models.dart';
import '../kit.dart';
import '../live.dart';
import '../nav.dart';
import '../usage_chart.dart';
import '../widgets.dart';
import 'common.dart';

/// OverviewView.vue
class OverviewPage extends StatefulWidget {
  const OverviewPage({super.key});

  @override
  State<OverviewPage> createState() => _OverviewPageState();
}

class _OverviewPageState extends State<OverviewPage> {
  String span = '1h';
  List<Map<String, dynamic>> points = [];
  Map<String, dynamic>? security, update;
  List<DockerContainer>? containers;
  List<Service> services = [];
  String dockerError = '';
  Timer? _timer;
  String _lastSample = '';

  LiveSession get live => Live.read(context);

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      _history();
      _load();
      Live.read(context).addListener(_onSnapshot);
    });
    _timer = Timer.periodic(const Duration(seconds: 30), (_) => _load());
  }

  @override
  void dispose() {
    _timer?.cancel();
    try {
      live.removeListener(_onSnapshot);
    } catch (_) {}
    super.dispose();
  }

  // New samples arrive with every snapshot; append them to the 1 h view.
  void _onSnapshot() {
    final s = live.snapshot;
    if (s == null || span != '1h' || s.time == _lastSample || !mounted) return;
    _lastSample = s.time;
    final t = DateTime.tryParse(s.time);
    if (t == null) return;
    final ts = t.millisecondsSinceEpoch ~/ 1000;
    if (points.isNotEmpty && ts <= (points.last['t'] as num)) return;
    setState(() {
      points = [
        ...points.length > 719 ? points.sublist(points.length - 719) : points,
        {
          't': ts,
          'cpu': s.cpu,
          'mem': s.memTotal > 0 ? 100 * s.memUsed / s.memTotal : 0,
          'load': s.load.isNotEmpty ? s.load[0] : 0,
        },
      ];
    });
  }

  Future<void> _history() async {
    try {
      final p = await live.api.history(span);
      if (mounted) setState(() => points = p);
    } catch (e) {
      if (mounted) showError(context, e);
    }
  }

  Future<void> _load() async {
    final api = live.api;
    await Future.wait([
      api.securityRaw().then<void>((v) => security = v, onError: (_) {}),
      api.updateRaw().then<void>((v) => update = v, onError: (_) {}),
      api.services().then<void>((v) => services = v, onError: (_) {}),
      api
          .containers()
          .then((v) {
            containers = v;
            dockerError = '';
          })
          .catchError((Object e) {
            dockerError = e.toString();
          }),
    ]);
    if (mounted) setState(() {});
  }

  @override
  Widget build(BuildContext context) {
    final l = Live.of(context);
    final s = l.snapshot;
    final cs = Theme.of(context).colorScheme;
    final memPct = s != null && s.memTotal > 0
        ? 100 * s.memUsed / s.memTotal
        : 0.0;
    final disks = s?.disks ?? [];
    final root =
        disks.where((d) => d.mount == '/').firstOrNull ?? disks.firstOrNull;
    final diskPct = root == null || root.total == 0
        ? 0.0
        : 100 * root.used / root.total;
    final running = containers?.where((c) => c.state == 'running').length ?? 0;
    final healthy = containers?.where((c) => c.health == 'healthy').length ?? 0;
    final failed = services.where((x) => x.active == 'failed').toList();
    final bad = containers?.where((c) => c.bad).toList() ?? [];
    final checks = ((security?['checks'] as List?) ?? [])
        .cast<Map<String, dynamic>>();
    final fail = checks.where((c) => c['status'] == 'fail').firstOrNull;
    final toFix = checks
        .where((c) => c['status'] == 'warn' || c['status'] == 'fail')
        .length;
    const order = {'fail': 0, 'warn': 1, 'ok': 2, 'unknown': 3};
    final top = [...checks]
      ..sort((a, b) => (order[a['status']] ?? 3) - (order[b['status']] ?? 3));
    final packages = checks.where((c) => c['id'] == 'updates').firstOrNull;
    final rebootNeeded = checks.any(
      (c) => c['id'] == 'reboot' && c['status'] == 'warn',
    );

    (String, String)? alert;
    if (failed.isNotEmpty) {
      alert = (
        '${failed.length} service${failed.length > 1 ? 's' : ''} failed: ${failed.map((x) => x.name).join(', ')}',
        'services',
      );
    } else if (bad.isNotEmpty) {
      alert = (
        'Container trouble: ${bad.map((c) => '${c.name} (${c.health ?? c.state})').join(', ')}',
        'containers',
      );
    } else if (fail != null) {
      alert = ('${fail['title']}: ${fail['detail']}', 'security');
    }

    final groups = groupContainers(containers ?? []);

    return WPage(
      onRefresh: () async {
        await Future.wait([_load(), _history()]);
      },
      children: [
        if (alert != null)
          AlertBanner(
            text: alert.$1,
            action: 'View',
            onAction: () => Nav.go(context, alert!.$2),
          ),
        if (!Nav.barShown(context))
          ResponsiveGrid(
            minWidth: 230,
            children: [
              WMetricCard(
                label: 'CPU · ${l.host?.cores ?? '?'} cores',
                value: s == null ? '…' : pct(s.cpu),
                percent: s?.cpu ?? 0,
                sub: s == null
                    ? ''
                    : 'Load ${s.load.map((x) => x.toStringAsFixed(2)).join(' · ')}',
                shape: M3Shape.cookie,
                tint: cs.primaryContainer,
              ),
              WMetricCard(
                label: 'Memory',
                value: s == null ? '…' : pct(memPct),
                percent: memPct,
                sub: s == null
                    ? ''
                    : '${bytes(s.memUsed)} of ${bytes(s.memTotal)}',
                shape: M3Shape.clover,
                tint: cs.secondaryContainer,
              ),
              WMetricCard(
                label: 'Disk ${root?.mount ?? ''}',
                value: root == null ? '…' : pct(diskPct),
                percent: diskPct,
                sub: root == null
                    ? ''
                    : '${bytes(root.used)} of ${bytes(root.total)}',
                shape: M3Shape.sunny,
                tint: cs.tertiaryContainer,
              ),
              WMetricCard(
                label: 'Containers',
                value: containers != null
                    ? '$running'
                    : dockerError.isNotEmpty
                    ? 'n/a'
                    : '…',
                percent: (containers?.isNotEmpty ?? false)
                    ? 100 * running / containers!.length
                    : 0,
                sub: dockerError.isNotEmpty
                    ? 'Docker not reachable'
                    : '$running running · $healthy healthy',
                shape: M3Shape.flower,
                tint: cs.primaryContainer,
                onTap: () => Nav.go(context, 'containers'),
              ),
            ],
          ),
        WCard(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Wrap(
                spacing: 12,
                runSpacing: 10,
                crossAxisAlignment: WrapCrossAlignment.center,
                children: [
                  Text('Usage', style: Theme.of(context).textTheme.titleMedium),
                  if (s != null)
                    WChip(
                      'Network ↓ ${rate(s.netRx)} · ↑ ${rate(s.netTx)}',
                      outline: true,
                    ),
                  Seg<String>(
                    options: const [('1h', 'Last hour'), ('24h', 'Last day')],
                    value: span,
                    onChanged: (v) {
                      setState(() => span = v);
                      _history();
                    },
                  ),
                ],
              ),
              const SizedBox(height: 12),
              UsageChart(points: points, span: span),
            ],
          ),
        ),
        ResponsiveGrid(
          minWidth: 420,
          children: [
            WCard(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  Row(
                    children: [
                      SizedBox(
                        width: 84,
                        height: 84,
                        child: Stack(
                          alignment: Alignment.center,
                          children: [
                            ShapeIcon(
                              M3Shape.sunny,
                              size: 84,
                              color: cs.tertiaryContainer,
                            ),
                            Text(
                              security == null
                                  ? '…'
                                  : '${((security!['score'] as num) / 10).round()}/10',
                              style: TextStyle(
                                fontSize: 22,
                                fontWeight: FontWeight.w600,
                                color: cs.onTertiaryContainer,
                              ),
                            ),
                          ],
                        ),
                      ),
                      const SizedBox(width: 16),
                      Expanded(
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Text(
                              'Security',
                              style: Theme.of(context).textTheme.titleLarge,
                            ),
                            Muted(
                              security == null
                                  ? 'Checking…'
                                  : toFix > 0
                                  ? '$toFix point${toFix > 1 ? 's' : ''} to fix'
                                  : 'Nothing to fix',
                            ),
                          ],
                        ),
                      ),
                      WButton(
                        'Details',
                        onPressed: () => Nav.go(context, 'security'),
                      ),
                    ],
                  ),
                  const SizedBox(height: 14),
                  ResponsiveGrid(
                    minWidth: 180,
                    gap: 8,
                    children: [
                      for (final c in top.take(8))
                        Container(
                          padding: const EdgeInsets.symmetric(
                            horizontal: 12,
                            vertical: 8,
                          ),
                          decoration: BoxDecoration(
                            color: cs.surfaceContainerHigh,
                            borderRadius: BorderRadius.circular(12),
                          ),
                          child: Row(
                            children: [
                              Mark(c['status'] as String),
                              const SizedBox(width: 10),
                              Expanded(
                                child: Text(
                                  c['title'] as String,
                                  maxLines: 1,
                                  overflow: TextOverflow.ellipsis,
                                  style: const TextStyle(fontSize: 14),
                                ),
                              ),
                            ],
                          ),
                        ),
                    ],
                  ),
                ],
              ),
            ),
            WCard(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  CardHead(
                    'Updates',
                    trailing: [
                      if (update != null)
                        WChip(
                          'Auto-update ${update!['auto'] == true ? 'on' : 'off'}'
                          '${update!['prerelease'] == true ? ' · pre-releases' : ''}',
                        ),
                    ],
                  ),
                  Tile(
                    shape: M3Shape.cookie,
                    tint: cs.primaryContainer,
                    title: 'VPS Admin agent ${update?['current'] ?? l.version}',
                    sub: update?['available'] == true
                        ? '${(update!['latest'] as Map?)?['tag']} is available'
                        : (update?['error'] as String? ?? '').isNotEmpty
                        ? 'Update check failed: ${update!['error']}'
                        : update?['checkedAt'] != null
                        ? 'Up to date, signed builds only'
                        : 'Not checked yet',
                    trailing: WButton(
                      update?['available'] == true ? 'Install' : 'Open',
                      kind: update?['available'] == true
                          ? BtnKind.filled
                          : BtnKind.tonal,
                      onPressed: () => Nav.go(context, 'updates'),
                    ),
                  ),
                  const SizedBox(height: 14),
                  Tile(
                    shape: M3Shape.clover,
                    tint: cs.tertiaryContainer,
                    title: 'System packages',
                    sub:
                        '${packages?['detail'] ?? 'Checking…'}'
                        '${rebootNeeded ? ' · a reboot is waiting' : ''}',
                    trailing: rebootNeeded
                        ? WButton(
                            'Restart',
                            kind: BtnKind.tonal,
                            onPressed: () => rebootDialog(context, l),
                          )
                        : null,
                  ),
                ],
              ),
            ),
          ],
        ),
        if (failed.isNotEmpty)
          WCard(
            child: Column(
              children: [
                CardHead(
                  'Failed services',
                  leading: ShapeIcon(
                    M3Shape.clover,
                    size: 28,
                    color: cs.errorContainer,
                  ),
                  trailing: [
                    TextButton(
                      onPressed: () => Nav.go(context, 'services'),
                      child: const Text('Open services'),
                    ),
                  ],
                ),
                for (var i = 0; i < failed.length; i++)
                  WRow(
                    first: i == 0,
                    children: [
                      const StatusDot(Tone.bad, label: 'Failed'),
                      Expanded(
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Text(
                              failed[i].name,
                              style: const TextStyle(
                                fontSize: 16,
                                fontWeight: FontWeight.w500,
                              ),
                            ),
                            Muted(failed[i].description, maxLines: 1),
                          ],
                        ),
                      ),
                    ],
                  ),
              ],
            ),
          ),
        for (var gi = 0; gi < groups.length; gi++)
          GroupCard(
            group: groups[gi],
            index: gi,
            trailing: TextButton(
              onPressed: () => Nav.go(context, 'containers'),
              child: const Text('Manage'),
            ),
            children: [
              for (var i = 0; i < groups[gi].items.length; i++)
                Builder(
                  builder: (context) {
                    final c = groups[gi].items[i];
                    return WRow(
                      first: i == 0,
                      children: [
                        StatusDot(containerTone(c), label: c.health ?? c.state),
                        Expanded(
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              Text(
                                c.name,
                                style: const TextStyle(
                                  fontSize: 16,
                                  fontWeight: FontWeight.w500,
                                ),
                              ),
                              Muted(imageName(c.image), maxLines: 1),
                            ],
                          ),
                        ),
                        if (c.exposed)
                          const WChip('public port', outline: true),
                        Flexible(child: Muted(c.status, maxLines: 2)),
                      ],
                    );
                  },
                ),
            ],
          ),
        if (dockerError.isNotEmpty) Muted('Docker: $dockerError'),
      ],
    );
  }
}
