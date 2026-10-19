import 'package:flutter/material.dart';

import '../../data/api.dart';
import '../../data/models.dart';
import '../kit.dart';
import '../live.dart';
import '../widgets.dart';
import 'common.dart';

/// ServicesView.vue: search, Running / Failed / Inactive / All filters with counts,
/// and each unit with start, restart, stop and logs.
class ServicesPage extends StatefulWidget {
  const ServicesPage({super.key, this.filter = 'running'});
  final String filter;

  @override
  State<ServicesPage> createState() => _ServicesPageState();
}

class _ServicesPageState extends State<ServicesPage> {
  List<Service> list = [];
  bool loading = true;
  String query = '';
  late String filter = widget.filter;
  final busy = <String>{};

  Api get api => Live.read(context).api;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) => _load());
  }

  Future<void> _load() async {
    try {
      final l = await api.services();
      if (mounted) setState(() => list = l);
    } catch (e) {
      if (mounted) showError(context, e);
    } finally {
      if (mounted) setState(() => loading = false);
    }
  }

  bool _matches(Service s, String f) =>
      f == 'all' ||
      (f == 'running' && s.active == 'active') ||
      (f == 'failed' && s.active == 'failed') ||
      (f == 'inactive' && s.active == 'inactive');

  Tone _tone(Service s) {
    if (s.active == 'failed') return Tone.bad;
    if (const ['activating', 'deactivating', 'reloading'].contains(s.active)) {
      return Tone.warn;
    }
    return s.active == 'active' ? Tone.ok : Tone.idle;
  }

  Future<void> _act(Service s, String action) async {
    if (action != 'start' &&
        !await wConfirm(
          context,
          title: '${action == 'stop' ? 'Stop' : 'Restart'} ${s.name}?',
          body: action == 'stop'
              ? 'It stays stopped until you start it again, or until the next boot if it is enabled.'
              : 'It will be unavailable for a moment.',
          action: action == 'stop' ? 'Stop' : 'Restart',
          danger: action == 'stop',
        )) {
      return;
    }
    setState(() => busy.add(s.name));
    try {
      await api.serviceAction(s.name, action);
      if (mounted) showInfo(context, '${s.name}: $action done');
      await _load();
    } catch (e) {
      if (mounted) showError(context, e);
    } finally {
      if (mounted) setState(() => busy.remove(s.name));
    }
  }

  @override
  Widget build(BuildContext context) {
    final q = query.trim().toLowerCase();
    final shown = list
        .where(
          (s) =>
              _matches(s, filter) &&
              (q.isEmpty ||
                  s.name.toLowerCase().contains(q) ||
                  s.description.toLowerCase().contains(q)),
        )
        .toList();
    int count(String f) => list.where((s) => _matches(s, f)).length;
    String cap(String f) => '${f[0].toUpperCase()}${f.substring(1)}';
    return WPage(
      onRefresh: _load,
      children: [
        const PageTitle('Services'),
        SearchField(
          hint: 'Search services',
          onChanged: (v) => setState(() => query = v),
        ),
        FilterChipRow<String>(
          options: [
            for (final f in const ['running', 'failed', 'inactive', 'all'])
              (f, '${cap(f)} · ${count(f)}'),
          ],
          value: filter,
          onChanged: (v) => setState(() => filter = v),
        ),
        WCard(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              if (loading)
                const Muted('Loading…', size: 15)
              else if (shown.isEmpty)
                const Muted('No service matches.', size: 15),
              for (var i = 0; i < shown.length; i++)
                WRow(
                  first: i == 0,
                  padding: const EdgeInsets.fromLTRB(6, 12, 0, 12),
                  children: [
                    StatusDot(_tone(shown[i]), label: shown[i].active),
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(
                            shown[i].name,
                            style: const TextStyle(
                              fontSize: 16,
                              fontWeight: FontWeight.w500,
                            ),
                          ),
                          Muted(shown[i].description, maxLines: 1),
                          const SizedBox(height: 4),
                          Row(
                            children: [
                              if (shown[i].enabled.isNotEmpty) ...[
                                WChip(shown[i].enabled, outline: true),
                                const SizedBox(width: 8),
                              ],
                              Flexible(
                                child: Muted(
                                  '${shown[i].active} · ${shown[i].sub}',
                                  maxLines: 1,
                                ),
                              ),
                            ],
                          ),
                        ],
                      ),
                    ),
                    Row(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        if (shown[i].active != 'active')
                          WIconButton(
                            'play',
                            tooltip: 'Start ${shown[i].name}',
                            onPressed: busy.contains(shown[i].name)
                                ? null
                                : () => _act(shown[i], 'start'),
                          )
                        else
                          WIconButton(
                            'restart',
                            tooltip: 'Restart ${shown[i].name}',
                            onPressed: busy.contains(shown[i].name)
                                ? null
                                : () => _act(shown[i], 'restart'),
                          ),
                        WIconButton(
                          'stop',
                          tooltip: 'Stop ${shown[i].name}',
                          size: 18,
                          onPressed:
                              busy.contains(shown[i].name) ||
                                  shown[i].active != 'active'
                              ? null
                              : () => _act(shown[i], 'stop'),
                        ),
                        WIconButton(
                          'logs',
                          tooltip: 'Logs of ${shown[i].name}',
                          onPressed: () => showLogs(
                            context,
                            title: '${shown[i].name} logs',
                            load: () => api.serviceLogs(shown[i].name),
                          ),
                        ),
                      ],
                    ),
                  ],
                ),
            ],
          ),
        ),
      ],
    );
  }
}
