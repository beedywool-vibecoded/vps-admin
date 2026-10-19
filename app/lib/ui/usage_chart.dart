import 'dart:ui' as ui;

import 'package:flutter/material.dart';

import 'kit.dart';

/// CPU and memory over time (UsageChart.vue): one 0 to 100 % axis, the memory line
/// dashed, both labeled at their end, a crosshair with values on touch, and a table view.
class UsageChart extends StatefulWidget {
  const UsageChart({super.key, required this.points, required this.span});
  final List<Map<String, dynamic>> points; // t, cpu, mem, load
  final String span; // 1h | 24h

  @override
  State<UsageChart> createState() => _UsageChartState();
}

double _d(Object? v) => (v as num?)?.toDouble() ?? 0;

String _time(num t, {bool seconds = false}) {
  final d = DateTime.fromMillisecondsSinceEpoch((t * 1000).round());
  String two(int v) => v.toString().padLeft(2, '0');
  return seconds
      ? '${two(d.hour)}:${two(d.minute)}:${two(d.second)}'
      : '${two(d.hour)}:${two(d.minute)}';
}

class _UsageChartState extends State<UsageChart> {
  bool _table = false;
  int? _hover;

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    final t = Tones.of(context);
    final pts = widget.points;
    Widget sw(Color c, {bool dashed = false}) => SizedBox(
      width: 14,
      height: 3,
      child: CustomPaint(painter: _Swatch(c, dashed)),
    );
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Row(
          children: [
            sw(t.series1),
            const SizedBox(width: 8),
            const Muted('CPU'),
            const SizedBox(width: 18),
            sw(t.series2, dashed: true),
            const SizedBox(width: 8),
            const Muted('Memory'),
            const Spacer(),
            TextButton(
              onPressed: () => setState(() => _table = !_table),
              child: Text(_table ? 'Chart' : 'Table'),
            ),
          ],
        ),
        if (pts.length < 2)
          const Padding(
            padding: EdgeInsets.symmetric(vertical: 40),
            child: Center(
              child: Muted(
                'Collecting data, the chart fills in a few seconds.',
                size: 15,
              ),
            ),
          )
        else if (_table)
          _TableView(points: pts, span: widget.span)
        else
          LayoutBuilder(
            builder: (context, box) {
              final w = box.maxWidth;
              void at(Offset p) {
                final iw = w - 36 - 56;
                final f = ((p.dx - 36) / iw).clamp(0.0, 1.0);
                setState(() => _hover = (f * (pts.length - 1)).round());
              }

              final hp = _hover == null ? null : pts[_hover!];
              return GestureDetector(
                onHorizontalDragUpdate: (d) => at(d.localPosition),
                onTapDown: (d) => at(d.localPosition),
                onHorizontalDragEnd: (_) => setState(() => _hover = null),
                onTapUp: (_) => Future.delayed(
                  const Duration(seconds: 2),
                  () => mounted ? setState(() => _hover = null) : null,
                ),
                child: Stack(
                  clipBehavior: Clip.none,
                  children: [
                    CustomPaint(
                      size: Size(w, 220),
                      painter: _ChartPainter(
                        pts,
                        _hover,
                        cpu: t.series1,
                        mem: t.series2,
                        grid: cs.outlineVariant,
                        text: cs.onSurfaceVariant,
                        strong: cs.onSurface,
                        card: cs.surfaceContainer,
                      ),
                    ),
                    if (hp != null)
                      Positioned(
                        top: 4,
                        left:
                            (36 + (_hover! / (pts.length - 1)) * (w - 92) - 80)
                                .clamp(0, w - 160),
                        child: Container(
                          width: 160,
                          padding: const EdgeInsets.symmetric(
                            horizontal: 12,
                            vertical: 8,
                          ),
                          decoration: BoxDecoration(
                            color: cs.surfaceContainerHighest,
                            borderRadius: BorderRadius.circular(12),
                            boxShadow: const [
                              BoxShadow(color: Colors.black38, blurRadius: 12),
                            ],
                          ),
                          child: DefaultTextStyle.merge(
                            style: const TextStyle(fontSize: 12),
                            child: Column(
                              crossAxisAlignment: CrossAxisAlignment.start,
                              children: [
                                Text(
                                  _time(
                                    hp['t'] as num,
                                    seconds: widget.span == '1h',
                                  ),
                                  style: const TextStyle(
                                    fontWeight: FontWeight.w700,
                                  ),
                                ),
                                Row(
                                  children: [
                                    sw(t.series1),
                                    const SizedBox(width: 6),
                                    Text(
                                      'CPU ${_d(hp['cpu']).toStringAsFixed(1)}%',
                                    ),
                                  ],
                                ),
                                Row(
                                  children: [
                                    sw(t.series2, dashed: true),
                                    const SizedBox(width: 6),
                                    Text(
                                      'Memory ${_d(hp['mem']).toStringAsFixed(1)}%',
                                    ),
                                  ],
                                ),
                                Muted(
                                  'Load ${_d(hp['load']).toStringAsFixed(2)}',
                                  size: 12,
                                ),
                              ],
                            ),
                          ),
                        ),
                      ),
                  ],
                ),
              );
            },
          ),
      ],
    );
  }
}

