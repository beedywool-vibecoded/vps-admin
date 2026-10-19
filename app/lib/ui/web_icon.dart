import 'dart:math' as math;

import 'package:flutter/widgets.dart';

import 'icons_gen.dart';

/// The panel's icons, drawn from the same SVG paths (24 x 24 viewBox). Stroke icons use
/// a 2 unit round stroke, the few filled ones are filled, exactly as on the web.
class WIcon extends StatelessWidget {
  const WIcon(this.name, {super.key, this.size = 22, this.color});
  final String name;
  final double size;
  final Color? color;

  @override
  Widget build(BuildContext context) {
    final c = color ?? IconTheme.of(context).color ?? const Color(0xFF888888);
    final filled = filledIcons.containsKey(name);
    final d = filledIcons[name] ?? strokeIcons[name] ?? '';
    return SizedBox.square(
      dimension: size,
      child: CustomPaint(painter: _IconPainter(svgPath(d), c, filled)),
    );
  }
}

class _IconPainter extends CustomPainter {
  _IconPainter(this.path, this.color, this.filled);
  final Path path;
  final Color color;
  final bool filled;

  @override
  void paint(Canvas canvas, Size size) {
    canvas.scale(size.width / 24, size.height / 24);
    canvas.drawPath(
      path,
      Paint()
        ..color = color
        ..isAntiAlias = true
        ..style = filled ? PaintingStyle.fill : PaintingStyle.stroke
        ..strokeWidth = 2
        ..strokeCap = StrokeCap.round
        ..strokeJoin = StrokeJoin.round,
    );
  }

  @override
  bool shouldRepaint(_IconPainter old) =>
      old.path != path || old.color != color || old.filled != filled;
}

final _cache = <String, Path>{};

/// Parses SVG path data (all commands, relative and absolute, arcs included).
Path svgPath(String d) => _cache[d] ??= _parse(d);

Path _parse(String d) {
  final p = Path();
  final tokens = RegExp(
    r'[MmLlHhVvCcSsQqTtAaZz]|[-+]?(?:\d+\.?\d*|\.\d+)(?:[eE][-+]?\d+)?',
  ).allMatches(d).map((m) => m.group(0)!).toList();
  var i = 0;
  var cmd = '';
  var x = 0.0, y = 0.0, sx = 0.0, sy = 0.0;
  // Last control point, for the smooth S and T commands.
  var cx = 0.0, cy = 0.0;
  var prev = '';
  bool isCmd(String t) => RegExp(r'^[A-Za-z]$').hasMatch(t);
  double n() => double.parse(tokens[i++]);
  // Arc flags may be written without separators ("a1 1 0 0 0-2 0" or "011").
  double flag() {
    final t = tokens[i];
    if (t.length > 1 && (t[0] == '0' || t[0] == '1') && !t.contains('.')) {
      tokens[i] = t.substring(1);
      return t[0] == '1' ? 1 : 0;
    }
    i++;
    return double.parse(t);
  }

  while (i < tokens.length) {
    if (isCmd(tokens[i])) cmd = tokens[i++];
    final rel = cmd == cmd.toLowerCase();
    final ox = rel ? x : 0.0, oy = rel ? y : 0.0;
    switch (cmd.toUpperCase()) {
      case 'M':
        x = ox + n();
        y = oy + n();
        p.moveTo(x, y);
        sx = x;
        sy = y;
        // Further pairs after a moveto are linetos.
        cmd = rel ? 'l' : 'L';
      case 'L':
        x = ox + n();
        y = oy + n();
        p.lineTo(x, y);
      case 'H':
        x = ox + n();
        p.lineTo(x, y);
      case 'V':
        y = oy + n();
        p.lineTo(x, y);
      case 'C':
        final x1 = ox + n(), y1 = oy + n(), x2 = ox + n(), y2 = oy + n();
        x = ox + n();
        y = oy + n();
        p.cubicTo(x1, y1, x2, y2, x, y);
        cx = x2;
        cy = y2;
      case 'S':
        final smooth = prev == 'C' || prev == 'S';
        final x1 = smooth ? 2 * x - cx : x, y1 = smooth ? 2 * y - cy : y;
        final x2 = ox + n(), y2 = oy + n();
        x = ox + n();
        y = oy + n();
        p.cubicTo(x1, y1, x2, y2, x, y);
        cx = x2;
        cy = y2;
      case 'Q':
        final x1 = ox + n(), y1 = oy + n();
        x = ox + n();
        y = oy + n();
        p.quadraticBezierTo(x1, y1, x, y);
        cx = x1;
        cy = y1;
      case 'T':
        final smooth = prev == 'Q' || prev == 'T';
        final x1 = smooth ? 2 * x - cx : x, y1 = smooth ? 2 * y - cy : y;
        x = ox + n();
        y = oy + n();
        p.quadraticBezierTo(x1, y1, x, y);
        cx = x1;
        cy = y1;
      case 'A':
        final rx = n(), ry = n(), rot = n();
        final large = flag() == 1, sweep = flag() == 1;
        final ex = ox + n(), ey = oy + n();
        _arc(p, x, y, rx, ry, rot, large, sweep, ex, ey);
        x = ex;
        y = ey;
      case 'Z':
        p.close();
        x = sx;
        y = sy;
      default:
        i++;
    }
    prev = cmd.toUpperCase();
  }
  return p;
}

