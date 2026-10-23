import 'dart:async';

import 'package:flutter/material.dart';

import '../../data/api.dart';
import '../../data/updater.dart';
import '../kit.dart';
import '../live.dart';
import '../widgets.dart';
import 'app_forms.dart';
import 'common.dart';
import 'new_app.dart';

const buildLabel = {
  'auto': 'Detect automatically',
  'dockerfile': 'Dockerfile',
  'compose': 'Docker Compose',
  'node': 'Node app',
  'static': 'Static site',
};
const statusLabel = {
  'queued': 'Queued',
  'building': 'Building',
  'starting': 'Starting',
  'live': 'Live',
  'failed': 'Failed',
  'superseded': 'Previous',
  'cancelled': 'Cancelled',
};

Tone depTone(Map<String, dynamic>? d) {
  if (d == null) return Tone.idle;
  return switch (d['status']) {
    'live' => Tone.ok,
    'failed' => Tone.bad,
    'superseded' || 'cancelled' => Tone.idle,
    _ => Tone.warn,
  };
}

Tone appTone(String status, Map<String, dynamic>? latest) {
  if (latest != null &&
      const ['queued', 'building', 'starting'].contains(latest['status'])) {
    return Tone.warn;
  }
  if (status == 'running') return Tone.ok;
  if (const ['degraded', 'restarting', 'missing'].contains(status)) {
    return Tone.bad;
  }
  return Tone.idle;
}

void openApp(BuildContext context, String id) => Navigator.push(
  context,
  MaterialPageRoute(
    builder: (_) => Live(
      session: Live.read(context),
      child: AppDetailPage(id: id),
    ),
  ),
);

/// AppsView.vue: GitHub connection, then the apps with their state and last deployment.
class AppsPage extends StatefulWidget {
  const AppsPage({super.key});

  @override
  State<AppsPage> createState() => _AppsPageState();
}

class _AppsPageState extends State<AppsPage> {
  List<Map<String, dynamic>> apps = [];
  Map<String, dynamic>? gh;
  bool loading = true;
  Timer? _timer;