class _Swatch extends CustomPainter {
  _Swatch(this.color, this.dashed);
  final Color color;
  final bool dashed;
  @override
  void paint(Canvas c, Size s) {
    final p = Paint()..color = color;
    if (!dashed) {
      c.drawRRect(
        RRect.fromRectAndRadius(Offset.zero & s, const Radius.circular(2)),
        p,
      );
      return;
    }
    for (var x = 0.0; x < s.width; x += 8) {
      c.drawRect(Rect.fromLTWH(x, 0, 5, s.height), p);
    }
  }

  @override
  bool shouldRepaint(_Swatch o) => o.color != color;
}

class _ChartPainter extends CustomPainter {
  _ChartPainter(
    this.pts,
    this.hover, {
    required this.cpu,
    required this.mem,
    required this.grid,
    required this.text,
    required this.strong,
    required this.card,
  });
  final List<Map<String, dynamic>> pts;
  final int? hover;
  final Color cpu, mem, grid, text, strong, card;

  static const l = 36.0, r = 56.0, top = 12.0, b = 26.0, h = 220.0;

  @override
  void paint(Canvas c, Size size) {
    final w = size.width;
    final iw = w - l - r, ih = h - top - b;
    double x(int i) => l + (pts.length <= 1 ? iw : i / (pts.length - 1) * iw);
    double y(double v) => top + ih - v.clamp(0, 100) / 100 * ih;

    void label(
      String s,
      Offset at, {
      TextAlign align = TextAlign.left,
      Color? color,
      FontWeight? weight,
    }) {
      final tp = TextPainter(
        text: TextSpan(
          text: s,
          style: TextStyle(
            fontFamily: kFont,
            fontSize: 11,
            color: color ?? text,
            fontWeight: weight,
          ),
        ),
        textDirection: TextDirection.ltr,
      )..layout();
      final dx = switch (align) {
        TextAlign.right => at.dx - tp.width,
        TextAlign.center => at.dx - tp.width / 2,
        _ => at.dx,
      };
      tp.paint(c, Offset(dx, at.dy - tp.height / 2));
    }

    final gp = Paint()
      ..color = grid.withValues(alpha: .6)
      ..strokeWidth = 1;
    for (final v in [0, 50, 100]) {
      c.drawLine(
        Offset(l, y(v.toDouble())),
        Offset(w - r, y(v.toDouble())),
        gp,
      );
      label('$v%', Offset(l - 8, y(v.toDouble())), align: TextAlign.right);
    }
    final seen = <String>{};
    for (final f in [0, .25, .5, .75, 1]) {
      final i = (f * (pts.length - 1)).round();
      final s = _time(pts[i]['t'] as num);
      if (seen.add(s)) label(s, Offset(x(i), h - 10), align: TextAlign.center);
    }

    Path line(String k) {
      final p = Path();
      for (var i = 0; i < pts.length; i++) {
        final o = Offset(x(i), y(_d(pts[i][k])));
        i == 0 ? p.moveTo(o.dx, o.dy) : p.lineTo(o.dx, o.dy);
      }
      return p;
    }

    Paint stroke(Color col) => Paint()
      ..color = col
      ..style = PaintingStyle.stroke
      ..strokeWidth = 2
      ..strokeJoin = StrokeJoin.round
      ..strokeCap = StrokeCap.round;
    // Memory, dashed 6 4
    final memPath = line('mem');
    for (final m in memPath.computeMetrics()) {
      for (var d = 0.0; d < m.length; d += 10) {
        c.drawPath(m.extractPath(d, d + 6), stroke(mem));
      }
    }
    c.drawPath(line('cpu'), stroke(cpu));

    final last = pts.last;
    var cy = y(_d(last['cpu'])), my = y(_d(last['mem']));
    if ((cy - my).abs() < 14) {
      final mid = (cy + my) / 2;
      (cy, my) = cy <= my ? (mid - 7, mid + 7) : (mid + 7, mid - 7);
    }
    label(
      '${_d(last['cpu']).round()}%',
      Offset(w - r + 8, cy),
      color: strong,
      weight: FontWeight.w600,
    );
    label(
      '${_d(last['mem']).round()}%',
      Offset(w - r + 8, my),
      color: strong,
      weight: FontWeight.w600,
    );

    if (hover != null) {
      final hx = x(hover!);
      c.drawLine(
        Offset(hx, top),
        Offset(hx, top + ih),
        Paint()
          ..color = text
          ..strokeWidth = 1,
      );
      for (final (k, col) in [('cpu', cpu), ('mem', mem)]) {
        final o = Offset(hx, y(_d(pts[hover!][k])));
        c.drawCircle(o, 5, Paint()..color = col);
        c.drawCircle(
          o,
          5,
          Paint()
            ..color = card
            ..style = PaintingStyle.stroke
            ..strokeWidth = 2,
        );
      }
    }
  }