/// SVG endpoint arc to center parameterization, drawn as cubic segments.
void _arc(
  Path p,
  double x1,
  double y1,
  double rx,
  double ry,
  double angle,
  bool large,
  bool sweep,
  double x2,
  double y2,
) {
  if (rx == 0 || ry == 0) {
    p.lineTo(x2, y2);
    return;
  }
  rx = rx.abs();
  ry = ry.abs();
  final phi = angle * math.pi / 180;
  final cosP = math.cos(phi), sinP = math.sin(phi);
  final dx = (x1 - x2) / 2, dy = (y1 - y2) / 2;
  final x1p = cosP * dx + sinP * dy, y1p = -sinP * dx + cosP * dy;
  final lambda = (x1p * x1p) / (rx * rx) + (y1p * y1p) / (ry * ry);
  if (lambda > 1) {
    rx *= math.sqrt(lambda);
    ry *= math.sqrt(lambda);
  }
  final top = rx * rx * ry * ry - rx * rx * y1p * y1p - ry * ry * x1p * x1p;
  final den = rx * rx * y1p * y1p + ry * ry * x1p * x1p;
  var coef = math.sqrt(math.max(0, top / den));
  if (large == sweep) coef = -coef;
  final cxp = coef * rx * y1p / ry, cyp = -coef * ry * x1p / rx;
  final cx = cosP * cxp - sinP * cyp + (x1 + x2) / 2;
  final cy = sinP * cxp + cosP * cyp + (y1 + y2) / 2;
  double ang(double ux, double uy, double vx, double vy) {
    final a = math.atan2(ux * vy - uy * vx, ux * vx + uy * vy);
    return a;
  }

  final t1 = ang(1, 0, (x1p - cxp) / rx, (y1p - cyp) / ry);
  var dt = ang(
    (x1p - cxp) / rx,
    (y1p - cyp) / ry,
    (-x1p - cxp) / rx,
    (-y1p - cyp) / ry,
  );
  if (!sweep && dt > 0) dt -= 2 * math.pi;
  if (sweep && dt < 0) dt += 2 * math.pi;
  final segs = (dt.abs() / (math.pi / 2)).ceil().clamp(1, 8);
  final step = dt / segs;
  final k = 4 / 3 * math.tan(step / 4);
  var t = t1;
  for (var s = 0; s < segs; s++) {
    final c1 = math.cos(t), s1 = math.sin(t);
    final c2 = math.cos(t + step), s2 = math.sin(t + step);
    Offset pt(double ex, double ey) => Offset(
      cx + rx * ex * cosP - ry * ey * sinP,
      cy + rx * ex * sinP + ry * ey * cosP,
    );
    final a = pt(c1 - k * s1, s1 + k * c1);
    final b = pt(c2 + k * s2, s2 - k * c2);
    final e = pt(c2, s2);
    p.cubicTo(a.dx, a.dy, b.dx, b.dy, e.dx, e.dy);
    t += step;
  }
}
