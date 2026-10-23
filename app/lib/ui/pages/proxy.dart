import 'package:flutter/material.dart';

import '../../data/api.dart';
import '../../data/models.dart';
import '../../data/updater.dart';
import '../kit.dart';
import '../live.dart';
import '../widgets.dart';
import 'apps.dart';

/// ProxyView.vue: the domains Caddy or nginx publishes on this machine, the upstream each
/// one forwards to, and the ones the panel wrote itself.
class ProxyPage extends StatefulWidget {
  const ProxyPage({super.key});

  @override
  State<ProxyPage> createState() => _ProxyPageState();
}

class _ProxyPageState extends State<ProxyPage> {
  ProxyReport? rep;
  bool loading = true;
  String query = '';
  String busy = '';

  Api get api => Live.read(context).api;

  String _key(ProxySite s) => s.id.isNotEmpty
      ? 'app:${s.id}'
      : 'file:${s.file}|${s.domains.isEmpty ? '' : s.domains.first}';

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) => _load());
  }

  Future<void> _load() async {
    try {
      final r = await api.proxy();
      if (mounted) setState(() => rep = r);
    } catch (e) {
      if (mounted) showError(context, e);
    } finally {
      if (mounted) setState(() => loading = false);
    }
  }

  Future<void> _pause(ProxySite s) async {
    final name = s.domains.isEmpty ? s.file : s.domains.first;
    setState(() => busy = _key(s));
    try {
      final r = await api.proxyPause(s.id, !s.paused);
      if (!mounted) return;
      setState(() => rep = r);
      showInfo(
        context,
        s.paused ? '$name is back on' : '$name paused, the app keeps running',
      );
    } catch (e) {
      if (mounted) showError(context, e);
    } finally {
      if (mounted) setState(() => busy = '');
    }
  }

  Future<void> _removeOrphan(ProxySite s) async {
    final name = s.domains.isEmpty ? s.file : s.domains.first;
    if (!await wConfirm(
      context,
      title: 'Remove $name?',
      body:
          'The panel wrote this site for an app that no longer exists. '
          'Removing it takes the domain out of the proxy; nothing else is touched.',
      action: 'Remove',
      danger: true,
    )) {
      return;
    }
    setState(() => busy = _key(s));
    try {
      final r = await api.removeProxyOrphan(s.file);
      if (!mounted) return;
      setState(() => rep = r);
      showInfo(context, 'Left-over site removed');
    } catch (e) {
      if (mounted) showError(context, e);
    } finally {
      if (mounted) setState(() => busy = '');
    }
  }

  /// Names served by more than one site: what makes a deploy fail on "domain already used".
  Set<String> _shared(List<ProxySite> sites) {
    final seen = <String, int>{};
    for (final s in sites) {
      for (final d in s.domains) {
        seen[d] = (seen[d] ?? 0) + 1;
      }
    }
    return {
      for (final e in seen.entries)
        if (e.value > 1) e.key,
    };
  }

  Widget _empty() => const WCard(
    child: Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      spacing: 8,
      children: [
        Text(
          'No reverse proxy found',
          style: TextStyle(fontSize: 18, fontWeight: FontWeight.w500),
        ),
        Muted(
          'Neither Caddy nor nginx answers on this machine, so no domain is published. '
          'Reinstall the agent with --proxy caddy or --proxy nginx to serve apps on a domain.',
          size: 13,
        ),
      ],
    ),
  );

  Widget _site(ProxySite s, bool first, Set<String> shared, ColorScheme cs) {
    final warn = StatusDot.color(Tone.warn, cs);
    final warnBg = warn.withValues(alpha: .16);
    final extra = s.domains.length > 1 ? s.domains.skip(1).join(' · ') : '';
    return Opacity(
      opacity: s.paused ? .62 : 1,
      child: WRow(
        first: first,
        padding: const EdgeInsets.fromLTRB(6, 12, 0, 12),
        children: [
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                InkWell(
                  onTap: s.url.isEmpty ? null : () => Updater.openUrl(s.url),
                  borderRadius: BorderRadius.circular(6),
                  child: Row(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      Flexible(
                        child: Text(
                          s.domains.isEmpty ? s.file : s.domains.first,
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: TextStyle(
                            fontSize: 16,
                            fontWeight: FontWeight.w500,
                            color: cs.primary,
                          ),
                        ),
                      ),
                      if (s.url.isNotEmpty) ...[
                        const SizedBox(width: 4),
                        WIcon('external', size: 14, color: cs.primary),
                      ],
                    ],
                  ),
                ),
                if (extra.isNotEmpty) Muted(extra, maxLines: 1),
                Muted(s.file, mono: true, size: 12, maxLines: 1),
                const SizedBox(height: 8),
                Wrap(
                  spacing: 6,
                  runSpacing: 6,
                  crossAxisAlignment: WrapCrossAlignment.center,
                  children: [
                    if (s.target.isNotEmpty)
                      WChip(s.target)
                    else
                      const WChip('no upstream', outline: true),
                    if (s.vpnOnly) const WChip('VPN only', outline: true),
                    if (s.scheme == 'http')
                      WChip('no HTTPS', color: warnBg, textColor: warn),
                    if (s.domains.any(shared.contains))
                      WChip('also elsewhere', color: warnBg, textColor: warn),
                    if (s.paused)
                      WChip('paused', color: warnBg, textColor: warn),
                    if (s.id.isNotEmpty)
                      InkWell(
                        onTap: () => openApp(context, s.id),
                        borderRadius: BorderRadius.circular(8),
                        child: WChip('App ${s.id}'),
                      )
                    else if (s.managed)
                      const WChip('left over', outline: true)
                    else
                      const WChip('by hand', outline: true),
                  ],
                ),
              ],
            ),
          ),
          if (s.id.isNotEmpty)
            WIconButton(
              s.paused ? 'play' : 'stop',
              tooltip: s.paused ? 'Turn this URL back on' : 'Pause this URL',
              size: 18,
              onPressed: busy == _key(s) ? null : () => _pause(s),
            )
          else if (s.managed)
            WIconButton(
              'trash',
              tooltip: 'Remove this left-over site',
              size: 18,
              onPressed: busy == _key(s) ? null : () => _removeOrphan(s),
            ),
        ],
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    final r = rep;
    final sites = r?.sites ?? const <ProxySite>[];
    final shared = _shared(sites);
    final names = {for (final s in sites) ...s.domains};
    final q = query.trim().toLowerCase();
    final shown = sites
        .where(
          (s) =>
              q.isEmpty ||
              s.domains.any((d) => d.toLowerCase().contains(q)) ||
              s.target.toLowerCase().contains(q) ||
              s.file.toLowerCase().contains(q),
        )
        .toList();
    final engine = r?.engine == 'caddy'
        ? 'Caddy'
        : r?.engine == 'nginx'
        ? 'nginx'
        : '';

    return WPage(
      onRefresh: _load,
      children: [
        const PageTitle('Proxy'),
        SearchField(
          hint: 'Search domains',
          onChanged: (v) => setState(() => query = v),
        ),
        const Muted(
          'Pausing a site takes its domain out of the proxy and leaves the app running; it stays in this list, ready to turn back on.',
          size: 12,
        ),
        if (loading)
          const WCard(child: LoadingBlock(label: 'Loading domains'))
        else if (r == null || engine.isEmpty)
          _empty()
        else ...[
          Wrap(
            spacing: 8,
            runSpacing: 8,
            crossAxisAlignment: WrapCrossAlignment.center,
            children: [
              WChip(engine, outline: true),
              Muted(r.root, mono: true, size: 12),
              Muted(
                '${names.length} ${names.length == 1 ? 'domain' : 'domains'}',
                size: 12,
              ),
            ],
          ),
          WCard(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                if (shown.isEmpty)
                  Muted(
                    sites.isEmpty
                        ? 'The proxy serves no site yet.'
                        : 'No domain matches.',
                    size: 15,
                  ),
                for (var i = 0; i < shown.length; i++)
                  _site(shown[i], i == 0, shared, cs),
              ],
            ),
          ),
          if (r.unreadable.isNotEmpty)
            WCard(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  const Text(
                    'Some files could not be read',
                    style: TextStyle(fontSize: 16, fontWeight: FontWeight.w500),
                  ),
                  const SizedBox(height: 6),
                  const Muted(
                    'The agent runs as its own user, so a configuration file '
                    'it cannot open is left out of this list.',
                    size: 13,
                  ),
                  for (final f in r.unreadable)
                    Padding(
                      padding: const EdgeInsets.only(top: 6),
                      child: Muted(f, mono: true, size: 12),
                    ),
                ],
              ),
            ),
        ],
      ],
    );
  }
}
