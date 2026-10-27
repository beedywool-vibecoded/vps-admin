import 'dart:async';

import 'package:flutter/material.dart';

import '../kit.dart';
import '../live.dart';
import '../widgets.dart';
import 'common.dart';

/// UpdatesView.vue
class UpdatesPage extends StatefulWidget {
  const UpdatesPage({super.key});

  @override
  State<UpdatesPage> createState() => _UpdatesPageState();
}

class _UpdatesPageState extends State<UpdatesPage> {
  Map<String, dynamic>? st, packages, reboot;
  bool checking = false, installing = false;

  LiveSession get live => Live.read(context);

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) => _load());
  }

  Future<void> _load() async {
    try {
      final u = await live.api.updateRaw();
      final sec = await live.api.securityRaw();
      final checks = ((sec['checks'] as List?) ?? [])
          .cast<Map<String, dynamic>>();
      if (!mounted) return;
      setState(() {
        st = u;
        packages = checks.where((c) => c['id'] == 'updates').firstOrNull;
        reboot = checks.where((c) => c['id'] == 'reboot').firstOrNull;
      });
    } catch (e) {
      if (mounted) showError(context, e);
    }
  }

  Future<void> _save({bool? auto, bool? prerelease}) async {
    try {
      await live.api.updateSettings(auto: auto, prerelease: prerelease);
      final u = await live.api.updateRaw();
      if (mounted) setState(() => st = u);
    } catch (e) {
      if (mounted) showError(context, e);
    }
  }

  Future<void> _check() async {
    setState(() => checking = true);
    try {
      await live.api.checkUpdate();
      final u = await live.api.updateRaw();
      if (!mounted) return;
      setState(() => st = u);
      showInfo(
        context,
        u['available'] == true
            ? '${(u['latest'] as Map?)?['tag']} is available'
            : 'The agent is up to date',
      );
    } catch (e) {
      if (mounted) showError(context, e);
    } finally {
      if (mounted) setState(() => checking = false);
    }
  }

  // The agent restarts after installing; poll until the new version answers.
  Future<void> _install() async {
    final tag = (st?['latest'] as Map?)?['tag'] as String?;
    if (!await wConfirm(
      context,
      title: 'Install $tag?',
      body:
          'The agent checks the signature, swaps the binary and restarts. The app reconnects by itself; if the new version fails to start, the previous one comes back automatically.',
      action: 'Install',
    )) {
      return;
    }
    setState(() => installing = true);
    try {
      await live.api.applyUpdate();
      for (var i = 0; i < 60; i++) {
        await Future<void>.delayed(const Duration(seconds: 2));
        try {
          final h = await live.api.get('/api/health');
          if (h['version'] == tag) {
            if (mounted) showInfo(context, 'Updated to $tag');
            await _load();
            return;
          }
        } catch (_) {}
      }
      if (mounted) {
        showError(
          context,
          'The agent did not come back with the new version. It may have rolled back, check the status below.',
        );
      }
      await _load();
    } catch (e) {
      if (mounted) showError(context, e);
    } finally {
      if (mounted) setState(() => installing = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    final latest = st?['latest'] as Map<String, dynamic>?;
    final last = st?['lastResult'] as String? ?? '';
    final rolled = last.startsWith('rolled back');
    Widget note(String icon, String text, {bool bad = false}) => Container(
      padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 10),
      decoration: BoxDecoration(
        color: bad ? cs.errorContainer : cs.surfaceContainerHigh,
        borderRadius: BorderRadius.circular(12),
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          WIcon(
            icon,
            size: 18,
            color: bad ? cs.onErrorContainer : cs.onSurfaceVariant,
          ),
          const SizedBox(width: 8),
          Expanded(
            child: Text(
              text,
              style: TextStyle(
                fontSize: 14,
                color: bad ? cs.onErrorContainer : cs.onSurface,
              ),
            ),
          ),
        ],
      ),
    );
    Widget setting(
      String title,
      String sub,
      bool value,
      ValueChanged<bool> on,
    ) => Container(
      padding: const EdgeInsets.symmetric(vertical: 12),
      decoration: BoxDecoration(
        border: Border(top: BorderSide(color: cs.surfaceContainerHigh)),
      ),
      child: Row(
        children: [
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [Text(title), Muted(sub)],
            ),
          ),
          Switch(value: value, onChanged: st == null ? null : on),
        ],
      ),
    );
    return WPage(
      onRefresh: _load,
      children: [
        const PageTitle('Updates'),
        WCard(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            spacing: 14,
            children: [
              Wrap(
                spacing: 16,
                runSpacing: 12,
                crossAxisAlignment: WrapCrossAlignment.center,
                children: [
                  ShapeIcon(
                    M3Shape.cookie,
                    size: 56,
                    color: cs.primaryContainer,
                  ),
                  Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      Text(
                        'VPS Admin agent',
                        style: Theme.of(context).textTheme.titleMedium,
                      ),
                      Text.rich(
                        TextSpan(
                          text: 'Version ',
                          children: [
                            TextSpan(
                              text: st?['current'] as String? ?? '…',
                              style: const TextStyle(
                                fontWeight: FontWeight.w700,
                              ),
                            ),
                            TextSpan(
                              text:
                                  ' · ${st?['checkedAt'] != null ? 'checked ${agoIso(st!['checkedAt'] as String)}' : 'never checked'}',
                            ),
                          ],
                        ),
                        style: TextStyle(
                          fontSize: 13,
                          color: cs.onSurfaceVariant,
                        ),
                      ),
                    ],
                  ),
                  WButton(
                    checking ? 'Checking…' : 'Check now',
                    kind: BtnKind.tonal,
                    onPressed: checking || installing ? null : _check,
                  ),
                  if (st?['available'] == true)
                    WButton(
                      installing ? 'Installing…' : 'Install ${latest?['tag']}',
                      onPressed: installing ? null : _install,
                    ),
                ],
              ),
              if ((st?['error'] as String? ?? '').isNotEmpty)
                note('alert', st!['error'] as String, bad: true),
              if (last.isNotEmpty)
                note(
                  rolled ? 'alert' : 'history',
                  'Last update: $last${rolled ? ' (it failed to start, the previous version was restored and this one will not be retried automatically)' : ''}',
                  bad: rolled,
                ),
              if (latest != null)
                Container(
                  padding: const EdgeInsets.all(16),
                  decoration: BoxDecoration(
                    color: cs.surfaceContainerHigh,
                    borderRadius: BorderRadius.circular(16),
                  ),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Wrap(
                        spacing: 10,
                        crossAxisAlignment: WrapCrossAlignment.center,
                        children: [
                          Text(
                            latest['tag'] as String? ?? '',
                            style: const TextStyle(fontWeight: FontWeight.w700),
                          ),
                          if (latest['prerelease'] == true)
                            const WChip('pre-release'),
                          if (latest['published'] != null)
                            Muted(dateTime(latest['published'] as String)),
                        ],
                      ),
                      const SizedBox(height: 8),
                      Text(
                        (latest['notes'] as String? ?? '').isEmpty
                            ? 'No release notes.'
                            : latest['notes'] as String,
                        style: const TextStyle(
                          fontFamily: kMono,
                          fontSize: 12,
                          height: 1.5,
                        ),
                      ),
                    ],
                  ),
                ),
              Column(
                children: [
                  setting(
                    'Install updates automatically',
                    'Checks GitHub every 6 hours. Only builds signed with the project key are installed.',
                    st?['auto'] == true,
                    (v) => _save(auto: v),
                  ),
                  setting(
                    'Follow pre-releases',
                    'Get release candidates before they are marked stable.',
                    st?['prerelease'] == true,
                    (v) => _save(prerelease: v),
                  ),
                ],
              ),
            ],
          ),
        ),
        WCard(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            spacing: 14,
            children: [
              Wrap(
                spacing: 16,
                runSpacing: 12,
                crossAxisAlignment: WrapCrossAlignment.center,
                children: [
                  ShapeIcon(
                    M3Shape.clover,
                    size: 56,
                    color: cs.tertiaryContainer,
                  ),
                  ConstrainedBox(
                    constraints: const BoxConstraints(maxWidth: 420),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          'System packages',
                          style: Theme.of(context).textTheme.titleMedium,
                        ),
                        Muted(packages?['detail'] as String? ?? 'Checking…'),
                      ],
                    ),
                  ),
                  WButton(
                    'Restart the server',
                    icon: 'power',
                    kind: reboot != null && reboot!['status'] != 'ok'
                        ? BtnKind.filled
                        : BtnKind.outlined,
                    onPressed: () => rebootDialog(context, live),
                  ),
                ],
              ),
              if (reboot != null && reboot!['status'] != 'ok')
                note('info', reboot!['detail'] as String? ?? ''),
              CmdText(
                'Installing packages and rebooting from the panel comes with the assistant, behind an approval for each step. Until then, use `sudo apt full-upgrade` (or your distribution\'s equivalent) over SSH.',
                style: TextStyle(fontSize: 13, color: cs.onSurfaceVariant),
              ),
            ],
          ),
        ),
      ],
    );
  }
}
