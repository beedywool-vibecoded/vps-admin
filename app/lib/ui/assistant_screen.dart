import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';

import '../data/api.dart';
import '../data/models.dart';
import 'scope.dart';
import 'widgets.dart';

class AssistantScreen extends StatelessWidget {
  const AssistantScreen({super.key});

  @override
  Widget build(BuildContext context) {
    final api = AppScope.of(context).api!;
    return Loader<(bool, List<Conversation>)>(
      refreshEvery: const Duration(seconds: 15),
      load: () async {
        final s = await api.assistant();
        final configured =
            (s['settings'] as Map<String, dynamic>)['configured'] as bool? ??
            false;
        return (
          configured,
          configured ? await api.conversations() : <Conversation>[],
        );
      },
      builder: (context, d, reload) {
        final (configured, list) = d;
        final cs = Theme.of(context).colorScheme;
        if (!configured) {
          return ListView(
            padding: const EdgeInsets.all(32),
            children: [
              ShapeIcon(
                M3Shape.flower,
                size: 72,
                color: cs.primaryContainer,
                child: Icon(Icons.auto_awesome, color: cs.onPrimaryContainer),
              ),
              const SizedBox(height: 16),
              const Text(
                'Connect an AI in the web panel (Assistant): Ollama for a local model, Gemini for a free one, or your Anthropic key.',
                textAlign: TextAlign.center,
              ),
            ],
          );
        }
        void open(String? id) => Navigator.push(
          context,
          MaterialPageRoute(builder: (_) => ChatScreen(id: id)),
        ).then((_) => reload());
        return Stack(
          children: [
            ListView(
              padding: const EdgeInsets.fromLTRB(12, 12, 12, 96),
              children: [
                if (list.isEmpty)
                  const Padding(
                    padding: EdgeInsets.all(24),
                    child: Text(
                      'No conversation yet.',
                      textAlign: TextAlign.center,
                    ),
                  ),
                for (final c in list)
                  Card(
                    margin: const EdgeInsets.only(bottom: 8),
                    child: ListTile(
                      title: Text(
                        c.title,
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                      ),
                      subtitle: Text(
                        c.state == 'awaiting'
                            ? 'Waiting for your approval'
                            : ago(c.updatedAt),
                      ),
                      leading: c.state == 'awaiting'
                          ? Icon(Icons.shield_outlined, color: cs.primary)
                          : const Icon(Icons.chat_bubble_outline),
                      onTap: () => open(c.id),
                    ),
                  ),
              ],
            ),
            Positioned(
              right: 16,
              bottom: 16,
              child: FloatingActionButton.extended(
                onPressed: () => open(null),
                icon: const Icon(Icons.add),
                label: const Text('New'),
              ),
            ),
          ],
        );
      },
    );
  }
}

/// One conversation: stored messages, live text, tool cards with approvals.
class ChatScreen extends StatefulWidget {
  const ChatScreen({super.key, this.id, this.initial});
  final String? id;
  // A suggestion to send right away, for a new conversation.
  final String? initial;

  @override
  State<ChatScreen> createState() => _ChatScreenState();
}

class _ChatScreenState extends State<ChatScreen> {
  String? id;
  String title = 'New conversation';
  String state = 'idle';
  String error = '';
  List<Map<String, dynamic>> messages = [];
  final Map<String, ToolCall> calls = {};
  final Map<int, (String, String)> draft = {}; // index -> (kind, text)
  final _input = TextEditingController();
  final _scroll = ScrollController();
  StreamSubscription<(String, dynamic)>? _sub;
  bool _sending = false;

  Api get api => AppScope.of(context).api!;

  @override
  void initState() {
    super.initState();
    id = widget.id;
    if (id != null) {
      WidgetsBinding.instance.addPostFrameCallback((_) => _open());
    } else if (widget.initial != null) {
      _input.text = widget.initial!;
      WidgetsBinding.instance.addPostFrameCallback((_) => _send());
    }
  }

  @override
  void dispose() {
    _sub?.cancel();
    super.dispose();
  }

  Future<void> _open() async {
    try {
      final j = await api.conversation(id!);
      final conv = Conversation(j['conversation'] as Map<String, dynamic>);
      setState(() {
        title = conv.title;
        state = conv.state;
        error = conv.error;
        messages = (j['messages'] as List).cast<Map<String, dynamic>>();
        calls
          ..clear()
          ..addEntries(
            (j['toolCalls'] as List)
                .map((t) => ToolCall(t as Map<String, dynamic>))
                .map((t) => MapEntry(t.id, t)),
          );
      });
      _down();
      _listen();
    } catch (e) {
      if (mounted) showError(context, e);
    }
  }