  Api get api => Live.read(context).api;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) => _load());
    _timer = Timer.periodic(const Duration(seconds: 10), (_) => _load());
  }

  @override
  void dispose() {
    _timer?.cancel();
    super.dispose();
  }

  Future<void> _load() async {
    try {
      final a = await api.appsRaw();
      final g = await api.github();
      if (!mounted) return;
      setState(() {
        apps = (a['apps'] as List).cast<Map<String, dynamic>>();
        gh = g;
      });
    } catch (e) {
      if (mounted) showError(context, e);
    } finally {
      if (mounted) setState(() => loading = false);
    }
  }

  Future<void> _disconnect() async {
    if (!await wConfirm(
      context,
      title: 'Disconnect GitHub?',
      body:
          'Apps keep running, but cannot be redeployed until GitHub is connected again. Delete the App on GitHub too if you no longer need it.',
      action: 'Disconnect',
      danger: true,
    )) {
      return;
    }
    try {
      await api.delete('/api/github');
      await _load();
    } catch (e) {
      if (mounted) showError(context, e);
    }
  }

  void _newApp() => Navigator.push(
    context,
    MaterialPageRoute(
      builder: (_) =>
          Live(session: Live.read(context), child: const NewAppPage()),
    ),
  ).then((_) => _load());

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    final connected = gh?['connected'] == true;
    final app = gh?['app'] as Map<String, dynamic>?;
    final inst =
        (gh?['installations'] as List?)?.cast<Map<String, dynamic>>() ?? [];
    final shapes = M3Shape.values;
    return WPage(
      onRefresh: _load,
      children: [
        PageTitle(
          'Apps',
          actions: [
            if (connected)
              WButton('New app', icon: 'plus', large: true, onPressed: _newApp),
          ],
        ),
        if (gh != null && !connected)
          WCard(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              spacing: 12,
              children: [
                WIcon('github', size: 40, color: cs.onSurface),
                Text(
                  'Deploy from GitHub',
                  style: Theme.of(context).textTheme.titleMedium,
                ),
                const Muted(
                  'Creates a private GitHub App for this server in one click: it can only read the repositories you choose, and tells the server when you push so it redeploys by itself. GitHub then lets you pick your account or any of your organizations.',
                  size: 15,
                ),
                const Muted(
                  'GitHub needs a browser for this step: it opens the panel, where you press Connect GitHub.',
                ),
                WButton(
                  'Connect in the browser',
                  icon: 'github',
                  large: true,
                  onPressed: () => Updater.openUrl(
                    '${(gh!['publicUrl'] as String?) ?? api.base}/apps',
                  ),
                ),
              ],
            ),
          )
        else if (connected)
          WCard(
            child: Wrap(
              spacing: 12,
              runSpacing: 8,
              crossAxisAlignment: WrapCrossAlignment.center,
              children: [
                WIcon('github', size: 28, color: cs.onSurface),
                Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    Text(
                      app?['owner'] as String? ?? '',
                      style: const TextStyle(
                        fontSize: 16,
                        fontWeight: FontWeight.w500,
                      ),
                    ),
                    Muted(
                      '${app?['name'] ?? ''} · ${inst.isNotEmpty ? 'installed on ${inst.map((i) => i['account']).join(', ')}' : 'not installed on any account yet'}',
                    ),
                  ],
                ),
                TextButton(
                  onPressed: () =>
                      Updater.openUrl(app?['installUrl'] as String? ?? ''),
                  child: const Text('Choose repositories'),
                ),
                TextButton(
                  onPressed: _disconnect,
                  child: const Text('Disconnect'),
                ),
              ],
            ),
          ),
        if (gh != null && gh!['domains'] != true)
          const Muted(
            'No reverse proxy (Caddy or nginx) was set up by the installer, so apps get no public domain. They can still be served on Tailscale or stay on 127.0.0.1.',
          ),
        if (loading)
          const LoadingBlock(label: 'Loading apps')
        else if (apps.isEmpty && connected)
          EmptyCard(
            shape: M3Shape.flower,
            text:
                'No app yet. Pick a repository and it is online in a few minutes.',
            child: WButton('New app', kind: BtnKind.tonal, onPressed: _newApp),
          ),
        for (var i = 0; i < apps.length; i++)
          Builder(
            builder: (context) {
              final a = apps[i];
              final latest = a['latest'] as Map<String, dynamic>?;
              final url = a['url'] as String? ?? '';
              return WCard(
                onTap: () => openApp(context, a['id'] as String),
                child: Row(
                  children: [
                    ShapeIcon(
                      shapes[i % shapes.length],
                      size: 48,
                      color: i.isOdd
                          ? cs.secondaryContainer
                          : cs.primaryContainer,
                    ),
                    const SizedBox(width: 16),
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Wrap(
                            spacing: 8,
                            runSpacing: 4,
                            crossAxisAlignment: WrapCrossAlignment.center,
                            children: [
                              StatusDot(
                                appTone(a['status'] as String? ?? '', latest),
                                label: a['status'] as String? ?? '',
                              ),
                              Text(
                                a['id'] as String,
                                style: const TextStyle(
                                  fontSize: 18,
                                  fontWeight: FontWeight.w500,
                                ),
                              ),
                              if ((a['dir'] as String? ?? '').isNotEmpty)
                                const WChip('Existing folder', icon: 'folder'),
                            ],
                          ),
                          Muted(
                            '${a['repo']} · ${a['branch']} · ${buildLabel[a['build']] ?? a['build']}${url.isNotEmpty ? ' · ${url.replaceFirst('https://', '')}' : ''}',
                            maxLines: 2,
                          ),
                          const SizedBox(height: 6),
                          if (latest != null)
                            Row(
                              children: [
                                WChip(
                                  statusLabel[latest['status']] ??
                                      '${latest['status']}',
                                ),
                                const SizedBox(width: 8),
                                Flexible(
                                  child: Muted(
                                    '${(latest['sha'] as String).padRight(7).substring(0, 7)} · ${agoIso(latest['createdAt'] as String?)}',
                                    maxLines: 1,
                                  ),
                                ),
                              ],
                            )
                          else
                            const Muted('Never deployed'),
                        ],
                      ),
                    ),
                    WIcon('chevron-right', color: cs.onSurfaceVariant),
                  ],
                ),
              );
            },
          ),
      ],
    );
  }
}

