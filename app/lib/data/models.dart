// Mirrors the JSON of the agent API (internal/ in the Go code).

double _d(dynamic v) => (v as num?)?.toDouble() ?? 0;
int _i(dynamic v) => (v as num?)?.toInt() ?? 0;
DateTime? _t(dynamic v) =>
    v == null ? null : DateTime.tryParse(v as String)?.toLocal();

class Host {
  Host(Map<String, dynamic> j)
    : hostname = j['hostname'] as String? ?? '',
      os = j['os'] as String? ?? '',
      kernel = j['kernel'] as String? ?? '',
      cores = _i(j['cores']);
  final String hostname, os, kernel;
  final int cores;
}

class Disk {
  Disk(Map<String, dynamic> j)
    : mount = j['mount'] as String,
      total = _d(j['total']),
      used = _d(j['used']);
  final String mount;
  final double total, used;
  double get percent => total == 0 ? 0 : 100 * used / total;
}

class Snapshot {
  Snapshot(Map<String, dynamic> j)
    : uptime = _d(j['uptime']),
      cpu = _d(j['cpu']),
      load = ((j['load'] as List?) ?? [0, 0, 0]).map(_d).toList(),
      memTotal = _d(j['memTotal']),
      memUsed = _d(j['memUsed']),
      netRx = _d(j['netRx']),
      netTx = _d(j['netTx']),
      disks = ((j['disks'] as List?) ?? [])
          .map((d) => Disk(d as Map<String, dynamic>))
          .toList();
  final double uptime, cpu, memTotal, memUsed, netRx, netTx;
  final List<double> load;
  final List<Disk> disks;
  double get memPercent => memTotal == 0 ? 0 : 100 * memUsed / memTotal;
  Disk? get root =>
      disks.where((d) => d.mount == '/').firstOrNull ?? disks.firstOrNull;
}

class Service {
  Service(Map<String, dynamic> j)
    : name = j['name'] as String,
      description = j['description'] as String? ?? '',
      active = j['active'] as String? ?? '',
      sub = j['sub'] as String? ?? '',
      enabled = j['enabled'] as String? ?? '';
  final String name, description, active, sub, enabled;
}

class DockerContainer {
  DockerContainer(Map<String, dynamic> j)
    : id = j['id'] as String,
      name = j['name'] as String,
      image = j['image'] as String,
      state = j['state'] as String,
      status = j['status'] as String,
      health = j['health'] as String?,
      project = j['project'] as String?,
      workDir = j['workDir'] as String?,
      service = j['service'] as String?,
      exposed = j['exposed'] as bool? ?? false;
  final String id, name, image, state, status;
  final String? health, project, workDir, service;
  final bool exposed;
  bool get bad =>
      health == 'unhealthy' || state == 'restarting' || state == 'dead';
}

class Check {
  Check(Map<String, dynamic> j)
    : id = j['id'] as String,
      group = j['group'] as String,
      title = j['title'] as String,
      status = j['status'] as String,
      detail = j['detail'] as String? ?? '',
      fix = j['fix'] as String?;
  final String id, group, title, status, detail;
  final String? fix;
}

class Security {
  Security(Map<String, dynamic> j)
    : score = _i(j['score']),
      checks = ((j['checks'] as List?) ?? [])
          .map((c) => Check(c as Map<String, dynamic>))
          .toList();
  final int score;
  final List<Check> checks;
}

class UpdateStatus {
  UpdateStatus(Map<String, dynamic> j)
    : current = j['current'] as String? ?? '',
      latest = (j['latest'] as Map<String, dynamic>?)?['tag'] as String?,
      available = j['available'] as bool? ?? false,
      auto = j['auto'] as bool? ?? false,
      lastResult = j['lastResult'] as String?,
      error = j['error'] as String?,
      prerelease = j['prerelease'] as bool? ?? false,
      running = j['running'] as bool? ?? false;
  final String current;
  final String? latest, lastResult, error;
  final bool available, auto, prerelease, running;
}

