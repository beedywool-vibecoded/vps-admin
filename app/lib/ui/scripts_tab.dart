import 'dart:async';

import 'package:flutter/material.dart';

import '../data/api.dart';
import 'scope.dart';
import 'widgets.dart';

/// Scripts linked on the server with `sudo vps-admin link FILE`.
class ScriptsTab extends StatelessWidget {
  const ScriptsTab({super.key});

  @override
  Widget build(BuildContext context) {
    final api = AppScope.of(context).api!;
    return Loader(
      load: api.scripts,
      refreshEvery: const Duration(seconds: 15),
      builder: (context, d, reload) {
        final cs = Theme.of(context).colorScheme;
        if (d.scripts.isEmpty) {
          return ListView(
            padding: const EdgeInsets.all(32),
            children: [
              ShapeIcon(M3Shape.sunny, size: 64, color: cs.tertiaryContainer),
              const SizedBox(height: 16),
              const Text(
                'No script yet. On the server, run:',
                textAlign: TextAlign.center,
              ),
              const SizedBox(height: 8),
              const Cmd('sudo vps-admin link /path/to/script.sh'),
            ],
          );
        }
        return ListView(
          padding: const EdgeInsets.fromLTRB(12, 12, 12, 16),
          children: [
            for (final s in d.scripts)
              Card(
                margin: const EdgeInsets.only(bottom: 8),
                child: ListTile(
                  onLongPress: () => _rename(context, api, s, reload),
                  title: Row(
                    children: [
                      Flexible(
                        child: Text.rich(
                          TextSpan(
                            text: _title(s),
                            children: [
                              if ((s['title'] as String? ?? '').isNotEmpty)
                                TextSpan(
                                  text: '  (${s['name']})',
                                  style: TextStyle(
                                    color: cs.onSurfaceVariant,
                                    fontSize: 13,
                                  ),
                                ),
                            ],
                          ),
                          overflow: TextOverflow.ellipsis,
                        ),
                      ),
                      if (s['running'] == true) ...[
                        const SizedBox(width: 8),
                        const StatusDot(Tone.warn, label: 'running'),
                      ],
                    ],
                  ),
                  subtitle: Text(
                    [
                      if ((s['description'] as String? ?? '').isNotEmpty)
                        s['description'] as String,
                      s['target'] as String,
                    ].join('\n'),
                    maxLines: 3,
                    overflow: TextOverflow.ellipsis,
                  ),
                  isThreeLine: (s['description'] as String? ?? '').isNotEmpty,
                  trailing: s['running'] == true
                      ? IconButton(
                          tooltip: 'Output',
                          icon: const Icon(Icons.terminal),
                          onPressed: () =>
                              _openRun(context, api, s['name'] as String),
                        )
                      : IconButton.filledTonal(
                          tooltip: 'Run',
                          icon: const Icon(Icons.play_arrow),
                          onPressed:
                              s['missing'] == true || s['executable'] != true
                              ? null
                              : () => _run(context, api, s, reload),
                        ),
                ),
              ),
            if (d.runs.isNotEmpty) ...[
              Padding(
                padding: const EdgeInsets.fromLTRB(8, 12, 8, 6),
                child: Text(
                  'History',
                  style: Theme.of(context).textTheme.titleMedium,
                ),
              ),
              Card(
                child: Column(
                  children: [
                    for (final r in d.runs.take(15))
                      ListTile(
                        dense: true,
                        leading: StatusDot(
                          r['finished'] == null
                              ? Tone.warn
                              : (r['exitCode'] == 0 && (r['error'] ?? '') == '')
                              ? Tone.ok
                              : Tone.bad,
                          label: 'exit ${r['exitCode']}',
                        ),
                        title: Text('${r['script']} ${r['args'] ?? ''}'.trim()),
                        subtitle: Text(
                          '${r['device'] ?? 'assistant'} · ${ago(DateTime.parse(r['started'] as String).toLocal())}',
                        ),
                        onTap: () => Navigator.push(
                          context,
                          MaterialPageRoute(
                            builder: (_) => LogsPage(
                              title: r['script'] as String,
                              load: () =>
                                  api.scriptRunOutput(r['id'] as String),
                            ),
                          ),
                        ),
                      ),
                  ],
                ),
              ),
            ],
          ],
        );
      },
    );
  }

