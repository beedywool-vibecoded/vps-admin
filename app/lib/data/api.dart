import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';

import 'package:http/http.dart' as http;

import 'models.dart';

class ApiException implements Exception {
  ApiException(this.status, this.code, this.message);
  final int status;
  final String code;
  final String message;
  @override
  String toString() => message;
}

/// Client for one server, authenticated with its device token.
class Api {
  Api(this.base, this.token);
  final String base;
  final String token;
  static const _timeout = Duration(seconds: 20);

  Map<String, String> get _headers => {
    'Authorization': 'Bearer $token',
    'Accept': 'application/json',
  };

  static Future<String> pair(
    String base,
    String code,
    String name,
    String owner,
  ) async {
    final res = await http
        .post(
          Uri.parse('$base/api/auth/pair'),
          headers: {'Content-Type': 'application/json'},
          body: jsonEncode({
            'code': code,
            'name': name,
            'owner': owner,
            'kind': 'app',
          }),
        )
        .timeout(_timeout);
    final body = _decode(res);
    if (res.statusCode != 200) {
      final err = body['error'] as String? ?? 'http_${res.statusCode}';
      throw ApiException(res.statusCode, err, switch (err) {
        'bad_code' =>
          'This code is wrong, expired or already used. Run `sudo vps-admin pair` for a new one.',
        'rate_limited' => 'Too many attempts. Wait a few minutes.',
        _ => body['message'] as String? ?? 'Pairing failed (${res.statusCode})',
      });
    }
    return body['token'] as String;
  }

  static Map<String, dynamic> _decode(http.Response r) {
    try {
      final v = jsonDecode(utf8.decode(r.bodyBytes));
      return v is Map<String, dynamic> ? v : {};
    } catch (_) {
      return {};
    }
  }

  Future<Map<String, dynamic>> _send(
    String method,
    String path, [
    Object? body,
  ]) async {
    final req = http.Request(method, Uri.parse('$base$path'))
      ..headers.addAll(_headers);
    if (body != null) {
      req.headers['Content-Type'] = 'application/json';
      req.body = jsonEncode(body);
    }
    final res = await http.Response.fromStream(
      await req.send().timeout(_timeout),
    );
    final data = _decode(res);
    if (res.statusCode >= 400) {
      final code = data['error'] as String? ?? 'http_${res.statusCode}';
      throw ApiException(res.statusCode, code, switch (res.statusCode) {
        401 =>
          'This phone is no longer paired with the server. Remove it and pair again.',
        _ => data['message'] as String? ?? code,
      });
    }
    return data;
  }

  Future<Map<String, dynamic>> get(String path) => _send('GET', path);
  Future<Map<String, dynamic>> post(String path, [Object? body]) =>
      _send('POST', path, body);
  Future<Map<String, dynamic>> put(String path, Object body) =>
      _send('PUT', path, body);
  Future<Map<String, dynamic>> delete(String path) => _send('DELETE', path);

  Future<({Host host, Snapshot snapshot, String version})> system() async {
    final j = await get('/api/system');
    return (
      host: Host(j['host'] as Map<String, dynamic>),
      snapshot: Snapshot(j['snapshot'] as Map<String, dynamic>),
      version: j['version'] as String,
    );
  }

  Future<List<Service>> services() async =>
      ((await get('/api/services'))['services'] as List)
          .map((s) => Service(s as Map<String, dynamic>))
          .toList();
  Future<void> serviceAction(String name, String action) =>
      post('/api/services/${Uri.encodeComponent(name)}/$action');
  Future<String> serviceLogs(String name) async =>
      (await get(
            '/api/services/${Uri.encodeComponent(name)}/logs?lines=300',
          ))['logs']
          as String;

  Future<List<DockerContainer>> containers() async =>
      ((await get('/api/docker/containers'))['containers'] as List)
          .map((c) => DockerContainer(c as Map<String, dynamic>))
          .toList();
  Future<void> containerAction(String id, String action) =>
      post('/api/docker/containers/$id/$action');
  Future<String> containerLogs(String id) async =>
      (await get('/api/docker/containers/$id/logs?tail=300'))['logs'] as String;

  Future<Security> security({bool refresh = false}) async =>
      Security(await get('/api/security${refresh ? '?refresh=1' : ''}'));
  Future<UpdateStatus> update() async => UpdateStatus(await get('/api/update'));
  Future<UpdateStatus> checkUpdate() async =>
      UpdateStatus(await post('/api/update/check'));
  Future<void> applyUpdate() => post('/api/update/apply');
  Future<UpdateStatus> updateSettings({bool? auto, bool? prerelease}) async =>
      UpdateStatus(
        await put('/api/settings/update', {
          'auto': ?auto,
          'prerelease': ?prerelease,
        }),
      );

