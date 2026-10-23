import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:vps_admin/data/api.dart';
import 'package:vps_admin/ui/kit.dart';
import 'package:vps_admin/ui/live.dart';
import 'package:vps_admin/ui/pages/app_forms.dart';
import 'package:vps_admin/ui/pages/new_app.dart';

/// Answers the New app page without a server.
class FakeApi extends Api {
  FakeApi() : super('http://fake', 'token');

  @override
  Future<Map<String, dynamic>> get(String path) async {
    if (path == '/api/github') {
      return {
        'connected': true,
        'domains': true,
        'publicUrl': 'https://admin.example.com',
        'app': {'installUrl': ''},
      };
    }
    if (path == '/api/github/repos') {
      return {
        'repos': [
          {
            'fullName': 'beedywool/vps-admin',
            'defaultBranch': 'main',
            'installationId': 1,
            'language': 'Go',
            'pushedAt': DateTime.now().toUtc().toIso8601String(),
            'private': false,
          },
        ],
      };
    }
    if (path.startsWith('/api/github/branches')) {
      return {
        'branches': ['main'],
      };
    }
    if (path.startsWith('/api/github/detect')) {
      return {
        'detection': {
          'build': 'dockerfile',
          'port': 8080,
          'reason': 'A Dockerfile is present.',
        },
        'files': <String>[],
      };
    }
    if (path == '/api/system/environment') {
      return {
        'sites': true,
        'environment': {
          'caddy': {'installed': true},
          'nginx': {'installed': false},
          'certbot': {'installed': false},
          'wireguard': <Object>[],
          'zerotier': <Object>[],
          'tailscale': {'running': false, 'https': false, 'name': ''},
        },
      };
    }
    return {};
  }

  @override
  Future<Never> system() async => throw StateError('offline');

  @override
  Future<Map<String, dynamic>> appearance() async => {};

  @override
  Stream<(String, dynamic)> events(String path) =>
      Stream<(String, dynamic)>.multi((_) {});
}

void main() {
  testWidgets('New app keeps one step on screen and marks finished tabs', (
    tester,
  ) async {
    final live = LiveSession(FakeApi());
    addTearDown(live.dispose);
    final semantics = tester.ensureSemantics();

    await tester.pumpWidget(
      MaterialApp(
        theme: webTheme(
          ColorScheme.fromSeed(seedColor: const Color(0xFF6750A4)),
        ),
        home: Live(session: live, child: const NewAppPage()),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('Pick a repository'), findsOneWidget);
    // Only the step being filled keeps its name on screen.
    expect(find.text('Source'), findsOneWidget);
    expect(find.text('Build and settings'), findsNothing);
    expect(find.text('Deploy'), findsNothing);
    expect(find.bySemanticsLabel('Step 2: Build and settings'), findsOneWidget);
    expect(
      find.byWidgetPredicate((w) => w is WIcon && w.name == 'check'),
      findsNothing,
    );

    await tester.tap(find.text('vps-admin').first);
    await tester.pumpAndSettle();
    expect(find.text('Pick a repository'), findsNothing);
    expect(find.text('Source'), findsNothing);
    expect(find.text('Build and settings'), findsOneWidget);
    expect(find.text('Environment variables'), findsOneWidget);
    expect(
      find.byWidgetPredicate((w) => w is WIcon && w.name == 'check'),
      findsOneWidget,
    );

    // Each idea has its own heading and a line that says what it is for.
    expect(
      find.text(
        'The name it gets here, the branch to deploy and the folder to build.',
      ),
      findsOneWidget,
    );
    expect(
      find.text(
        'What turns the repository into an image, and how much it may use.',
      ),
      findsOneWidget,
    );
    expect(
      find.text('What happens after each push to the branch.'),
      findsOneWidget,
    );

    // The explanation sits under the box, so every field in the row is as tall.
    final nameField = find.byType(TextField).first;
    expect(
      tester.getTopLeft(find.text('Also the container name')).dy,
      greaterThanOrEqualTo(tester.getBottomLeft(nameField).dy),
    );
    final branchBox = tester.getSize(
      find
          .descendant(
            of: find.byType(SelectField<String>),
            matching: find.byType(Material),
          )
          .first,
    );
    expect(branchBox.height, tester.getSize(nameField).height);

    await tester.ensureVisible(find.text('Continue'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Continue'));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 100));
    // Both steps share the transition: the old one is still fading out.
    expect(find.text('Environment variables'), findsOneWidget);
    expect(find.text('Ready to deploy'), findsOneWidget);
    await tester.pumpAndSettle();
    expect(find.text('Ready to deploy'), findsOneWidget);
    expect(find.text('Environment variables'), findsNothing);
    expect(
      find.text(
        'The app is created and its first deployment starts immediately.',
      ),
      findsOneWidget,
    );
    // The step row is the first item of the page list: scroll back to it.
    await tester.drag(find.byType(ListView).first, const Offset(0, 2000));
    await tester.pumpAndSettle();
    expect(
      find.byWidgetPredicate((w) => w is WIcon && w.name == 'check'),
      findsNWidgets(2),
    );
    semantics.dispose();
  });
}
