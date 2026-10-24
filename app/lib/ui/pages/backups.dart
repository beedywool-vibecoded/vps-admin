import 'dart:async';

import 'package:flutter/material.dart';

import '../../data/api.dart';
import '../../data/models.dart';
import '../kit.dart';
import '../live.dart';
import '../widgets.dart';

/// BackupCard.vue: the Google Drive connection, the daily archive, the manual runs and
/// the archives to restore. Works from the phone like from the panel.
class BackupsPage extends StatefulWidget {
  const BackupsPage({super.key});

  @override
  State<BackupsPage> createState() => _BackupsPageState();
}

class _BackupsPageState extends State<BackupsPage> {
  BackupStatus? st;
  bool loading = true;
  bool _filled = false;
  bool editing = false;
  String busy = '';
  bool enabled = false, full = false, mountOn = true;

  final token = TextEditingController();
  final passphrase = TextEditingController();
  final path = TextEditingController();
  final hour = TextEditingController();
  final keep = TextEditingController();
  Timer? timer;

  Api get api => Live.read(context).api;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) => _load());
  }

  @override
  void dispose() {
    timer?.cancel();
    for (final c in [token, passphrase, path, hour, keep]) {
      c.dispose();
    }
    super.dispose();
  }

  Future<void> _load({bool archives = false}) async {
    try {
      final s = BackupStatus(await api.backupRaw(archives: archives));
      if (!mounted) return;
      setState(() {
        st = s;
        loading = false;
        if (!_filled && !editing) {
          path.text = s.path;
          hour.text = '${s.hour}';
          keep.text = '${s.keep}';
          enabled = s.enabled;
          full = s.kind == 'full';
          mountOn = s.mount;
          _filled = true;
        }
      });
      if (s.running) _poll();
    } catch (e) {
      if (!mounted) return;
      setState(() => loading = false);
      showError(context, e);
    }
  }

  // The panel reports its progress; ask again while a run is writing.
  void _poll() {
    timer?.cancel();
    timer = Timer(const Duration(seconds: 2), () {
      if (mounted) _load(archives: true);
    });
  }

  Future<void> _save(Map<String, dynamic> patch, String done) async {
    setState(() => busy = 'save');
    try {
      final s = BackupStatus(await api.saveBackup(patch));
      if (!mounted) return;
      setState(() {
        st = s;
        editing = false;
        token.clear();
        passphrase.clear();
      });
      showInfo(context, done);
      // A first connection has nothing to prefill yet: read the defaults now.
      if (!_filled) await _load(archives: true);
    } catch (e) {
      if (mounted) showError(context, e);
    } finally {
      if (mounted) setState(() => busy = '');
    }
  }

  Future<void> _connect() async {
    final t = token.text.trim();
    final p = passphrase.text;
    if (t.isEmpty && st?.hasToken != true) {
      showError(context, 'Paste the rclone token first');
      return;
    }
    if (p.isEmpty && st?.hasPassphrase != true) {
      showError(context, 'Choose a passphrase: it encrypts every archive');
      return;
    }
    final patch = <String, dynamic>{
      if (t.isNotEmpty) 'token': t,
      if (p.isNotEmpty) 'passphrase': p,
    };
    await _save(patch, 'Google Drive connected');
  }

  Future<void> _disconnect() async {
    if (!await wConfirm(
      context,
      title: 'Disconnect Google Drive?',
      body:
          'The token and the rclone configuration are deleted. The archives already on the Drive stay there, but this server stops backing up until it is connected again.',
      action: 'Disconnect',
      danger: true,
    )) {
      return;
    }
    await _save({'token': ''}, 'Google Drive disconnected');
  }

  Future<void> _saveSchedule() async {
    final h = (int.tryParse(hour.text) ?? 3).clamp(0, 23);
    final k = (int.tryParse(keep.text) ?? 7).clamp(1, 60);
    setState(() {
      hour.text = '$h';
      keep.text = '$k';
    });
    await _save({
      'path': path.text.trim(),
      'hour': h,
      'keep': k,
      'kind': full ? 'full' : 'settings',
      'enabled': enabled,
      'mount': mountOn,
    }, 'Backup settings saved');
  }

  Future<void> _test() async {
    setState(() => busy = 'test');
    try {
      final r = await api.testBackup();
      if (mounted) showInfo(context, r['detail'] as String? ?? 'Google Drive answers');
    } catch (e) {
      if (mounted) showError(context, e);
    } finally {
      if (mounted) setState(() => busy = '');
    }
  }

  Future<void> _toggleMount() async {
    final on = !(st?.mounted ?? false);
    setState(() => busy = 'mount');
    try {
      final s = BackupStatus(await api.mountBackup(on));
      if (!mounted) return;
      setState(() => st = s);
      showInfo(context, on ? 'Google Drive mounted' : 'Google Drive unmounted');
    } catch (e) {
      if (mounted) showError(context, e);
    } finally {
      if (mounted) setState(() => busy = '');
    }
  }

  Future<void> _run(String kind) async {
    setState(() => busy = 'run');
    try {
      await api.runBackup(kind);
      if (!mounted) return;
      showInfo(
        context,
        kind == 'full'
            ? 'Full backup started: sources and volumes'
            : 'Backup started',
      );
      await _load(archives: true);
    } catch (e) {
      if (mounted) showError(context, e);
    } finally {
      if (mounted) setState(() => busy = '');
    }
  }

  Future<void> _restore(BackupArchive a) async {
    final pass = TextEditingController();
    final ok = await showWDialog<bool>(
      context,
      title: 'Restore this backup',
      body: (_) => Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        mainAxisSize: MainAxisSize.min,
        children: [
          Muted(
            'The panel replaces its database and its key with ${a.name}, then restarts. '
            'A full archive also brings back the app folders, the wallpaper and the Docker '
            'volumes, with the containers paused while they are unpacked. The files it '
            'replaces are kept next to the database.',
          ),
          const SizedBox(height: 14),
          TextField(
            controller: pass,
            obscureText: true,
            autofocus: true,
            decoration: const InputDecoration(
              hintText: 'Passphrase of the archive',
            ),
          ),
        ],
      ),
      actions: (c) => [
        WButton(
          'Cancel',
          kind: BtnKind.text,
          onPressed: () => Navigator.pop(c, false),
        ),
        WButton(
          'Restore and restart',
          onPressed: () => Navigator.pop(c, pass.text.isNotEmpty),
        ),
      ],
    );
    if (ok != true || !mounted) return;
    setState(() => busy = 'restore');
    try {
      await api.restoreBackup(a.name, pass.text);
      if (!mounted) return;
      showInfo(
        context,
        'Restoring: the panel restarts with the backup in a few seconds',
      );
      timer?.cancel();
      // The agent is unreachable while it swaps the files; wait, then ask again.
      for (var i = 0; i < 8; i++) {
        await Future<void>.delayed(const Duration(seconds: 2));
        try {
          final s = BackupStatus(await api.backupRaw(archives: true));
          if (mounted) setState(() => st = s);
          break;
        } catch (_) {}
      }
    } catch (e) {
      if (mounted) showError(context, e);
    } finally {
      if (mounted) setState(() => busy = '');
    }
  }

  @override
  Widget build(BuildContext context) {
    final s = st;
    final cs = Theme.of(context).colorScheme;
    final t = Tones.of(context);

    Widget field(
      TextEditingController c,
      String label, {
      bool secret = false,
      int lines = 1,
      TextInputType? keyboard,
      String? hint,
    }) => Padding(
      padding: const EdgeInsets.only(bottom: 12),
      child: TextField(
        controller: c,
        obscureText: secret,
        maxLines: lines,
        minLines: lines > 1 ? 3 : 1,
        keyboardType: keyboard,
        autocorrect: false,
        enableSuggestions: false,
        decoration: InputDecoration(labelText: label, hintText: hint),
      ),
    );

    Widget row({
      required Widget left,
      required List<Widget> actions,
      bool first = false,
    }) => Container(
      padding: const EdgeInsets.symmetric(vertical: 10),
      decoration: BoxDecoration(
        border: first
            ? null
            : Border(top: BorderSide(color: cs.surfaceContainerHigh)),
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.center,
        children: [
          Expanded(child: left),
          for (final a in actions) ...[const SizedBox(width: 6), a],
        ],
      ),
    );

    Widget titled(String a, String b) => Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      mainAxisSize: MainAxisSize.min,
      children: [Text(a), Muted(b)],
    );

    if (loading) {
      return const WPage(
        children: [
          PageTitle('Backups'),
          WCard(child: Center(child: PeloteLoader(label: 'Loading backups'))),
        ],
      );
    }

    final connected = s?.connected ?? false;
    final tone = s == null
        ? t.warn
        : s.running
        ? t.warn
        : s.last?.ok == true
        ? t.ok
        : s.last?.error.isNotEmpty == true
        ? t.bad
        : cs.onSurfaceVariant;

    final connectCard = WCard(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          CardHead('Google Drive', leading: WIcon('cloud', color: cs.primary)),
          Muted(
            'On a computer, install rclone and run `rclone authorize "drive"`, then paste '
            'the JSON it prints below. The token stays on this server. The passphrase '
            'encrypts every archive: without it a backup cannot be opened, and with it an '
            'archive gives back every secret the panel holds.',
          ),
          const SizedBox(height: 14),
          field(
            token,
            'rclone token',
            lines: 4,
            hint: s?.hasToken == true
                ? 'Saved: paste a new token to replace it'
                : '{"access_token":"…","refresh_token":"…"}',
          ),
          field(
            passphrase,
            'Passphrase (12 characters or more)',
            secret: true,
            hint: s?.hasPassphrase == true
                ? 'Saved: paste a new passphrase to replace it'
                : '',
          ),
          Row(
            children: [
              WButton(
                connected ? 'Save changes' : 'Connect the Drive',
                icon: 'check',
                busy: busy == 'save',
                onPressed: busy == 'save' ? null : _connect,
              ),
              if (editing) ...[
                const SizedBox(width: 8),
                WButton(
                  'Cancel',
                  kind: BtnKind.text,
                  onPressed: () {
                    setState(() {
                      editing = false;
                      token.clear();
                      passphrase.clear();
                    });
                  },
                ),
              ],
            ],
          ),
          if (s?.hasPassphrase == true) ...[
            const SizedBox(height: 10),
            Muted(
              'Changing the passphrase makes the archives already on the Drive '
              'unreadable. Download them first if you need them.',
            ),
          ],
        ],
      ),
    );

    return WPage(
      onRefresh: () => _load(archives: true),
      children: [
        PageTitle(
          'Backups',
          sub: 'Google Drive, encrypted',
          actions: [
            if (s?.rclone.isNotEmpty == true)
              Chip(
                avatar: WIcon('cloud', size: 16, color: cs.onSecondaryContainer),
                label: const Text('Google Drive'),
              ),
          ],
        ),
        if (s?.warning.isNotEmpty == true)
          WCard(
            color: cs.errorContainer,
            child: Row(
              children: [
                WIcon('alert', color: cs.onErrorContainer),
                const SizedBox(width: 10),
                Expanded(
                  child: Text(
                    s!.warning,
                    style: TextStyle(color: cs.onErrorContainer),
                  ),
                ),
              ],
            ),
          ),
        if (s?.restoreStaged == true)
          WCard(
            child: Row(
              children: [
                WIcon('history', color: cs.primary),
                const SizedBox(width: 10),
                const Expanded(
                  child: Text(
                    'A restore is staged: the panel installs it at its next restart.',
                  ),
                ),
              ],
            ),
          ),
        if (!connected || editing) connectCard,
        if (connected && !editing) ...[
          WCard(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                CardHead(
                  'Google Drive',
                  leading: WIcon('cloud', color: tone),
                  trailing: [
                    WButton(
                      'Test',
                      kind: BtnKind.text,
                      busy: busy == 'test',
                      onPressed: busy == 'test' ? null : _test,
                    ),
                    WButton(
                      s!.mounted ? 'Unmount' : 'Mount',
                      kind: BtnKind.tonal,
                      busy: busy == 'mount',
                      onPressed: busy == 'mount' ? null : _toggleMount,
                    ),
                  ],
                ),
                row(
                  first: true,
                  left: titled(
                    s.mounted ? 'Mounted' : 'Connected, not mounted',
                    s.mounted
                        ? 'Read and written at ${s.mountPoint}'
                        : 'Archives go through rclone until it is mounted',
                  ),
                  actions: const [],
                ),
                row(
                  left: titled(
                    'Mount at boot',
                    'The panel mounts the Drive again if the unit stops',
                  ),
                  actions: [
                    Switch(
                      value: mountOn,
                      onChanged: (v) {
                        setState(() => mountOn = v);
                        _save({'mount': v}, v ? 'Drive mounts at boot' : 'No mount at boot');
                      },
                    ),
                  ],
                ),
                const SizedBox(height: 6),
                Wrap(
                  spacing: 8,
                  runSpacing: 8,
                  children: [
                    WButton(
                      'Change Drive token',
                      icon: 'key',
                      kind: BtnKind.text,
                      onPressed: () => setState(() => editing = true),
                    ),
                    WButton(
                      'Disconnect',
                      icon: 'logout',
                      kind: BtnKind.text,
                      onPressed: busy == 'save' ? null : _disconnect,
                    ),
                  ],
                ),
              ],
            ),
          ),
          WCard(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                CardHead(
                  'Daily backup',
                  leading: WIcon('history', color: cs.primary),
                ),
                row(
                  first: true,
                  left: titled(
                    'Enabled',
                    enabled
                        ? 'A backup every day at ${s.hour}:00'
                        : 'Nothing runs on its own',
                  ),
                  actions: [
                    Switch(
                      value: enabled,
                      onChanged: (v) => setState(() => enabled = v),
                    ),
                  ],
                ),
                row(
                  left: titled(
                    'Everything, not just the panel state',
                    'Adds app sources, the wallpaper and Docker volumes. '
                        'Containers are paused while volumes are copied.',
                  ),
                  actions: [
                    Switch(
                      value: full,
                      onChanged: (v) => setState(() => full = v),
                    ),
                  ],
                ),
                const SizedBox(height: 12),
                TextField(
                  controller: path,
                  decoration: const InputDecoration(
                    labelText: 'Folder on the Drive',
                    hintText: 'vps-admin',
                  ),
                ),
                const SizedBox(height: 12),
                Row(
                  children: [
                    Expanded(
                      child: TextField(
                        controller: hour,
                        keyboardType: TextInputType.number,
                        decoration: const InputDecoration(
                          labelText: 'Hour (0-23)',
                        ),
                      ),
                    ),
                    const SizedBox(width: 12),
                    Expanded(
                      child: TextField(
                        controller: keep,
                        keyboardType: TextInputType.number,
                        decoration: const InputDecoration(
                          labelText: 'Archives to keep',
                        ),
                      ),
                    ),
                  ],
                ),
                const SizedBox(height: 14),
                Align(
                  alignment: Alignment.centerLeft,
                  child: WButton(
                    'Save',
                    icon: 'check',
                    kind: BtnKind.outlined,
                    busy: busy == 'save',
                    onPressed: busy == 'save' ? null : _saveSchedule,
                  ),
                ),
              ],
            ),
          ),
          WCard(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                CardHead(
                  'Back up now',
                  leading: WIcon('database', color: cs.primary),
                ),
                Muted(
                  'Both write an encrypted archive and upload it to the Drive. '
                  'The full one pauses the containers while it copies their volumes.',
                ),
                const SizedBox(height: 12),
                Wrap(
                  spacing: 8,
                  runSpacing: 8,
                  children: [
                    WButton(
                      s.running ? (s.progress.isEmpty ? 'Backing up…' : s.progress) : 'Panel state',
                      icon: 'cloud',
                      busy: busy == 'run' || s.running,
                      onPressed: busy == 'run' || s.running
                          ? null
                          : () => _run('settings'),
                    ),
                    WButton(
                      'Everything',
                      icon: 'database',
                      kind: BtnKind.outlined,
                      onPressed: busy == 'run' || s.running
                          ? null
                          : () => _run('full'),
                    ),
                  ],
                ),
              ],
            ),
          ),
          if (s.last != null)
            WCard(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  CardHead(
                    'Last backup',
                    leading: WIcon(
                      s.last!.ok ? 'check' : 'alert',
                      color: s.last!.ok ? t.ok : t.bad,
                    ),
                    trailing: [
                      Chip(
                        label: Text(s.last!.kind == 'full' ? 'Everything' : 'Panel state'),
                      ),
                    ],
                  ),
                  Text(
                    s.last!.ok
                        ? 'Succeeded ${ago(s.last!.at)}'
                        : 'Failed ${ago(s.last!.at)}',
                    style: TextStyle(color: s.last!.ok ? t.ok : t.bad),
                  ),
                  const SizedBox(height: 4),
                  Muted(
                    [
                      if (s.last!.file.isNotEmpty) s.last!.file,
                      if (s.last!.size > 0) bytes(s.last!.size.toDouble()),
                    ].join(' · '),
                  ),
                  if (s.last!.error.isNotEmpty) ...[
                    const SizedBox(height: 6),
                    Text(s.last!.error, style: TextStyle(color: t.bad)),
                  ],
                ],
              ),
            ),
          WCard(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                CardHead(
                  'Archives on the Drive',
                  leading: WIcon('folder', color: cs.primary),
                  trailing: [
                    WIconButton(
                      'refresh',
                      tooltip: 'Refresh',
                      onPressed: () => _load(archives: true),
                    ),
                  ],
                ),
                if (s.archives.isEmpty)
                  const Muted('None yet.')
                else
                  for (var i = 0; i < s.archives.length; i++)
                    row(
                      first: i == 0,
                      left: titled(
                        s.archives[i].name,
                        '${dateTime(s.archives[i].time.toIso8601String())}'
                            '${s.archives[i].size > 0 ? ' · ${bytes(s.archives[i].size.toDouble())}' : ''}',
                      ),
                      actions: [
                        WButton(
                          'Restore',
                          kind: BtnKind.text,
                          onPressed: busy == 'restore'
                              ? null
                              : () => _restore(s.archives[i]),
                        ),
                      ],
                    ),
              ],
            ),
          ),
        ],
      ],
    );
  }
}
