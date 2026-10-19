import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../../data/api.dart';
import '../../data/models.dart';
import '../kit.dart';
import '../live.dart';
import '../widgets.dart';
import 'common.dart';
import 'scripts.dart';

/// ContainersView.vue: search, groups collapsed by default with their icon, the folder's
/// scripts, and each container with its stats and actions (start, restart, stop,
/// rebuild, logs).
class ContainersPage extends StatefulWidget {
  const ContainersPage({super.key});

  @override
  State<ContainersPage> createState() => _ContainersPageState();
}

class _ContainersPageState extends State<ContainersPage> {
  List<DockerContainer> list = [];
  Map<String, Map<String, dynamic>> stats = {};
  List<Map<String, dynamic>> scripts = [];
  bool loading = true;
  String error = '', query = '';
  final busy = <String>{};
  Set<String> open = {};
  Timer? _timer;

  Api get api => Live.read(context).api;

  @override
  void initState() {
    super.initState();
    _readOpen();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      _load();
      _stats();
      _scripts();
    });
    _timer = Timer.periodic(const Duration(seconds: 15), (_) {
      _load();
      _stats();
    });
  }

  @override
  void dispose() {
    _timer?.cancel();
    super.dispose();
  }

  // Open groups are remembered on this phone, like the panel does per browser.
  Future<void> _readOpen() async {
    try {
      final p = await SharedPreferences.getInstance();
      final v = jsonDecode(p.getString('openGroups') ?? '[]') as List;
      if (mounted) setState(() => open = v.cast<String>().toSet());
    } catch (_) {}
  }

  Future<void> _toggle(String name) async {
    setState(() => open.contains(name) ? open.remove(name) : open.add(name));
    try {
      final p = await SharedPreferences.getInstance();
      await p.setString('openGroups', jsonEncode(open.toList()));
    } catch (_) {}
  }

  Future<void> _load() async {
    try {
      final l = await api.containers();
      if (mounted) setState(() {
        list = l;
        error = '';
      });
    } catch (e) {
      if (mounted) setState(() => error = errText(e));
    } finally {
      if (mounted) setState(() => loading = false);
    }
  }

  Future<void> _stats() async {
    try {
      final s = await api.containerStats();
      if (mounted) setState(() => stats = s);
    } catch (_) {}
  }

  Future<void> _scripts() async {
    try {
      final s = await api.scripts();
      if (mounted) setState(() => scripts = s.scripts);
    } catch (_) {}
  }

  Future<void> _act(DockerContainer c, String action) async {
    if (action == 'stop' &&
        !await wConfirm(
          context,
          title: 'Stop ${c.name}?',
          body: 'Its restart policy may start it again at the next reboot.',
          action: 'Stop',
          danger: true,
        )) {
      return;
    }
    setState(() => busy.add(c.id));
    try {
      await api.containerAction(c.id, action);
      if (mounted) showInfo(context, '${c.name}: $action done');
      await _load();
    } catch (e) {
      if (mounted) showError(context, e);
    } finally {
      if (mounted) setState(() => busy.remove(c.id));
    }
  }

  String _ports(DockerContainer c) => c.ports
      .where((p) => p['publicPort'] != null)
      .map((p) {
        final ip = p['ip'] as String? ?? '';
        final pre = ip.isNotEmpty && ip != '0.0.0.0' && ip != '::'
            ? '$ip:'
            : '';
        return '$pre${p['publicPort']}→${p['privatePort']}';
      })
      .join(', ');

  Future<void> _pickIcon(String project) async {
    final live = Live.read(context);
    await showWDialog(
      context,
      title: 'Icon of $project',
      body: (c) => Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          const Muted('Everyone sees it, on the panel and in the app.'),
          const SizedBox(height: 16),
          Flexible(
            child: SingleChildScrollView(
              child: Wrap(
                spacing: 8,
                runSpacing: 8,
                children: [
                  for (final name in groupIconChoices)
                    _IconOption(
                      name: name,
                      selected: live.groupIcons[project] == name,
                      onTap: () async {
                        Navigator.pop(c);
                        await _setIcon(project, name);
                      },
                    ),
                ],
              ),
            ),
          ),
        ],
      ),
      actions: (c) => [
        if (live.groupIcons[project] != null)
          WButton(
            'Default shape',
            kind: BtnKind.text,
            onPressed: () {
              Navigator.pop(c);
              _setIcon(project, '');
            },
          ),
        WButton('Close', kind: BtnKind.text, onPressed: () => Navigator.pop(c)),
      ],
    );
  }

  Future<void> _setIcon(String project, String icon) async {
    final live = Live.read(context);
    try {
      live.applyAppearance(await live.api.setGroupIcon(project, icon));
    } catch (e) {
      if (mounted) showError(context, e);
    }
  }

  Future<void> _rebuild(DockerContainer c) async {
    await showWDialog(
      context,
      title: 'Rebuild ${c.name}',
      wide: true,
      body: (_) => _RebuildBody(api: api, container: c, done: _load),
    );
  }

  @override
  Widget build(BuildContext context) {
    final live = Live.of(context);
    final cs = Theme.of(context).colorScheme;
    final groups = groupContainers(list, query: query);
    return WPage(
      onRefresh: () async => Future.wait([_load(), _stats(), _scripts()]),
      children: [
        PageTitle('Containers'),
        SearchField(
          hint: 'Search containers, images, projects',
          onChanged: (v) => setState(() => query = v),
        ),
        if (loading)
          const Muted('Loading…', size: 15)
        else if (error.isNotEmpty)
          WCard(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  'Docker is not reachable',
                  style: Theme.of(context).textTheme.titleMedium,
                ),
                const SizedBox(height: 8),
                Muted(
                  '$error. Check that Docker runs and that the vps-admin user is in the docker group.',
                  size: 15,
                ),
              ],
            ),
          )
        else if (groups.isEmpty)
          const Muted('No container.', size: 15),
        for (var gi = 0; gi < groups.length; gi++)
          _group(context, live, cs, groups[gi], gi),
      ],
    );
  }

  Widget _group(
    BuildContext context,
    LiveSession live,
    ColorScheme cs,
    ContainerGroup g,
    int gi,
  ) {
    final isOpen = query.trim().isNotEmpty || open.contains(g.name);
    final folderScripts = g.dir == null
        ? <Map<String, dynamic>>[]
        : scripts.where((s) => s['dir'] == g.dir).toList();
    return WCard(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Row(
            children: [
              InkWell(
                borderRadius: BorderRadius.circular(12),
                onTap: g.name == 'Standalone' ? null : () => _pickIcon(g.name),
                child: GroupBadge(icon: live.groupIcons[g.name], index: gi),
              ),
              const SizedBox(width: 12),
              Expanded(
                child: InkWell(
                  borderRadius: BorderRadius.circular(16),
                  onTap: () => _toggle(g.name),
                  child: Padding(
                    padding: const EdgeInsets.symmetric(
                      horizontal: 8,
                      vertical: 6,
                    ),
                    child: Row(
                      children: [
                        Expanded(
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              Text(
                                g.name,
                                style: const TextStyle(
                                  fontSize: 20,
                                  fontWeight: FontWeight.w500,
                                ),
                              ),
                              if (g.dir != null)
                                Muted(
                                  g.dir!,
                                  size: 12,
                                  mono: true,
                                  maxLines: 2,
                                ),
                            ],
                          ),
                        ),
                        for (final c in g.items.take(8))
                          Padding(
                            padding: const EdgeInsets.only(left: 4),
                            child: StatusDot(containerTone(c), label: c.state),
                          ),
                        const SizedBox(width: 10),
                        WChip('${g.items.length}'),
                        const SizedBox(width: 6),
                        AnimatedRotation(
                          turns: isOpen ? .5 : 0,
                          duration: const Duration(milliseconds: 250),
                          child: WIcon(
                            'chevron',
                            size: 20,
                            color: cs.onSurfaceVariant,
                          ),
                        ),
                      ],
                    ),
                  ),
                ),
              ),
            ],
          ),
          AnimatedSize(
            duration: const Duration(milliseconds: 300),
            curve: const Cubic(0.05, 0.7, 0.1, 1),
            alignment: Alignment.topCenter,
            child: !isOpen
                ? const SizedBox(width: double.infinity)
                : Column(
                    crossAxisAlignment: CrossAxisAlignment.stretch,
                    children: [
                      const SizedBox(height: 12),
                      if (folderScripts.isNotEmpty)
                        Padding(
                          padding: const EdgeInsets.only(bottom: 10),
                          child: Wrap(
                            spacing: 8,
                            runSpacing: 8,
                            children: [
                              for (final sc in folderScripts)
                                Tooltip(
                                  message: sc['target'] as String? ?? '',
                                  child: WButton(
                                    scriptTitle(sc),
                                    icon: 'terminal',
                                    kind: BtnKind.outlined,
                                    onPressed: () => runScriptDialog(
                                      context,
                                      api,
                                      sc,
                                      onDone: _scripts,
                                    ),
                                  ),
                                ),
                            ],
                          ),
                        ),
                      for (var i = 0; i < g.items.length; i++)
                        _row(
                          g.items[i],
                          first: i == 0 && folderScripts.isEmpty,
                        ),
                    ],
                  ),
          ),
        ],
      ),
    );
  }

  Widget _row(DockerContainer c, {required bool first}) {
    final st = stats[c.id];
    final ports = _ports(c);
    final running = c.state == 'running';
    return WRow(
      first: first,
      padding: const EdgeInsets.fromLTRB(6, 12, 0, 12),
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
              Muted(
                '${imageName(c.image)}${ports.isNotEmpty ? ' · $ports' : ''}',
                maxLines: 2,
              ),
              const SizedBox(height: 4),
              Row(
                children: [
                  if (c.exposed) ...[
                    const Tooltip(
                      message:
                          'Docker bypasses ufw for ports published on every interface',
                      child: WChip('public', outline: true),
                    ),
                    const SizedBox(width: 8),
                  ],
                  Flexible(
                    child: Muted(
                      st != null
                          ? '${(st['cpu'] as num).toStringAsFixed(1)}% · ${bytes((st['memUsed'] as num).toDouble())}'
                          : c.status,
                      maxLines: 1,
                    ),
                  ),
                ],
              ),
            ],
          ),
        ),
        Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            if (!running)
              WIconButton(
                'play',
                tooltip: 'Start ${c.name}',
                onPressed: busy.contains(c.id) ? null : () => _act(c, 'start'),
              )
            else
              WIconButton(
                'restart',
                tooltip: 'Restart ${c.name}',
                onPressed: busy.contains(c.id)
                    ? null
                    : () => _act(c, 'restart'),
              ),
            WIconButton(
              'stop',
              tooltip: 'Stop ${c.name}',
              size: 18,
              onPressed: busy.contains(c.id) || !running
                  ? null
                  : () => _act(c, 'stop'),
            ),
            if (c.service != null)
              WIconButton(
                'wrench',
                tooltip: 'Rebuild ${c.name}',
                size: 18,
                onPressed: () => _rebuild(c),
              ),
            WIconButton(
              'logs',
              tooltip: 'Logs of ${c.name}',
              onPressed: () => showLogs(
                context,
                title: '${c.name} logs',
                load: () => api.containerLogs(c.id),
              ),
            ),
          ],
        ),
      ],
    );
  }
}

