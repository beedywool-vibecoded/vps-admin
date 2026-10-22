import 'package:flutter_test/flutter_test.dart';
import 'package:vps_admin/data/models.dart';
import 'package:vps_admin/data/servers.dart';
import 'package:vps_admin/ui/widgets.dart';

void main() {
  test('server URLs are normalized to an https origin', () {
    expect(AppState.normalizeUrl('admin.example.com'), 'https://admin.example.com');
    expect(AppState.normalizeUrl('https://admin.example.com/apps?x=1'), 'https://admin.example.com');
    expect(AppState.normalizeUrl('http://localhost:7171'), 'http://localhost:7171');
    expect(AppState.normalizeUrl('   '), isNull);
  });

  test('snapshot picks the root disk and computes percentages', () {
    final s = Snapshot({
      'uptime': 90061, 'cpu': 12.5, 'load': [0.5, 0.4, 0.3], 'memTotal': 1000, 'memUsed': 250,
      'netRx': 0, 'netTx': 0,
      'disks': [
        {'mount': '/boot', 'fs': 'ext4', 'total': 100, 'used': 50},
        {'mount': '/', 'fs': 'ext4', 'total': 200, 'used': 150},
      ],
    });
    expect(s.memPercent, 25);
    expect(s.root!.mount, '/');
    expect(s.root!.percent, 75);
    expect(duration(s.uptime), '1 d 1 h');
  });

  test('bytes are formatted in binary units', () {
    expect(bytes(512), '512 B');
    expect(bytes(1536), '1.5 KiB');
    expect(bytes(3.7 * 1024 * 1024 * 1024), '3.7 GiB');
  });

  test('proxy sites keep their domain, upstream and flags', () {
    final r = ProxyReport({
      'engine': 'caddy',
      'root': '/etc/caddy',
      'sites': [
        {'domains': ['blog.example.com', 'www.blog.example.com'], 'target': '127.0.0.1:8080', 'file': '/etc/caddy/vps-admin-apps/blog.caddy', 'managed': true, 'id': 'blog', 'vpnOnly': true, 'scheme': 'https'},
        {'domains': ['files.example.com'], 'target': '', 'managed': false},
      ],
      'unreadable': ['/etc/caddy/old.caddy'],
    });
    expect(r.engine, 'caddy');
    final blog = r.sites.first;
    expect(blog.url, 'https://blog.example.com');
    expect(blog.domains, ['blog.example.com', 'www.blog.example.com']);
    expect(blog.target, '127.0.0.1:8080');
    expect(blog.managed, isTrue);
    expect(blog.id, 'blog');
    expect(blog.vpnOnly, isTrue);
    final files = r.sites.last;
    expect(files.target, isEmpty);
    expect(files.scheme, 'https');
    expect(files.managed, isFalse);
    expect(r.unreadable, ['/etc/caddy/old.caddy']);
  });

  test('deployment helpers', () {
    final d = Deployment({'id': 3, 'sha': 'abcdef1234567', 'status': 'building', 'trigger': 'push', 'createdAt': '2026-10-18T10:00:00Z'});
    expect(d.short, 'abcdef1');
    expect(d.running, isTrue);
  });
}
