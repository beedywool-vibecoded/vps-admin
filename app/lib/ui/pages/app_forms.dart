import 'package:flutter/material.dart';

import '../../data/api.dart';
import '../kit.dart';
import '../widgets.dart';

/// The panel's filled field (.tf): label in primary, value, hint, a bottom line.
/// The explanation sits under the box, so a field is the same height with or without one.
class FilledField extends StatelessWidget {
  const FilledField({
    super.key,
    required this.label,
    required this.controller,
    this.hint,
    this.help,
    this.mono = false,
    this.number = false,
    this.enabled = true,
    this.onChanged,
  });
  final String label;
  final TextEditingController controller;
  final String? hint, help;
  final bool mono, number, enabled;
  final ValueChanged<String>? onChanged;

  /// Every filled field, text or select, is exactly this tall.
  static const double height = 56;

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    final line = UnderlineInputBorder(
      borderRadius: const BorderRadius.vertical(
        top: Radius.circular(12),
        bottom: Radius.circular(4),
      ),
      borderSide: BorderSide(color: cs.onSurfaceVariant),
    );
    return FieldHelp(
      help: help,
      child: SizedBox(
        height: height,
        child: TextField(
          controller: controller,
          enabled: enabled,
          onChanged: onChanged,
          autocorrect: false,
          keyboardType: number ? TextInputType.number : null,
          style: TextStyle(
            fontFamily: mono ? kMono : kFont,
            fontSize: mono ? 14 : 16,
          ),
          decoration: InputDecoration(
            labelText: label,
            hintText: hint,
            floatingLabelBehavior: FloatingLabelBehavior.always,
            labelStyle: TextStyle(
              color: cs.primary,
              fontWeight: FontWeight.w500,
            ),
            filled: true,
            fillColor: cs.surfaceContainerHighest,
            isDense: true,
            border: line,
            enabledBorder: line,
            focusedBorder: line.copyWith(
              borderSide: BorderSide(color: cs.primary, width: 2),
            ),
            disabledBorder: line.copyWith(
              borderSide: BorderSide(color: cs.outlineVariant),
            ),
            contentPadding: const EdgeInsets.fromLTRB(16, 6, 16, 6),
          ),
        ),
      ),
    );
  }
}

/// The explanation under a field. It lives outside the box, so a row of fields lines
/// up whatever each one has to say.
class FieldHelp extends StatelessWidget {
  const FieldHelp({super.key, required this.help, required this.child});
  final String? help;
  final Widget child;

  @override
  Widget build(BuildContext context) {
    final text = help;
    if (text == null || text.isEmpty) return child;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        child,
        Padding(
          padding: const EdgeInsets.fromLTRB(16, 4, 0, 0),
          child: Text(
            text,
            style: TextStyle(
              fontSize: 12,
              height: 16 / 12,
              color: Theme.of(context).colorScheme.onSurfaceVariant,
            ),
          ),
        ),
      ],
    );
  }
}