/// AppDetailView.vue: header with actions, update check, then deployments (with the live
/// build log), environment and settings tabs.
class AppDetailPage extends StatefulWidget {
  const AppDetailPage({super.key, required this.id});
  final String id;

  @override
  State<AppDetailPage> createState() => _AppDetailPageState();
}

class _AppDetailPageState extends State<AppDetailPage> {
  Map<String, dynamic>? app;
  List<Map<String, dynamic>> deployments = [], env = [];
  List<String> branches = [];
  int tab = 0;
  int? selected;
  String log = '';
  bool busy = false, checking = false, deleteVolumes = false;
  // The list shows the latest deployments; older ones are one tap away.
  static const deployLimit = 5;
  bool showAllDeps = false;
  Map<String, dynamic>? check;
  StreamSubscription<(String, dynamic)>? _stream;
  Timer? _timer;
  final _logScroll = ScrollController();
  // Settings form
  final f = <String, TextEditingController>{
    for (final k in [
      'rootDir',
      'port',
      'memoryMb',
      'keep',
      'domain',
      'allow',
      'installCmd',
      'buildCmd',
      'startCmd',
      'dockerfile',
      'composeFile',
    ])
      k: TextEditingController(),
  };
  String branch = '', buildType = 'auto', access = 'public';
  bool autoDeploy = true, formDirty = false, envDirty = false;
  // The variables go to the app's environment, or to a file compose reads.
  bool envFile = false;
  final _envPath = TextEditingController();

