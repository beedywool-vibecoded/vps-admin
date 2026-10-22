import 'package:flutter/material.dart';

import '../../data/api.dart';
import '../../data/models.dart';
import '../assistant_screen.dart';
import '../kit.dart';
import '../live.dart';
import '../widgets.dart';
import 'app_forms.dart';

/// AssistantView.vue's side panel: the AI provider (local, free online or Claude), its
/// model, what the assistant may do, and the conversations. A conversation opens full
/// screen.
class AssistantPage extends StatefulWidget {
  const AssistantPage({super.key});

  @override
  State<AssistantPage> createState() => _AssistantPageState();
}

class _AssistantPageState extends State<AssistantPage> {
  Map<String, dynamic>? settings;
  List<Map<String, dynamic>> models = [];
  List<Conversation> conversations = [];

  Api get api => Live.read(context).api;

  static const _efforts = [
    ('low', 'Low'),
    ('medium', 'Medium'),
    ('high', 'High'),
    ('xhigh', 'Extra high'),
    ('max', 'Max'),
  ];
  static const _never = [
    'Delete Docker volumes',
    'Change SSH or the firewall',
    'Read keys and secrets',
    'Power off the server',
  ];
  static const _suggestions = [
    'Is anything wrong on this server right now?',
    'Why is the disk filling up?',
    'Check the security report and tell me what to fix first',
  ];

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) => _load());
  }

  Future<void> _load() async {
    try {
      final s = await api.assistant();
      final list = await api.conversations();
      if (!mounted) return;
      setState(() {
        settings = s['settings'] as Map<String, dynamic>;
        models = ((s['models'] as List?) ?? []).cast<Map<String, dynamic>>();
        conversations = list;
      });
    } catch (e) {
      if (mounted) showError(context, e);
    }
  }

  Future<void> _set(Map<String, dynamic> o) async {
    try {
      final r = await api.assistantUpdate(o);
      if (!mounted) return;
      setState(() {
        settings = r['settings'] as Map<String, dynamic>;
        models = ((r['models'] as List?) ?? []).cast<Map<String, dynamic>>();
      });
    } catch (e) {
      if (mounted) showError(context, e);
    }
  }

  Map<String, dynamic>? _active() {
    final list = ((settings?['providers'] as List?) ?? [])
        .cast<Map<String, dynamic>>();
    for (final p in list) {
      if (p['active'] == true) return p;
    }
    return null;
  }

  /// Switching to a provider that still needs a key opens its dialog right away.
  Future<void> _choose(String id) async {
    await _set({'provider': id});
    final p = _active();
    if (p != null && p['needsKey'] == true && p['ready'] != true) await _key();
  }

  Future<void> _key() async {
    final c = TextEditingController();
    final cli = _active()?['id'] == 'claude-code';
    final label = _active()?['label'] as String? ?? 'The assistant';
    final res = await showWDialog<String>(
      context,
      title: cli ? 'Claude subscription token' : 'Gemini API key',
      body: (_) => Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Muted(
            cli
                ? 'On your computer, run claude setup-token and paste the token it prints (sk-ant-oat…). It bills your Pro or Max subscription. The panel runs the claude command installed on this server with its own tools disabled: every change still waits for your approval here.'
                : 'Create a free key at aistudio.google.com/apikey, no card needed. It is encrypted on this server and never shown again. Leave empty to remove it.',
            size: 14,
          ),
          const SizedBox(height: 16),
          TextField(
            controller: c,
            obscureText: true,
            autocorrect: false,
            decoration: InputDecoration(
              hintText: cli ? 'sk-ant-oat…' : 'AIza…',
            ),
          ),
        ],
      ),
      actions: (d) => [
        WButton(
          'Cancel',
          kind: BtnKind.text,
          onPressed: () => Navigator.pop(d),
        ),
        WButton('Save', onPressed: () => Navigator.pop(d, c.text.trim())),
      ],
    );
    if (res == null) return;
    await _set({'apiKey': res});
    if (mounted) {
      showInfo(
        context,
        settings?['configured'] == true
            ? '$label connected'
            : 'API key removed',
      );
    }
  }

  Future<void> _remove(Conversation c) async {
    if (!await wConfirm(
      context,
      title: 'Delete this conversation?',
      body: c.title,
      action: 'Delete',
      danger: true,
    )) {
      return;
    }
    try {
      await api.deleteConversation(c.id);
      await _load();
    } catch (e) {
      if (mounted) showError(context, e);
    }
  }

  void _open({String? id, String? initial}) => Navigator.push(
    context,
    MaterialPageRoute(
      builder: (_) => ChatScreen(id: id, initial: initial),
    ),
  ).then((_) => _load());

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    final s = settings;
    final configured = s?['configured'] == true;
    final providers = ((s?['providers'] as List?) ?? [])
        .cast<Map<String, dynamic>>();
    final active = _active();
    final needsKey = active?['needsKey'] == true;
    final label = active?['label'] as String? ?? 'The assistant';
    Widget perm(String title, String sub, Widget trailing) => Container(
      padding: const EdgeInsets.symmetric(vertical: 10),
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
          trailing,
        ],
      ),
    );
    return WPage(
      onRefresh: _load,
      children: [
        const PageTitle('Assistant'),
        WCard(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              CardHead(
                'Conversations',
                trailing: [
                  if (configured)
                    WButton(
                      'New',
                      icon: 'plus',
                      kind: BtnKind.tonal,
                      onPressed: () => _open(),
                    ),
                ],
              ),
              if (!configured)
                Column(
                  children: [
                    ShapeIcon(
                      M3Shape.flower,
                      size: 72,
                      color: cs.primaryContainer,
                    ),
                    const SizedBox(height: 12),
                    Text(
                      'Connect $label',
                      style: Theme.of(context).textTheme.titleSmall,
                    ),
                    const SizedBox(height: 6),
                    Muted(
                      active?['hint'] as String? ??
                          '$label answers with the tools of this panel; every change waits for your approval.',
                      size: 14,
                    ),
                    const SizedBox(height: 12),
                    if (needsKey)
                      WButton('Connect', large: true, onPressed: _key)
                    else
                      WButton('Check again', large: true, onPressed: _load),
                  ],
                )
              else ...[
                if (conversations.isEmpty) ...[
                  const Muted(
                    'None yet. The assistant reads the server with the tools of this panel; every change waits for your approval.',
                    size: 14,
                  ),
                  const SizedBox(height: 12),
                  Wrap(
                    spacing: 8,
                    runSpacing: 8,
                    children: [
                      for (final q in _suggestions)
                        ActionChip(
                          label: Text(q),
                          onPressed: () => _open(initial: q),
                        ),
                    ],
                  ),
                ],
                for (final c in conversations)
                  InkWell(
                    borderRadius: BorderRadius.circular(12),
                    onTap: () => _open(id: c.id),
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
                                  c.title,
                                  maxLines: 1,
                                  overflow: TextOverflow.ellipsis,
                                ),
                                Muted(
                                  c.state == 'awaiting'
                                      ? 'waiting for you'
                                      : ago(c.updatedAt),
                                ),
                              ],
                            ),
                          ),
                          WIconButton(
                            'trash',
                            tooltip: 'Delete ${c.title}',
                            size: 18,
                            onPressed: () => _remove(c),
                          ),
                        ],
                      ),
                    ),
                  ),
              ],
            ],
          ),
        ),
        WCard(
          child: Theme(
            data: Theme.of(context).copyWith(dividerColor: Colors.transparent),
            child: ExpansionTile(
              tilePadding: EdgeInsets.zero,
              childrenPadding: EdgeInsets.zero,
              shape: const Border(),
              collapsedShape: const Border(),
              leading: Container(
                width: 10,
                height: 10,
                decoration: BoxDecoration(
                  shape: BoxShape.circle,
                  color: configured ? Tones.of(context).ok : cs.outline,
                ),
              ),
              title: const Text('Assistant settings'),
              subtitle: Muted(label),
              children: [
                Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  spacing: 12,
                  children: [
                    const CardHead('AI provider'),
                    for (final p in providers)
                      InkWell(
                        borderRadius: BorderRadius.circular(16),
                        onTap: () => _choose(p['id'] as String),
                        child: Container(
                          padding: const EdgeInsets.symmetric(
                            horizontal: 12,
                            vertical: 10,
                          ),
                          decoration: BoxDecoration(
                            borderRadius: BorderRadius.circular(16),
                            border: Border.all(
                              color: p['active'] == true
                                  ? cs.primary
                                  : cs.outlineVariant,
                            ),
                            color: p['active'] == true
                                ? cs.surfaceContainerHigh
                                : null,
                          ),
                          child: Row(
                            children: [
                              Container(
                                width: 10,
                                height: 10,
                                decoration: BoxDecoration(
                                  shape: BoxShape.circle,
                                  color: p['ready'] == true
                                      ? Tones.of(context).ok
                                      : cs.outline,
                                ),
                              ),
                              const SizedBox(width: 12),
                              Expanded(
                                child: Column(
                                  crossAxisAlignment: CrossAxisAlignment.start,
                                  children: [
                                    Text(
                                      p['label'] as String? ?? '',
                                      style: const TextStyle(
                                        fontWeight: FontWeight.w500,
                                      ),
                                    ),
                                    Muted(p['badge'] as String? ?? ''),
                                  ],
                                ),
                              ),
                              if (p['active'] == true)
                                const Icon(Icons.check, size: 18),
                            ],
                          ),
                        ),
                      ),
                    if ((active?['hint'] as String? ?? '').isNotEmpty)
                      Muted(active!['hint'] as String, size: 13),
                    // The Claude Code CLI has to exist on the server before a token is any use.
                    if (active?['kind'] == 'cli' &&
                        active?['ready'] != true &&
                        (active?['install'] as String? ?? '').isNotEmpty)
                      Cmd(active!['install'] as String),
                    if (needsKey)
                      Row(
                        children: [
                          Container(
                            width: 10,
                            height: 10,
                            decoration: BoxDecoration(
                              shape: BoxShape.circle,
                              color: configured
                                  ? Tones.of(context).ok
                                  : cs.outline,
                            ),
                          ),
                          const SizedBox(width: 12),
                          Expanded(
                            child: Column(
                              crossAxisAlignment: CrossAxisAlignment.start,
                              children: [
                                Text(
                                  configured ? 'Connected' : 'Not connected',
                                  style: const TextStyle(
                                    fontWeight: FontWeight.w500,
                                  ),
                                ),
                                if ((s?['keyHint'] as String? ?? '').isNotEmpty)
                                  Muted(s!['keyHint'] as String, mono: true),
                              ],
                            ),
                          ),
                          TextButton(
                            onPressed: _key,
                            child: Text(configured ? 'Change' : 'Connect'),
                          ),
                        ],
                      )
                    else
                      Align(
                        alignment: AlignmentDirectional.centerStart,
                        child: WButton(
                          'Check again',
                          kind: BtnKind.outlined,
                          onPressed: _load,
                        ),
                      ),
                    if (models.isNotEmpty)
                      SelectField<String>(
                        label: 'Model',
                        value: s?['model'] as String? ?? '',
                        options: [
                          for (final m in models)
                            (m['id'] as String, m['label'] as String),
                        ],
                        onChanged: (v) => _set({'model': v}),
                      ),
                    if (active?['kind'] == 'cli')
                      SelectField<String>(
                        label: 'Effort',
                        value: s?['effort'] as String? ?? 'high',
                        options: _efforts,
                        onChanged: (v) => _set({'effort': v}),
                      ),
                    Muted(active?['note'] as String? ?? ''),
                    const Divider(height: 24),
                    const CardHead('What the assistant may do'),
                    perm(
                      'Read the server',
                      'Status, logs, metrics, config files',
                      const WChip('always'),
                    ),
                    perm(
                      'Run read-only commands without asking',
                      'Off: every command waits for you',
                      Switch(
                        value: s?['autoRead'] as bool? ?? true,
                        onChanged: s == null
                            ? null
                            : (v) => _set({'autoRead': v}),
                      ),
                    ),
                    perm(
                      'Changes',
                      'Restart, deploy, any command that modifies: you approve each one',
                      const WChip('ask'),
                    ),
                    const SizedBox(height: 8),
                    Wrap(
                      spacing: 8,
                      runSpacing: 8,
                      crossAxisAlignment: WrapCrossAlignment.center,
                      children: [
                        const Muted('Never allowed'),
                        for (final n in _never)
                          WChip(
                            n,
                            color: cs.errorContainer,
                            textColor: cs.onErrorContainer,
                          ),
                      ],
                    ),
                  ],
                ),
              ],
            ),
          ),
        ),
      ],
    );
  }
}