class _IconOption extends StatelessWidget {
  const _IconOption({
    required this.name,
    required this.selected,
    required this.onTap,
  });
  final String name;
  final bool selected;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    return Tooltip(
      message: name,
      child: Material(
        color: selected ? cs.primaryContainer : cs.surfaceContainerHighest,
        borderRadius: BorderRadius.circular(selected ? 24 : 14),
        child: InkWell(
          borderRadius: BorderRadius.circular(selected ? 24 : 14),
          onTap: onTap,
          child: SizedBox.square(
            dimension: 48,
            child: Center(
              child: WIcon(
                name,
                size: 22,
                color: selected ? cs.onPrimaryContainer : cs.onSurface,
              ),
            ),
          ),
        ),
      ),
    );
  }
}

/// RebuildDialog.vue: explanation, then the live output with a done/failed chip.
class _RebuildBody extends StatefulWidget {
  const _RebuildBody({
    required this.api,
    required this.container,
    required this.done,
  });
  final Api api;
  final DockerContainer container;
  final Future<void> Function() done;

  @override
  State<_RebuildBody> createState() => _RebuildBodyState();
}

class _RebuildBodyState extends State<_RebuildBody> {
  String state = 'ready', out = '';
  final _scroll = ScrollController();
  StreamSubscription<String>? _sub;

