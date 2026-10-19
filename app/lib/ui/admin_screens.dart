import 'package:flutter/material.dart';

import '../data/api.dart';
import '../data/models.dart';
import 'scope.dart';
import 'widgets.dart';

/// Devices paired with the server, grouped by the person using them, as in the panel.
class DevicesScreen extends StatelessWidget {
  const DevicesScreen({super.key});

  @override
  Widget build(BuildContext context) {
    final api = AppScope.of(context).api!;
    return Scaffold(
      appBar: AppBar(title: const Text('Devices')),
      body: Loader(
        load: api.devices,
        builder: (context, d, reload) {
          final cs = Theme.of(context).colorScheme;
          final me = d.devices.where((x) => x.id == d.current).firstOrNull;
          final people = <String, List<Device>>{};
          for (final x in d.devices) {
            (people[x.owner] ??= []).add(x);
          }
          final names = people.keys.toList()
            ..sort((a, b) {
              if (a == me?.owner) return -1;
              if (b == me?.owner) return 1;
              if (a.isEmpty) return 1;
              if (b.isEmpty) return -1;
              return a.toLowerCase().compareTo(b.toLowerCase());
            });
          return ListView(
            padding: const EdgeInsets.fromLTRB(12, 8, 12, 24),
            children: [
              if (me != null && me.owner.isEmpty)
                Card(
                  color: cs.tertiaryContainer,
                  child: ListTile(
                    leading: Icon(Icons.person, color: cs.onTertiaryContainer),
                    title: Text(
                      'Who uses this phone?',
                      style: TextStyle(color: cs.onTertiaryContainer),
                    ),
                    subtitle: Text(
                      'Admins tell each other apart by name',
                      style: TextStyle(color: cs.onTertiaryContainer),
                    ),
                    trailing: FilledButton(
                      onPressed: () => _rename(context, api, me, reload),
                      child: const Text('Name'),
                    ),
                  ),
                ),
              const Padding(
                padding: EdgeInsets.fromLTRB(8, 8, 8, 8),
                child: CmdText(
                  'To add a device, run `sudo vps-admin pair` on the server.',
                ),
              ),
              for (final owner in names) ...[
                Padding(
                  padding: const EdgeInsets.fromLTRB(8, 12, 8, 6),
                  child: Row(
                    children: [
                      CircleAvatar(
                        radius: 18,
                        backgroundColor: owner.isEmpty
                            ? cs.surfaceContainerHighest
                            : cs.primaryContainer,
                        child: Text(
                          owner.isEmpty ? '?' : owner[0].toUpperCase(),
                          style: TextStyle(
                            color: owner.isEmpty
                                ? cs.onSurfaceVariant
                                : cs.onPrimaryContainer,
                            fontWeight: FontWeight.w600,
                          ),
                        ),
                      ),
                      const SizedBox(width: 12),
                      Text(
                        owner.isEmpty ? 'Not named yet' : owner,
                        style: Theme.of(context).textTheme.titleMedium,
                      ),
                      const SizedBox(width: 8),
                      Text(
                        '${people[owner]!.length} device${people[owner]!.length == 1 ? '' : 's'}',
                        style: TextStyle(color: cs.onSurfaceVariant),
                      ),
                    ],
                  ),
                ),
                Card(
                  margin: const EdgeInsets.only(bottom: 8),
                  child: Column(
                    children: [
                      for (final x in people[owner]!)
                        ListTile(
                          leading: Icon(
                            x.kind == 'app' ? Icons.smartphone : Icons.language,
                          ),
                          title: Row(
                            children: [
                              Flexible(
                                child: Text(
                                  x.name,
                                  overflow: TextOverflow.ellipsis,
                                ),
                              ),
                              if (x.id == d.current) ...[
                                const SizedBox(width: 8),
                                Chip(
                                  label: const Text('This phone'),
                                  visualDensity: VisualDensity.compact,
                                  padding: EdgeInsets.zero,
                                  labelStyle: Theme.of(
                                    context,
                                  ).textTheme.labelSmall,
                                ),
                              ],
                            ],
                          ),
                          subtitle: Text(
                            '${x.kind == 'app' ? 'Android app' : 'Browser · ${x.passkeys} passkey${x.passkeys == 1 ? '' : 's'}'}'
                            ' · seen ${x.lastSeenAt == null ? 'never' : ago(x.lastSeenAt!)}'
                            '${x.lastIp.isNotEmpty ? ' from ${x.lastIp}' : ''}',
                          ),
                          trailing: PopupMenuButton<String>(
                            onSelected: (v) => v == 'rename'
                                ? _rename(context, api, x, reload)
                                : _remove(context, api, x, d.current, reload),
                            itemBuilder: (_) => const [
                              PopupMenuItem(
                                value: 'rename',
                                child: Text('Rename'),
                              ),
                              PopupMenuItem(
                                value: 'remove',
                                child: Text('Remove'),
                              ),
                            ],
                          ),
                        ),
                    ],
                  ),
                ),
              ],
            ],
          );
        },
      ),
    );
  }

