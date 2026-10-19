import 'dart:async';
import 'dart:math' as math;

import 'package:flutter/material.dart';

import '../data/api.dart';

/// Material 3 Expressive shapes: same polar formula as the web panel and the portfolio.
enum M3Shape {
  cookie(9, 0.07),
  clover(4, 0.17),
  sunny(8, 0.10),
  flower(6, 0.13);

  const M3Shape(this.lobes, this.depth);
  final int lobes;
  final double depth;

  Path path(Size size) {
    final r = size.shortestSide / 2 * 0.92;
    final c = size.center(Offset.zero);
    final p = Path();
    for (var i = 0; i <= 96; i++) {
      final t = 2 * math.pi * i / 96;
      final rr = r * (1 + depth * math.cos(lobes * t));
      final o = c + Offset(rr * math.cos(t), rr * math.sin(t));
      i == 0 ? p.moveTo(o.dx, o.dy) : p.lineTo(o.dx, o.dy);
    }
    return p..close();
  }
}

class ShapeIcon extends StatelessWidget {
  const ShapeIcon(
    this.shape, {
    super.key,
    this.size = 40,
    required this.color,
    this.child,
  });
  final M3Shape shape;
  final double size;
  final Color color;
  final Widget? child;

  @override
  Widget build(BuildContext context) => SizedBox.square(
    dimension: size,
    child: CustomPaint(
      painter: _ShapePainter(shape, color),
      child: Center(child: child),
    ),
  );
}

class _ShapePainter extends CustomPainter {
  _ShapePainter(this.shape, this.color);
  final M3Shape shape;
  final Color color;
  @override
  void paint(Canvas canvas, Size size) =>
      canvas.drawPath(shape.path(size), Paint()..color = color);
  @override
  bool shouldRepaint(_ShapePainter old) =>
      old.color != color || old.shape != shape;
}

/// M3 Expressive wavy progress: a wave up to [value], then a flat track.
class WaveProgress extends StatelessWidget {
  const WaveProgress(this.value, {super.key});
  final double value; // 0..1

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    return SizedBox(
      height: 14,
      child: CustomPaint(
        painter: _WavePainter(value.clamp(0, 1), cs.primary, cs.outlineVariant),
        size: Size.infinite,
      ),
    );
  }
}

class _WavePainter extends CustomPainter {
  _WavePainter(this.value, this.active, this.track);
  final double value;
  final Color active, track;

  @override
  void paint(Canvas canvas, Size size) {
    final mid = size.height / 2;
    final end = size.width * value;
    final wave = Path()..moveTo(0, mid);
    for (double x = 0; x <= end; x += 1) {
      wave.lineTo(x, mid + 3.5 * math.sin(x / 24 * 2 * math.pi));
    }
    canvas.drawPath(
      wave,
      Paint()
        ..color = active
        ..style = PaintingStyle.stroke
        ..strokeWidth = 4
        ..strokeCap = StrokeCap.round,
    );
    if (end + 8 < size.width) {
      canvas.drawLine(
        Offset(end + 8, mid),
        Offset(size.width, mid),
        Paint()
          ..color = track
          ..strokeWidth = 4
          ..strokeCap = StrokeCap.round,
      );
    }
  }

  @override
  bool shouldRepaint(_WavePainter old) =>
      old.value != value || old.active != active;
}

enum Tone { ok, warn, bad, idle }

class StatusDot extends StatelessWidget {
  const StatusDot(this.tone, {super.key, required this.label});
  final Tone tone;
  final String label;

  static Color color(Tone t, ColorScheme cs) => switch (t) {
    Tone.ok =>
      cs.brightness == Brightness.dark
          ? const Color(0xFF8FD694)
          : const Color(0xFF2E7D32),
    Tone.warn =>
      cs.brightness == Brightness.dark
          ? const Color(0xFFF6C177)
          : const Color(0xFF8A5A00),
    Tone.bad => cs.error,
    Tone.idle => cs.outline,
  };

