import 'package:flutter/material.dart';

import '../../data/api.dart';
import '../../data/models.dart';
import '../../data/servers.dart';
import '../../data/updater.dart';
import '../add_server_screen.dart';
import '../kit.dart';
import '../live.dart';
import '../scope.dart';
import '../widgets.dart';

/// SettingsView.vue: appearance, server background, devices by person, activity, plus
/// what only the app has: the servers it knows and its own update.
class SettingsPage extends StatefulWidget {
  const SettingsPage({super.key});

  @override
  State<SettingsPage> createState() => _SettingsPageState();
}

class _SettingsPageState extends State<SettingsPage> {
  List<Device> devices = [];
  String current = '';
  final audit = <AuditEntry>[];
  bool moreAudit = true,
      auditOpen = false,
      loadingAudit = false,
      uploading = false;
  double? opacity;

  LiveSession get live => Live.read(context);
  Api get api => live.api;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      _devices();
      _audit();
    });
  }

  Future<void> _devices() async {
    try {
      final d = await api.devices();
      if (mounted) {
        setState(() {
          devices = d.devices;
          current = d.current;
        });
      }
    } catch (e) {
      if (mounted) showError(context, e);
    }
  }

  Future<void> _audit({bool more = false}) async {
    setState(() => loadingAudit = true);
    try {
      final page = await api.audit(
        before: more && audit.isNotEmpty ? audit.last.id : 0,
      );
      if (!mounted) return;
      setState(() {
        if (!more) audit.clear();
        audit.addAll(page);
        moreAudit = page.length == 50;
      });
    } catch (e) {
      if (mounted) showError(context, e);
    } finally {
      if (mounted) setState(() => loadingAudit = false);
    }
  }

  Future<void> _pickBackground() async {
    try {
      final img = await Updater.pickImage();
      if (img == null) return;
      setState(() => uploading = true);
      live.applyAppearance(await api.uploadBackground(img.bytes, img.type));
      await live.loadAppearance();
      if (mounted) showInfo(context, 'Background updated for everyone');
    } catch (e) {
      if (mounted) showError(context, e);
    } finally {
      if (mounted) setState(() => uploading = false);
    }
  }

  Future<void> _dropBackground() async {
    if (!await wConfirm(
      context,
      title: 'Remove the background?',
      body: 'Everyone gets the plain background again.',
      action: 'Remove',
    )) {
      return;
    }
    try {
      live.applyAppearance(await api.removeBackground());
      await live.loadAppearance();
    } catch (e) {
      if (mounted) showError(context, e);
    }
  }

  Future<void> _rename(Device d) async {
    final owner = TextEditingController(text: d.owner);
    final name = TextEditingController(text: d.name);
    final ok = await showWDialog<bool>(
      context,
      title: 'Rename the device',
      body: (_) => Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          TextField(
            controller: owner,
            autofocus: d.owner.isEmpty,
            textCapitalization: TextCapitalization.words,
            decoration: InputDecoration(
              hintText: 'Person',
              prefixIcon: Padding(
                padding: const EdgeInsets.all(14),
                child: WIcon(
                  'person',
                  size: 20,
                  color: Theme.of(context).colorScheme.onSurfaceVariant,
                ),
              ),
            ),
          ),
          const SizedBox(height: 12),
          TextField(
            controller: name,
            decoration: InputDecoration(
              hintText: 'Device',
              prefixIcon: Padding(
                padding: const EdgeInsets.all(14),
                child: WIcon(
                  d.kind == 'app' ? 'phone' : 'globe',
                  size: 20,
                  color: Theme.of(context).colorScheme.onSurfaceVariant,
                ),
              ),
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
        WButton('Save', onPressed: () => Navigator.pop(c, true)),
      ],
    );
    if (ok != true) return;
    if (owner.text.trim().isEmpty || name.text.trim().isEmpty) {
      if (mounted) {
        showError(context, 'Both the person and the device name are needed');
      }
      return;
    }
    try {
      await api.renameDevice(d.id, owner.text.trim(), name.text.trim());
      await _devices();
    } catch (e) {
      if (mounted) showError(context, e);
    }
  }

  Future<void> _revoke(Device d) async {
    final self = d.id == current;
    if (!await wConfirm(
      context,
      title: "Remove ${d.owner.isNotEmpty ? "${d.owner}'s " : ''}${d.name}?",
      body: self
          ? 'This is the phone you are using: it loses access and you will need a new pairing code to come back.'
          : 'Its ${d.kind == 'app' ? 'token stops working' : 'passkeys and sessions are deleted'} right away.',
      action: 'Remove',
      danger: true,
    )) {
      return;
    }
    try {
      await api.revokeDevice(d.id);
      if (!mounted) return;
      showInfo(context, '${d.name} removed');
      if (!self) await _devices();
    } catch (e) {
      if (mounted) showError(context, e);
    }
  }

  static const _actions = {
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
    final l = _actions[e.action];
    if (l != null) return l;
    final p = e.action.split('.');
    final verb = p.length > 1 ? p[1] : e.action;
    final kind = p[0] == 'container'
        ? 'container'
        : p[0] == 'service'
        ? 'service'
        : '';
    return '${verb[0].toUpperCase()}${verb.substring(1)} $kind'.trim();
  }

  @override
  Widget build(BuildContext context) {
    final state = AppScope.of(context);
    final updater = AppScope.updaterOf(context);
    final l = Live.of(context);
    final cs = Theme.of(context).colorScheme;
    final t = Tones.of(context);
    Widget setting(Widget left, Widget right, {bool first = false}) =>
        Container(
          padding: const EdgeInsets.symmetric(vertical: 12),
          decoration: BoxDecoration(
            border: first
                ? null
                : Border(top: BorderSide(color: cs.surfaceContainerHigh)),
          ),
          child: Row(
            children: [
              Expanded(child: left),
              right,
            ],
          ),
        );
    Widget titled(String a, String b) => Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [Text(a), Muted(b)],
    );

    // Devices by person, the current person first.
    final me = devices.where((d) => d.id == current).firstOrNull;
    final people = <String, List<Device>>{};
    for (final d in devices) {
      (people[d.owner] ??= []).add(d);
    }
    final names = people.keys.toList()
      ..sort(
        (a, b) => a == me?.owner
            ? -1
            : b == me?.owner
            ? 1
            : a.isEmpty
            ? 1
            : b.isEmpty
            ? -1
            : a.toLowerCase().compareTo(b.toLowerCase()),
      );
    final shownAudit = auditOpen ? audit : audit.take(5).toList();

    return WPage(
      children: [
        const PageTitle('Settings'),
        WCard(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              const CardHead('Appearance'),
              setting(
                titled('Theme', 'Saved on this phone'),
                Seg<ThemeMode>(
                  options: const [
                    (ThemeMode.system, 'System'),
                    (ThemeMode.dark, 'Dark'),
                    (ThemeMode.light, 'Light'),
                  ],
                  value: state.themeMode,
                  onChanged: (m) => state.setTheme(mode: m),
                ),
                first: true,
              ),
              setting(
                titled('Material You', 'The palette follows your wallpaper'),
                Switch(
                  value: state.useDynamicColor,
                  onChanged: (v) => state.setTheme(dynamic: v),
                ),
              ),
              if (!state.useDynamicColor)
                Container(
                  padding: const EdgeInsets.symmetric(vertical: 12),
                  decoration: BoxDecoration(
                    border: Border(
                      top: BorderSide(color: cs.surfaceContainerHigh),
                    ),
                  ),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      titled(
                        'Seed color',
                        'The whole palette is generated from it',
                      ),
                      const SizedBox(height: 12),
                      Wrap(
                        spacing: 12,
                        runSpacing: 12,
                        children: [
                          for (final s in Seed.values)
                            Tooltip(
                              message: s.label,
                              child: GestureDetector(
                                onTap: () => state.setTheme(seed: s),
                                child: Container(
                                  width: 44,
                                  height: 44,
                                  decoration: BoxDecoration(
                                    color: s.color,
                                    shape: BoxShape.circle,
                                    boxShadow: state.seed == s
                                        ? [
                                            BoxShadow(
                                              color: cs.surfaceContainer,
                                              spreadRadius: 3,
                                            ),
                                            BoxShadow(
                                              color: cs.onSurface,
                                              spreadRadius: 5,
                                            ),
                                          ]
                                        : null,
                                  ),
                                ),
                              ),
                            ),
                        ],
                      ),
                    ],
                  ),
                ),
            ],
          ),
        ),
        WCard(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              const CardHead(
                'Server background',
                sub: 'Shown to everyone, on the panel and in the app',
              ),
              Wrap(
                spacing: 20,
                runSpacing: 16,
                children: [
                  Container(
                    width: 240,
                    height: 150,
                    clipBehavior: Clip.antiAlias,
                    decoration: BoxDecoration(
                      color: cs.surface,
                      borderRadius: BorderRadius.circular(16),
                      border: Border.all(color: cs.outlineVariant),
                    ),
                    child: l.background == null
                        ? const Center(child: Muted('Plain'))
                        : Opacity(
                            opacity: opacity ?? l.opacity,
                            child: Image.memory(
                              l.background!,
                              fit: BoxFit.cover,
                              gaplessPlayback: true,
                            ),
                          ),
                  ),
                  ConstrainedBox(
                    constraints: const BoxConstraints(maxWidth: 420),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Wrap(
                          spacing: 8,
                          runSpacing: 8,
                          children: [
                            WButton(
                              l.background != null
                                  ? 'Change image'
                                  : 'Choose an image',
                              icon: 'plus',
                              kind: BtnKind.tonal,
                              onPressed: uploading ? null : _pickBackground,
                            ),
                            if (l.background != null)
                              WButton(
                                'Use a plain background',
                                kind: BtnKind.text,
                                onPressed: _dropBackground,
                              ),
                          ],
                        ),
                        if (l.background != null) ...[
                          const SizedBox(height: 12),
                          const Muted('Opacity'),
                          Row(
                            children: [
                              Expanded(
                                child: Slider(
                                  value: opacity ?? l.opacity,
                                  min: .05,
                                  max: 1,
                                  divisions: 19,
                                  label:
                                      '${((opacity ?? l.opacity) * 100).round()}%',
                                  onChanged: (v) => setState(() => opacity = v),
                                  onChangeEnd: (v) async {
                                    try {
                                      l.applyAppearance(
                                        await api.setAppearance(v),
                                      );
                                    } catch (e) {
                                      if (context.mounted) {
                                        showError(context, e);
                                      }
                                    }
                                    if (mounted) setState(() => opacity = null);
                                  },
                                ),
                              ),
                              Text(
                                '${((opacity ?? l.opacity) * 100).round()}%',
                              ),
                            ],
                          ),
                        ],
                        const SizedBox(height: 8),
                        const Muted(
                          'JPEG, PNG or WebP, up to 8 MB. Cards stay opaque so text keeps its contrast.',
                        ),
                      ],
                    ),
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
              const CardHead('Devices'),
              if (me != null && me.owner.isEmpty)
                Container(
                  margin: const EdgeInsets.only(bottom: 12),
                  padding: const EdgeInsets.symmetric(
                    horizontal: 16,
                    vertical: 14,
                  ),
                  decoration: BoxDecoration(
                    color: cs.tertiaryContainer,
                    borderRadius: BorderRadius.circular(16),
                  ),
                  child: Row(
                    children: [
                      WIcon('person', size: 20, color: cs.onTertiaryContainer),
                      const SizedBox(width: 12),
                      Expanded(
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Text(
                              'Who uses this phone?',
                              style: TextStyle(color: cs.onTertiaryContainer),
                            ),
                            Text(
                              'Every device now has the name of its person',
                              style: TextStyle(
                                fontSize: 13,
                                color: cs.onTertiaryContainer,
                              ),
                            ),
                          ],
                        ),
                      ),
                      WButton('Name', onPressed: () => _rename(me)),
                    ],
                  ),
                ),
              const CmdText(
                'To add a phone or another browser, run `sudo vps-admin pair` on the server and enter the code there.',
                style: TextStyle(fontSize: 13),
              ),
              const SizedBox(height: 8),
              for (var pi = 0; pi < names.length; pi++) ...[
                Container(
                  padding: const EdgeInsets.fromLTRB(4, 14, 4, 6),
                  decoration: BoxDecoration(
                    border: pi == 0
                        ? null
                        : Border(
                            top: BorderSide(color: cs.surfaceContainerHigh),
                          ),
                  ),
                  child: Row(
                    children: [
                      CircleAvatar(
                        radius: 18,
                        backgroundColor: names[pi].isEmpty
                            ? cs.surfaceContainerHigh
                            : cs.primaryContainer,
                        child: Text(
                          names[pi].isEmpty ? '?' : names[pi][0].toUpperCase(),
                          style: TextStyle(
                            fontWeight: FontWeight.w600,
                            color: names[pi].isEmpty
                                ? cs.onSurfaceVariant
                                : cs.onPrimaryContainer,
                          ),
                        ),
                      ),
                      const SizedBox(width: 12),
                      Text(
                        names[pi].isEmpty ? 'Not named yet' : names[pi],
                        style: Theme.of(context).textTheme.titleSmall,
                      ),
                      const SizedBox(width: 8),
                      Muted(
                        '${people[names[pi]]!.length} device${people[names[pi]]!.length == 1 ? '' : 's'}',
                      ),
                    ],
                  ),
                ),
                for (final d in people[names[pi]]!)
                  Padding(
                    padding: const EdgeInsets.only(left: 20),
                    child: WRow(
                      children: [
                        Container(
                          width: 40,
                          height: 40,
                          decoration: BoxDecoration(
                            color: cs.secondaryContainer,
                            shape: BoxShape.circle,
                          ),
                          child: Center(
                            child: WIcon(
                              d.kind == 'app' ? 'phone' : 'globe',
                              size: 20,
                              color: cs.onSecondaryContainer,
                            ),
                          ),
                        ),
                        Expanded(
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              Wrap(
                                spacing: 8,
                                crossAxisAlignment: WrapCrossAlignment.center,
                                children: [
                                  Text(
                                    d.name,
                                    style: const TextStyle(
                                      fontSize: 16,
                                      fontWeight: FontWeight.w500,
                                    ),
                                  ),
                                  if (d.id == current)
                                    const WChip('This device'),
                                ],
                              ),
                              Muted(
                                '${d.kind == 'app' ? 'App' : 'Browser · ${d.passkeys} passkey${d.passkeys == 1 ? '' : 's'}'} · '
                                'added ${ago(d.createdAt)} · seen ${d.lastSeenAt == null ? 'never' : ago(d.lastSeenAt!)}'
                                '${d.lastIp.isNotEmpty ? ' from ${d.lastIp}' : ''}',
                              ),
                            ],
                          ),
                        ),
                        WIconButton(
                          'edit',
                          tooltip: 'Rename',
                          size: 18,
                          onPressed: () => _rename(d),
                        ),
                        TextButton(
                          onPressed: () => _revoke(d),
                          child: const Text('Remove'),
                        ),
                      ],
                    ),
                  ),
              ],
            ],
          ),
        ),
        WCard(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              const CardHead(
                'Activity',
                sub: 'Every sign-in and action, kept for a year',
              ),
              ConstrainedBox(
                constraints: BoxConstraints(
                  maxHeight: auditOpen ? 560 : double.infinity,
                ),
                child: SingleChildScrollView(
                  primary: false,
                  child: Column(
                    children: [
                      for (var i = 0; i < shownAudit.length; i++)
                        WRow(
                          first: i == 0,
                          padding: const EdgeInsets.symmetric(
                            horizontal: 4,
                            vertical: 10,
                          ),
                          children: [
                            Container(
                              width: 22,
                              height: 22,
                              alignment: Alignment.center,
                              decoration: BoxDecoration(
                                color: shownAudit[i].ok ? t.ok : t.bad,
                                shape: BoxShape.circle,
                              ),
                              child: Text(
                                shownAudit[i].ok ? '✓' : '!',
                                style: TextStyle(
                                  fontSize: 12,
                                  fontWeight: FontWeight.w700,
                                  color: shownAudit[i].ok ? t.onOk : t.onBad,
                                ),
                              ),
                            ),
                            Expanded(
                              child: Column(
                                crossAxisAlignment: CrossAxisAlignment.start,
                                children: [
                                  Text.rich(
                                    TextSpan(
                                      text: _describe(shownAudit[i]),
                                      children: [
                                        if (shownAudit[i]
                                            .target
                                            .isNotEmpty) ...[
                                          const TextSpan(text: ' · '),
                                          TextSpan(
                                            text: shownAudit[i].target,
                                            style: cmdStyle(
                                              context,
                                              fontSize: 12,
                                            ),
                                          ),
                                        ],
                                      ],
                                    ),
                                    style: const TextStyle(fontSize: 14),
                                  ),
                                  Muted(
                                    '${ago(shownAudit[i].at)} · ${shownAudit[i].device.isEmpty ? 'system' : shownAudit[i].device}'
                                    '${shownAudit[i].ip.isNotEmpty ? ' · ${shownAudit[i].ip}' : ''}'
                                    '${!shownAudit[i].ok && shownAudit[i].detail.isNotEmpty ? ' · ${shownAudit[i].detail}' : ''}',
                                  ),
                                ],
                              ),
                            ),
                          ],
                        ),
                    ],
                  ),
                ),
              ),
              const SizedBox(height: 8),
              Wrap(
                spacing: 8,
                children: [
                  if (audit.length > 5)
                    TextButton.icon(
                      onPressed: () => setState(() => auditOpen = !auditOpen),
                      icon: AnimatedRotation(
                        turns: auditOpen ? .5 : 0,
                        duration: const Duration(milliseconds: 200),
                        child: WIcon('chevron', size: 18, color: cs.primary),
                      ),
                      label: Text(
                        auditOpen
                            ? 'Show less'
                            : 'Show more (${audit.length - 5})',
                      ),
                    ),
                  if (auditOpen && moreAudit)
                    WButton(
                      loadingAudit ? 'Loading…' : 'Load older',
                      kind: BtnKind.tonal,
                      onPressed: loadingAudit ? null : () => _audit(more: true),
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
              const CardHead(
                'Servers',
                sub: 'Each server has its own panel; the app keeps them all',
              ),
              for (var i = 0; i < state.servers.length; i++)
                WRow(
                  first: i == 0,
                  onTap: () => state.select(state.servers[i]),
                  children: [
                    ShapeIcon(
                      M3Shape.cookie,
                      size: 40,
                      color: state.servers[i].id == state.currentId
                          ? cs.primaryContainer
                          : cs.surfaceContainerHighest,
                    ),
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(
                            state.servers[i].name,
                            style: const TextStyle(
                              fontSize: 16,
                              fontWeight: FontWeight.w500,
                            ),
                          ),
                          Muted(Uri.parse(state.servers[i].url).host),
                        ],
                      ),
                    ),
                    if (state.servers[i].id == state.currentId)
                      const WChip('Current'),
                    WIconButton(
                      'trash',
                      tooltip: 'Forget',
                      size: 18,
                      onPressed: () async {
                        final s = state.servers[i];
                        if (await wConfirm(
                          context,
                          title: 'Forget ${s.name}?',
                          body:
                              'The phone forgets this server. Remove its device in Devices first to cut its access too.',
                          action: 'Forget',
                          danger: true,
                        )) {
                          state.remove(s);
                        }
                      },
                    ),
                  ],
                ),
              const SizedBox(height: 8),
              Align(
                alignment: Alignment.centerLeft,
                child: WButton(
                  'Add a server',
                  icon: 'plus',
                  kind: BtnKind.tonal,
                  onPressed: () => Navigator.push(
                    context,
                    MaterialPageRoute(builder: (_) => const AddServerScreen()),
                  ),
                ),
              ),
            ],
          ),
        ),
        ListenableBuilder(
          listenable: updater,
          builder: (context, _) => WCard(
            child: Row(
              children: [
                ShapeIcon(
                  M3Shape.flower,
                  size: 40,
                  color: cs.secondaryContainer,
                ),
                const SizedBox(width: 14),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        'VPS Admin app ${updater.currentLabel}',
                        style: Theme.of(context).textTheme.titleSmall,
                      ),
                      Muted(
                        updater.progress != null
                            ? 'Downloading ${(updater.progress! * 100).round()}%'
                            : updater.error ??
                                  (updater.available != null
                                      ? '${updater.available!.versionName} is available'
                                      : 'Up to date (checked on the current server)'),
                      ),
                    ],
                  ),
                ),
                if (updater.available != null && updater.progress == null)
                  WButton(
                    'Update',
                    onPressed: () => updater.install(state.api!),
                  )
                else
                  WIconButton(
                    'restart',
                    tooltip: 'Check now',
                    onPressed: () => updater.check(state.api),
                  ),
              ],
            ),
          ),
        ),
      ],
    );
  }
}
