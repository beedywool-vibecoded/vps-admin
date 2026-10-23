import 'dart:async';

import 'package:flutter/material.dart';

import '../../data/api.dart';
import '../kit.dart';
import '../live.dart';
import '../widgets.dart';
import 'app_forms.dart';
import 'apps.dart';
import 'common.dart';

/// NewAppView.vue: pick a repository, then the branch, access, build and variables.
class NewAppPage extends StatefulWidget {
  const NewAppPage({super.key});

  @override
  State<NewAppPage> createState() => _NewAppPageState();
}

String slugify(String s) {
  final v = s
      .toLowerCase()
      .replaceAll(RegExp('[^a-z0-9]+'), '-')
      .replaceAll(RegExp(r'^-+|-+$'), '');
  return v.length > 40 ? v.substring(0, 40) : v;
}

// Suggests app.example.com from a panel at admin.example.com.
String suggestDomain(String publicUrl, String name) {
  final host = Uri.tryParse(publicUrl)?.host ?? '';
  final parts = host.split('.');
  if (parts.length < 3 || host == 'localhost') return '';
  return '$name.${parts.sublist(1).join('.')}';
}

class _NewAppPageState extends State<NewAppPage> {
  List<Map<String, dynamic>> repos = [];
  Map<String, dynamic>? gh, repo, detection;
  List<String> branches = [];
  String query = '', branch = '', buildType = 'auto', access = 'public';
  bool loadingRepos = true,
      detecting = false,
      saving = false,
      autoDeploy = true;
  List<Map<String, dynamic>> env = [];
  final name = TextEditingController();
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
  Timer? _detectTimer;

  Api get api => Live.read(context).api;

  @override
  void initState() {
    super.initState();
    f['keep']!.text = '5';
    WidgetsBinding.instance.addPostFrameCallback((_) async {
      try {
        final g = await api.github();
        final r = await api.repos();
        if (!mounted) return;
        setState(() {
          gh = g;
          repos = r;
          access = g['domains'] == true ? 'public' : 'local';
        });
      } catch (e) {
        if (mounted) showError(context, e);
      } finally {
        if (mounted) setState(() => loadingRepos = false);
      }
    });
  }

  @override
  void dispose() {
    _detectTimer?.cancel();
    super.dispose();
  }

  String get effectiveBuild =>
      buildType == 'auto' ? (detection?['build'] as String? ?? '') : buildType;
  bool get needsPort =>
      effectiveBuild == 'dockerfile' &&
      (int.tryParse(f['port']!.text) ?? 0) == 0;

  void _pick(Map<String, dynamic> r) {
    final n = slugify((r['fullName'] as String).split('/').last);
    setState(() {
      repo = r;
      name.text = n;
      branch = r['defaultBranch'] as String? ?? 'main';
      branches = [];
      f['rootDir']!.text = '';
      buildType = 'auto';
      f['port']!.text = '';
      f['domain']!.text = gh?['domains'] == true
          ? suggestDomain(gh?['publicUrl'] as String? ?? '', n)
          : '';
    });
    api
        .get(
          '/api/github/branches?installation=${r['installationId']}&repo=${Uri.encodeQueryComponent(r['fullName'] as String)}',
        )
        .then(
          (b) => mounted && repo == r
              ? setState(
                  () => branches = (b['branches'] as List).cast<String>(),
                )
              : null,
        )
        .catchError((_) => null);
    _detect();
  }

  Future<void> _detect() async {
    final r = repo;
    if (r == null) return;
    setState(() => detecting = true);
    try {
      final res = await api.detect(r, branch, f['rootDir']!.text.trim());
      if (!mounted) return;
      final d = res['detection'] as Map<String, dynamic>;
      setState(() {
        detection = d;
        final p = (d['port'] as num?)?.toInt() ?? 0;
        if (p != 0 && (f['port']!.text.isEmpty || buildType == 'auto')) {
          f['port']!.text = '$p';
        }
      });
    } catch (e) {
      if (mounted) {
        setState(
          () => detection = {'build': '', 'port': 0, 'reason': errText(e)},
        );
      }
    } finally {
      if (mounted) setState(() => detecting = false);
    }
  }

  void _redetect([_]) {
    _detectTimer?.cancel();
    _detectTimer = Timer(const Duration(milliseconds: 500), _detect);
  }