  Future<void> _rename(
    BuildContext context,
    Api api,
    Device d,
    Future<void> Function() reload,
  ) async {
    final owner = TextEditingController(text: d.owner);
    final name = TextEditingController(text: d.name);
    final ok = await showDialog<bool>(
      context: context,
      builder: (c) => AlertDialog(
        title: const Text('Rename the device'),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            TextField(
              controller: owner,
              autofocus: d.owner.isEmpty,
              textCapitalization: TextCapitalization.words,
              decoration: const InputDecoration(
                labelText: 'Person using it',
                border: OutlineInputBorder(),
              ),
            ),
            const SizedBox(height: 12),
            TextField(
              controller: name,
              decoration: const InputDecoration(
                labelText: 'Device',
                border: OutlineInputBorder(),
              ),
            ),
          ],
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(c, false),
            child: const Text('Cancel'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(c, true),
            child: const Text('Save'),
          ),
        ],
      ),
    );
    if (ok != true || !context.mounted) return;
    if (owner.text.trim().isEmpty || name.text.trim().isEmpty) {
      showError(context, 'Both the person and the device name are needed');
      return;
    }
    try {
      await api.renameDevice(d.id, owner.text.trim(), name.text.trim());
      await reload();
    } catch (e) {
      if (context.mounted) showError(context, e);
    }
  }

  Future<void> _remove(
    BuildContext context,
    Api api,
    Device d,
    String current,
    Future<void> Function() reload,
  ) async {
    final self = d.id == current;
    if (!await confirmAction(
      context,
      title: 'Remove ${d.owner.isNotEmpty ? "${d.owner}'s " : ''}${d.name}?',
      body: self
          ? 'This phone loses its access to the server right away.'
          : 'Its access stops right away.',
      action: 'Remove',
      danger: true,
    )) {
      return;
    }
    try {
      await api.revokeDevice(d.id);
      if (!self) await reload();
    } catch (e) {
      if (context.mounted) showError(context, e);
    }
  }
}

/// Every sign-in and action on the server, newest first.
class ActivityScreen extends StatefulWidget {
  const ActivityScreen({super.key});

  @override
  State<ActivityScreen> createState() => _ActivityScreenState();
}

class _ActivityScreenState extends State<ActivityScreen> {
  final _entries = <AuditEntry>[];
  bool _loading = true;
  bool _more = true;
  String? _error;

  static const _labels = {
    'auth.pair': 'Paired a device',
    'auth.passkey_login': 'Signed in with a passkey',
    'auth.passkey_register': 'Added a passkey',
    'device.revoke': 'Removed a device',
    'device.rename': 'Renamed a device',
    'update.apply': 'Installed an update',
    'update.auto': 'Auto-update',
    'settings.update': 'Changed update settings',
  };

  String _describe(AuditEntry e) {
    final l = _labels[e.action];
    if (l != null) return l;
    final parts = e.action.split('.');
    final verb = parts.length > 1 ? parts[1].replaceAll('_', ' ') : e.action;
    return '${verb[0].toUpperCase()}${verb.substring(1)} ${parts[0]}';
  }