  @override
  Widget build(BuildContext context) {
    final c = color(tone, Theme.of(context).colorScheme);
    return Semantics(
      label: label,
      child: Container(
        width: 12,
        height: 12,
        decoration: BoxDecoration(
          color: c,
          shape: BoxShape.circle,
          boxShadow: [
            BoxShadow(color: c.withValues(alpha: .25), spreadRadius: 4),
          ],
        ),
      ),
    );
  }
}

class MetricCard extends StatelessWidget {
  const MetricCard({
    super.key,
    required this.label,
    required this.value,
    required this.percent,
    required this.sub,
    required this.shape,
    required this.tint,
  });
  final String label, value, sub;
  final double percent;
  final M3Shape shape;
  final Color tint;

  @override
  Widget build(BuildContext context) {
    final t = Theme.of(context);
    return Card(
      margin: EdgeInsets.zero,
      clipBehavior: Clip.antiAlias,
      child: Stack(
        children: [
          Positioned(
            right: -24,
            top: -26,
            child: Opacity(
              opacity: .55,
              child: ShapeIcon(shape, size: 84, color: tint),
            ),
          ),
          Padding(
            padding: const EdgeInsets.all(16),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  label,
                  style: t.textTheme.labelLarge?.copyWith(
                    color: t.colorScheme.onSurfaceVariant,
                  ),
                ),
                const SizedBox(height: 6),
                Text(value, style: t.textTheme.headlineMedium),
                const SizedBox(height: 8),
                WaveProgress(percent / 100),
                const SizedBox(height: 6),
                Text(
                  sub,
                  style: t.textTheme.bodySmall?.copyWith(
                    color: t.colorScheme.onSurfaceVariant,
                  ),
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

String bytes(double n) {
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB'];
  var i = 0;
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024;
    i++;
  }
  return '${n.toStringAsFixed(i == 0 ? 0 : 1)} ${units[i]}';
}

String duration(double seconds) {
  final d = seconds ~/ 86400,
      h = (seconds % 86400) ~/ 3600,
      m = (seconds % 3600) ~/ 60;
  if (d >= 7) return '${d ~/ 7} w ${d % 7} d';
  if (d > 0) return '$d d $h h';
  if (h > 0) return '$h h $m min';
  return '$m min';
}

const _months = [
  'Jan',
  'Feb',
  'Mar',
  'Apr',
  'May',
  'Jun',
  'Jul',
  'Aug',
  'Sep',
  'Oct',
  'Nov',
  'Dec',
];

/// Relative time worded like the panel (Intl.RelativeTimeFormat, en).
String ago(DateTime t) {
  final s = t.difference(DateTime.now()).inSeconds;
  final a = s.abs();
  String unit(int n, String u) {
    if (n == 0) return 'now';
    if (n == -1) return u == 'day' ? 'yesterday' : '1 $u ago';
    if (n == 1) return u == 'day' ? 'tomorrow' : 'in 1 $u';
    return n < 0 ? '${-n} ${u}s ago' : 'in $n ${u}s';
  }

  if (a < 60) return 'just now';
  if (a < 3600) return unit((s / 60).round(), 'minute');
  if (a < 86400) return unit((s / 3600).round(), 'hour');
  if (a < 86400 * 30) return unit((s / 86400).round(), 'day');
  return '${_months[t.month - 1]} ${t.day}, ${t.year}';
}

String agoIso(String? iso) =>
    iso == null || iso.isEmpty ? 'never' : ago(DateTime.parse(iso).toLocal());

/// "Oct 19, 09:41 PM" like the panel's dateTime.
String dateTime(String iso) {
  final t = DateTime.parse(iso).toLocal();
  final h = t.hour % 12 == 0 ? 12 : t.hour % 12;
  final m = t.minute.toString().padLeft(2, '0');
  return '${_months[t.month - 1]} ${t.day}, ${h.toString().padLeft(2, '0')}:$m ${t.hour < 12 ? 'AM' : 'PM'}';
}

String rate(double n) => '${bytes(n)}/s';
String pct(double n) => '${n.round()}%';

/// Untagged images come back as a digest; the short form is what docker ps shows.
String imageName(String ref) =>
    ref.startsWith('sha256:') ? ref.substring(7, 19) : ref;

/// Commands sit on a dark surface in both themes, so they stand apart from prose.
TextStyle cmdStyle(BuildContext context, {double? fontSize}) {
  final dark = Theme.of(context).brightness == Brightness.dark;
  final cs = Theme.of(context).colorScheme;
  return TextStyle(
    fontFamily: 'monospace',
    fontSize: fontSize,
    color: dark ? cs.onSurface : const Color(0xFFE6E0E9),
    backgroundColor: dark ? cs.surfaceContainerLowest : const Color(0xFF1D1B20),
  );
}

/// A command on its own line, in a dark selectable block.
class Cmd extends StatelessWidget {
  const Cmd(this.command, {super.key});
  final String command;

  @override
  Widget build(BuildContext context) {
    final style = cmdStyle(context, fontSize: 13);
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 12),
      decoration: BoxDecoration(
        color: style.backgroundColor,
        borderRadius: BorderRadius.circular(12),
        border: Border.all(color: Theme.of(context).colorScheme.outlineVariant),
      ),
      child: SelectableText(
        command,
        style: style.copyWith(backgroundColor: Colors.transparent),
      ),
    );
  }
}

/// Text where `backticked` parts are commands.
class CmdText extends StatelessWidget {
  const CmdText(this.text, {super.key, this.style, this.textAlign});
  final String text;
  final TextStyle? style;
  final TextAlign? textAlign;

  @override
  Widget build(BuildContext context) {
    final parts = text.split('`');
    final code = cmdStyle(context);
    return Text.rich(
      TextSpan(
        style: style,
        children: [
          for (var i = 0; i < parts.length; i++)
            TextSpan(
              text: i.isOdd ? '\u00a0${parts[i]}\u00a0' : parts[i],
              style: i.isOdd ? code : null,
            ),
        ],
      ),
      textAlign: textAlign,
    );
  }
}

void showError(BuildContext context, Object e) {
  ScaffoldMessenger.of(context).showSnackBar(
    SnackBar(
      content: CmdText(e is ApiException ? e.message : e.toString()),
      backgroundColor: Theme.of(context).colorScheme.errorContainer,
      showCloseIcon: true,
    ),
  );
}

void showInfo(BuildContext context, String text) => ScaffoldMessenger.of(
  context,
).showSnackBar(SnackBar(content: Text(text), showCloseIcon: true));

Future<bool> confirmAction(
  BuildContext context, {
  required String title,
  required String body,
  required String action,
  bool danger = false,
}) async {
  final cs = Theme.of(context).colorScheme;
  return await showDialog<bool>(
        context: context,
        builder: (c) => AlertDialog(
          title: Text(title),
          content: Text(body),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(c, false),
              child: const Text('Cancel'),
            ),
            FilledButton(
              style: danger
                  ? FilledButton.styleFrom(
                      backgroundColor: cs.error,
                      foregroundColor: cs.onError,
                    )
                  : null,
              onPressed: () => Navigator.pop(c, true),
              child: Text(action),
            ),
          ],
        ),
      ) ??
      false;
}

/// Loads data with pull-to-refresh, an error view with retry, and a spinner.
class Loader<T> extends StatefulWidget {
  const Loader({
    super.key,
    required this.load,
    required this.builder,
    this.refreshEvery,
  });
  final Future<T> Function() load;
  final Widget Function(
    BuildContext context,
    T data,
    Future<void> Function() reload,
  )
  builder;
  final Duration? refreshEvery;