  void _listen() {
    _sub?.cancel();
    _sub = api
        .events('/api/assistant/conversations/$id/stream')
        .listen(
          (ev) {
            final (type, data) = ev;
            setState(() {
              switch (type) {
                case 'message':
                  final m = data as Map<String, dynamic>;
                  if (!messages.any((x) => x['seq'] == m['seq'])) {
                    messages.add(m);
                  }
                  if (m['role'] == 'assistant') draft.clear();
                case 'text' || 'thinking':
                  final d = data as Map<String, dynamic>;
                  final i = (d['index'] as num).toInt();
                  draft[i] = (
                    type,
                    (draft[i]?.$2 ?? '') + (d['text'] as String),
                  );
                case 'tool':
                  final t = ToolCall(data as Map<String, dynamic>);
                  calls[t.id] = t;
                case 'state':
                  final s = data as Map<String, dynamic>;
                  state = s['state'] as String;
                  error = s['error'] as String? ?? '';
              }
            });
            _down();
          },
          onError: (_) {},
          onDone: () {
            // Reconnect while the screen is open, e.g. after the phone slept.
            if (mounted) {
              Future.delayed(
                const Duration(seconds: 2),
                () => mounted ? _open() : null,
              );
            }
          },
        );
  }

  void _down() => WidgetsBinding.instance.addPostFrameCallback((_) {
    if (_scroll.hasClients) {
      _scroll.animateTo(
        _scroll.position.maxScrollExtent,
        duration: const Duration(milliseconds: 200),
        curve: Curves.easeOut,
      );
    }
  });

  Future<void> _send() async {
    final text = _input.text.trim();
    if (text.isEmpty || _sending) return;
    setState(() => _sending = true);
    try {
      if (id == null) {
        final c = await api.newConversation(text);
        id = c.id;
        title = c.title;
        _input.clear();
        await _open();
      } else {
        await api.sendMessage(id!, text);
        _input.clear();
      }
    } catch (e) {
      if (mounted) showError(context, e);
    } finally {
      if (mounted) setState(() => _sending = false);
    }
  }

  Future<void> _decide(ToolCall t, bool approve) async {
    try {
      final r = await api.decide(t.id, approve);
      setState(() => calls[r.id] = r);
    } catch (e) {
      if (mounted) showError(context, e);
    }
  }