  Future<({List<Device> devices, String current})> devices() async {
    final j = await get('/api/devices');
    return (
      devices: [
        for (final d in j['devices'] as List) Device(d as Map<String, dynamic>),
      ],
      current: j['current'] as String? ?? '',
    );
  }

  Future<void> renameDevice(String id, String owner, String name) => _send(
    'PATCH',
    '/api/devices/${Uri.encodeComponent(id)}',
    {'owner': owner, 'name': name},
  );
  Future<void> revokeDevice(String id) =>
      delete('/api/devices/${Uri.encodeComponent(id)}');
  Future<List<AuditEntry>> audit({int before = 0}) async => [
    for (final e
        in (await get('/api/audit?limit=50&before=$before'))['entries'] as List)
      AuditEntry(e as Map<String, dynamic>),
  ];

  /// Pulls or rebuilds the compose service of a container and recreates it; the
  /// output arrives as it is written.
  Stream<String> rebuildContainer(String id) async* {
    final req = http.Request(
      'POST',
      Uri.parse(
        '$base/api/docker/containers/${Uri.encodeComponent(id)}/rebuild',
      ),
    )..headers.addAll(_headers);
    final res = await req.send().timeout(_timeout);
    if (res.statusCode >= 400) {
      final data = jsonDecode(await res.stream.bytesToString()) as Map;
      throw ApiException(
        res.statusCode,
        data['error'] as String? ?? 'http_${res.statusCode}',
        data['message'] as String? ?? 'Rebuild failed',
      );
    }
    yield* res.stream.transform(utf8.decoder);
  }

  /// Raw app settings, sent back whole by [saveApp].
  Future<Map<String, dynamic>> appRaw(String id) async =>
      (await get('/api/apps/$id'))['app'] as Map<String, dynamic>;
  Future<void> saveApp(String id, Map<String, dynamic> app) =>
      put('/api/apps/$id', app);

  Future<List<AppInfo>> apps() async =>
      ((await get('/api/apps'))['apps'] as List)
          .map((a) => AppInfo(a as Map<String, dynamic>))
          .toList();
  Future<({AppInfo app, List<Deployment> deployments})> app(String id) async {
    final j = await get('/api/apps/$id');
    return (
      app: AppInfo(j['app'] as Map<String, dynamic>),
      deployments: (j['deployments'] as List)
          .map((d) => Deployment(d as Map<String, dynamic>))
          .toList(),
    );
  }

  Future<void> deploy(String id) => post('/api/apps/$id/deploy');
  Future<UpdateCheck> checkApp(String id) async =>
      UpdateCheck(await get('/api/apps/$id/check'));
  Future<void> rollback(String id, int dep) =>
      post('/api/apps/$id/rollback/$dep');
  Future<void> restartApp(String id) => post('/api/apps/$id/restart');
  Future<String> deploymentLog(String id, int dep) async =>
      (await get('/api/apps/$id/deployments/$dep'))['log'] as String;

  Future<Map<String, dynamic>> assistant() => get('/api/assistant');

  Future<Map<String, dynamic>> appearance() => get('/api/appearance');
  Future<Uint8List?> background() async {
    final res = await http
        .get(Uri.parse('$base/api/appearance/background'), headers: _headers)
        .timeout(const Duration(seconds: 30));
    return res.statusCode == 200 ? res.bodyBytes : null;
  }

  Future<void> reboot(String confirm) =>
      post('/api/system/reboot', {'confirm': confirm});

  Future<
    ({List<Map<String, dynamic>> scripts, List<Map<String, dynamic>> runs})
  >
  scripts() async {
    final j = await get('/api/scripts');
    return (
      scripts: (j['scripts'] as List).cast<Map<String, dynamic>>(),
      runs: (j['runs'] as List).cast<Map<String, dynamic>>(),
    );
  }

  Future<void> renameScript(String name, String title) =>
      put('/api/scripts/${Uri.encodeComponent(name)}/title', {'title': title});
  Future<void> runScript(String name, String args) =>
      post('/api/scripts/${Uri.encodeComponent(name)}/run', {'args': args});
  Future<void> stopScript(String name) =>
      post('/api/scripts/${Uri.encodeComponent(name)}/stop');
  Future<String> scriptRunOutput(String id) async =>
      (await get('/api/script-runs/${Uri.encodeComponent(id)}'))['output']
          as String;
  Future<List<Conversation>> conversations() async =>
      ((await get('/api/assistant/conversations'))['conversations'] as List)
          .map((c) => Conversation(c as Map<String, dynamic>))
          .toList();
  Future<Map<String, dynamic>> conversation(String id) =>
      get('/api/assistant/conversations/$id');
  Future<Conversation> newConversation(String message) async => Conversation(
    (await post('/api/assistant/conversations', {
          'message': message,
        }))['conversation']
        as Map<String, dynamic>,
  );
  Future<void> sendMessage(String id, String message) =>
      post('/api/assistant/conversations/$id/messages', {'message': message});
  Future<void> stopConversation(String id) =>
      post('/api/assistant/conversations/$id/stop');
  Future<ToolCall> decide(String call, bool approve) async =>
      ToolCall(await post('/api/assistant/tools/$call', {'approve': approve}));

