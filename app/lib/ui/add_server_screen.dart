import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

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
    final raw = v.text.toUpperCase().split('').where(_allowed.hasMatch).take(9).join();
    final out = StringBuffer();
    for (var i = 0; i < raw.length; i++) {
      if (i > 0 && i % 3 == 0) out.write('-');
      out.write(raw[i]);
    }
    final s = out.toString();
    return TextEditingValue(text: s, selection: TextSelection.collapsed(offset: s.length));
  }
}

class _AddServerScreenState extends State<AddServerScreen> {
  final _url = TextEditingController();
  final _code = TextEditingController();
  bool _busy = false;

  Future<void> _pair() async {
    setState(() => _busy = true);
    try {
      await AppScope.of(context).pair(_url.text, _code.text, 'Android app');
      if (mounted && !widget.first) Navigator.pop(context);
    } catch (e) {
      if (mounted) showError(context, e);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    final cs = t.colorScheme;
    Widget step(int n, String title, Widget child) => Row(crossAxisAlignment: CrossAxisAlignment.start, children: [
          CircleAvatar(radius: 14, backgroundColor: cs.primaryContainer, child: Text('$n', style: TextStyle(color: cs.onPrimaryContainer, fontWeight: FontWeight.w600, fontSize: 14))),
          const SizedBox(width: 12),
          Expanded(child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [Text(title, style: t.textTheme.titleSmall), const SizedBox(height: 8), child])),
        ]);
    return Scaffold(
      appBar: widget.first ? null : AppBar(title: const Text('Add a server')),
      body: SafeArea(
        child: ListView(padding: const EdgeInsets.all(20), children: [
          if (widget.first) ...[
            const SizedBox(height: 24),
            Center(child: ShapeIcon(M3Shape.cookie, size: 80, color: cs.primaryContainer, child: Icon(Icons.dns_rounded, color: cs.onPrimaryContainer, size: 34))),
            const SizedBox(height: 16),
            Center(child: Text('VPS ADMIN', style: t.textTheme.titleMedium?.copyWith(letterSpacing: 4, fontWeight: FontWeight.w600))),
            const SizedBox(height: 24),
          ],
          Card(
            margin: EdgeInsets.zero,
            child: Padding(
              padding: const EdgeInsets.all(20),
              child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
                Text('Add a server', style: t.textTheme.titleLarge),
                const SizedBox(height: 8),
                Text('Works with any Linux VPS running the VPS Admin agent.', style: t.textTheme.bodyMedium?.copyWith(color: cs.onSurfaceVariant)),
                const SizedBox(height: 20),
                step(1, 'Address of the panel', TextField(
                  controller: _url,
                  keyboardType: TextInputType.url,
                  autocorrect: false,
                  decoration: const InputDecoration(hintText: 'admin.example.com', filled: true),
                )),
                const SizedBox(height: 16),
                step(2, 'On the server, run sudo vps-admin pair and type the code', TextField(
                  controller: _code,
                  inputFormatters: [_CodeFormatter()],
                  textCapitalization: TextCapitalization.characters,
                  autocorrect: false,
                  style: const TextStyle(fontFamily: 'monospace', fontSize: 24, letterSpacing: 3),
                  textAlign: TextAlign.center,
                  decoration: const InputDecoration(hintText: 'XXX-XXX-XXX', border: OutlineInputBorder()),
                  onChanged: (_) => setState(() {}),
                )),
                const SizedBox(height: 20),
                FilledButton(
                  onPressed: _busy || _code.text.length != 11 ? null : _pair,
                  style: FilledButton.styleFrom(minimumSize: const Size.fromHeight(48)),
                  child: Text(_busy ? 'Pairing…' : 'Pair server'),
                ),
              ]),
            ),
          ),
        ]),
      ),
    );
  }
}
