import 'dart:async';

import 'package:flutter/material.dart';

import '../../data/api.dart';
import '../kit.dart';
import '../live.dart';
import '../widgets.dart';
import 'common.dart';

String scriptTitle(Map<String, dynamic> s) {
  final t = s['title'] as String? ?? '';
  return t.isNotEmpty ? t : s['name'] as String;
}

/// Follows a script's output: (output chunk) and (end with exit code).
StreamSubscription<(String, dynamic)> followScript(
  Api api,
  String name, {
  required void Function(String) output,
  required void Function(Map<String, dynamic>) end,
}) =>
    api.events('/api/scripts/${Uri.encodeComponent(name)}/stream').listen((ev) {
      if (ev.$1 == 'output') output(ev.$2 as String);
      if (ev.$1 == 'end') end((ev.$2 as Map).cast<String, dynamic>());
    }, onError: (_) {});

/// ScriptRunDialog.vue: arguments, Run, then the live output with Stop.
Future<void> runScriptDialog(
  BuildContext context,
  Api api,
  Map<String, dynamic> script, {
  Future<void> Function()? onDone,
}) => showWDialog(
  context,
  title: script['title'] != null && (script['title'] as String).isNotEmpty
      ? '${script['title']} (${script['name']})'
      : script['name'] as String,
  wide: true,
  body: (_) => _RunBody(api: api, script: script, onDone: onDone),
);

class _RunBody extends StatefulWidget {
  const _RunBody({required this.api, required this.script, this.onDone});
  final Api api;
  final Map<String, dynamic> script;
  final Future<void> Function()? onDone;

  @override
  State<_RunBody> createState() => _RunBodyState();
}

class _RunBodyState extends State<_RunBody> {
  late String state = widget.script['running'] == true ? 'running' : 'ready';
  String out = '';
  Map<String, dynamic> result = {};
  final args = TextEditingController();
  final _scroll = ScrollController();
  StreamSubscription<(String, dynamic)>? _sub;

  String get name => widget.script['name'] as String;

  @override
  void initState() {
    super.initState();
    if (state == 'running') _follow();
  }

  void _follow() {
    _sub = followScript(
      widget.api,
      name,
      output: (c) {
        setState(() => out += c);
        WidgetsBinding.instance.addPostFrameCallback((_) {
          if (_scroll.hasClients)
            _scroll.jumpTo(_scroll.position.maxScrollExtent);
        });
      },
      end: (r) {
        setState(() {
          state = 'done';
          result = r;
        });
        widget.onDone?.call();
      },
    );
  }

  Future<void> _run() async {
    try {
      await widget.api.runScript(name, args.text.trim());
      setState(() => state = 'running');
      _follow();
    } catch (e) {
      if (mounted) showError(context, e);
    }
  }

  @override
  void dispose() {
    _sub?.cancel();
    _scroll.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final t = Tones.of(context);
    final okRun = result['exitCode'] == 0 && (result['error'] ?? '') == '';
    return Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        if (state == 'running')
          const Align(alignment: Alignment.centerLeft, child: WChip('running')),
        if (state == 'done')
          Align(
            alignment: Alignment.centerLeft,
            child: WChip(
              'exit ${result['exitCode']}',
              color: okRun ? t.ok : t.bad,
              textColor: okRun ? t.onOk : t.onBad,
            ),
          ),
        if (state == 'ready')
          Row(
            children: [
              Expanded(
                child: TextField(
                  controller: args,
                  autofocus: true,
                  style: const TextStyle(fontFamily: kMono, fontSize: 13),
                  onSubmitted: (_) => _run(),
                  decoration: const InputDecoration(
                    hintText: 'Arguments (optional)',
                  ),
                ),
              ),
              const SizedBox(width: 8),
              WButton('Run', icon: 'play', onPressed: _run),
            ],
          )
        else ...[
          const SizedBox(height: 12),
          Flexible(
            child: OutputBox(
              '${out.isEmpty ? 'Waiting for output…' : out}${(result['error'] ?? '') != '' ? '\n${result['error']}' : ''}',
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
            if (state == 'running')
              WButton(
                'Stop',
                kind: BtnKind.danger,
                onPressed: () => widget.api.stopScript(name),
              ),
            WButton(
              'Close',
              kind: BtnKind.text,
              onPressed: () => Navigator.pop(context),
            ),
          ],
        ),
      ],
    );
  }
}