/// M3Select.vue: a filled field that opens a menu, as tall as the text fields.
class SelectField<T> extends StatelessWidget {
  const SelectField({
    super.key,
    required this.label,
    required this.value,
    required this.options,
    required this.onChanged,
    this.help,
  });
  final String label;
  final T value;
  final List<(T, String)> options;
  final ValueChanged<T> onChanged;
  final String? help;

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    final current = options.where((o) => o.$1 == value).firstOrNull;
    return MenuAnchor(
      style: MenuStyle(
        backgroundColor: WidgetStateProperty.all(cs.surfaceContainerHighest),
        shape: WidgetStateProperty.all(
          RoundedRectangleBorder(borderRadius: BorderRadius.circular(16)),
        ),
        maximumSize: WidgetStateProperty.all(const Size(double.infinity, 360)),
      ),
      menuChildren: [
        for (final o in options)
          MenuItemButton(
            onPressed: () => onChanged(o.$1),
            trailingIcon: o.$1 == value
                ? WIcon('check', size: 18, color: cs.primary)
                : null,
            child: Text(o.$2),
          ),
      ],
      builder: (context, ctl, _) => FieldHelp(
        help: help,
        child: SizedBox(
          height: FilledField.height,
          child: Material(
            color: cs.surfaceContainerHighest,
            borderRadius: const BorderRadius.vertical(
              top: Radius.circular(12),
              bottom: Radius.circular(4),
            ),
            child: InkWell(
              onTap: () => ctl.isOpen ? ctl.close() : ctl.open(),
              child: Container(
                padding: const EdgeInsets.fromLTRB(16, 6, 12, 6),
                decoration: BoxDecoration(
                  border: Border(
                    bottom: BorderSide(color: cs.onSurfaceVariant),
                  ),
                ),
                child: Row(
                  children: [
                    Expanded(
                      child: Column(
                        mainAxisAlignment: MainAxisAlignment.center,
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(
                            label,
                            style: TextStyle(
                              fontSize: 12,
                              height: 16 / 12,
                              fontWeight: FontWeight.w500,
                              color: cs.primary,
                            ),
                          ),
                          const SizedBox(height: 2),
                          Text(
                            current?.$2 ?? '',
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                            style: const TextStyle(
                              fontSize: 16,
                              height: 24 / 16,
                            ),
                          ),
                        ],
                      ),
                    ),
                    WIcon('chevron', size: 20, color: cs.onSurfaceVariant),
                  ],
                ),
              ),
            ),
          ),
        ),
      ),
    );
  }
}

/// EnvEditor.vue: rows of name, value and secret; secrets stay "unchanged" until typed.
class EnvEditor extends StatefulWidget {
  const EnvEditor({super.key, required this.vars, required this.onChanged});
  final List<Map<String, dynamic>> vars; // key, value, secret, keep
  final ValueChanged<List<Map<String, dynamic>>> onChanged;

  @override
  State<EnvEditor> createState() => _EnvEditorState();
}

class _EnvEditorState extends State<EnvEditor> {
  final _keys = <TextEditingController>[];
  final _vals = <TextEditingController>[];

  @override
  void initState() {
    super.initState();
    _sync();
  }

  @override
  void didUpdateWidget(EnvEditor old) {
    super.didUpdateWidget(old);
    if (old.vars.length != widget.vars.length) _sync();
  }

  void _sync() {
    _keys.clear();
    _vals.clear();
    for (final v in widget.vars) {
      _keys.add(TextEditingController(text: v['key'] as String? ?? ''));
      _vals.add(TextEditingController(text: v['value'] as String? ?? ''));
    }
  }

  void _emit() => widget.onChanged([...widget.vars]);

