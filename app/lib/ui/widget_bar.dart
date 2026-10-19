import 'dart:async';

import 'package:flutter/material.dart';

import '../data/models.dart';
import 'kit.dart';
import 'live.dart';
import 'nav.dart';
import 'shell.dart';
import 'widgets.dart';

const _widgets = [
  ('cpu', 'CPU'),
  ('memory', 'Memory'),
  ('swap', 'Swap'),
  ('disks', 'Disks'),
  ('network', 'Network'),
  ('system', 'Load and uptime'),
  ('containers', 'Containers'),
  ('updates', 'Updates'),
];

/// WidgetBar.vue: live resources beside the content on very wide screens.
class WidgetBar extends StatefulWidget {
  const WidgetBar({super.key, required this.onClose});
  final VoidCallback onClose;

  @override
  State<WidgetBar> createState() => _WidgetBarState();
}

class _WidgetBarState extends State<WidgetBar> {
  Set<String> hidden = {};
  List<DockerContainer>? containers;
  Map<String, dynamic>? update;
  Timer? _timer;

  @override
  void initState() {
    super.initState();
    readHiddenWidgets().then(
      (h) => mounted ? setState(() => hidden = h) : null,
    );
    WidgetsBinding.instance.addPostFrameCallback((_) => _load());
    _timer = Timer.periodic(const Duration(seconds: 30), (_) => _load());
  }

  @override
  void dispose() {
    _timer?.cancel();
    super.dispose();
  }

  Future<void> _load() async {
    final api = Live.read(context).api;
    try {
      final c = await api.containers();
      final u = await api.updateRaw();
      if (mounted) {
        setState(() {
          containers = c;
          update = u;
        });
      }
    } catch (_) {}
  }

  Future<void> _choose() => showWDialog(
    context,
    title: 'Widgets',
    body: (_) => StatefulBuilder(
      builder: (c, set) => Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          const Muted(
            'Shown in the bar of this phone. The Overview leaves out the cards the bar shows.',
          ),
          const SizedBox(height: 12),
          for (final w in _widgets)
            CheckboxListTile(
              contentPadding: EdgeInsets.zero,
              value: !hidden.contains(w.$1),
              title: Text(w.$2),
              onChanged: (v) {
                setState(
                  () => v == true ? hidden.remove(w.$1) : hidden.add(w.$1),
                );
                set(() {});
                saveHiddenWidgets(hidden);
              },
            ),
        ],
      ),
    ),
    actions: (c) => [WButton('Done', onPressed: () => Navigator.pop(c))],
  );

  @override
  Widget build(BuildContext context) {
    final l = Live.of(context);
    final s = l.snapshot;
    final cs = Theme.of(context).colorScheme;
    bool on(String id) => !hidden.contains(id);
    final mem = s != null && s.memTotal > 0
        ? 100 * s.memUsed / s.memTotal
        : 0.0;
    final swap = s != null && s.swapTotal > 0
        ? 100 * s.swapUsed / s.swapTotal
        : 0.0;
    final running = containers?.where((c) => c.state == 'running').length ?? 0;
    final troubled = containers?.where((c) => c.bad).length ?? 0;
    return ListView(
      padding: const EdgeInsets.fromLTRB(16, 20, 16, 24),
      children: [
        Row(
          children: [
            const Expanded(
              child: Text(
                'Widgets',
                style: TextStyle(fontSize: 18, fontWeight: FontWeight.w500),
              ),
            ),
            WIconButton(
              'settings',
              tooltip: 'Choose the widgets',
              size: 18,
              onPressed: _choose,
            ),
            WIconButton(
              'close',
              tooltip: 'Hide the bar',
              size: 18,
              onPressed: widget.onClose,
            ),
          ],
        ),
        const SizedBox(height: 6),
        for (final card in [
          if (on('cpu'))
            WMetricCard(
              compact: true,
              label: 'CPU · ${l.host?.cores ?? '?'} cores',
              value: s == null ? '…' : pct(s.cpu),
              percent: s?.cpu ?? 0,
              sub: s == null
                  ? ''
                  : 'Load ${s.load.map((x) => x.toStringAsFixed(2)).join(' · ')}',
              shape: M3Shape.cookie,
              tint: cs.primaryContainer,
            ),
          if (on('memory'))
            WMetricCard(
              compact: true,
              label: 'Memory',
              value: s == null ? '…' : pct(mem),
              percent: mem,
              sub: s == null
                  ? ''
                  : '${bytes(s.memUsed)} of ${bytes(s.memTotal)}',
              shape: M3Shape.clover,
              tint: cs.secondaryContainer,
            ),
          if (on('swap') && (s?.swapTotal ?? 0) > 0)
            WMetricCard(
              compact: true,
              label: 'Swap',
              value: pct(swap),
              percent: swap,
              sub: '${bytes(s!.swapUsed)} of ${bytes(s.swapTotal)}',
              shape: M3Shape.flower,
              tint: cs.tertiaryContainer,
            ),
          if (on('disks'))
            for (final (i, d) in (s?.disks ?? []).indexed)
              WMetricCard(
                compact: true,
                label: 'Disk ${d.mount}',
                value: pct(d.total == 0 ? 0 : 100 * d.used / d.total),
                percent: d.total == 0 ? 0 : 100 * d.used / d.total,
                sub: '${bytes(d.total - d.used)} free of ${bytes(d.total)}',
                shape: i.isOdd ? M3Shape.cookie : M3Shape.sunny,
                tint: i.isOdd ? cs.secondaryContainer : cs.tertiaryContainer,
              ),
          if (on('network') && s != null)
            WMetricCard(
              compact: true,
              label: 'Network',
              value: rate(s.netRx),
              sub: 'Down ${rate(s.netRx)} · up ${rate(s.netTx)}',
              shape: M3Shape.clover,
              tint: cs.primaryContainer,
            ),
          if (on('system') && s != null)
            WMetricCard(
              compact: true,
              label: 'Load',
              value: s.load.isEmpty ? '…' : s.load[0].toStringAsFixed(2),
              sub:
                  '${s.load.map((x) => x.toStringAsFixed(2)).join(' · ')} · up ${duration(s.uptime)}',
              shape: M3Shape.flower,
              tint: cs.secondaryContainer,
            ),
          if (on('containers'))
            WMetricCard(
              compact: true,
              label: 'Containers',
              value: containers == null
                  ? '…'
                  : '$running/${containers!.length}',
              percent: (containers?.isNotEmpty ?? false)
                  ? 100 * running / containers!.length
                  : 0,
              sub: troubled > 0
                  ? '$troubled in trouble'
                  : containers == null
                  ? 'Docker not reachable yet'
                  : 'All fine',
              shape: M3Shape.sunny,
              tint: cs.primaryContainer,
              onTap: () => Nav.go(context, 'containers'),
            ),
          if (on('updates'))
            WMetricCard(
              compact: true,
              label: 'Updates',
              value: update?['available'] == true
                  ? '${(update!['latest'] as Map?)?['tag'] ?? 'New'}'
                  : 'Up to date',
              sub: 'Agent ${update?['current'] ?? l.version}',
              shape: M3Shape.cookie,
              tint: cs.tertiaryContainer,
              onTap: () => Nav.go(context, 'updates'),
            ),
        ]) ...[card, const SizedBox(height: 10)],
      ],
    );
  }
}
