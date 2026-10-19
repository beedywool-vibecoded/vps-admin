import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'api.dart';

/// A paired server. The token lives in the Android keystore, the rest in preferences.
class Server {
  Server({required this.id, required this.url, required this.name});
  final String id;
  final String url; // origin, e.g. https://admin.example.com
  String name;

  Map<String, dynamic> toJson() => {'id': id, 'url': url, 'name': name};
  static Server fromJson(Map<String, dynamic> j) => Server(
    id: j['id'] as String,
    url: j['url'] as String,
    name: j['name'] as String,
  );
}

enum Seed {
  purple('Purple', Color(0xFF6750A4)),
  teal('Teal', Color(0xFF006A6A)),
  blue('Blue', Color(0xFF0061A4)),
  pink('Pink', Color(0xFF984061)),
  green('Green', Color(0xFF386A20));

  const Seed(this.label, this.color);
  final String label;
  final Color color;
}

/// App-wide state: servers, the current one, appearance.
class AppState extends ChangeNotifier {
  AppState._(this._prefs);
  // null when the stored settings cannot be read: the app still works, without saving.
  final SharedPreferences? _prefs;
  static const _secure = FlutterSecureStorage(
    aOptions: AndroidOptions(encryptedSharedPreferences: true),
  );

  List<Server> servers = [];
  String? currentId;
  ThemeMode themeMode = ThemeMode.system;
  bool useDynamicColor = true;
  Seed seed = Seed.purple;
  final Map<String, Api> _apis = {};

  /// A state with nothing stored, used when the stored one cannot be read.
  static Future<AppState> empty() async {
    try {
      return AppState._(await SharedPreferences.getInstance());
    } catch (_) {
      return AppState._(null);
    }
  }

  static Future<AppState> load() async {
    final s = AppState._(await SharedPreferences.getInstance());
    final prefs = s._prefs!;
    final raw = prefs.getString('servers');
    if (raw != null) {
      s.servers = (jsonDecode(raw) as List)
          .map((e) => Server.fromJson(e as Map<String, dynamic>))
          .toList();
    }
    s.currentId = prefs.getString('current');
    if (s.servers.every((x) => x.id != s.currentId)) {
      s.currentId = s.servers.firstOrNull?.id;
    }
    s.themeMode = ThemeMode.values[prefs.getInt('themeMode') ?? 0];
    s.useDynamicColor = prefs.getBool('dynamic') ?? true;
    s.seed = Seed
        .values[(prefs.getInt('seed') ?? 0).clamp(0, Seed.values.length - 1)];
    for (final srv in s.servers) {
      final token = await _secure.read(key: 'token:${srv.id}');
      if (token != null) s._apis[srv.id] = Api(srv.url, token);
    }
    return s;
  }

  Server? get current => servers.where((s) => s.id == currentId).firstOrNull;
  Api? get api => currentId == null ? null : _apis[currentId];
  Api? apiFor(Server s) => _apis[s.id];

  Future<void> _save() async {
    await _prefs?.setString(
      'servers',
      jsonEncode(servers.map((s) => s.toJson()).toList()),
    );
    if (currentId != null) await _prefs?.setString('current', currentId!);
  }

  /// Pairs with a server using a code from `sudo vps-admin pair`.
  Future<Server> pair(String input, String code, String deviceName) async {
    final url = normalizeUrl(input);
    if (url == null) {
      throw ApiException(
        0,
        'invalid_url',
        'Enter the address of the panel, like admin.example.com',
      );
    }
    final token = await Api.pair(url, code, deviceName);
    final api = Api(url, token);
    String name = Uri.parse(url).host;
    try {
      name = (await api.system()).host.hostname;
    } catch (_) {}
    final id = DateTime.now().microsecondsSinceEpoch.toRadixString(36);
    await _secure.write(key: 'token:$id', value: token);
    final srv = Server(id: id, url: url, name: name);
    servers = [...servers.where((s) => s.url != url), srv];
    _apis[id] = api;
    currentId = id;
    await _save();
    notifyListeners();
    return srv;
  }

  Future<void> select(Server s) async {
    currentId = s.id;
    await _save();
    notifyListeners();
  }

  Future<void> remove(Server s) async {
    servers = servers.where((x) => x.id != s.id).toList();
    _apis.remove(s.id);
    await _secure.delete(key: 'token:${s.id}');
    if (currentId == s.id) currentId = servers.firstOrNull?.id;
    await _save();
    if (currentId == null) await _prefs?.remove('current');
    notifyListeners();
  }

  Future<void> setTheme({ThemeMode? mode, bool? dynamic, Seed? seed}) async {
    if (mode != null) {
      themeMode = mode;
      await _prefs?.setInt('themeMode', mode.index);
    }
    if (dynamic != null) {
      useDynamicColor = dynamic;
      await _prefs?.setBool('dynamic', dynamic);
    }
    if (seed != null) {
      this.seed = seed;
      await _prefs?.setInt('seed', seed.index);
    }
    notifyListeners();
  }

  static String? normalizeUrl(String input) {
    var v = input.trim();
    if (v.isEmpty) return null;
    if (!v.startsWith('http://') && !v.startsWith('https://')) v = 'https://$v';
    final u = Uri.tryParse(v);
    if (u == null || u.host.isEmpty) return null;
    return u.origin;
  }
}