  Api get api => Live.read(context).api;
  bool get running => deployments.any(
    (d) => const ['queued', 'building', 'starting'].contains(d['status']),
  );

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) => _load(first: true));
    _timer = Timer.periodic(const Duration(seconds: 5), (_) {
      if (running) _load();
    });
  }

  @override
  void dispose() {
    _timer?.cancel();
    _stream?.cancel();
    _logScroll.dispose();
    _envPath.dispose();
    super.dispose();
  }

  Future<void> _load({bool first = false}) async {
    try {
      final r = await api.appFull(widget.id);
      if (!mounted) return;
      final a = r['app'] as Map<String, dynamic>;
      setState(() {
        app = a;
        deployments = (r['deployments'] as List).cast<Map<String, dynamic>>();
        if (!envDirty) {
          env = [
            for (final e in (r['env'] as List).cast<Map<String, dynamic>>())
              {...e, 'keep': e['secret'] == true && e['hasValue'] == true},
          ];
          envFile = a['envFile'] == true;
          _envPath.text = a['envPath'] as String? ?? '';
        }
        if (!formDirty) {
          branch = a['branch'] as String? ?? '';
          buildType = a['build'] as String? ?? 'auto';
          access = a['accessMode'] as String? ?? 'public';
          autoDeploy = a['autoDeploy'] == true;
          for (final k in f.keys) {
            f[k]!.text = '${a[k] ?? ''}';
          }
        }
      });
      if (first) {
        if (deployments.isNotEmpty) _select(deployments.first['id'] as int);
        api
            .get(
              '/api/github/branches?installation=${a['installationId']}&repo=${Uri.encodeQueryComponent(a['repo'] as String)}',
            )
            .then(
              (b) => mounted
                  ? setState(
                      () => branches = (b['branches'] as List).cast<String>(),
                    )
                  : null,
            )
            .catchError((_) => null);
      }
    } catch (e) {
      if (mounted) showError(context, e);
    }
  }

  void _select(int id) {
    _stream?.cancel();
    setState(() {
      selected = id;
      log = '';
    });
    _stream = api
        .events('/api/apps/${widget.id}/deployments/$id/stream')
        .listen((ev) {
          if (!mounted) return;
          if (ev.$1 == 'log') {
            final atBottom =
                !_logScroll.hasClients ||
                _logScroll.position.maxScrollExtent - _logScroll.offset < 60;
            setState(() => log += ev.$2 as String);
            if (atBottom) {
              WidgetsBinding.instance.addPostFrameCallback((_) {
                if (_logScroll.hasClients) {
                  _logScroll.jumpTo(_logScroll.position.maxScrollExtent);
                }
              });
            }
          } else if (ev.$1 == 'status') {
            final d = (ev.$2 as Map).cast<String, dynamic>();
            final i = deployments.indexWhere((x) => x['id'] == d['id']);
            if (i >= 0) setState(() => deployments[i] = d);
          } else if (ev.$1 == 'end') {
            _load();
          }
        }, onError: (_) {});
  }

  Future<void> _action(
    Future<Map<String, dynamic>?> Function() fn,
    String done,
  ) async {
    setState(() => busy = true);
    try {
      final res = await fn();
      if (mounted) showInfo(context, done);
      await _load();
      if (res != null && res['id'] is int) _select(res['id'] as int);
    } catch (e) {
      if (mounted) showError(context, e);
    } finally {
      if (mounted) setState(() => busy = false);
    }
  }

  Future<void> _redeploy() => _action(
    () => api.post('/api/apps/${widget.id}/deploy'),
    'Deployment started',
  );
  Future<void> _restart() => _action(() async {
    await api.restartApp(widget.id);
    return null;
  }, 'Restarted');

  Future<void> _rollback(Map<String, dynamic> d) async {
    if (await wConfirm(
      context,
      title: 'Roll back to ${(d['sha'] as String).substring(0, 7)}?',
      body:
          '"${d['message']}" goes live again${app?['build'] == 'compose' ? ' (rebuilt from that commit)' : ', without rebuilding'}. Automatic deploys stay on: the next push deploys again.',
      action: 'Roll back',
    )) {
      _action(
        () => api.post('/api/apps/${widget.id}/rollback/${d['id']}'),
        'Rollback started',
      );
    }
  }

  Future<void> _checkUpdates() async {
    setState(() => checking = true);
    try {
      final c = await api.get('/api/apps/${widget.id}/check');
      if (mounted) setState(() => check = c);
    } catch (e) {
      if (mounted) showError(context, e);
    } finally {
      if (mounted) setState(() => checking = false);
    }
  }

  Future<void> _saveEnv() async {
    setState(() => busy = true);
    try {
      await api.setEnv(
        widget.id,
        env,
        envFile: envFile,
        envPath: _envPath.text.trim(),
      );
      envDirty = false;
      await _load();
      if (mounted &&
          await wConfirm(
            context,
            title: 'Variables saved',
            body: 'They apply at the next deployment. Redeploy now?',
            action: 'Redeploy',
          )) {
        _redeploy();
      }
    } catch (e) {
      if (mounted) showError(context, e);
    } finally {
      if (mounted) setState(() => busy = false);
    }
  }

  Future<void> _saveSettings() async {
    setState(() => busy = true);
    int n(String k, int def) => int.tryParse(f[k]!.text.trim()) ?? def;
    try {
      await api.put('/api/apps/${widget.id}', {
        'branch': branch,
        'rootDir': f['rootDir']!.text.trim(),
        'build': buildType,
        'port': n('port', 0),
        'domain': access == 'public' || access == 'vpn'
            ? f['domain']!.text.trim()
            : '',
        'autoDeploy': autoDeploy,
        'keep': n('keep', 5),
        'memoryMb': n('memoryMb', 0),
        'access': access,
        'allow': f['allow']!.text.trim(),
        for (final k in [
          'installCmd',
          'buildCmd',
          'startCmd',
          'dockerfile',
          'composeFile',
        ])
          k: f[k]!.text.trim(),
      });
      formDirty = false;
      if (mounted) {
        showInfo(context, 'Settings saved, they apply at the next deployment');
      }
      await _load();
    } catch (e) {
      if (mounted) showError(context, e);
    } finally {
      if (mounted) setState(() => busy = false);
    }
  }

  Future<void> _remove() async {
    final linked = (app?['dir'] as String? ?? '').isNotEmpty;
    if (!await wConfirm(
      context,
      title: linked ? 'Unlink ${widget.id}?' : 'Delete ${widget.id}?',
      body: linked
          ? 'Pushes stop updating ${app!['dir']}. The folder and its containers stay as they are.'
          : 'Stops and removes its containers, images, proxy site and files${deleteVolumes ? ', and its data volumes' : '. Named volumes are kept'}. The repository is not touched.',
      action: linked ? 'Unlink' : 'Delete',
      danger: !linked,
    )) {
      return;
    }
    setState(() => busy = true);
    try {
      await api.deleteApp(widget.id, volumes: deleteVolumes);
      if (!mounted) return;
      showInfo(context, '${widget.id} ${linked ? 'unlinked' : 'deleted'}');
      Navigator.pop(context);
    } catch (e) {
      if (mounted) {
        showError(context, e);
        setState(() => busy = false);
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    final a = app;
    return Scaffold(
      backgroundColor: cs.surface,
      appBar: AppBar(
        leading: IconButton(
          icon: WIcon('back', color: cs.onSurface),
          onPressed: () => Navigator.pop(context),
        ),
        title: Text(widget.id),
      ),
      body: a == null
          ? const Center(child: PeloteLoader(label: 'Loading app'))
          : WPage(
              onRefresh: _load,
              children: [
                _header(context, a),
                if (check != null) _checkBanner(context, a),
                Row(
                  children: [
                    for (final (i, label) in [
                      (0, 'Deployments'),
                      (1, 'Environment'),
                      (2, 'Settings'),
                    ])
                      InkWell(
                        onTap: () => setState(() => tab = i),
                        child: Container(
                          padding: const EdgeInsets.symmetric(
                            horizontal: 16,
                            vertical: 12,
                          ),
                          decoration: BoxDecoration(
                            border: Border(
                              bottom: BorderSide(
                                color: tab == i
                                    ? cs.primary
                                    : Colors.transparent,
                                width: 3,
                              ),
                            ),
                          ),
                          child: Text(
                            label,
                            style: TextStyle(
                              fontWeight: FontWeight.w500,
                              color: tab == i
                                  ? cs.primary
                                  : cs.onSurfaceVariant,
                            ),
                          ),
                        ),
                      ),
                  ],
                ),
                if (tab == 0) ..._deployments(context, a),
                if (tab == 1) _envCard(context),
                if (tab == 2) ..._settings(context, a),
              ],
            ),
    );
  }

  Widget _header(BuildContext context, Map<String, dynamic> a) {
    final cs = Theme.of(context).colorScheme;
    final latest = deployments.isNotEmpty ? deployments.first : null;
    final live = a['live'] as Map<String, dynamic>?;
    final url = a['url'] as String? ?? '';
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      spacing: 12,
      children: [
        Row(
          children: [
            ShapeIcon(M3Shape.cookie, size: 52, color: cs.primaryContainer),
            const SizedBox(width: 14),
            Expanded(
              child: Wrap(
                spacing: 14,
                runSpacing: 4,
                crossAxisAlignment: WrapCrossAlignment.center,
                children: [
                  Row(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      StatusDot(
                        appTone(a['status'] as String? ?? '', latest),
                        label: a['status'] as String? ?? '',
                      ),
                      const SizedBox(width: 8),
                      Text(
                        running ? 'Deploying' : a['status'] as String? ?? '',
                        style: TextStyle(
                          fontSize: 13,
                          color: cs.onSurfaceVariant,
                        ),
                      ),
                    ],
                  ),
                  if (url.isNotEmpty)
                    InkWell(
                      onTap: () => Updater.openUrl(url),
                      child: Text(
                        url.replaceFirst('https://', ''),
                        style: TextStyle(fontSize: 13, color: cs.primary),
                      ),
                    ),
                  Row(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      WIcon('github', size: 14, color: cs.onSurfaceVariant),
                      const SizedBox(width: 4),
                      Muted('${a['repo']} · '),
                      WIcon('branch', size: 14, color: cs.onSurfaceVariant),
                      const SizedBox(width: 4),
                      Muted('${a['branch']}'),
                    ],
                  ),
                  Muted(
                    buildLabel[live?['build'] ??
                            latest?['build'] ??
                            a['build']] ??
                        '${a['build']}',
                  ),
                  if ((live?['hostPort'] ?? 0) != 0)
                    Muted('127.0.0.1:${live!['hostPort']}'),
                  if ((a['dir'] as String? ?? '').isNotEmpty)
                    Muted('${a['dir']}', mono: true),
                ],
              ),
            ),
          ],
        ),
        Wrap(
          spacing: 8,
          runSpacing: 8,
          children: [
            WButton(
              checking ? 'Checking…' : 'Check for updates',
              kind: BtnKind.text,
              onPressed: checking ? null : _checkUpdates,
            ),
            WButton(
              'Restart',
              kind: BtnKind.outlined,
              onPressed: busy || live == null ? null : _restart,
            ),
            WButton('Redeploy', onPressed: busy || running ? null : _redeploy),
          ],
        ),
      ],
    );
  }

  Widget _checkBanner(BuildContext context, Map<String, dynamic> a) {
    final cs = Theme.of(context).colorScheme;
    final c = check!;
    final fresh = c['upToDate'] != true;
    String short(Object? s) => '${s ?? ''}'.padRight(7).substring(0, 7).trim();
    return WCard(
      color: fresh ? cs.tertiaryContainer : null,
      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 12),
      child: Row(
        children: [
          WIcon(
            fresh ? 'updates' : 'check',
            size: 20,
            color: fresh ? cs.onTertiaryContainer : cs.onSurface,
          ),
          const SizedBox(width: 12),
          Expanded(
            child: Text(
              fresh
                  ? 'New commit on ${a['branch']}: ${short((c['latest'] as Map)['sha'])} ${(c['latest'] as Map)['message']} (running ${short(c['current']).isEmpty ? 'nothing' : short(c['current'])})'
                  : 'Up to date with ${a['branch']}: ${short(c['current'])}',
              style: TextStyle(
                fontSize: 13,
                color: fresh ? cs.onTertiaryContainer : cs.onSurface,
              ),
            ),
          ),
          if (fresh)
            WButton(
              'Update now',
              onPressed: busy || running
                  ? null
                  : () {
                      setState(() => check = null);
                      _redeploy();
                    },
            ),
          WIconButton(
            'close',
            tooltip: 'Dismiss',
            size: 18,
            onPressed: () => setState(() => check = null),
          ),
        ],
      ),
    );
  }

  void _toggleDeps() {
    setState(() => showAllDeps = !showAllDeps);
    // Collapsing hides the older rows, so keep the log on a deployment that stays listed.
    if (!showAllDeps &&
        selected != null &&
        deployments.isNotEmpty &&
        !deployments.take(deployLimit).any((d) => d['id'] == selected)) {
      _select(deployments.first['id'] as int);
    }
  }

  List<Widget> _deployments(BuildContext context, Map<String, dynamic> a) {
    final cs = Theme.of(context).colorScheme;
    final sel = deployments.where((d) => d['id'] == selected).firstOrNull;
    final shown = showAllDeps
        ? deployments
        : deployments.take(deployLimit).toList();
    return [
      WCard(
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            CardHead(
              'Deployments',
              trailing: [
                if (a['autoDeploy'] == true) const WChip('Auto-deploy on push'),
              ],
            ),
            if (deployments.isEmpty)
              const Muted('No deployment yet.', size: 15),
            for (final d in shown)
              Container(
                margin: const EdgeInsets.only(bottom: 4),
                decoration: BoxDecoration(
                  color: d['id'] == selected ? cs.surfaceContainerHigh : null,
                  borderRadius: BorderRadius.circular(16),
                ),
                child: InkWell(
                  borderRadius: BorderRadius.circular(16),
                  onTap: () => _select(d['id'] as int),
                  child: Padding(
                    padding: const EdgeInsets.symmetric(
                      horizontal: 12,
                      vertical: 10,
                    ),
                    child: Row(
                      children: [
                        StatusDot(depTone(d), label: d['status'] as String),
                        const SizedBox(width: 10),
                        Expanded(
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              Row(
                                children: [
                                  Text(
                                    statusLabel[d['status']] ??
                                        '${d['status']}',
                                    style: const TextStyle(
                                      fontSize: 14,
                                      fontWeight: FontWeight.w500,
                                    ),
                                  ),
                                  const SizedBox(width: 8),
                                  Text(
                                    (d['sha'] as String).isEmpty
                                        ? '…'
                                        : (d['sha'] as String).substring(0, 7),
                                    style: cmdStyle(context, fontSize: 12),
                                  ),
                                ],
                              ),
                              Muted(
                                '${(d['message'] as String? ?? '').isEmpty ? d['trigger'] : d['message']} · ${agoIso(d['createdAt'] as String?)}',
                                maxLines: 1,
                              ),
                            ],
                          ),
                        ),
                        if (d['status'] == 'superseded')
                          WButton(
                            'Roll back',
                            kind: BtnKind.outlined,
                            onPressed: busy || running
                                ? null
                                : () => _rollback(d),
                          ),
                      ],
                    ),
                  ),
                ),
              ),
            if (deployments.length > deployLimit)
              Align(
                alignment: Alignment.centerLeft,
                child: WButton(
                  showAllDeps
                      ? 'Show fewer'
                      : 'Show all ${deployments.length} deployments',
                  kind: BtnKind.text,
                  icon: 'chevron',
                  onPressed: _toggleDeps,
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
              sel == null
                  ? 'Build log'
                  : 'Build log · ${(sel['sha'] as String).padRight(7).substring(0, 7)}',
              sub: sel == null
                  ? null
                  : '${sel['trigger']} · ${dateTime(sel['createdAt'] as String)}',
            ),
            if ((sel?['error'] as String? ?? '').isNotEmpty)
              Container(
                margin: const EdgeInsets.only(bottom: 10),
                padding: const EdgeInsets.symmetric(
                  horizontal: 12,
                  vertical: 10,
                ),
                decoration: BoxDecoration(
                  color: cs.errorContainer,
                  borderRadius: BorderRadius.circular(12),
                ),
                child: Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    WIcon('alert', size: 16, color: cs.onErrorContainer),
                    const SizedBox(width: 8),
                    Expanded(
                      child: Text(
                        sel!['error'] as String,
                        style: TextStyle(
                          fontSize: 13,
                          color: cs.onErrorContainer,
                        ),
                      ),
                    ),
                  ],
                ),
              ),
            OutputBox(
              log.isEmpty
                  ? (selected != null
                        ? 'Waiting for output…'
                        : 'Select a deployment')
                  : log,
              height: 460,
              controller: _logScroll,
            ),
          ],
        ),
      ),
    ];
  }

  // Only a compose stack reads a file: another build gets the variables directly.
  bool get composeApp {
    final persisted = app?['build'] as String? ?? '';
    final live =
        (app?['live'] as Map<String, dynamic>?)?['build'] as String? ?? '';
    return persisted == 'compose' ||
        buildType == 'compose' ||
        (buildType == 'auto' && live == 'compose');
  }

  Widget _envCard(BuildContext context) => WCard(
    child: Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        const CardHead(
          'Environment variables',
          sub:
              'Encrypted on the server · secrets are never sent back to the panel',
        ),
        EnvEditor(
          vars: env,
          onChanged: (v) => setState(() {
            env = v;
            envDirty = true;
          }),
        ),
        const SizedBox(height: 16),
        if (composeApp)
          Row(
            children: [
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    const Text('Write a .env file'),
                    Muted(
                      envFile
                          ? 'The variables go into a file in the deployed folder instead of the app environment.'
                          : 'The variables are handed to the app and to compose as environment variables.',
                    ),
                  ],
                ),
              ),
              Switch(
                value: envFile,
                onChanged: (v) => setState(() {
                  envFile = v;
                  envDirty = true;
                }),
              ),
            ],
          ),
        if (composeApp && envFile)
          Padding(
            padding: const EdgeInsets.only(top: 8),
            child: FilledField(
              label: 'Path of the file',
              controller: _envPath,
              hint: '.env',
              mono: true,
              help:
                  "Relative to the app's folder, for example .env or config/.env. It is rewritten at each deployment and never committed.",
              onChanged: (_) => envDirty = true,
            ),
          ),
        const SizedBox(height: 12),
        Align(
          alignment: Alignment.centerRight,
          child: WButton('Save', onPressed: busy ? null : _saveEnv),
        ),
      ],
    ),
  );

  List<Widget> _settings(BuildContext context, Map<String, dynamic> a) {
    final cs = Theme.of(context).colorScheme;
    final linked = (a['dir'] as String? ?? '').isNotEmpty;
    final live = a['live'] as Map<String, dynamic>?;
    final effective = buildType == 'auto'
        ? (live?['build'] as String? ?? '')
        : buildType;
    void dirty([_]) => formDirty = true;
    final branchOpts = {
      ...[branch, ...branches].where((b) => b.isNotEmpty),
    }.map((b) => (b, b)).toList();
    return [
      WCard(
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          spacing: 12,
          children: [
            const CardHead('Settings'),
            ResponsiveGrid(
              minWidth: 220,
              children: [
                SelectField<String>(
                  label: 'Branch',
                  value: branch,
                  options: branchOpts,
                  onChanged: (v) => setState(() {
                    branch = v;
                    dirty();
                  }),
                ),
                FilledField(
                  label: 'Root folder',
                  controller: f['rootDir']!,
                  hint: '/',
                  onChanged: dirty,
                ),
                FilledField(
                  label: 'Port',
                  controller: f['port']!,
                  number: true,
                  onChanged: dirty,
                ),
                if (!linked) ...[
                  FilledField(
                    label: 'Memory limit (MiB, 0 for none)',
                    controller: f['memoryMb']!,
                    number: true,
                    onChanged: dirty,
                  ),
                  FilledField(
                    label: 'Versions kept',
                    controller: f['keep']!,
                    number: true,
                    onChanged: dirty,
                  ),
                  SelectField<String>(
                    label: 'Build',
                    value: buildType,
                    options: [
                      for (final e in buildLabel.entries) (e.key, e.value),
                    ],
                    onChanged: (v) => setState(() {
                      buildType = v;
                      dirty();
                    }),
                  ),
                ],
              ],
            ),
            Text('Access', style: Theme.of(context).textTheme.titleSmall),
            AccessPicker(
              api: api,
              access: access,
              onAccess: (v) => setState(() {
                access = v;
                dirty();
              }),
              domain: f['domain']!,
              allow: f['allow']!,
              tsPort: (a['tsPort'] as num?)?.toInt() ?? 0,
            ),
            CommandsFields(ctl: f, build: linked ? 'compose' : effective),
            Row(
              children: [
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        linked
                            ? 'Update the folder on every push'
                            : 'Deploy on every push',
                      ),
                      Muted('On $branch'),
                    ],
                  ),
                ),
                Switch(
                  value: autoDeploy,
                  onChanged: (v) => setState(() {
                    autoDeploy = v;
                    dirty();
                  }),
                ),
              ],
            ),
            Align(
              alignment: Alignment.centerRight,
              child: WButton('Save', onPressed: busy ? null : _saveSettings),
            ),
          ],
        ),
      ),
      Container(
        padding: const EdgeInsets.fromLTRB(22, 20, 22, 20),
        decoration: BoxDecoration(
          color: cs.surfaceContainer,
          borderRadius: BorderRadius.circular(28),
          border: Border.all(color: cs.error.withValues(alpha: .6)),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          spacing: 12,
          children: [
            Text(
              linked ? 'Unlink from GitHub' : 'Delete app',
              style: TextStyle(
                fontSize: 20,
                fontWeight: FontWeight.w500,
                color: cs.error,
              ),
            ),
            Muted(
              linked
                  ? 'Pushes stop updating ${a['dir']}. Nothing is deleted: the folder and its containers keep running.'
                  : 'Stops and removes the containers, images, the proxy site and the source folder. Named volumes are kept unless you tick the box.',
            ),
            if (!linked)
              Row(
                children: [
                  Checkbox(
                    value: deleteVolumes,
                    onChanged: (v) => setState(() => deleteVolumes = v == true),
                  ),
                  const Text('Also delete its data volumes'),
                ],
              ),
            WButton(
              linked ? 'Unlink ${widget.id}' : 'Delete ${widget.id}',
              kind: linked ? BtnKind.outlined : BtnKind.danger,
              onPressed: busy || running ? null : _remove,
            ),
          ],
        ),
      ),
    ];
  }
}
