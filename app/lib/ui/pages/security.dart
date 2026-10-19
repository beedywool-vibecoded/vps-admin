import 'package:flutter/material.dart';

import '../kit.dart';
import '../live.dart';
import '../widgets.dart';
import 'common.dart';

/// SecurityView.vue: the score, the tally, each check by group with its fix, and the
/// listening ports split between all interfaces and local ones.
class SecurityPage extends StatefulWidget {
  const SecurityPage({super.key});

  @override
  State<SecurityPage> createState() => _SecurityPageState();
}

class _SecurityPageState extends State<SecurityPage> {
  Map<String, dynamic>? report;
  bool loading = false;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) => _load());
  }

  Future<void> _load([bool refresh = false]) async {
    setState(() => loading = true);
    try {
      final r = await Live.read(context).api.securityRaw(refresh: refresh);
      if (mounted) setState(() => report = r);
    } catch (e) {
      if (mounted) showError(context, e);
    } finally {
      if (mounted) setState(() => loading = false);
    }
  }

  static const _label = {
    'ok': 'Good',
    'warn': 'To improve',
    'fail': 'To fix',
    'unknown': 'Unknown',
  };

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    final t = Tones.of(context);
    final checks = ((report?['checks'] as List?) ?? [])
        .cast<Map<String, dynamic>>();
    final ports = ((report?['ports'] as List?) ?? [])
        .cast<Map<String, dynamic>>();
    final counts = {'ok': 0, 'warn': 0, 'fail': 0, 'unknown': 0};
    for (final c in checks) {
      counts[c['status'] as String] = (counts[c['status']] ?? 0) + 1;
    }
    final groups = [
      ('ssh', 'SSH', M3Shape.cookie, cs.primaryContainer),
      ('network', 'Network', M3Shape.clover, cs.secondaryContainer),
      ('updates', 'Updates', M3Shape.sunny, cs.tertiaryContainer),
    ];
    Color stateColor(String s) => switch (s) {
      'ok' => t.ok,
      'warn' => t.warn,
      'fail' => t.bad,
      _ => cs.onSurfaceVariant,
    };
    Widget portList(String title, List<Map<String, dynamic>> l, bool local) =>
        Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Muted(title),
            const SizedBox(height: 6),
            if (l.isEmpty) const Muted('None'),
            for (final p in l)
              Padding(
                padding: const EdgeInsets.symmetric(vertical: 4),
                child: Row(
                  children: [
                    Text(
                      '${p['port']}/${p['proto']}',
                      style: cmdStyle(context, fontSize: 13),
                    ),
                    const SizedBox(width: 10),
                    Expanded(
                      child: Muted(
                        (p['process'] as String? ?? '').isNotEmpty
                            ? p['process'] as String
                            : local
                            ? p['addr'] as String? ?? ''
                            : 'unknown process',
                        maxLines: 1,
                      ),
                    ),
                  ],
                ),
              ),
          ],
        );
    return WPage(
      onRefresh: () => _load(true),
      children: [
        PageTitle(
          'Security',
          sub: report == null
              ? null
              : 'Checked ${agoIso(report!['checked'] as String?)}',
          actions: [
            WButton(
              loading ? 'Checking…' : 'Check again',
              icon: 'restart',
              kind: BtnKind.tonal,
              onPressed: loading ? null : () => _load(true),
            ),
          ],
        ),
        WCard(
          child: Wrap(
            spacing: 20,
            runSpacing: 16,
            crossAxisAlignment: WrapCrossAlignment.center,
            children: [
              SizedBox(
                width: 120,
                height: 120,
                child: Stack(
                  alignment: Alignment.center,
                  children: [
                    ShapeIcon(
                      M3Shape.sunny,
                      size: 120,
                      color: cs.tertiaryContainer,
                    ),
                    Text.rich(
                      TextSpan(
                        text: report == null ? '…' : '${report!['score']}',
                        style: TextStyle(
                          fontSize: 34,
                          fontWeight: FontWeight.w600,
                          color: cs.onTertiaryContainer,
                        ),
                        children: [
                          TextSpan(
                            text: '/100',
                            style: TextStyle(
                              fontSize: 14,
                              color: cs.onTertiaryContainer,
                            ),
                          ),
                        ],
                      ),
                    ),
                  ],
                ),
              ),
              ConstrainedBox(
                constraints: const BoxConstraints(maxWidth: 520),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      report == null
                          ? 'Running the checks…'
                          : counts['fail']! > 0
                          ? 'Some things need fixing'
                          : counts['warn']! > 0
                          ? 'Good, a few improvements left'
                          : 'Well protected',
                      style: Theme.of(context).textTheme.titleMedium,
                    ),
                    const SizedBox(height: 10),
                    Wrap(
                      spacing: 8,
                      runSpacing: 8,
                      children: [
                        WChip(
                          '${counts['fail']} to fix',
                          color: t.bad,
                          textColor: t.onBad,
                        ),
                        WChip(
                          '${counts['warn']} to improve',
                          color: t.warn,
                          textColor: t.onWarn,
                        ),
                        WChip(
                          '${counts['ok']} good',
                          color: t.ok,
                          textColor: t.onOk,
                        ),
                        if (counts['unknown']! > 0)
                          WChip('${counts['unknown']} unknown', outline: true),
                      ],
                    ),
                    const SizedBox(height: 10),
                    const Muted(
                      'The checks only read the configuration. Fixes are listed with each item; the assistant will be able to apply them with your approval.',
                    ),
                  ],
                ),
              ),
            ],
          ),
        ),
        for (final g in groups)
          WCard(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                CardHead(g.$2, leading: ShapeIcon(g.$3, size: 28, color: g.$4)),
                for (final c in checks.where((c) => c['group'] == g.$1))
                  Padding(
                    padding: const EdgeInsets.symmetric(vertical: 10),
                    child: Row(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Mark(c['status'] as String),
                        const SizedBox(width: 14),
                        Expanded(
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              Text(
                                c['title'] as String,
                                style: const TextStyle(
                                  fontSize: 16,
                                  fontWeight: FontWeight.w500,
                                ),
                              ),
                              Muted(c['detail'] as String? ?? ''),
                              if ((c['fix'] as String? ?? '').isNotEmpty &&
                                  c['status'] != 'ok')
                                Padding(
                                  padding: const EdgeInsets.only(top: 6),
                                  child: Row(
                                    crossAxisAlignment:
                                        CrossAxisAlignment.start,
                                    children: [
                                      WIcon(
                                        'info',
                                        size: 16,
                                        color: cs.primary,
                                      ),
                                      const SizedBox(width: 6),
                                      Expanded(
                                        child: CmdText(
                                          c['fix'] as String,
                                          style: TextStyle(
                                            fontSize: 13,
                                            color: cs.primary,
                                          ),
                                        ),
                                      ),
                                    ],
                                  ),
                                ),
                            ],
                          ),
                        ),
                        const SizedBox(width: 8),
                        Text(
                          _label[c['status']] ?? '',
                          style: TextStyle(
                            fontSize: 13,
                            fontWeight: FontWeight.w600,
                            color: stateColor(c['status'] as String),
                          ),
                        ),
                      ],
                    ),
                  ),
              ],
            ),
          ),
        WCard(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              CardHead(
                'Listening ports',
                sub: 'What the firewall then filters',
                leading: ShapeIcon(
                  M3Shape.flower,
                  size: 28,
                  color: cs.primaryContainer,
                ),
              ),
              ResponsiveGrid(
                minWidth: 260,
                gap: 20,
                children: [
                  portList(
                    'All interfaces',
                    ports.where((p) => p['public'] == true).toList(),
                    false,
                  ),
                  portList(
                    'Local or private only',
                    ports.where((p) => p['public'] != true).toList(),
                    true,
                  ),
                ],
              ),
            ],
          ),
        ),
      ],
    );
  }
}
