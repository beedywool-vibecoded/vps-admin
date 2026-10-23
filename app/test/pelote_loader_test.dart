import 'dart:math' as math;
import 'dart:ui' as ui;

import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:vps_admin/ui/widgets.dart';

double angleOf(WidgetTester tester) {
  final transform = tester
      .widget<Transform>(
        find
            .descendant(
              of: find.byType(PeloteLoader),
              matching: find.byType(Transform),
            )
            .first,
      )
      .transform;
  return math.atan2(transform.entry(1, 0), transform.entry(0, 0));
}

Widget host({bool reduce = false}) => MaterialApp(
  home: MediaQuery(
    data: MediaQueryData(disableAnimations: reduce),
    child: const Scaffold(body: Center(child: PeloteLoader())),
  ),
);

void main() {
  testWidgets('the loader turns while it waits', (tester) async {
    await tester.pumpWidget(host());
    final before = angleOf(tester);
    await tester.pump(const Duration(milliseconds: 250));
    expect(angleOf(tester), closeTo(before, 0.001));
    // The 300 ms timer fires during this pump; the rotation starts on the next frame.
    await tester.pump(const Duration(milliseconds: 250));
    await tester.pump(const Duration(milliseconds: 150));
    expect(angleOf(tester), isNot(closeTo(before, 0.1)));
    await tester.pumpWidget(const SizedBox());
  });

  testWidgets('the loader paints its shape', (tester) async {
    const key = ValueKey('loader-box');
    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(
          body: Center(
            child: RepaintBoundary(
              key: key,
              child: PeloteLoader(size: 96, label: 'Painting loader'),
            ),
          ),
        ),
      ),
    );
    final box = tester.renderObject<RenderRepaintBoundary>(
      find.byKey(key),
    );
    final data = await tester.runAsync(() async {
      final image = await box.toImage();
      return image.toByteData(format: ui.ImageByteFormat.rawRgba);
    });
    var painted = 0;
    for (var i = 3; i < data!.lengthInBytes; i += 4) {
      if (data.getUint8(i) > 0) painted++;
    }
    expect(painted, greaterThan(500));
    await tester.pumpWidget(const SizedBox());
  });

  testWidgets('reduced motion keeps the first shape still', (tester) async {
    await tester.pumpWidget(host(reduce: true));
    final before = angleOf(tester);
    await tester.pump(const Duration(milliseconds: 1500));
    expect(angleOf(tester), closeTo(before, 0.001));
    await tester.pumpWidget(const SizedBox());
  });
}
