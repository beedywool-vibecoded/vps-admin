import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../../data/api.dart';
import '../../data/models.dart';
import '../kit.dart';
import '../live.dart';
import '../widgets.dart';

/// CSS grid repeat(auto-fit, minmax(minWidth, 1fr)): as many equal columns as fit.
class ResponsiveGrid extends StatelessWidget {
  const ResponsiveGrid({
    super.key,
    required this.minWidth,
    required this.children,
    this.gap = 12,
  });
  final double minWidth, gap;
  final List<Widget> children;

  @override
  Widget build(BuildContext context) => LayoutBuilder(
    builder: (context, box) {
      final cols = ((box.maxWidth + gap) / (minWidth + gap)).floor().clamp(
        1,
        children.isEmpty ? 1 : children.length,
      );
      final w = (box.maxWidth - gap * (cols - 1)) / cols;
      return Wrap(
        spacing: gap,
        runSpacing: gap,
        children: [for (final c in children) SizedBox(width: w, child: c)],
      );
    },
  );
}

class ContainerGroup {
  ContainerGroup(this.name, this.items);
  final String name;
  final List<DockerContainer> items;
  String? get dir => items.first.workDir;
}

/// Containers by compose project, Standalone last, like the panel.
List<ContainerGroup> groupContainers(
  List<DockerContainer> list, {
  String query = '',
}) {
  final q = query.trim().toLowerCase();
  final map = <String, List<DockerContainer>>{};
  for (final c in list) {
    if (q.isNotEmpty &&
        !c.name.toLowerCase().contains(q) &&
        !c.image.toLowerCase().contains(q) &&
        !(c.project ?? '').contains(q)) {
      continue;
    }
    (map[c.project ?? 'Standalone'] ??= []).add(c);
  }
  final names = map.keys.toList()
    ..sort(
      (a, b) => a == 'Standalone'
          ? 1
          : b == 'Standalone'
          ? -1
          : a.compareTo(b),
    );
  return [for (final n in names) ContainerGroup(n, map[n]!)];
}

Tone containerTone(DockerContainer c) {
  if (c.bad) return Tone.bad;
  if (c.state != 'running') return Tone.idle;
  return c.health == 'starting' ? Tone.warn : Tone.ok;
}

Tone groupTone(List<DockerContainer> items) {
  final t = items.map(containerTone).toList();
  if (t.contains(Tone.bad)) return Tone.bad;
  if (t.contains(Tone.warn)) return Tone.warn;
  if (t.every((x) => x == Tone.idle)) return Tone.idle;
  return Tone.ok;
}

String errText(Object e) => e is ApiException ? e.message : e.toString();

/// RebootDialog.vue: type the server name, then wait for the agent to come back.
Future<void> rebootDialog(BuildContext context, LiveSession live) async {
  final host = live.host?.hostname ?? '';
  final typed = TextEditingController();
  final ok = await showWDialog<bool>(
    context,
    title: 'Restart the server',
    body: (c) => StatefulBuilder(
      builder: (c, set) => Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          const Muted(
            'Every site, container and service stops for about a minute while the server reboots. Containers with a restart policy and enabled services come back on their own.',
            size: 15,
          ),
          const SizedBox(height: 16),
          TextField(
            controller: typed,
            autocorrect: false,
            onChanged: (_) => set(() {}),
            decoration: InputDecoration(
              hintText:
                  'Type ${host.isEmpty ? 'the server name' : host} to confirm',
            ),
          ),
        ],
      ),
    ),
    actions: (c) => [
      WButton(
        'Cancel',
        kind: BtnKind.text,
        onPressed: () => Navigator.pop(c, false),
      ),
      ListenableBuilder(
        listenable: typed,
        builder: (_, _) => WButton(
          'Restart now',
          kind: BtnKind.danger,
          onPressed: typed.text.trim() == host && host.isNotEmpty
              ? () => Navigator.pop(c, true)
              : null,
        ),
      ),
    ],
  );
  if (ok != true || !context.mounted) return;
  try {
    await live.api.reboot(typed.text.trim());
  } catch (e) {
    if (context.mounted) showError(context, e);
    return;
  }
  if (!context.mounted) return;
  showInfo(context, 'Restarting $host, it comes back in about a minute');
}

/// LogViewer.vue: a wide dialog with refresh and a follow switch (every 4 s).
Future<void> showLogs(
  BuildContext context, {
  required String title,
  required Future<String> Function() load,
}) => showWDialog(
  context,
  title: title,
  wide: true,
  body: (_) => _Logs(load: load),
);