  @override
  State<Loader<T>> createState() => _LoaderState<T>();
}

class _LoaderState<T> extends State<Loader<T>> {
  T? data;
  Object? error;
  bool _disposed = false;

  @override
  void initState() {
    super.initState();
    reload();
    if (widget.refreshEvery != null) _tick();
  }

  void _tick() => Future.delayed(widget.refreshEvery!, () {
    if (_disposed) return;
    reload();
    _tick();
  });

  @override
  void dispose() {
    _disposed = true;
    super.dispose();
  }

  Future<void> reload() async {
    try {
      final d = await widget.load();
      if (mounted) {
        setState(() {
          data = d;
          error = null;
        });
      }
    } catch (e) {
      if (mounted) setState(() => error = e);
    }
  }

  @override
  Widget build(BuildContext context) {
    if (data == null && error != null) {
      return Center(
        child: Padding(
          padding: const EdgeInsets.all(32),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Icon(
                Icons.cloud_off,
                size: 48,
                color: Theme.of(context).colorScheme.error,
              ),
              const SizedBox(height: 12),
              Text(
                error is ApiException
                    ? (error as ApiException).message
                    : 'Cannot reach the server',
                textAlign: TextAlign.center,
              ),
              const SizedBox(height: 12),
              FilledButton.tonal(onPressed: reload, child: const Text('Retry')),
            ],
          ),
        ),
      );
    }
    if (data == null) return const Center(child: CircularProgressIndicator());
    return RefreshIndicator(
      onRefresh: reload,
      child: widget.builder(context, data as T, reload),
    );
  }
}

