import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../data/updater.dart';

import 'scope.dart';
import 'widgets.dart';

/// Pairs the phone with a server: its panel address and a code from `sudo vps-admin pair`.
class AddServerScreen extends StatefulWidget {
  const AddServerScreen({super.key, this.first = false});
  final bool first;

  @override
  State<AddServerScreen> createState() => _AddServerScreenState();
}

class _CodeFormatter extends TextInputFormatter {
  static final _allowed = RegExp('[23456789ABCDEFGHJKMNPQRSTUVWXYZ]');
  @override
  TextEditingValue formatEditUpdate(TextEditingValue old, TextEditingValue v) {
    final raw = v.text
        .toUpperCase()
        .split('')
        .where(_allowed.hasMatch)
        .take(9)
        .join();
    final out = StringBuffer();
    for (var i = 0; i < raw.length; i++) {
      if (i > 0 && i % 3 == 0) out.write('-');
      out.write(raw[i]);
    }
    final s = out.toString();
    return TextEditingValue(
      text: s,
      selection: TextSelection.collapsed(offset: s.length),
    );
  }
}

class _AddServerScreenState extends State<AddServerScreen> {
  final _url = TextEditingController();
  final _code = TextEditingController();
  final _owner = TextEditingController();
  final _codeFocus = FocusNode();
  final _ownerFocus = FocusNode();
  bool _busy = false;

  @override
  void initState() {
    super.initState();
    // The name typed for the last server, so adding another one is quicker.
    SharedPreferences.getInstance()
        .then((p) {
          final last = p.getString('owner') ?? '';
          if (mounted && _owner.text.isEmpty) {
            setState(() => _owner.text = last);
          }
        })
        .catchError((_) {});
  }

  bool get _ready =>
      !_busy && _code.text.length == 11 && _owner.text.trim().isNotEmpty;

  Future<void> _pair() async {
    setState(() => _busy = true);
    try {
      final owner = _owner.text.trim();
      final model = await Updater.deviceModel();
      if (!mounted) return;
      await AppScope.of(context).pair(_url.text, _code.text, model, owner);
      SharedPreferences.getInstance()
          .then((p) => p.setString('owner', owner))
          .catchError((_) => false);
      if (mounted && !widget.first) Navigator.pop(context);
    } catch (e) {
      if (mounted) showError(context, e);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  void dispose() {
    _url.dispose();
    _code.dispose();
    _owner.dispose();
    _codeFocus.dispose();
    _ownerFocus.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    final cs = t.colorScheme;
    Widget step(int n, String title, Widget child) => Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        CircleAvatar(
          radius: 14,
          backgroundColor: cs.primaryContainer,
          child: Text(
            '$n',
            style: TextStyle(
              color: cs.onPrimaryContainer,
              fontWeight: FontWeight.w600,
              fontSize: 14,
            ),
          ),
        ),
        const SizedBox(width: 12),
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              CmdText(title, style: t.textTheme.titleSmall),
              const SizedBox(height: 8),
              child,
            ],
          ),
        ),
      ],
    );
    return Scaffold(
      appBar: widget.first ? null : AppBar(title: const Text('Add a server')),
      body: SafeArea(
        child: Center(
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 560),
            child: ListView(
              shrinkWrap: true,
              padding: const EdgeInsets.all(20),
              children: [
                if (widget.first) ...[
                  const SizedBox(height: 24),
                  Center(
                    child: ShapeIcon(
                      M3Shape.cookie,
                      size: 80,
                      color: cs.primaryContainer,
                      child: Icon(
                        Icons.dns_rounded,
                        color: cs.onPrimaryContainer,
                        size: 34,
                      ),
                    ),
                  ),
                  const SizedBox(height: 16),
                  Center(
                    child: Text(
                      'VPS ADMIN',
                      style: t.textTheme.titleMedium?.copyWith(
                        letterSpacing: 4,
                        fontWeight: FontWeight.w600,
                      ),
                    ),
                  ),
                  const SizedBox(height: 24),
                ],
                Card(
                  margin: EdgeInsets.zero,
                  child: Padding(
                    padding: const EdgeInsets.all(20),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.stretch,
                      children: [
                        Text('Add a server', style: t.textTheme.titleLarge),
                        const SizedBox(height: 8),
                        Text(
                          'Works with any Linux VPS running the VPS Admin agent.',
                          style: t.textTheme.bodyMedium?.copyWith(
                            color: cs.onSurfaceVariant,
                          ),
                        ),
                        const SizedBox(height: 20),
                        step(
                          1,
                          'Address of the panel',
                          TextField(
                            controller: _url,
                            keyboardType: TextInputType.url,
                            autocorrect: false,
                            textInputAction: TextInputAction.next,
                            onSubmitted: (_) => _ownerFocus.requestFocus(),
                            decoration: const InputDecoration(
                              hintText: 'admin.example.com',
                              filled: true,
                            ),
                          ),
                        ),
                        const SizedBox(height: 16),
                        step(
                          2,
                          'Your name, so the other admins know who uses this phone',
                          TextField(
                            controller: _owner,
                            focusNode: _ownerFocus,
                            textCapitalization: TextCapitalization.words,
                            textInputAction: TextInputAction.next,
                            autofillHints: const [AutofillHints.name],
                            onSubmitted: (_) => _codeFocus.requestFocus(),
                            onChanged: (_) => setState(() {}),
                            decoration: const InputDecoration(
                              hintText: 'Your name',
                              filled: true,
                            ),
                          ),
                        ),
                        const SizedBox(height: 16),
                        step(
                          3,
                          'On the server, run `sudo vps-admin pair` and type the code',
                          TextField(
                            controller: _code,
                            focusNode: _codeFocus,
                            inputFormatters: [_CodeFormatter()],
                            textInputAction: TextInputAction.done,
                            onSubmitted: (_) => _ready ? _pair() : null,
                            textCapitalization: TextCapitalization.characters,
                            autocorrect: false,
                            style: const TextStyle(
                              fontFamily: 'monospace',
                              fontSize: 24,
                              letterSpacing: 3,
                            ),
                            textAlign: TextAlign.center,
                            decoration: const InputDecoration(
                              hintText: 'XXX-XXX-XXX',
                              border: OutlineInputBorder(),
                            ),
                            onChanged: (_) => setState(() {}),
                          ),
                        ),
                        const SizedBox(height: 20),
                        FilledButton(
                          onPressed: _ready ? _pair : null,
                          style: FilledButton.styleFrom(
                            minimumSize: const Size.fromHeight(48),
                          ),
                          child: Text(_busy ? 'Pairing…' : 'Pair server'),
                        ),
                      ],
                    ),
                  ),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}