class _Logs extends StatefulWidget {
  const _Logs({required this.load});
  final Future<String> Function() load;

  @override
  State<_Logs> createState() => _LogsState();
}

class _LogsState extends State<_Logs> {
  String text = '';
  bool loading = false, follow = true;
  final _scroll = ScrollController();
  Timer? _timer;

  @override
  void initState() {
    super.initState();
    _refresh();
    _arm();
  }

  void _arm() {
    _timer?.cancel();
    if (follow) {
      _timer = Timer.periodic(const Duration(seconds: 4), (_) => _refresh());
    }
  }

  Future<void> _refresh() async {
    setState(() => loading = true);
    try {
      final atBottom =
          !_scroll.hasClients ||
          _scroll.position.maxScrollExtent - _scroll.offset < 40;
      final t = await widget.load();
      if (!mounted) return;
      setState(() => text = t.isEmpty ? 'No output yet.' : t);
      if (atBottom) {
        WidgetsBinding.instance.addPostFrameCallback((_) {
          if (_scroll.hasClients) {
            _scroll.jumpTo(_scroll.position.maxScrollExtent);
          }
        });
      }
    } catch (e) {
      if (mounted) showError(context, e);
    } finally {
      if (mounted) setState(() => loading = false);
    }
  }

  @override
  void dispose() {
    _timer?.cancel();
    _scroll.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    return Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Row(
          children: [
            const Muted('Follow'),
            const SizedBox(width: 8),
            Switch(
              value: follow,
              onChanged: (v) {
                setState(() => follow = v);
                _arm();
              },
            ),
            const Spacer(),
            WButton(
              'Refresh',
              kind: BtnKind.tonal,
              onPressed: loading ? null : _refresh,
            ),
          ],
        ),
        const SizedBox(height: 12),
        Flexible(
          child: Container(
            constraints: BoxConstraints(
              maxHeight: MediaQuery.sizeOf(context).height * .6,
            ),
            decoration: BoxDecoration(
              color: cs.surfaceContainerLowest,
              borderRadius: BorderRadius.circular(16),
              border: Border.all(color: cs.surfaceContainerHigh),
            ),
            child: SingleChildScrollView(
              controller: _scroll,
              padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 14),
              child: SelectableText(
                text.isEmpty ? 'Loading…' : text,
                style: const TextStyle(
                  fontFamily: kMono,
                  fontSize: 12,
                  height: 1.6,
                ),
              ),
            ),
          ),
        ),
      ],
    );
  }
}

/// A dark output block like .output / .log (pre).
class OutputBox extends StatelessWidget {
  const OutputBox(this.text, {super.key, this.height = 380, this.controller});
  final String text;
  final double height;
  final ScrollController? controller;

  @override
  Widget build(BuildContext context) {
    final cs = Theme.of(context).colorScheme;
    return Container(
      height: height,
      decoration: BoxDecoration(
        color: cs.surfaceContainerLowest,
        borderRadius: BorderRadius.circular(16),
        border: Border.all(color: cs.surfaceContainerHigh),
      ),
      child: SingleChildScrollView(
        controller: controller,
        padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 14),
        child: SizedBox(
          width: double.infinity,
          child: SelectableText(
            text,
            style: const TextStyle(
              fontFamily: kMono,
              fontSize: 12,
              height: 1.6,
            ),
          ),
        ),
      ),
    );
  }
}

/// Empty state card: a big shape, a line, an optional action.
class EmptyCard extends StatelessWidget {
  const EmptyCard({
    super.key,
    required this.shape,
    required this.text,
    this.child,
  });
  final M3Shape shape;
  final String text;
  final Widget? child;

  @override
  Widget build(BuildContext context) => WCard(
    padding: const EdgeInsets.all(36),
    child: Column(
      children: [
        ShapeIcon(
          shape,
          size: 64,
          color: Theme.of(context).colorScheme.tertiaryContainer,
        ),
        const SizedBox(height: 10),
        Muted(text, size: 15),
        if (child != null) ...[const SizedBox(height: 10), child!],
      ],
    ),
  );
}