  @override
  bool shouldRepaint(_ChartPainter o) =>
      o.pts != pts || o.hover != hover || o.cpu != cpu;
}

class _TableView extends StatelessWidget {
  const _TableView({required this.points, required this.span});
  final List<Map<String, dynamic>> points;
  final String span;

  @override
  Widget build(BuildContext context) {
    final step = (points.length / 24).floor().clamp(1, 1 << 30);
    final rows = [
      for (var i = 0; i < points.length; i += step) points[i],
    ].reversed.toList();
    TableRow row(List<String> cells, {bool head = false}) => TableRow(
      children: [
        for (final c in cells)
          Padding(
            padding: const EdgeInsets.symmetric(vertical: 6, horizontal: 4),
            child: head
                ? Muted(c, size: 12)
                : Text(
                    c,
                    style: const TextStyle(
                      fontSize: 13,
                      fontFeatures: [ui.FontFeature.tabularFigures()],
                    ),
                  ),
          ),
      ],
    );
    return ConstrainedBox(
      constraints: const BoxConstraints(maxHeight: 260),
      child: SingleChildScrollView(
        child: Table(
          children: [
            row(['Time', 'CPU', 'Memory', 'Load'], head: true),
            for (final p in rows)
              row([
                _time(p['t'] as num, seconds: span == '1h'),
                '${_d(p['cpu']).toStringAsFixed(1)}%',
                '${_d(p['mem']).toStringAsFixed(1)}%',
                _d(p['load']).toStringAsFixed(2),
              ]),
          ],
        ),
      ),
    );
  }
}
