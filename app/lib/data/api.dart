import 'dart:async';
import 'dart:convert';

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

  Map<String, String> get _headers => {'Authorization': 'Bearer $token', 'Accept': 'application/json'};

  static Future<String> pair(String base, String code, String name) async {
    final res = await http
        .post(Uri.parse('$base/api/auth/pair'),
            headers: {'Content-Type': 'application/json'}, body: jsonEncode({'code': code, 'name': name, 'kind': 'app'}))
        .timeout(_timeout);
    final body = _decode(res);
    if (res.statusCode != 200) {
      final err = body['error'] as String? ?? 'http_${res.statusCode}';
      throw ApiException(res.statusCode, err, switch (err) {
        'bad_code' => 'This code is wrong, expired or already used. Run "sudo vps-admin pair" for a new one.',
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

  Future<Map<String, dynamic>> _send(String method, String path, [Object? body]) async {
    final req = http.Request(method, Uri.parse('$base$path'))..headers.addAll(_headers);
    if (body != null) {
      req.headers['Content-Type'] = 'application/json';
      req.body = jsonEncode(body);
    }
    final res = await http.Response.fromStream(await req.send().timeout(_timeout));
    final data = _decode(res);
    if (res.statusCode >= 400) {
      final code = data['error'] as String? ?? 'http_${res.statusCode}';
      throw ApiException(res.statusCode, code, switch (res.statusCode) {
        401 => 'This phone is no longer paired with the server. Remove it and pair again.',
        _ => data['message'] as String? ?? code,
      });
    }
    return data;
  }

  Future<Map<String, dynamic>> get(String path) => _send('GET', path);
  Future<Map<String, dynamic>> post(String path, [Object? body]) => _send('POST', path, body);
  Future<Map<String, dynamic>> put(String path, Object body) => _send('PUT', path, body);
  Future<Map<String, dynamic>> delete(String path) => _send('DELETE', path);

  Future<({Host host, Snapshot snapshot, String version})> system() async {
    final j = await get('/api/system');
    return (host: Host(j['host'] as Map<String, dynamic>), snapshot: Snapshot(j['snapshot'] as Map<String, dynamic>), version: j['version'] as String);
  }

  Future<List<Service>> services() async =>
      ((await get('/api/services'))['services'] as List).map((s) => Service(s as Map<String, dynamic>)).toList();
  Future<void> serviceAction(String name, String action) => post('/api/services/${Uri.encodeComponent(name)}/$action');
  Future<String> serviceLogs(String name) async => (await get('/api/services/${Uri.encodeComponent(name)}/logs?lines=300'))['logs'] as String;

  Future<List<DockerContainer>> containers() async =>
      ((await get('/api/docker/containers'))['containers'] as List).map((c) => DockerContainer(c as Map<String, dynamic>)).toList();
  Future<void> containerAction(String id, String action) => post('/api/docker/containers/$id/$action');
  Future<String> containerLogs(String id) async => (await get('/api/docker/containers/$id/logs?tail=300'))['logs'] as String;

  Future<Security> security({bool refresh = false}) async => Security(await get('/api/security${refresh ? '?refresh=1' : ''}'));
  Future<UpdateStatus> update() async => UpdateStatus(await get('/api/update'));
  Future<UpdateStatus> checkUpdate() async => UpdateStatus(await post('/api/update/check'));
  Future<void> applyUpdate() => post('/api/update/apply');

  Future<List<AppInfo>> apps() async =>
      ((await get('/api/apps'))['apps'] as List).map((a) => AppInfo(a as Map<String, dynamic>)).toList();
  Future<({AppInfo app, List<Deployment> deployments})> app(String id) async {
    final j = await get('/api/apps/$id');
    return (
      app: AppInfo(j['app'] as Map<String, dynamic>),
      deployments: (j['deployments'] as List).map((d) => Deployment(d as Map<String, dynamic>)).toList(),
    );
  }

  Future<void> deploy(String id) => post('/api/apps/$id/deploy');
  Future<void> rollback(String id, int dep) => post('/api/apps/$id/rollback/$dep');
  Future<void> restartApp(String id) => post('/api/apps/$id/restart');
  Future<String> deploymentLog(String id, int dep) async => (await get('/api/apps/$id/deployments/$dep'))['log'] as String;

  Future<Map<String, dynamic>> assistant() => get('/api/assistant');
  Future<List<Conversation>> conversations() async =>
      ((await get('/api/assistant/conversations'))['conversations'] as List).map((c) => Conversation(c as Map<String, dynamic>)).toList();
  Future<Map<String, dynamic>> conversation(String id) => get('/api/assistant/conversations/$id');
  Future<Conversation> newConversation(String message) async =>
      Conversation((await post('/api/assistant/conversations', {'message': message}))['conversation'] as Map<String, dynamic>);
  Future<void> sendMessage(String id, String message) => post('/api/assistant/conversations/$id/messages', {'message': message});
  Future<void> stopConversation(String id) => post('/api/assistant/conversations/$id/stop');
  Future<ToolCall> decide(String call, bool approve) async => ToolCall(await post('/api/assistant/tools/$call', {'approve': approve}));

  /// Server-sent events from [path], as (event, data) pairs. Reconnects are left to
  /// the caller, which reloads state anyway.
  Stream<(String, dynamic)> events(String path) async* {
    final client = http.Client();
    try {
      final req = http.Request('GET', Uri.parse('$base$path'))
        ..headers.addAll({..._headers, 'Accept': 'text/event-stream'});
      final res = await client.send(req);
      if (res.statusCode != 200) throw ApiException(res.statusCode, 'stream_failed', 'Live updates failed (${res.statusCode})');
      var event = 'message';
      final data = StringBuffer();
      await for (final line in res.stream.transform(utf8.decoder).transform(const LineSplitter())) {
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