/// Which container groups are expanded, shared by the Overview and Containers pages and
/// remembered on this phone. Collapsed by default.
class OpenGroups extends ChangeNotifier {
  OpenGroups._() {
    SharedPreferences.getInstance()
        .then((p) {
          try {
            open = (jsonDecode(p.getString('openGroups') ?? '[]') as List)
                .cast<String>()
                .toSet();
            notifyListeners();
          } catch (_) {}
        })
        .catchError((_) {});
  }
  static final instance = OpenGroups._();
  Set<String> open = {};

  bool has(String name) => open.contains(name);
  void toggle(String name) {
    open.contains(name) ? open.remove(name) : open.add(name);
    notifyListeners();
    SharedPreferences.getInstance()
        .then((p) => p.setString('openGroups', jsonEncode(open.toList())))
        .catchError((_) => false);
  }
}

/// A container group card (header with icon, name, folder, state dots, count, chevron,
/// collapsible body), the same on the Overview and on Containers.
class GroupCard extends StatelessWidget {
  const GroupCard({
    super.key,
    required this.group,
    required this.index,
    required this.children,
    this.forceOpen = false,
    this.onIcon,
    this.onRename,
    this.trailing,
  });
  final ContainerGroup group;
  final int index;
  final List<Widget> children;
  final bool forceOpen;
  final VoidCallback? onIcon, onRename;
  final Widget? trailing;

  @override
  Widget build(BuildContext context) {
    final live = Live.of(context);
    final cs = Theme.of(context).colorScheme;
    final g = group;
    final titled = live.groupTitles.containsKey(g.name);
    return ListenableBuilder(
      listenable: OpenGroups.instance,
      builder: (context, _) {
        final isOpen = forceOpen || OpenGroups.instance.has(g.name);
        return WCard(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Row(
                children: [
                  InkWell(
                    borderRadius: BorderRadius.circular(12),
                    onTap: onIcon,
                    child: GroupBadge(
                      icon: live.groupIcons[g.name],
                      index: index,
                    ),
                  ),
                  if (onRename != null)
                    WIconButton(
                      'edit',
                      tooltip: 'Rename ${live.groupTitle(g.name)}',
                      size: 16,
                      onPressed: onRename,
                    ),
                  const SizedBox(width: 4),
                  Expanded(
                    child: InkWell(
                      borderRadius: BorderRadius.circular(16),
                      onTap: () => OpenGroups.instance.toggle(g.name),
                      child: Padding(
                        padding: const EdgeInsets.symmetric(
                          horizontal: 8,
                          vertical: 6,
                        ),
                        child: Row(
                          children: [
                            Expanded(
                              child: Column(
                                crossAxisAlignment: CrossAxisAlignment.start,
                                children: [
                                  Text.rich(
                                    TextSpan(
                                      text: live.groupTitle(g.name),
                                      style: const TextStyle(
                                        fontSize: 20,
                                        fontWeight: FontWeight.w500,
                                      ),
                                      children: [
                                        if (titled)
                                          TextSpan(
                                            text: ' (${g.name})',
                                            style: TextStyle(
                                              fontSize: 14,
                                              fontWeight: FontWeight.w400,
                                              color: cs.onSurfaceVariant,
                                            ),
                                          ),
                                      ],
                                    ),
                                  ),
                                  if (g.dir != null)
                                    Muted(
                                      g.dir!,
                                      size: 12,
                                      mono: true,
                                      maxLines: 2,
                                    ),
                                ],
                              ),
                            ),
                            for (final c in g.items.take(6))
                              Padding(
                                padding: const EdgeInsets.only(left: 4),
                                child: StatusDot(
                                  containerTone(c),
                                  label: c.state,
                                ),
                              ),
                            const SizedBox(width: 10),
                            WChip('${g.items.length}'),
                            const SizedBox(width: 6),
                            AnimatedRotation(
                              turns: isOpen ? .5 : 0,
                              duration: const Duration(milliseconds: 250),
                              child: WIcon(
                                'chevron',
                                size: 20,
                                color: cs.onSurfaceVariant,
                              ),
                            ),
                          ],
                        ),
                      ),
                    ),
                  ),
                  ?trailing,
                ],
              ),
              AnimatedSize(
                duration: const Duration(milliseconds: 300),
                curve: const Cubic(0.05, 0.7, 0.1, 1),
                alignment: Alignment.topCenter,
                child: isOpen
                    ? Column(
                        crossAxisAlignment: CrossAxisAlignment.stretch,
                        children: [const SizedBox(height: 12), ...children],
                      )
                    : const SizedBox(width: double.infinity),
              ),
            ],
          ),
        );
      },
    );
  }
}
