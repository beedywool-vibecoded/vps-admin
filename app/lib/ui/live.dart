import 'dart:async';
import 'dart:typed_data';

import 'package:flutter/widgets.dart';

import '../data/api.dart';
import '../data/models.dart';

/// What every page shares for the current server, like the panel's session: the host,
/// the agent version, the latest system snapshot (streamed) and the appearance.
class LiveSession extends ChangeNotifier {
  LiveSession(this.api) {
    _start();
  }
  final Api api;

  Host? host;
  Snapshot? snapshot;
  Map<String, dynamic>? snapshotRaw;
  String version = '';
  String? error;

  // Appearance, shared by all admins: background image, its opacity, group icons.
  Uint8List? background;
  double opacity = 0.35;
  Map<String, String> groupIcons = {};
  Map<String, String> groupTitles = {};

  /// The name shown for a compose project: its title if one was given.
  String groupTitle(String project) => groupTitles[project] ?? project;

  StreamSubscription<(String, dynamic)>? _sub;
  Timer? _retry;
  bool _closed = false;

  Future<void> _start() async {
    try {
      final s = await api.system();
      host = s.host;
      snapshot = s.snapshot;
      version = s.version;
      error = null;
      notifyListeners();
    } catch (e) {
      error = e is ApiException ? e.message : e.toString();
      notifyListeners();
    }
    loadAppearance();
    _stream();
  }

  void _stream() {
    _sub?.cancel();
    _sub = api
        .events('/api/system/stream')
        .listen(
          (ev) {
            final data = ev.$2;
            if (data is Map<String, dynamic>) {
              snapshotRaw = data;
              snapshot = Snapshot(data);
              notifyListeners();
            }
          },
          onError: (_) => _again(),
          onDone: _again,
        );
  }

  void _again() {
    if (_closed) return;
    _retry?.cancel();
    _retry = Timer(const Duration(seconds: 5), _stream);
  }

  Future<void> loadAppearance() async {
    try {
      final a = await api.appearance();
      applyAppearance(a);
      background = a['background'] == true ? await api.background() : null;
      notifyListeners();
    } catch (_) {}
  }

  /// Takes an appearance answer from the server (after a change) without refetching.
  void applyAppearance(Map<String, dynamic> a) {
    opacity = (a['opacity'] as num?)?.toDouble() ?? 0.35;
    groupIcons = {
      for (final e in ((a['groupIcons'] as Map?) ?? {}).entries)
        e.key as String: e.value as String,
    };
    groupTitles = {
      for (final e in ((a['groupTitles'] as Map?) ?? {}).entries)
        e.key as String: e.value as String,
    };
    notifyListeners();
  }

  @override
  void dispose() {
    _closed = true;
    _sub?.cancel();
    _retry?.cancel();
    super.dispose();
  }
}

class Live extends InheritedNotifier<LiveSession> {
  const Live({super.key, required LiveSession session, required super.child})
    : super(notifier: session);

  static LiveSession of(BuildContext context) =>
      context.dependOnInheritedWidgetOfExactType<Live>()!.notifier!;

  /// Without listening, for callbacks.
  static LiveSession read(BuildContext context) =>
      context.getInheritedWidgetOfExactType<Live>()!.notifier!;
}