  Future<void> _submit() async {
    final r = repo;
    if (r == null) return;
    setState(() => saving = true);
    int n(String k, int def) => int.tryParse(f[k]!.text.trim()) ?? def;
    try {
      final res = await api.createApp({
        'id': name.text.trim(),
        'repo': r['fullName'],
        'branch': branch,
        'rootDir': f['rootDir']!.text.trim(),
        'build': buildType,
        'port': n('port', 0),
        'domain': access == 'public' || access == 'vpn'
            ? f['domain']!.text.trim()
            : '',
        'access': access,
        'allow': f['allow']!.text.trim(),
        'autoDeploy': autoDeploy,
        'keep': n('keep', 5),
        'memoryMb': n('memoryMb', 0),
        for (final k in [
          'installCmd',
          'buildCmd',
          'startCmd',
          'dockerfile',
          'composeFile',
        ])
          k: f[k]!.text.trim(),
        'env': env,
        'deploy': true,
      });
      if (!mounted) return;
      final id = (res['app'] as Map)['id'] as String;
      Navigator.pop(context);
      openApp(context, id);
    } catch (e) {
      if (mounted) showError(context, e);
    } finally {
      if (mounted) setState(() => saving = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    final step = repo == null ? 1 : 2;
    final q = query.trim().toLowerCase();
    final shown = repos
        .where(
          (r) =>
              q.isEmpty || (r['fullName'] as String).toLowerCase().contains(q),
        )
        .toList();
    Widget stepDot(
      int n,
      String label, {
      bool current = false,
      bool done = false,
    }) => Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        Container(
          width: 28,
          height: 28,
          alignment: Alignment.center,
          decoration: BoxDecoration(
            color: current || done ? cs.primary : cs.surfaceContainerHigh,
            shape: BoxShape.circle,
          ),
          child: Text(
            done ? '✓' : '$n',
            style: TextStyle(
              fontWeight: FontWeight.w600,
              color: current || done ? cs.onPrimary : cs.onSurfaceVariant,
            ),
          ),
        ),
        const SizedBox(width: 8),
        Text(
          label,
          style: TextStyle(
            fontWeight: FontWeight.w500,
            color: current ? cs.onSurface : cs.onSurfaceVariant,
          ),
        ),
      ],
    );
    return Scaffold(
      backgroundColor: cs.surface,
      appBar: AppBar(
        leading: IconButton(
          icon: WIcon('back', color: cs.onSurface),
          onPressed: () => Navigator.pop(context),
        ),
        title: const Text('New app'),
      ),
      body: WPage(
        children: [
          Wrap(
            spacing: 20,
            runSpacing: 10,
            children: [
              stepDot(1, 'Source', current: step == 1, done: step > 1),
              stepDot(2, 'Build and settings', current: step == 2),
              stepDot(3, 'Deploy'),
            ],
          ),
          WCard(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              spacing: 14,
              children: [
                Text(
                  'Pick a repository',
                  style: Theme.of(context).textTheme.titleMedium,
                ),
                SearchField(
                  hint: 'Search repositories',
                  onChanged: (v) => setState(() => query = v),
                ),
                if (loadingRepos)
                  const LoadingBlock(label: 'Loading repositories')
                else if (repos.isEmpty)
                  const Muted(
                    'The GitHub App cannot read any repository yet. Choose repositories on GitHub (Apps page), then come back.',
                  ),
                ConstrainedBox(
                  constraints: BoxConstraints(
                    maxHeight: repo == null ? 520 : 240,
                  ),
                  child: ListView(
                    shrinkWrap: true,
                    children: [
                      for (final r in shown)
                        Padding(
                          padding: const EdgeInsets.only(bottom: 6),
                          child: Material(
                            color: repo?['fullName'] == r['fullName']
                                ? cs.primaryContainer
                                : Colors.transparent,
                            shape: RoundedRectangleBorder(
                              borderRadius: BorderRadius.circular(16),
                              side: BorderSide(
                                color: repo?['fullName'] == r['fullName']
                                    ? cs.primary
                                    : Colors.transparent,
                                width: 2,
                              ),
                            ),
                            child: InkWell(
                              borderRadius: BorderRadius.circular(16),
                              onTap: () => _pick(r),
                              child: Container(
                                constraints: const BoxConstraints(
                                  minHeight: 64,
                                ),
                                padding: const EdgeInsets.symmetric(
                                  horizontal: 16,
                                  vertical: 12,
                                ),
                                child: Row(
                                  children: [
                                    Expanded(
                                      child: Column(
                                        crossAxisAlignment:
                                            CrossAxisAlignment.start,
                                        children: [
                                          Text(
                                            (r['fullName'] as String)
                                                .split('/')
                                                .last,
                                            style: const TextStyle(
                                              fontSize: 15,
                                              fontWeight: FontWeight.w500,
                                            ),
                                          ),
                                          Muted(
                                            '${(r['fullName'] as String).split('/').first}${(r['language'] as String? ?? '').isNotEmpty ? ' · ${r['language']}' : ''} · pushed ${agoIso(r['pushedAt'] as String?)}',
                                            maxLines: 1,
                                          ),
                                        ],
                                      ),
                                    ),
                                    if (r['private'] == true)
                                      WIcon(
                                        'lock',
                                        size: 16,
                                        color: cs.onSurfaceVariant,
                                      ),
                                  ],
                                ),
                              ),
                            ),
                          ),
                        ),
                    ],
                  ),
                ),
              ],
            ),
          ),
          if (repo != null) _config(context),
        ],
      ),
    );
  }

  Widget _config(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    final r = repo!;
    final branchOpts = {
      ...[branch, ...branches].where((b) => b.isNotEmpty),
    }.map((b) => (b, b)).toList();
    final reason = detection?['reason'] as String? ?? '';
    return WCard(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        spacing: 18,
        children: [
          Row(
            children: [
              ShapeIcon(M3Shape.cookie, size: 44, color: cs.primaryContainer),
              const SizedBox(width: 14),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      (r['fullName'] as String).split('/').last,
                      style: Theme.of(context).textTheme.titleMedium,
                    ),
                    Muted(
                      '${r['fullName']}${r['private'] == true ? ' · private' : ''}',
                    ),
                  ],
                ),
              ),
            ],
          ),
          ResponsiveGrid(
            minWidth: 220,
            children: [
              FilledField(
                label: 'Name',
                controller: name,
                help: 'Also the container name',
                onChanged: (v) {
                  final s = slugify(v);
                  if (s != v) {
                    name.value = TextEditingValue(
                      text: s,
                      selection: TextSelection.collapsed(offset: s.length),
                    );
                  }
                },
              ),
              SelectField<String>(
                label: 'Branch',
                value: branch,
                options: branchOpts,
                onChanged: (v) {
                  setState(() => branch = v);
                  _detect();
                },
              ),
              FilledField(
                label: 'Root folder',
                controller: f['rootDir']!,
                hint: '/',
                help: 'For a monorepo, the folder of this app',
                onChanged: _redetect,
              ),
            ],
          ),
          Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            spacing: 8,
            children: [
              Text('Access', style: Theme.of(context).textTheme.titleSmall),
              AccessPicker(
                api: api,
                access: access,
                onAccess: (v) => setState(() => access = v),
                domain: f['domain']!,
                allow: f['allow']!,
              ),
            ],
          ),
          Container(
            padding: const EdgeInsets.all(14),
            decoration: BoxDecoration(
              color: cs.surfaceContainerHigh,
              borderRadius: BorderRadius.circular(20),
            ),
            child: Wrap(
              spacing: 14,
              runSpacing: 12,
              crossAxisAlignment: WrapCrossAlignment.center,
              children: [
                ShapeIcon(
                  M3Shape.clover,
                  size: 36,
                  color: cs.secondaryContainer,
                ),
                ConstrainedBox(
                  constraints: const BoxConstraints(maxWidth: 420),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        detecting
                            ? 'Looking at the repository…'
                            : (detection?['build'] as String? ?? '').isNotEmpty
                            ? 'Detected: ${buildLabel[detection!['build']]}'
                            : 'Build',
                        style: Theme.of(context).textTheme.titleSmall,
                      ),
                      if (reason.isNotEmpty) Muted(reason),
                    ],
                  ),
                ),
                SizedBox(
                  width: 240,
                  child: SelectField<String>(
                    label: 'Build',
                    value: buildType,
                    options: [
                      for (final e in buildLabel.entries) (e.key, e.value),
                    ],
                    onChanged: (v) => setState(() => buildType = v),
                  ),
                ),
              ],
            ),
          ),
          ResponsiveGrid(
            minWidth: 220,
            children: [
              FilledField(
                label: effectiveBuild == 'compose'
                    ? 'Published port to route the domain to'
                    : 'Port the app listens on',
                controller: f['port']!,
                number: true,
                onChanged: (_) => setState(() {}),
                help: effectiveBuild == 'compose'
                    ? 'A port your compose file publishes on 127.0.0.1'
                    : needsPort
                    ? 'Required: the Dockerfile has no EXPOSE line'
                    : 'Inside the container',
              ),
              FilledField(
                label: 'Memory limit (MiB)',
                controller: f['memoryMb']!,
                hint: '0',
                number: true,
                help: '0 for no limit',
              ),
            ],
          ),
          CommandsFields(ctl: f, build: effectiveBuild),
          Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            spacing: 8,
            children: [
              Text(
                'Environment variables',
                style: Theme.of(context).textTheme.titleSmall,
              ),
              EnvEditor(vars: env, onChanged: (v) => setState(() => env = v)),
            ],
          ),
          Row(
            children: [
              const Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text('Deploy on every push'),
                    Muted(
                      'GitHub notifies the server; the new version replaces the old one without downtime',
                    ),
                  ],
                ),
              ),
              Switch(
                value: autoDeploy,
                onChanged: (v) => setState(() => autoDeploy = v),
              ),
            ],
          ),
          Row(
            children: [
              const Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text('Versions kept for rollback'),
                    Muted('Older images are removed after each deploy'),
                  ],
                ),
              ),
              SizedBox(
                width: 90,
                child: FilledField(
                  label: 'Versions',
                  controller: f['keep']!,
                  number: true,
                ),
              ),
            ],
          ),
          Align(
            alignment: Alignment.centerRight,
            child: WButton(
              saving ? 'Creating…' : 'Deploy',
              large: true,
              onPressed:
                  saving ||
                      name.text.isEmpty ||
                      branch.isEmpty ||
                      needsPort ||
                      (effectiveBuild.isEmpty && !detecting)
                  ? null
                  : _submit,
            ),
          ),
        ],
      ),
    );
  }
}