  // Pasting KEY=value lines (a .env file) into a name adds them all.
  void _paste(int i, String text) {
    if (!text.contains('=') || !text.contains('\n')) return;
    final parsed = <Map<String, dynamic>>[];
    final re = RegExp(
      r'^\s*(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*)\s*$',
    );
    for (final line in text.split(RegExp(r'\r?\n'))) {
      final m = re.firstMatch(line);
      if (m == null) continue;
      var v = m.group(2)!;
      if (RegExp(r'''^(['"]).*\1$''').hasMatch(v)) {
        v = v.substring(1, v.length - 1);
      }
      parsed.add({
        'key': m.group(1),
        'value': v,
        'secret': RegExp(
          'SECRET|TOKEN|PASSWORD|KEY|DATABASE_URL',
          caseSensitive: false,
        ).hasMatch(m.group(1)!),
      });
    }
    final next = [...widget.vars]..replaceRange(i, i + 1, parsed);
    widget.onChanged(next);
  }

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        for (var i = 0; i < widget.vars.length; i++)
          Padding(
            padding: const EdgeInsets.only(bottom: 8),
            child: Wrap(
              spacing: 8,
              runSpacing: 8,
              crossAxisAlignment: WrapCrossAlignment.center,
              children: [
                SizedBox(
                  width: 200,
                  child: TextField(
                    controller: _keys[i],
                    style: const TextStyle(fontFamily: kMono, fontSize: 13),
                    decoration: const InputDecoration(
                      hintText: 'NAME',
                      isDense: true,
                    ),
                    onChanged: (v) {
                      if (v.contains('\n')) return _paste(i, v);
                      widget.vars[i]['key'] = v.trim();
                      _emit();
                    },
                    maxLines: 1,
                  ),
                ),
                SizedBox(
                  width: 260,
                  child: TextField(
                    controller: _vals[i],
                    obscureText: widget.vars[i]['secret'] == true,
                    style: const TextStyle(fontFamily: kMono, fontSize: 13),
                    decoration: InputDecoration(
                      hintText: widget.vars[i]['keep'] == true
                          ? 'unchanged'
                          : 'value',
                      isDense: true,
                    ),
                    onChanged: (v) {
                      widget.vars[i]['value'] = v;
                      widget.vars[i]['keep'] = false;
                      _emit();
                    },
                  ),
                ),
                Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    Checkbox(
                      value: widget.vars[i]['secret'] == true,
                      onChanged: (v) {
                        setState(() => widget.vars[i]['secret'] = v == true);
                        _emit();
                      },
                    ),
                    const Muted('secret'),
                  ],
                ),
                WIconButton(
                  'close',
                  tooltip: 'Remove ${widget.vars[i]['key'] ?? 'variable'}',
                  size: 18,
                  onPressed: () {
                    widget.onChanged([...widget.vars]..removeAt(i));
                  },
                ),
              ],
            ),
          ),
        if (widget.vars.isEmpty)
          const Padding(
            padding: EdgeInsets.only(bottom: 8),
            child: Muted(
              'No variable. Tip: paste a whole .env file into a name field.',
            ),
          ),
        Align(
          alignment: Alignment.centerLeft,
          child: WButton(
            'Add a variable',
            icon: 'plus',
            kind: BtnKind.tonal,
            onPressed: () {
              widget.onChanged([
                ...widget.vars,
                {'key': '', 'value': '', 'secret': false},
              ]);
            },
          ),
        ),
        Divider(height: 1, color: cs.surface.withValues(alpha: 0)),
      ],
    );
  }
}

/// BuildCommands.vue: the optional overrides, folded by default.
class CommandsFields extends StatefulWidget {
  const CommandsFields({super.key, required this.ctl, required this.build});
  final Map<String, TextEditingController>
  ctl; // installCmd, buildCmd, startCmd, dockerfile, composeFile
  final String build;

  @override
  State<CommandsFields> createState() => _CommandsFieldsState();
}