  void _run() {
    setState(() => state = 'running');
    _sub = widget.api
        .rebuildContainer(widget.container.id)
        .listen(
          (chunk) {
            setState(() => out += chunk);
            WidgetsBinding.instance.addPostFrameCallback((_) {
              if (_scroll.hasClients)
                _scroll.jumpTo(_scroll.position.maxScrollExtent);
            });
          },
          onError: (Object e) {
            if (mounted) {
              setState(() => state = 'ready');
              showError(context, e);
            }
          },
          onDone: () {
            if (!mounted) return;
            setState(
              () => state = out.trimRight().endsWith('==> Done')
                  ? 'ok'
                  : 'failed',
            );
            widget.done();
          },
        );
  }

  @override
  void dispose() {
    _sub?.cancel();
    _scroll.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final c = widget.container;
    final t = Tones.of(context);
    return Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        if (state == 'running')
          const Align(alignment: Alignment.centerLeft, child: WChip('running')),
        if (state == 'ok')
          Align(
            alignment: Alignment.centerLeft,
            child: WChip('done', color: t.ok, textColor: t.onOk),
          ),
        if (state == 'failed')
          Align(
            alignment: Alignment.centerLeft,
            child: WChip('failed', color: t.bad, textColor: t.onBad),
          ),
        if (state == 'ready')
          CmdText(
            'Pulls a newer image of `${c.service}` (or rebuilds it if it has a build section), then recreates this container alone from ${c.workDir}. The other containers of ${c.project} keep running.',
            style: TextStyle(
              fontSize: 15,
              color: Theme.of(context).colorScheme.onSurfaceVariant,
            ),
          )
        else ...[
          const SizedBox(height: 12),
          Flexible(
            child: OutputBox(
              out.isEmpty ? 'Starting…' : out,
              height: 360,
              controller: _scroll,
            ),
          ),
        ],
        const SizedBox(height: 16),
        Wrap(
          alignment: WrapAlignment.end,
          spacing: 8,
          children: [
            WButton(
              state == 'ready' ? 'Cancel' : 'Close',
              kind: BtnKind.text,
              onPressed: () => Navigator.pop(context),
            ),
            if (state == 'ready')
              WButton('Rebuild', icon: 'restart', onPressed: _run),
          ],
        ),
      ],
    );
  }
}