  List<Widget> _items(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    final out = <Widget>[];
    for (final m in messages) {
      final content = (m['content'] as Map<String, dynamic>)['content'];
      final blocks = content is String
          ? [
              <String, dynamic>{'type': 'text', 'text': content},
            ]
          : (content as List).cast<Map<String, dynamic>>();
      if (m['role'] == 'user') {
        final text = blocks
            .where((b) => b['type'] == 'text')
            .map((b) => b['text'])
            .join('\n');
        if (text.isNotEmpty) {
          out.add(
            Align(
              alignment: Alignment.centerRight,
              child: Container(
                margin: const EdgeInsets.only(left: 48, bottom: 12),
                padding: const EdgeInsets.symmetric(
                  horizontal: 16,
                  vertical: 12,
                ),
                decoration: BoxDecoration(
                  color: cs.primaryContainer,
                  borderRadius: const BorderRadius.only(
                    topLeft: Radius.circular(24),
                    topRight: Radius.circular(24),
                    bottomLeft: Radius.circular(24),
                    bottomRight: Radius.circular(6),
                  ),
                ),
                child: Text(
                  text,
                  style: TextStyle(color: cs.onPrimaryContainer),
                ),
              ),
            ),
          );
        }
        continue;
      }
      for (final b in blocks) {
        switch (b['type']) {
          case 'text' when (b['text'] as String).isNotEmpty:
            out.add(_Reply(b['text'] as String));
          case 'thinking' when (b['thinking'] as String? ?? '').isNotEmpty:
            out.add(_Thinking(b['thinking'] as String));
          case 'tool_use':
            final t = calls[b['id']];
            if (t != null) out.add(_ToolCard(t, onDecide: _decide));
        }
      }
    }
    for (final d in draft.values) {
      out.add(d.$1 == 'text' ? _Reply(d.$2) : _Thinking(d.$2));
    }
    if (state == 'running' && draft.isEmpty) {
      out.add(
        const Padding(
          padding: EdgeInsets.all(8),
          child: Row(
            children: [
              SizedBox.square(
                dimension: 16,
                child: CircularProgressIndicator(strokeWidth: 2),
              ),
              SizedBox(width: 10),
              Text('The assistant is working…'),
            ],
          ),
        ),
      );
    }
    if (error.isNotEmpty) {
      out.add(
        Card(
          color: state == 'error' ? cs.errorContainer : null,
          child: Padding(padding: const EdgeInsets.all(12), child: Text(error)),
        ),
      );
    }
    return out;
  }

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    return Scaffold(
      appBar: AppBar(title: Text(title, overflow: TextOverflow.ellipsis)),
      body: Column(
        children: [
          Expanded(
            child: messages.isEmpty && id == null
                ? Center(
                    child: Padding(
                      padding: const EdgeInsets.all(32),
                      child: Column(
                        mainAxisSize: MainAxisSize.min,
                        children: [
                          ShapeIcon(
                            M3Shape.sunny,
                            size: 64,
                            color: cs.tertiaryContainer,
                          ),
                          const SizedBox(height: 12),
                          const Text(
                            'Ask about this server. Every change waits for your approval.',
                            textAlign: TextAlign.center,
                          ),
                        ],
                      ),
                    ),
                  )
                : ListView(
                    controller: _scroll,
                    padding: const EdgeInsets.fromLTRB(12, 12, 12, 12),
                    children: _items(context),
                  ),
          ),
          SafeArea(
            top: false,
            child: Padding(
              padding: const EdgeInsets.fromLTRB(12, 4, 12, 12),
              child: Row(
                children: [
                  Expanded(
                    child: TextField(
                      controller: _input,
                      minLines: 1,
                      maxLines: 5,
                      textInputAction: TextInputAction.send,
                      onSubmitted: (_) => _send(),
                      decoration: InputDecoration(
                        hintText: 'Ask about this server',
                        filled: true,
                        fillColor: cs.surfaceContainerHigh,
                        border: OutlineInputBorder(
                          borderRadius: BorderRadius.circular(28),
                          borderSide: BorderSide.none,
                        ),
                        contentPadding: const EdgeInsets.symmetric(
                          horizontal: 20,
                          vertical: 14,
                        ),
                      ),
                    ),
                  ),
                  const SizedBox(width: 8),
                  state == 'running'
                      ? IconButton.filled(
                          style: composerButton(cs),
                          tooltip: 'Stop',
                          onPressed: () => api.stopConversation(id!),
                          icon: const Icon(Icons.stop),
                        )
                      : IconButton.filled(
                          style: composerButton(cs),
                          tooltip: 'Send',
                          onPressed: _sending ? null : _send,
                          icon: const Icon(Icons.arrow_upward),
                        ),
                ],
              ),
            ),
          ),
        ],
      ),
    );
  }
}

/// The composer's action: filled with the primary colour, and muted rather than faded to
/// almost nothing while it cannot be pressed, so the arrow keeps its contrast against the
/// circle. Material dims a disabled filled button to 12% of the surface and 38% of the
/// text colour, which reads as a smudge on a phone.
ButtonStyle composerButton(ColorScheme cs) => IconButton.styleFrom(
  backgroundColor: cs.primary,
  foregroundColor: cs.onPrimary,
  disabledBackgroundColor: cs.surfaceContainerHighest,
  disabledForegroundColor: cs.onSurfaceVariant,
);

/// Assistant text: paragraphs, with ``` blocks shown as code.
class _Reply extends StatelessWidget {
  const _Reply(this.text);
  final String text;

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    final parts = text.split('```');
    return Container(
      margin: const EdgeInsets.only(right: 24, bottom: 12),
      padding: const EdgeInsets.all(14),
      decoration: BoxDecoration(
        color: cs.surfaceContainer,
        borderRadius: const BorderRadius.only(
          topLeft: Radius.circular(6),
          topRight: Radius.circular(24),
          bottomLeft: Radius.circular(24),
          bottomRight: Radius.circular(24),
        ),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          for (var i = 0; i < parts.length; i++)
            if (i.isOdd)
              Container(
                margin: const EdgeInsets.symmetric(vertical: 6),
                padding: const EdgeInsets.all(10),
                decoration: BoxDecoration(
                  color: cs.surfaceContainerLowest,
                  borderRadius: BorderRadius.circular(12),
                ),
                child: SelectableText(
                  parts[i].replaceFirst(RegExp(r'^\w*\n'), '').trimRight(),
                  style: const TextStyle(fontFamily: 'monospace', fontSize: 12),
                ),
              )
            else if (parts[i].trim().isNotEmpty)
              SelectableText(
                parts[i].trim().replaceAll('**', ''),
                style: const TextStyle(height: 1.5),
              ),
        ],
      ),
    );
  }
}