class Deployment {
  Deployment(Map<String, dynamic> j)
    : id = _i(j['id']),
      sha = j['sha'] as String? ?? '',
      message = j['message'] as String? ?? '',
      trigger = j['trigger'] as String? ?? '',
      status = j['status'] as String? ?? '',
      error = j['error'] as String? ?? '',
      createdAt = _t(j['createdAt']) ?? DateTime.now(),
      finishedAt = _t(j['finishedAt']);
  final int id;
  final String sha, message, trigger, status, error;
  final DateTime createdAt;
  final DateTime? finishedAt;
  String get short => sha.length > 7 ? sha.substring(0, 7) : sha;
  bool get running =>
      status == 'queued' || status == 'building' || status == 'starting';
}

class AppInfo {
  AppInfo(Map<String, dynamic> j)
    : id = j['id'] as String,
      repo = j['repo'] as String,
      branch = j['branch'] as String,
      build = j['build'] as String,
      domain = j['domain'] as String? ?? '',
      dir = j['dir'] as String? ?? '',
      status = j['status'] as String? ?? '',
      autoDeploy = j['autoDeploy'] as bool? ?? false,
      live = j['live'] == null
          ? null
          : Deployment(j['live'] as Map<String, dynamic>),
      latest = j['latest'] == null
          ? null
          : Deployment(j['latest'] as Map<String, dynamic>);
  final String id, repo, branch, build, domain, dir, status;
  final bool autoDeploy;
  final Deployment? live, latest;
}

class UpdateCheck {
  UpdateCheck(Map<String, dynamic> j)
    : current = j['current'] as String? ?? '',
      latestSha = (j['latest'] as Map<String, dynamic>)['sha'] as String,
      latestMessage =
          (j['latest'] as Map<String, dynamic>)['message'] as String? ?? '',
      upToDate = j['upToDate'] as bool? ?? false;
  final String current, latestSha, latestMessage;
  final bool upToDate;
}

class Conversation {
  Conversation(Map<String, dynamic> j)
    : id = j['id'] as String,
      title = j['title'] as String,
      state = j['state'] as String,
      error = j['error'] as String? ?? '',
      updatedAt = _t(j['updatedAt']) ?? DateTime.now();
  final String id, title, state, error;
  final DateTime updatedAt;
}

class ToolCall {
  ToolCall(Map<String, dynamic> j)
    : id = j['id'] as String,
      name = j['name'] as String,
      input = j['input'] as String? ?? '{}',
      summary = j['summary'] as String? ?? '',
      kind = j['kind'] as String? ?? '',
      status = j['status'] as String? ?? '',
      result = j['result'] as String? ?? '',
      isError = j['isError'] as bool? ?? false;
  final String id, name, input, summary, kind, status, result;
  final bool isError;
}

class Device {
  Device(Map<String, dynamic> j)
    : id = j['id'] as String,
      name = j['name'] as String? ?? '',
      owner = j['owner'] as String? ?? '',
      kind = j['kind'] as String? ?? 'web',
      createdAt = DateTime.parse(j['createdAt'] as String).toLocal(),
      lastSeenAt = j['lastSeenAt'] == null
          ? null
          : DateTime.parse(j['lastSeenAt'] as String).toLocal(),
      lastIp = j['lastIp'] as String? ?? '',
      passkeys = (j['passkeys'] as num?)?.toInt() ?? 0;
  final String id, name, owner, kind, lastIp;
  final DateTime createdAt;
  final DateTime? lastSeenAt;
  final int passkeys;
}

class AuditEntry {
  AuditEntry(Map<String, dynamic> j)
    : id = (j['id'] as num).toInt(),
      at = DateTime.parse(j['at'] as String).toLocal(),
      device = j['device'] as String? ?? '',
      ip = j['ip'] as String? ?? '',
      action = j['action'] as String? ?? '',
      target = j['target'] as String? ?? '',
      ok = j['ok'] as bool? ?? true,
      detail = j['detail'] as String? ?? '';
  final int id;
  final DateTime at;
  final String device, ip, action, target, detail;
  final bool ok;
}