/// ScriptsView.vue: each linked script with arguments and Run or Stop, the output
/// panel, the history, and renaming.
class ScriptsPage extends StatefulWidget {
  const ScriptsPage({super.key});

  @override
  State<ScriptsPage> createState() => _ScriptsPageState();
}

class _ScriptsPageState extends State<ScriptsPage> {
  List<Map<String, dynamic>> scripts = [], runs = [];
  String dir = '/etc/vps-admin/scripts';
  bool loading = true;
  final args = <String, TextEditingController>{};
  final busy = <String>{};
  // Output panel: the live run or one from the history.
  String? shownTitle;
  String shownText = '';
  bool live = false;
  int? exit;
  String? runError;
  final _scroll = ScrollController();
  StreamSubscription<(String, dynamic)>? _sub;

  Api get api => Live.read(context).api;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) => _load());
  }

  @override
  void dispose() {
    _sub?.cancel();
    _scroll.dispose();
    for (final c in args.values) {
      c.dispose();
    }
    super.dispose();
  }

  Future<void> _load() async {
    try {
      final j = await api.get('/api/scripts');
      if (!mounted) return;
      setState(() {
        scripts = (j['scripts'] as List).cast<Map<String, dynamic>>();
        runs = (j['runs'] as List).cast<Map<String, dynamic>>();
        dir = j['dir'] as String? ?? dir;
      });
    } catch (e) {
      if (mounted) showError(context, e);
    } finally {
      if (mounted) setState(() => loading = false);
    }
  }

  void _down() => WidgetsBinding.instance.addPostFrameCallback((_) {
    if (_scroll.hasClients) _scroll.jumpTo(_scroll.position.maxScrollExtent);
  });

  void _follow(Map<String, dynamic> s) {
    _sub?.cancel();
    setState(() {
      shownTitle = scriptTitle(s);
      shownText = '';
      live = true;
      exit = null;
      runError = null;
    });
    _sub = followScript(
      api,
      s['name'] as String,
      output: (c) {
        setState(() => shownText += c);
        _down();
      },
      end: (r) {
        setState(() {
          live = false;
          exit = (r['exitCode'] as num?)?.toInt();
          runError = r['error'] as String?;
        });
        _load();
      },
    );
  }

  Future<void> _run(Map<String, dynamic> s) async {
    final name = s['name'] as String;
    final a = args[name]?.text.trim() ?? '';
    if (!await wConfirm(
      context,
      title: 'Run ${scriptTitle(s)}?',
      body:
          '${s['target']}${a.isNotEmpty ? ' $a' : ''}\nIt runs as the owner of the file, for 30 minutes at most.',
      action: 'Run',
    )) {
      return;
    }
    setState(() => busy.add(name));
    try {
      await api.runScript(name, a);
      await _load();
      _follow(s);
    } catch (e) {
      if (mounted) showError(context, e);
    } finally {
      if (mounted) setState(() => busy.remove(name));
    }
  }

  Future<void> _stop(Map<String, dynamic> s) async {
    try {
      await api.stopScript(s['name'] as String);
      if (mounted) showInfo(context, '${scriptTitle(s)} stopped');
    } catch (e) {
      if (mounted) showError(context, e);
    }
  }

  Future<void> _showRun(Map<String, dynamic> r) async {
    _sub?.cancel();
    try {
      final out = await api.scriptRunOutput(r['id'] as String);
      final title =
          scripts
              .where((s) => s['name'] == r['script'])
              .map(scriptTitle)
              .firstOrNull ??
          r['script'] as String;
      setState(() {
        shownTitle = '$title · ${dateTime(r['started'] as String)}';
        shownText = out.isEmpty ? 'No output.' : out;
        live = false;
        exit = (r['exitCode'] as num?)?.toInt();
        runError = r['error'] as String?;
      });
      _down();
    } catch (e) {
      if (mounted) showError(context, e);
    }
  }

  Future<void> _rename(Map<String, dynamic> s) async {
    final c = TextEditingController(text: scriptTitle(s));
    final name = s['name'] as String;
    final res = await showWDialog<String>(
      context,
      title: 'Rename the script',
      body: (_) => Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          CmdText(
            'Only the name shown in the panel changes. The command line keeps `$name`.',
            style: const TextStyle(fontSize: 13),
          ),
          const SizedBox(height: 16),
          TextField(
            controller: c,
            autofocus: true,
            decoration: InputDecoration(hintText: name),
          ),
        ],
      ),
      actions: (d) => [
        if ((s['title'] as String? ?? '').isNotEmpty)
          WButton(
            'Reset',
            kind: BtnKind.text,
            onPressed: () => Navigator.pop(d, ''),
          ),
        WButton(
          'Cancel',
          kind: BtnKind.text,
          onPressed: () => Navigator.pop(d),
        ),
        WButton('Save', onPressed: () => Navigator.pop(d, c.text.trim())),
      ],
    );
    if (res == null) return;
    try {
      await api.renameScript(name, res);
      await _load();
    } catch (e) {
      if (mounted) showError(context, e);
    }
  }

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    final t = Tones.of(context);
    Widget chip(String label, Color bg, Color fg) =>
        WChip(label, color: bg, textColor: fg);
    final shapes = M3Shape.values;
    return WPage(
      onRefresh: _load,
      children: [
        PageTitle(
          'Scripts',
          actions: [
            WButton(
              'Refresh',
              icon: 'restart',
              kind: BtnKind.tonal,
              onPressed: _load,
            ),
          ],
        ),
        if (loading)
          const Muted('Loading…', size: 15)
        else if (scripts.isEmpty)
          WCard(
            padding: const EdgeInsets.all(36),
            child: Column(
              children: [
                ShapeIcon(M3Shape.sunny, size: 64, color: cs.tertiaryContainer),
                const SizedBox(height: 10),
                Text(
                  'No script yet',
                  style: Theme.of(context).textTheme.titleMedium,
                ),
                const SizedBox(height: 10),
                const Muted(
                  'Link any executable file from the server, it shows up here with a Run button:',
                  size: 15,
                ),
                const SizedBox(height: 10),
                const Cmd('sudo vps-admin link /path/to/update-app.sh'),
                const SizedBox(height: 10),
                const CmdText(
                  'Add `-name` to choose its name and `-dir` to attach it to a compose folder (found automatically when the script sits in one).',
                  style: TextStyle(fontSize: 13),
                ),
              ],
            ),
          )
        else ...[
          for (var i = 0; i < scripts.length; i++)
            Builder(
              builder: (context) {
                final s = scripts[i];
                final name = s['name'] as String;
                final ctl = args.putIfAbsent(name, TextEditingController.new);
                final running = s['running'] == true;
                return WCard(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.stretch,
                    children: [
                      Row(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          ShapeIcon(
                            shapes[i % shapes.length],
                            size: 40,
                            color: i.isOdd
                                ? cs.secondaryContainer
                                : cs.primaryContainer,
                          ),
                          const SizedBox(width: 14),
                          Expanded(
                            child: Column(
                              crossAxisAlignment: CrossAxisAlignment.start,
                              children: [
                                Wrap(
                                  spacing: 8,
                                  runSpacing: 4,
                                  crossAxisAlignment: WrapCrossAlignment.center,
                                  children: [
                                    Text(
                                      scriptTitle(s),
                                      style: const TextStyle(
                                        fontSize: 17,
                                        fontWeight: FontWeight.w500,
                                      ),
                                    ),
                                    if ((s['title'] as String? ?? '')
                                        .isNotEmpty)
                                      Muted('($name)', size: 14),
                                    InkResponse(
                                      onTap: () => _rename(s),
                                      radius: 18,
                                      child: WIcon(
                                        'edit',
                                        size: 16,
                                        color: cs.onSurfaceVariant,
                                      ),
                                    ),
                                    if (running)
                                      chip('running', t.warn, t.onWarn),
                                    if (s['missing'] == true)
                                      chip('file missing', t.bad, t.onBad)
                                    else if (s['executable'] != true)
                                      chip('not executable', t.bad, t.onBad),
                                  ],
                                ),
                                const SizedBox(height: 4),
                                Muted(
                                  s['target'] as String? ?? '',
                                  size: 12,
                                  mono: true,
                                ),
                                if (s['dir'] != null) ...[
                                  const SizedBox(height: 4),
                                  Muted(
                                    'Shown with the containers of ${s['dir']}',
                                  ),
                                ],
                              ],
                            ),
                          ),
                        ],
                      ),
                      const SizedBox(height: 14),
                      Row(
                        children: [
                          Expanded(
                            child: TextField(
                              controller: ctl,
                              style: const TextStyle(
                                fontFamily: kMono,
                                fontSize: 13,
                              ),
                              onSubmitted: (_) => _run(s),
                              decoration: const InputDecoration(
                                hintText: 'Arguments (optional)',
                                isDense: true,
                              ),
                            ),
                          ),
                          const SizedBox(width: 8),
                          if (running) ...[
                            WButton(
                              'Output',
                              kind: BtnKind.outlined,
                              onPressed: () => _follow(s),
                            ),
                            const SizedBox(width: 8),
                            WButton(
                              'Stop',
                              icon: 'stop',
                              kind: BtnKind.danger,
                              onPressed: () => _stop(s),
                            ),
                          ] else
                            WButton(
                              'Run',
                              icon: 'play',
                              onPressed:
                                  busy.contains(name) ||
                                      s['missing'] == true ||
                                      s['executable'] != true
                                  ? null
                                  : () => _run(s),
                            ),
                        ],
                      ),
                    ],
                  ),
                );
              },
            ),
          CmdText(
            'Linked in `$dir` with `sudo vps-admin link FILE`. Remove one with `sudo vps-admin unlink NAME`.',
            style: TextStyle(fontSize: 13, color: cs.onSurfaceVariant),
          ),
        ],
        WCard(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              CardHead(
                shownTitle ?? 'Output',
                trailing: [
                  if (live)
                    chip('live', t.warn, t.onWarn)
                  else if (exit != null)
                    chip(
                      'exit $exit',
                      exit == 0 && (runError ?? '').isEmpty ? t.ok : t.bad,
                      exit == 0 && (runError ?? '').isEmpty ? t.onOk : t.onBad,
                    ),
                ],
              ),
              if ((runError ?? '').isNotEmpty)
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
                    children: [
                      WIcon('alert', size: 16, color: cs.onErrorContainer),
                      const SizedBox(width: 8),
                      Expanded(
                        child: Text(
                          runError!,
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
                shownTitle == null
                    ? 'Run a script or pick one from the history.'
                    : (shownText.isEmpty ? 'Waiting for output…' : shownText),
                controller: _scroll,
              ),
            ],
          ),
        ),
        WCard(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              const CardHead('History'),
              if (runs.isEmpty) const Muted('Nothing has run yet.'),
              for (final r in runs.take(20))
                InkWell(
                  borderRadius: BorderRadius.circular(12),
                  onTap: () => _showRun(r),
                  child: Padding(
                    padding: const EdgeInsets.symmetric(
                      horizontal: 8,
                      vertical: 10,
                    ),
                    child: Row(
                      children: [
                        StatusDot(
                          r['finished'] == null
                              ? Tone.warn
                              : r['exitCode'] == 0 && (r['error'] ?? '') == ''
                              ? Tone.ok
                              : Tone.bad,
                          label: r['finished'] == null
                              ? 'running'
                              : 'exit ${r['exitCode'] ?? '?'}',
                        ),
                        const SizedBox(width: 12),
                        Expanded(
                          child: Text.rich(
                            TextSpan(
                              children: [
                                TextSpan(
                                  text:
                                      scripts
                                          .where(
                                            (s) => s['name'] == r['script'],
                                          )
                                          .map(scriptTitle)
                                          .firstOrNull ??
                                      r['script'] as String,
                                  style: const TextStyle(
                                    fontWeight: FontWeight.w700,
                                  ),
                                ),
                                if ((r['args'] as String? ?? '').isNotEmpty)
                                  TextSpan(text: ' ${r['args']}'),
                              ],
                            ),
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                            style: const TextStyle(fontSize: 14),
                          ),
                        ),
                        const SizedBox(width: 8),
                        Flexible(
                          child: Muted(
                            '${(r['device'] as String? ?? '').isEmpty ? 'assistant' : r['device']} · ${agoIso(r['started'] as String?)}',
                            maxLines: 1,
                          ),
                        ),
                      ],
                    ),
                  ),
                ),
            ],
          ),
        ),
      ],
    );
  }
}