  Future<void> _load({bool more = false}) async {
    setState(() => _loading = true);
    try {
      final api = AppScope.of(context).api!;
      final page = await api.audit(
        before: more && _entries.isNotEmpty ? _entries.last.id : 0,
      );
      setState(() {
        if (!more) _entries.clear();
        _entries.addAll(page);
        _more = page.length == 50;
        _error = null;
      });
    } catch (e) {
      setState(() => _error = e is ApiException ? e.message : e.toString());
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    if (_entries.isEmpty && _error == null) _load();
  }

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    return Scaffold(
      appBar: AppBar(title: const Text('Activity')),
      body: RefreshIndicator(
        onRefresh: _load,
        child: ListView(
          padding: const EdgeInsets.fromLTRB(12, 8, 12, 24),
          children: [
            if (_error != null)
              Padding(
                padding: const EdgeInsets.all(16),
                child: Text(_error!, style: TextStyle(color: cs.error)),
              ),
            for (final e in _entries)
              ListTile(
                leading: CircleAvatar(
                  radius: 12,
                  backgroundColor: e.ok
                      ? cs.primaryContainer
                      : cs.errorContainer,
                  child: Icon(
                    e.ok ? Icons.check : Icons.priority_high,
                    size: 14,
                    color: e.ok ? cs.onPrimaryContainer : cs.onErrorContainer,
                  ),
                ),
                title: Text(
                  e.target.isEmpty
                      ? _describe(e)
                      : '${_describe(e)} · ${e.target}',
                ),
                subtitle: Text(
                  '${ago(e.at)} · ${e.device.isEmpty ? 'system' : e.device}'
                  '${e.ip.isNotEmpty ? ' · ${e.ip}' : ''}'
                  '${!e.ok && e.detail.isNotEmpty ? ' · ${e.detail}' : ''}',
                ),
              ),
            if (_loading)
              const Padding(
                padding: EdgeInsets.all(24),
                child: Center(child: CircularProgressIndicator()),
              )
            else if (_more && _entries.isNotEmpty)
              TextButton(
                onPressed: () => _load(more: true),
                child: const Text('Show older'),
              ),
          ],
        ),
      ),
    );
  }
}

/// The agent's own updates: signed releases, installed now or automatically.
class AgentUpdatesScreen extends StatelessWidget {
  const AgentUpdatesScreen({super.key});

  @override
  Widget build(BuildContext context) {
    final api = AppScope.of(context).api!;
    return Scaffold(
      appBar: AppBar(title: const Text('Server updates')),
      body: Loader<UpdateStatus>(
        load: api.update,
        refreshEvery: const Duration(seconds: 10),
        builder: (context, u, reload) {
          final t = Theme.of(context);
          Future<void> run(Future<void> Function() f, [String? done]) async {
            try {
              await f();
              await reload();
              if (done != null && context.mounted) showInfo(context, done);
            } catch (e) {
              if (context.mounted) showError(context, e);
            }
          }

          return ListView(
            padding: const EdgeInsets.all(16),
            children: [
              Card(
                margin: EdgeInsets.zero,
                child: Padding(
                  padding: const EdgeInsets.all(20),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Row(
                        children: [
                          ShapeIcon(
                            M3Shape.cookie,
                            size: 48,
                            color: t.colorScheme.primaryContainer,
                          ),
                          const SizedBox(width: 16),
                          Expanded(
                            child: Column(
                              crossAxisAlignment: CrossAxisAlignment.start,
                              children: [
                                Text(
                                  'VPS Admin agent ${u.current}',
                                  style: t.textTheme.titleMedium,
                                ),
                                Text(
                                  u.running
                                      ? 'Installing…'
                                      : u.available
                                      ? '${u.latest} is available'
                                      : u.error != null
                                      ? 'Check failed: ${u.error}'
                                      : 'Up to date, signed builds only',
                                  style: t.textTheme.bodySmall,
                                ),
                              ],
                            ),
                          ),
                        ],
                      ),
                      if (u.lastResult != null && u.lastResult!.isNotEmpty) ...[
                        const SizedBox(height: 12),
                        Text(
                          'Last: ${u.lastResult}',
                          style: t.textTheme.bodySmall,
                        ),
                      ],
                      const SizedBox(height: 16),
                      Row(
                        children: [
                          Expanded(
                            child: OutlinedButton(
                              onPressed: u.running
                                  ? null
                                  : () => run(api.checkUpdate),
                              child: const Text('Check now'),
                            ),
                          ),
                          const SizedBox(width: 8),
                          Expanded(
                            child: FilledButton(
                              onPressed: !u.available || u.running
                                  ? null
                                  : () => run(
                                      api.applyUpdate,
                                      'Installing, the server restarts the agent',
                                    ),
                              child: const Text('Install'),
                            ),
                          ),
                        ],
                      ),
                    ],
                  ),
                ),
              ),
              const SizedBox(height: 12),
              Card(
                margin: EdgeInsets.zero,
                child: Column(
                  children: [
                    SwitchListTile(
                      title: const Text('Automatic updates'),
                      subtitle: const Text(
                        'Installs signed releases by itself, with a rollback if the new version does not start',
                      ),
                      value: u.auto,
                      onChanged: (v) => run(() => api.updateSettings(auto: v)),
                    ),
                    SwitchListTile(
                      title: const Text('Pre-releases'),
                      subtitle: const Text('Also take test versions'),
                      value: u.prerelease,
                      onChanged: (v) =>
                          run(() => api.updateSettings(prerelease: v)),
                    ),
                  ],
                ),
              ),
            ],
          );
        },
      ),
    );
  }
}