class LogsPage extends StatelessWidget {
  const LogsPage({super.key, required this.title, required this.load});
  final String title;
  final Future<String> Function() load;

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(title: Text(title)),
    body: Loader<String>(
      load: load,
      refreshEvery: const Duration(seconds: 5),
      builder: (c, logs, _) => ListView(
        reverse: true,
        padding: const EdgeInsets.all(12),
        children: [
          SelectableText(
            logs.isEmpty ? 'No output yet.' : logs,
            style: const TextStyle(
              fontFamily: 'monospace',
              fontSize: 12,
              height: 1.5,
            ),
          ),
        ],
      ),
    ),
  );
}

/// Output of a long command that streams (a rebuild), shown as it arrives.
class StreamPage extends StatefulWidget {
  const StreamPage({super.key, required this.title, required this.start});
  final String title;
  final Stream<String> Function() start;

  @override
  State<StreamPage> createState() => _StreamPageState();
}

class _StreamPageState extends State<StreamPage> {
  final _out = StringBuffer();
  final _scroll = ScrollController();
  StreamSubscription<String>? _sub;
  bool _done = false;
  String? _error;

  @override
  void initState() {
    super.initState();
    _sub = widget.start().listen(
      (chunk) {
        setState(() => _out.write(chunk));
        WidgetsBinding.instance.addPostFrameCallback((_) {
          if (_scroll.hasClients) {
            _scroll.jumpTo(_scroll.position.maxScrollExtent);
          }
        });
      },
      onError: (Object e) => setState(() {
        _error = e is ApiException ? e.message : e.toString();
        _done = true;
      }),
      onDone: () => setState(() => _done = true),
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
    final text = _out.toString();
    final ok = text.trimRight().endsWith('==> Done');
    return Scaffold(
      appBar: AppBar(
        title: Text(widget.title),
        actions: [
          Padding(
            padding: const EdgeInsets.only(right: 16),
            child: _done
                ? StatusDot(
                    ok && _error == null ? Tone.ok : Tone.bad,
                    label: ok ? 'done' : 'failed',
                  )
                : const SizedBox(
                    width: 18,
                    height: 18,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  ),
          ),
        ],
      ),
      body: SingleChildScrollView(
        controller: _scroll,
        padding: const EdgeInsets.all(16),
        child: SelectableText(
          _error ?? (text.isEmpty ? 'Starting…' : text),
          style: const TextStyle(fontFamily: 'monospace', fontSize: 12),
        ),
      ),
    );
  }
}
