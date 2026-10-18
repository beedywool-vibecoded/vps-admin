import 'package:flutter/material.dart';

import '../data/models.dart';
import 'scope.dart';
import 'widgets.dart';

class SecurityScreen extends StatelessWidget {
  const SecurityScreen({super.key});

  @override
  Widget build(BuildContext context) {
    final api = AppScope.of(context).api!;
    return Scaffold(
      appBar: AppBar(title: const Text('Security')),
      body: Loader<Security>(
        load: () => api.security(refresh: true),
        builder: (context, s, _) {
          final cs = Theme.of(context).colorScheme;
          return ListView(padding: const EdgeInsets.all(16), children: [
            Center(child: ShapeIcon(M3Shape.sunny, size: 110, color: cs.tertiaryContainer,
                child: Text('${s.score}', style: TextStyle(fontSize: 34, fontWeight: FontWeight.w600, color: cs.onTertiaryContainer)))),
            const SizedBox(height: 16),
            for (final g in ['ssh', 'network', 'updates']) ...[
              Padding(padding: const EdgeInsets.fromLTRB(8, 12, 8, 6), child: Text(g == 'ssh' ? 'SSH' : g[0].toUpperCase() + g.substring(1), style: Theme.of(context).textTheme.titleMedium)),
              Card(
                margin: EdgeInsets.zero,
                child: Column(children: [
                  for (final c in s.checks.where((c) => c.group == g))
                    ListTile(
                      leading: StatusDot(switch (c.status) { 'ok' => Tone.ok, 'warn' => Tone.warn, 'fail' => Tone.bad, _ => Tone.idle }, label: c.status),
                      title: Text(c.title),
                      subtitle: Text(c.fix != null && c.status != 'ok' ? '${c.detail}\n→ ${c.fix}' : c.detail),
                      isThreeLine: c.fix != null && c.status != 'ok',
                    ),
                ]),
              ),
            ],
          ]);
        },
      ),
    );
  }
}