class _Thinking extends StatelessWidget {
  const _Thinking(this.text);
  final String text;

  @override
  Widget build(BuildContext context) => Theme(
    data: Theme.of(context).copyWith(dividerColor: Colors.transparent),
    child: ExpansionTile(
      dense: true,
      tilePadding: const EdgeInsets.symmetric(horizontal: 4),
      title: Text(
        'Reasoning',
        style: TextStyle(
          fontSize: 13,
          color: Theme.of(context).colorScheme.onSurfaceVariant,
        ),
      ),
      children: [
        Padding(
          padding: const EdgeInsets.fromLTRB(8, 0, 8, 8),
          child: Text(text, style: const TextStyle(fontSize: 13)),
        ),
      ],
    ),
  );
}

class _ToolCard extends StatelessWidget {
  const _ToolCard(this.t, {required this.onDecide});
  final ToolCall t;
  final Future<void> Function(ToolCall, bool) onDecide;

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    if (t.status == 'pending') {
      String? purpose;
      try {
        purpose =
            (jsonDecode(t.input) as Map<String, dynamic>)['purpose'] as String?;
      } catch (_) {}
      return Card(
        margin: const EdgeInsets.only(bottom: 12),
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(24),
          side: BorderSide(color: cs.primary, width: 2),
        ),
        child: Padding(
          padding: const EdgeInsets.all(16),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  Icon(Icons.shield_outlined, color: cs.primary),
                  const SizedBox(width: 8),
                  const Expanded(
                    child: Text(
                      'The assistant wants to run an action',
                      style: TextStyle(fontWeight: FontWeight.w500),
                    ),
                  ),
                ],
              ),
              const SizedBox(height: 10),
              Container(
                width: double.infinity,
                padding: const EdgeInsets.all(12),
                decoration: BoxDecoration(
                  color: cs.surfaceContainerLowest,
                  borderRadius: BorderRadius.circular(12),
                ),
                child: SelectableText(
                  t.summary,
                  style: TextStyle(
                    fontFamily: 'monospace',
                    fontSize: 13,
                    color: cs.primary,
                  ),
                ),
              ),
              if (purpose != null)
                Padding(
                  padding: const EdgeInsets.only(top: 8),
                  child: Text(
                    purpose,
                    style: TextStyle(fontSize: 13, color: cs.onSurfaceVariant),
                  ),
                ),
              const SizedBox(height: 12),
              Row(
                children: [
                  FilledButton(
                    onPressed: () => onDecide(t, true),
                    child: const Text('Approve'),
                  ),
                  const SizedBox(width: 8),
                  OutlinedButton(
                    onPressed: () => onDecide(t, false),
                    child: const Text('Deny'),
                  ),
                ],
              ),
            ],
          ),
        ),
      );
    }
    final ok = t.status == 'done' && !t.isError;
    final label = switch (t.status) {
      'running' => 'running…',
      'done' => t.isError ? 'failed' : 'done',
      'denied' => 'denied',
      'blocked' => 'blocked',
      _ => t.status,
    };
    return Card(
      margin: const EdgeInsets.only(bottom: 6),
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(10),
        side: BorderSide(color: cs.outlineVariant),
      ),
      child: ExpansionTile(
        dense: true,
        shape: const Border(),
        leading: Icon(
          ok
              ? Icons.check
              : t.status == 'running'
              ? Icons.hourglass_top
              : Icons.warning_amber,
          size: 16,
          color: ok
              ? StatusDot.color(Tone.ok, cs)
              : t.status == 'running'
              ? cs.primary
              : cs.error,
        ),
        title: Text(
          t.summary.isEmpty ? t.name : t.summary,
          style: const TextStyle(fontFamily: 'monospace', fontSize: 12),
          maxLines: 1,
          overflow: TextOverflow.ellipsis,
        ),
        trailing: Text(label, style: const TextStyle(fontSize: 12)),
        children: [
          if (t.result.isNotEmpty)
            Padding(
              padding: const EdgeInsets.fromLTRB(12, 0, 12, 12),
              child: SelectableText(
                t.result,
                style: const TextStyle(fontFamily: 'monospace', fontSize: 11),
              ),
            ),
        ],
      ),
    );
  }
}
