import 'dart:async';
import 'dart:math' as math;

import 'package:flutter/material.dart';

import '../data/api.dart';
import 'loader_shapes.dart';

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

/// Pelote's indeterminate loader: an M3 Expressive shape that changes and turns a little further
/// every 700 ms, like the one on the web panel. Reduced motion keeps the first shape still.
class PeloteLoader extends StatefulWidget {
  const PeloteLoader({
    super.key,
    this.size = 48,
    this.color,
    this.label = 'Loading',
  });
  final double size;
  final Color? color;
  final String label;

  @override
  State<PeloteLoader> createState() => _PeloteLoaderState();
}

class _PeloteLoaderState extends State<PeloteLoader>
    with SingleTickerProviderStateMixin {
  int _index = 0;
  bool? _reduce;
  Timer? _timer;
  late final AnimationController _spin;

  @override
  void initState() {
    super.initState();
    // Always turning, like Pelote's ring (ld-turn): it never sits still between shapes.
    _spin = AnimationController(
      vsync: this,
      duration: const Duration(milliseconds: 1400),
    );
  }

  void _advance() {
    if (!mounted) return;
    setState(() => _index = (_index + 1) % kLoaderSequence.length);
  }

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    final reduce = MediaQuery.disableAnimationsOf(context);
    if (reduce == _reduce) return;
    _reduce = reduce;
    _timer?.cancel();
    // The first change lands at 300 ms so a short wait already moves; then Pelote's 700 ms cadence.
    _timer = reduce
        ? null
        : Timer(const Duration(milliseconds: 300), () {
            _advance();
            _timer = Timer.periodic(
              const Duration(milliseconds: 700),
              (_) => _advance(),
            );
          });
    if (reduce) {
      _spin.stop();
      _spin.value = 0;
    } else if (!_spin.isAnimating) {
      _spin.repeat();
    }
  }

  @override
  void dispose() {
    _timer?.cancel();
    _spin.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final color = widget.color ?? Theme.of(context).colorScheme.primary;
    return Semantics(
      label: widget.label,
      child: ExcludeSemantics(
        child: SizedBox.square(
          dimension: widget.size,
          child: Center(
            child: RotationTransition(
              turns: _spin,
              child: CustomPaint(
                size: Size.square(widget.size * 0.79),
                painter: _LoaderPainter(kLoaderSequence[_index], color),
              ),
            ),
          ),
        ),
      ),
    );
  }
}

class _LoaderPainter extends CustomPainter {
  _LoaderPainter(this.shape, this.color);
  final String shape;
  final Color color;

  static final Map<String, Path> _paths = {};

  @override
  void paint(Canvas canvas, Size size) {
    final path = _paths.putIfAbsent(
      shape,
      () => _svgPath(kLoaderShapes[shape] ?? kLoaderShapes['cookie9']!),
    );
    canvas.save();
    canvas.scale(size.width / 100, size.height / 100);
    canvas.drawPath(path, Paint()..color = color);
    canvas.restore();
  }

  @override
  bool shouldRepaint(_LoaderPainter old) =>
      old.shape != shape || old.color != color;
}

final _svgToken = RegExp(r'[MCZ]|-?\d+(?:\.\d+)?');

/// Reads the small subset of SVG path data Pelote uses: absolute M, C and Z.
Path _svgPath(String d) {
  final tokens = _svgToken.allMatches(d).map((m) => m.group(0)!).toList();
  final path = Path();
  var i = 0;
  while (i < tokens.length) {
    final cmd = tokens[i++];
    if (cmd == 'M') {
      path.moveTo(double.parse(tokens[i++]), double.parse(tokens[i++]));
    } else if (cmd == 'C') {
      path.cubicTo(
        double.parse(tokens[i++]),
        double.parse(tokens[i++]),
        double.parse(tokens[i++]),
        double.parse(tokens[i++]),
        double.parse(tokens[i++]),
        double.parse(tokens[i++]),
      );
    } else if (cmd == 'Z') {
      path.close();
    }
  }
  return path;
}

/// A centred Pelote loader for a section whose content is still on its way.
class LoadingBlock extends StatelessWidget {
  const LoadingBlock({super.key, this.size = 40, this.label = 'Loading'});
  final double size;
  final String label;

  @override
  Widget build(BuildContext context) => Padding(
    padding: const EdgeInsets.symmetric(vertical: 28),
    child: Center(
      child: PeloteLoader(size: size, label: label),
    ),
  );
}

/// M3 Expressive wavy progress, as in Pelote: a wave in primary up to [value], a 6dp gap, a flat
/// track in secondary container and a stop dot. The phase slides one wavelength every 1.6 s; the
/// ticker follows TickerMode (a screen that is not showing stops it), and reduced motion freezes
/// the wave at its resting shape.
class WaveProgress extends StatefulWidget {
  const WaveProgress(this.value, {super.key});
  final double value; // 0..1

  @override
  State<WaveProgress> createState() => _WaveProgressState();
}

class _WaveProgressState extends State<WaveProgress>
    with SingleTickerProviderStateMixin {
  late final AnimationController _phase = AnimationController(
    vsync: this,
    duration: const Duration(milliseconds: 1600),
  );
  bool _reduce = false;

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    _reduce = MediaQuery.disableAnimationsOf(context);
    if (_reduce) {
      _phase.stop();
    } else if (!_phase.isAnimating) {
      _phase.repeat();
    }
  }

  @override
  void dispose() {
    _phase.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    return SizedBox(
      height: 14,
      child: AnimatedBuilder(
        animation: _phase,
        builder: (context, _) => CustomPaint(
          painter: _WavePainter(
            widget.value.clamp(0, 1),
            _reduce ? 0 : -2 * math.pi * _phase.value,
            cs.primary,
            cs.secondaryContainer,
          ),
          size: Size.infinite,
        ),
      ),
    );
  }
}

class _WavePainter extends CustomPainter {
  _WavePainter(this.value, this.phase, this.active, this.track);
  final double value, phase;
  final Color active, track;

  static const _mid = 7.0;
  static const _amp = 3.0;
  static const _lambda = 24.0;
  static const _gap = 6.0;

  @override
  void paint(Canvas canvas, Size size) {
    const left = 2.0;
    final right = size.width - 2;
    if (right - left < 4) return;
    const stroke = 4.0;
    final end = left + (right - 4 - left) * value;
    final wave = Path();
    for (double x = left; x <= end; x += 2) {
      final y = _mid + _amp * math.sin((x / _lambda) * 2 * math.pi + phase);
      x == left ? wave.moveTo(x, y) : wave.lineTo(x, y);
    }
    if (end - left >= 1) {
      canvas.drawPath(
        wave,
        Paint()
          ..color = active
          ..style = PaintingStyle.stroke
          ..strokeWidth = stroke
          ..strokeCap = StrokeCap.round,
      );
    }
    if (right - 6 - (end + _gap) >= 6) {
      canvas.drawLine(
        Offset(end + _gap, _mid),
        Offset(right - 6, _mid),
        Paint()
          ..color = track
          ..strokeWidth = stroke
          ..strokeCap = StrokeCap.round,
      );
    }
    canvas.drawCircle(Offset(right, _mid), 2, Paint()..color = active);
  }

  @override
  bool shouldRepaint(_WavePainter old) =>
      old.value != value ||
      old.phase != phase ||
      old.active != active ||
      old.track != track;
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
    if (data == null) return const Center(child: PeloteLoader());
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
                : const PeloteLoader(size: 18, label: 'Starting'),
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
