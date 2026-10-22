import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:vps_admin/ui/widgets.dart';

// The wavy progress slides on its own, and reduced motion keeps it still: a ticker is scheduled
// only when the wave is allowed to move.
Widget host(double value, {bool reduced = false}) => MaterialApp(
  home: MediaQuery(
    data: MediaQueryData(disableAnimations: reduced),
    child: Scaffold(
      body: Center(child: SizedBox(width: 200, child: WaveProgress(value))),
    ),
  ),
);

void main() {
  testWidgets('the wave animates', (tester) async {
    await tester.pumpWidget(host(.42));
    await tester.pump(const Duration(milliseconds: 100));
    expect(tester.binding.transientCallbackCount, greaterThan(0));
  });

  testWidgets('reduced motion keeps it still', (tester) async {
    await tester.pumpWidget(host(.42, reduced: true));
    await tester.pump(const Duration(milliseconds: 100));
    expect(tester.binding.transientCallbackCount, 0);
  });
}