  Future<void> _run(
    BuildContext context,
    Api api,
    Map<String, dynamic> s,
    Future<void> Function() reload,
  ) async {
    final args = TextEditingController();
    final go = await showDialog<bool>(
      context: context,
      builder: (c) => AlertDialog(
        title: Text('Run ${_title(s)}?'),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              s['target'] as String,
              style: const TextStyle(fontFamily: 'monospace', fontSize: 12),
            ),
            const SizedBox(height: 12),
            TextField(
              controller: args,
              decoration: const InputDecoration(
                labelText: 'Arguments (optional)',
                filled: true,
              ),
            ),
            const SizedBox(height: 8),
            const Text(
              'Runs as the owner of the file. Long press a script to rename it.',
              style: TextStyle(fontSize: 12),
            ),
          ],
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(c, false),
            child: const Text('Cancel'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(c, true),
            child: const Text('Run'),
          ),
        ],
      ),
    );
    if (go != true || !context.mounted) return;
    try {
      await api.runScript(s['name'] as String, args.text.trim());
      await reload();
      if (context.mounted) _openRun(context, api, s['name'] as String);
    } catch (e) {
      if (context.mounted) showError(context, e);
    }
  }

  static String _title(Map<String, dynamic> s) {
    final t = s['title'] as String? ?? '';
    return t.isNotEmpty ? t : s['name'] as String;
  }

  /// Only the name shown changes; the link keeps its name for the command line.
  Future<void> _rename(
    BuildContext context,
    Api api,
    Map<String, dynamic> s,
    Future<void> Function() reload,
  ) async {
    final c = TextEditingController(text: _title(s));
    final title = await showDialog<String>(
      context: context,
      builder: (d) => AlertDialog(
        title: const Text('Rename the script'),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            CmdText(
              'The command line keeps `${s['name']}`.',
              style: const TextStyle(fontSize: 13),
            ),
            const SizedBox(height: 12),
            TextField(
              controller: c,
              autofocus: true,
              decoration: const InputDecoration(
                labelText: 'Name shown',
                filled: true,
              ),
            ),
          ],
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(d),
            child: const Text('Cancel'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(d, c.text.trim()),
            child: const Text('Save'),
          ),
        ],
      ),
    );
    if (title == null || !context.mounted) return;
    try {
      await api.renameScript(s['name'] as String, title);
      await reload();
    } catch (e) {
      if (context.mounted) showError(context, e);
    }
  }

  void _openRun(BuildContext context, Api api, String name) => Navigator.push(
    context,
    MaterialPageRoute(
      builder: (_) => _RunPage(api: api, name: name),
    ),
  );
}

class _RunPage extends StatefulWidget {
  const _RunPage({required this.api, required this.name});
  final Api api;
  final String name;

  @override
  State<_RunPage> createState() => _RunPageState();
}

class _RunPageState extends State<_RunPage> {
  final _out = StringBuffer();
  String? _end;
  StreamSubscription<(String, dynamic)>? _sub;
  final _scroll = ScrollController();

  @override
  void initState() {
    super.initState();
    _sub = widget.api
        .events('/api/scripts/${Uri.encodeComponent(widget.name)}/stream')
        .listen((ev) {
          final (type, data) = ev;
          setState(() {
            if (type == 'output') _out.write(data as String);
            if (type == 'end') {
              final m = data as Map<String, dynamic>;
              _end =
                  'exit ${m['exitCode']}${(m['error'] ?? '') != '' ? ' · ${m['error']}' : ''}';
            }
          });
          WidgetsBinding.instance.addPostFrameCallback((_) {
            if (_scroll.hasClients) {
              _scroll.jumpTo(_scroll.position.maxScrollExtent);
            }
          });
        }, onError: (_) {});
  }

  @override
  void dispose() {
    _sub?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(
      title: Text(widget.name),
      actions: [
        if (_end == null)
          TextButton(
            onPressed: () => widget.api.stopScript(widget.name),
            child: const Text('Stop'),
          )
        else
          Padding(
            padding: const EdgeInsets.only(right: 16),
            child: Center(child: Text(_end!)),
          ),
      ],
    ),
    body: SingleChildScrollView(
      controller: _scroll,
      padding: const EdgeInsets.all(12),
      child: SelectableText(
        _out.isEmpty ? 'Waiting for output…' : _out.toString(),
        style: const TextStyle(
          fontFamily: 'monospace',
          fontSize: 12,
          height: 1.5,
        ),
      ),
    ),
  );
}

/// Runs a script from elsewhere in the app (the containers of its folder).
Future<void> runScriptFromAnywhere(
  BuildContext context,
  Api api,
  Map<String, dynamic> script,
  Future<void> Function() reload,
) => const ScriptsTab()._run(context, api, script, reload);