class _CommandsFieldsState extends State<CommandsFields> {
  late bool open = widget.ctl.values.any((c) => c.text.isNotEmpty);

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    final b = widget.build;
    final node = b == 'node' || b == 'static';
    final custom = widget.ctl.values.any((c) => c.text.isNotEmpty);
    return Container(
      decoration: BoxDecoration(
        border: Border.all(color: cs.outlineVariant),
        borderRadius: BorderRadius.circular(16),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          InkWell(
            borderRadius: BorderRadius.circular(16),
            onTap: () => setState(() => open = !open),
            child: Padding(
              padding: const EdgeInsets.all(14),
              child: Row(
                children: [
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        const Text('Custom commands'),
                        Muted(
                          custom
                              ? 'Some steps are customized'
                              : 'Optional: replace the default install, build and start steps',
                        ),
                      ],
                    ),
                  ),
                  AnimatedRotation(
                    turns: open ? .5 : 0,
                    duration: const Duration(milliseconds: 250),
                    child: WIcon(
                      'chevron',
                      size: 18,
                      color: cs.onSurfaceVariant,
                    ),
                  ),
                ],
              ),
            ),
          ),
          if (open)
            Padding(
              padding: const EdgeInsets.fromLTRB(14, 0, 14, 14),
              child: Column(
                spacing: 12,
                children: [
                  if (node || b.isEmpty || b == 'auto') ...[
                    FilledField(
                      label: 'Install',
                      controller: widget.ctl['installCmd']!,
                      hint: 'npm ci',
                      mono: true,
                    ),
                    FilledField(
                      label: 'Build',
                      controller: widget.ctl['buildCmd']!,
                      hint: 'npm run build',
                      mono: true,
                    ),
                    if (b != 'static')
                      FilledField(
                        label: 'Start',
                        controller: widget.ctl['startCmd']!,
                        hint: 'npm start',
                        mono: true,
                        help: 'Runs with sh -c, PORT=3000 is set',
                      ),
                  ],
                  if (b == 'dockerfile' || b.isEmpty || b == 'auto')
                    FilledField(
                      label: 'Dockerfile',
                      controller: widget.ctl['dockerfile']!,
                      hint: 'Dockerfile',
                      mono: true,
                      help: 'Relative to the root folder',
                    ),
                  if (b == 'compose' || b.isEmpty || b == 'auto')
                    FilledField(
                      label: 'Compose file',
                      controller: widget.ctl['composeFile']!,
                      hint: 'compose.yaml',
                      mono: true,
                      help: 'Relative to the root folder',
                    ),
                ],
              ),
            ),
        ],
      ),
    );
  }
}

/// AccessPicker.vue: how the app is reached, offered from what the server has.
class AccessPicker extends StatefulWidget {
  const AccessPicker({
    super.key,
    required this.api,
    required this.access,
    required this.onAccess,
    required this.domain,
    required this.allow,
    this.tsPort = 0,
  });
  final Api api;
  final String access;
  final ValueChanged<String> onAccess;
  final TextEditingController domain, allow;
  final int tsPort;

  @override
  State<AccessPicker> createState() => _AccessPickerState();
}

class _AccessPickerState extends State<AccessPicker> {
  Map<String, dynamic>? setup;

