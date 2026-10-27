import 'dart:async';
import 'dart:io';

import 'package:crypto/crypto.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:http/http.dart' as http;
import 'package:path_provider/path_provider.dart';

import 'api.dart';

class AppRelease {
  AppRelease(Map<String, dynamic> j)
    : versionCode = (j['versionCode'] as num).toInt(),
      versionName = j['versionName'] as String,
      sha256 = j['sha256'] as String?,
      size = (j['size'] as num?)?.toInt();
  final int versionCode;
  final String versionName;
  final String? sha256;
  final int? size;
}

/// Self-update through the connected server, which fetches the APK from the GitHub
/// release (it has the token). Android rejecting a different signing key is the final check.
class Updater extends ChangeNotifier {
  static const _channel = MethodChannel('fr.beedy.vps_admin/updater');

  static Future<void> openUrl(String url) =>
      _channel.invokeMethod('openUrl', {'url': url});

  /// Opens the system picker for a JPEG, PNG or WebP; null when cancelled.
  static Future<({List<int> bytes, String type})?> pickImage() async {
    final r = await _channel.invokeMapMethod<String, dynamic>('pickImage');
    if (r == null) return null;
    return (bytes: (r['bytes'] as List).cast<int>(), type: r['type'] as String);
  }

  /// The phone model, like "Google Pixel 10 Pro Fold".
  static Future<String> deviceModel() async {
    try {
      return await _channel.invokeMethod<String>('model') ?? 'Android phone';
    } catch (_) {
      return 'Android phone';
    }
  }

  int? currentCode;
  String? currentName;
  AppRelease? latest;
  double? progress;
  String? error;
  String? _waitingInstall;

  String get currentLabel =>
      currentName == null ? '?' : '$currentName ($currentCode)';
  AppRelease? get available =>
      latest != null &&
          currentCode != null &&
          latest!.versionCode > currentCode!
      ? latest
      : null;

  Future<void> init() async {
    try {
      final v = await _channel.invokeMapMethod<String, dynamic>('version');
      currentCode = (v!['code'] as num).toInt();
      currentName = v['name'] as String?;
    } on MissingPluginException {
      // tests
    }
  }

  Future<void> check(Api? api) async {
    if (api == null || currentCode == null) return;
    try {
      latest = AppRelease(await api.get('/api/app/latest'));
      error = null;
    } on ApiException catch (e) {
      latest = null;
      if (e.status != 404) error = e.message;
    } catch (e) {
      error = 'Could not check for updates';
    }
    notifyListeners();
  }

  Future<void> install(Api api) async {
    final rel = available;
    if (rel == null) return;
    error = null;
    progress = 0;
    notifyListeners();
    try {
      final dir = await getTemporaryDirectory();
      final file = File('${dir.path}/updates/vps-admin-${rel.versionCode}.apk');
      await file.parent.create(recursive: true);
      final req = http.Request('GET', Uri.parse('${api.base}/api/app/apk'))
        ..headers['Authorization'] = 'Bearer ${api.token}';
      final res = await http.Client().send(req);
      if (res.statusCode != 200) throw HttpException('HTTP ${res.statusCode}');
      final sink = file.openWrite();
      final total = res.contentLength ?? rel.size ?? 0;
      var got = 0;
      final digest = AccumulatorSink<Digest>();
      final hasher = sha256.startChunkedConversion(digest);
      await for (final chunk in res.stream) {
        sink.add(chunk);
        hasher.add(chunk);
        got += chunk.length;
        if (total > 0) {
          progress = got / total;
          notifyListeners();
        }
      }
      await sink.close();
      hasher.close();
      if (rel.sha256 != null && digest.events.single.toString() != rel.sha256) {
        await file.delete();
        throw const FormatException('The download is corrupted, try again');
      }
      final canInstall =
          await _channel.invokeMethod<bool>('canInstall') ?? false;
      if (!canInstall) {
        _waitingInstall = file.path;
        await _channel.invokeMethod('openInstallSettings');
      } else {
        await _channel.invokeMethod('install', {'path': file.path});
      }
    } catch (e) {
      error = e.toString();
    } finally {
      progress = null;
      notifyListeners();
    }
  }

  /// Called when the app comes back from the "install unknown apps" settings.
  Future<void> resume() async {
    final path = _waitingInstall;
    if (path == null) return;
    if (await _channel.invokeMethod<bool>('canInstall') ?? false) {
      _waitingInstall = null;
      await _channel.invokeMethod('install', {'path': path});
    }
  }
}

class AccumulatorSink<T> implements Sink<T> {
  final events = <T>[];
  @override
  void add(T event) => events.add(event);
  @override
  void close() {}
}