  // Raw JSON endpoints used by the panel-style pages ------------------------------

  Future<List<Map<String, dynamic>>> history(String span) async =>
      ((await get('/api/system/history?span=$span'))['points'] as List)
          .cast<Map<String, dynamic>>();
  Future<Map<String, Map<String, dynamic>>> containerStats() async => {
    for (final s
        in ((await get('/api/docker/stats'))['stats'] as List)
            .cast<Map<String, dynamic>>())
      s['id'] as String: s,
  };
  Future<Map<String, dynamic>> securityRaw({bool refresh = false}) =>
      get('/api/security${refresh ? '?refresh=1' : ''}');
  Future<Map<String, dynamic>> updateRaw() => get('/api/update');
  Future<Map<String, dynamic>> appsRaw() => get('/api/apps');
  Future<Map<String, dynamic>> appFull(String id) => get('/api/apps/$id');
  Future<Map<String, dynamic>> createApp(Map<String, dynamic> a) =>
      post('/api/apps', a);
  Future<void> deleteApp(String id, {bool volumes = false}) =>
      delete('/api/apps/$id${volumes ? '?volumes=1' : ''}');
  Future<void> setEnv(String id, List<Map<String, dynamic>> env) =>
      put('/api/apps/$id/env', {'env': env});
  Future<Map<String, dynamic>> github() => get('/api/github');
  Future<List<Map<String, dynamic>>> repos() async =>
      ((await get('/api/github/repos'))['repos'] as List)
          .cast<Map<String, dynamic>>();
  Future<Map<String, dynamic>> detect(
    Map<String, dynamic> repo,
    String ref,
    String root,
  ) => get(
    '/api/github/detect?installation=${repo['installationId']}'
    '&repo=${Uri.encodeQueryComponent(repo['fullName'] as String)}'
    '&ref=${Uri.encodeQueryComponent(ref)}&root=${Uri.encodeQueryComponent(root)}',
  );
  Future<Map<String, dynamic>> assistantUpdate(Map<String, dynamic> s) =>
      put('/api/assistant', s);
  Future<void> deleteConversation(String id) =>
      delete('/api/assistant/conversations/$id');
  Future<Map<String, dynamic>> setAppearance(double opacity) =>
      put('/api/appearance', {'opacity': opacity});
  Future<Map<String, dynamic>> setGroupIcon(String project, String icon) =>
      put('/api/appearance/icons', {'project': project, 'icon': icon});
  Future<Map<String, dynamic>> setGroupTitle(String project, String title) =>
      put('/api/appearance/titles', {'project': project, 'title': title});
  Future<Map<String, dynamic>> removeBackground() =>
      delete('/api/appearance/background');
  Future<Map<String, dynamic>> uploadBackground(
    List<int> bytes,
    String type,
  ) async {
    final res = await http
        .put(
          Uri.parse('$base/api/appearance/background'),
          headers: {..._headers, 'Content-Type': type},
          body: bytes,
        )
        .timeout(const Duration(seconds: 60));
    final data = _decode(res);
    if (res.statusCode >= 400) {
      throw ApiException(
        res.statusCode,
        data['error'] as String? ?? 'http_${res.statusCode}',
        data['message'] as String? ?? 'Upload failed',
      );
    }
    return data;
  }

  /// Server-sent events from [path], as (event, data) pairs. Reconnects are left to
  /// the caller, which reloads state anyway.
  Stream<(String, dynamic)> events(String path) async* {
    final client = http.Client();
    try {
      final req = http.Request('GET', Uri.parse('$base$path'))
        ..headers.addAll({..._headers, 'Accept': 'text/event-stream'});
      final res = await client.send(req);
      if (res.statusCode != 200) {
        throw ApiException(
          res.statusCode,
          'stream_failed',
          'Live updates failed (${res.statusCode})',
        );
      }
      var event = 'message';
      final data = StringBuffer();
      await for (final line
          in res.stream
              .transform(utf8.decoder)
              .transform(const LineSplitter())) {
        if (line.isEmpty) {
          if (data.isNotEmpty) yield (event, jsonDecode(data.toString()));
          event = 'message';
          data.clear();
        } else if (line.startsWith('event:')) {
          event = line.substring(6).trim();
        } else if (line.startsWith('data:')) {
          data.write(line.substring(5).trimLeft());
        }
      }
    } finally {
      client.close();
    }
  }
}