  @override
  void initState() {
    super.initState();
    widget.api
        .get('/api/system/environment')
        .then((v) {
          if (mounted) setState(() => setup = v);
        })
        .catchError((_) {});
  }

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    final env = setup?['environment'] as Map<String, dynamic>?;
    final sites = setup?['sites'] == true;
    final ts = env?['tailscale'] as Map<String, dynamic>?;
    bool installed(String t) => (env?[t] as Map?)?['installed'] == true;
    final proxy = env == null
        ? ''
        : installed('caddy') && sites
        ? 'Caddy, with automatic HTTPS'
        : installed('nginx') && sites
        ? (installed('certbot')
              ? 'nginx, HTTPS by certbot'
              : 'nginx (install certbot for HTTPS)')
        : '';
    final nets = <(String, String)>[
      for (final l in [
        ...(env?['wireguard'] as List? ?? []),
        ...(env?['zerotier'] as List? ?? []),
      ])
        for (final a in ((l as Map)['addresses'] as List).cast<String>().where(
          (a) => a.contains('.'),
        ))
          ('${l['name']} $a', a),
      if (ts?['running'] == true) ('Tailscale 100.64.0.0/10', '100.64.0.0/10'),
    ];
    final options = [
      (
        'public',
        'globe',
        'Public domain',
        proxy.isNotEmpty
            ? proxy
            : (setup != null
                  ? 'No reverse proxy set up by the installer'
                  : 'Through the reverse proxy'),
        setup != null && !sites,
      ),
      (
        'vpn',
        'lock',
        'VPN only',
        'The domain answers only to the networks you list (WireGuard, ZeroTier, an office)',
        setup != null && !sites,
      ),
      (
        'tailscale',
        'security',
        'Tailscale only',
        env == null
            ? "On this server's tailnet name"
            : ts == null
            ? 'Tailscale is not installed'
            : ts['running'] != true
            ? 'Tailscale is not running'
            : ts['https'] != true
            ? 'Enable HTTPS in the Tailscale console (DNS page) first'
            : 'https://${ts['name']}:port, private to your tailnet',
        env != null && !(ts?['running'] == true && ts?['https'] == true),
      ),
      (
        'local',
        'server',
        'No domain',
        'Reachable on 127.0.0.1 only, for other containers or an SSH tunnel',
        false,
      ),
    ];
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      spacing: 12,
      children: [
        Wrap(
          spacing: 8,
          runSpacing: 8,
          children: [
            for (final o in options)
              SizedBox(
                width: 260,
                child: Material(
                  color: widget.access == o.$1
                      ? cs.secondaryContainer
                      : Colors.transparent,
                  shape: RoundedRectangleBorder(
                    borderRadius: BorderRadius.circular(16),
                    side: BorderSide(
                      color: widget.access == o.$1
                          ? cs.secondaryContainer
                          : cs.outlineVariant,
                    ),
                  ),
                  child: InkWell(
                    borderRadius: BorderRadius.circular(16),
                    onTap: o.$5 && widget.access != o.$1
                        ? null
                        : () => widget.onAccess(o.$1),
                    child: Opacity(
                      opacity: o.$5 && widget.access != o.$1 ? .5 : 1,
                      child: Padding(
                        padding: const EdgeInsets.all(14),
                        child: Row(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            WIcon(
                              o.$2,
                              size: 20,
                              color: widget.access == o.$1
                                  ? cs.onSecondaryContainer
                                  : cs.onSurface,
                            ),
                            const SizedBox(width: 12),
                            Expanded(
                              child: Column(
                                crossAxisAlignment: CrossAxisAlignment.start,
                                children: [
                                  Text(
                                    o.$3,
                                    style: const TextStyle(
                                      fontWeight: FontWeight.w500,
                                    ),
                                  ),
                                  Text(
                                    o.$4,
                                    style: TextStyle(
                                      fontSize: 12,
                                      color:
                                          (widget.access == o.$1
                                                  ? cs.onSecondaryContainer
                                                  : cs.onSurface)
                                              .withValues(alpha: .8),
                                    ),
                                  ),
                                ],
                              ),
                            ),
                          ],
                        ),
                      ),
                    ),
                  ),
                ),
              ),
          ],
        ),
        if (widget.access == 'public' || widget.access == 'vpn')
          FilledField(
            label: 'Domain',
            controller: widget.domain,
            hint: 'app.example.com',
            help: 'Point its DNS to this server; HTTPS is automatic',
          ),
        if (widget.access == 'vpn') ...[
          FilledField(
            label: 'Allowed networks',
            controller: widget.allow,
            hint: '10.8.0.0/24, 192.168.1.0/24',
            help: 'Your devices must reach the domain through these networks',
          ),
          if (nets.isNotEmpty)
            Wrap(
              spacing: 8,
              runSpacing: 8,
              crossAxisAlignment: WrapCrossAlignment.center,
              children: [
                const Muted('Found here:'),
                for (final n in nets)
                  ActionChip(
                    avatar: WIcon('plus', size: 14, color: cs.onSurfaceVariant),
                    label: Text(n.$1, style: const TextStyle(fontSize: 12)),
                    onPressed: () {
                      final list = widget.allow.text
                          .split(RegExp(r'[\s,]+'))
                          .where((s) => s.isNotEmpty)
                          .toList();
                      if (!list.contains(n.$2)) {
                        widget.allow.text = [...list, n.$2].join(', ');
                      }
                    },
                  ),
              ],
            ),
        ],
        if (widget.access == 'tailscale' && widget.tsPort != 0 && ts != null)
          CmdText(
            'Served at `https://${ts['name']}:${widget.tsPort}`',
            style: const TextStyle(fontSize: 13),
          ),
      ],
    );
  }
}
