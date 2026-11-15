// P4-b · Cluster Diagnostics: a deterministic rule engine over read-only facts.
//
// Deliberately NOT a new Go RPC: one `es/batch` round-trip fetches everything a
// rule may look at, and every rule is a pure function of that snapshot plus the
// user's threshold/enable config. Two consequences we care about:
//   * the same snapshot always yields the same findings (no heuristics, no ML);
//   * changing a threshold re-evaluates instantly without touching the cluster.
// Rules only describe + suggest. Nothing in this module writes to Elasticsearch.

import { batch, cat, isError, num, record, str, type BatchRequest } from "./services";
import type { DbxBridge } from "./bridge";

export type Severity = "critical" | "warning" | "info";

export const SEVERITY_ORDER: Severity[] = ["critical", "warning", "info"];

export const SEVERITY_LABEL: Record<Severity, string> = {
  critical: "严重",
  warning: "警告",
  info: "提示",
};

export interface RuleThreshold {
  label: string;
  unit: string;
  min: number;
  max: number;
  step: number;
  fallback: number;
}

export interface RuleMeta {
  id: string;
  domain: string;
  severity: Severity;
  title: string;
  /** What the rule looks at — shown as a tooltip, not as body copy. */
  basis: string;
  threshold?: RuleThreshold;
}

// Rule ids are persisted in host.storage; never rename one without a migration.
export const RULES: RuleMeta[] = [
  {
    id: "health-red",
    domain: "健康",
    severity: "critical",
    title: "集群红灯：主分片未分配",
    basis: "GET /_cluster/health 的 status=red，说明至少一个主分片不可用。",
  },
  {
    id: "health-yellow",
    domain: "健康",
    severity: "warning",
    title: "集群黄灯：存在未分配分片",
    basis: "GET /_cluster/health 的 status=yellow。",
  },
  {
    id: "unassigned-shards",
    domain: "分配",
    severity: "warning",
    title: "分片未分配",
    basis: "_cluster/health 的 unassigned_shards 与 _cat/shards 的 unassigned.reason。",
  },
  {
    id: "single-node-replicas",
    domain: "分配",
    severity: "warning",
    title: "单节点集群仍配置副本",
    basis: "节点数为 1，但索引副本数 ≥1：副本永远无处分配。",
  },
  {
    id: "replicas-exceed-nodes",
    domain: "分配",
    severity: "info",
    title: "副本数达到或超过节点数",
    basis: "索引副本数 ≥ 数据节点数，多出来的副本会一直等节点。",
  },
  {
    id: "disk-watermark",
    domain: "容量",
    severity: "warning",
    title: "磁盘接近水位线",
    basis: "任一节点 disk.used_percent ≥ 阈值；达到 high 水位会触发分片外迁。",
    threshold: { label: "磁盘使用率", unit: "%", min: 50, max: 99, step: 1, fallback: 85 },
  },
  {
    id: "heap-pressure",
    domain: "容量",
    severity: "warning",
    title: "JVM 堆压力偏高",
    basis: "任一节点 heap.percent ≥ 阈值，常见伴随长时间 GC。",
    threshold: { label: "堆使用率", unit: "%", min: 50, max: 99, step: 1, fallback: 85 },
  },
  {
    id: "cpu-saturation",
    domain: "容量",
    severity: "warning",
    title: "CPU 使用率偏高",
    basis: "任一节点 cpu ≥ 阈值。采集是瞬时值，需结合监控曲线确认是否长期。",
    threshold: { label: "CPU 使用率", unit: "%", min: 50, max: 100, step: 1, fallback: 90 },
  },
  {
    id: "shards-per-node",
    domain: "容量",
    severity: "info",
    title: "单节点分片数偏多",
    basis: "活跃分片数 / 节点数 ≥ 阈值；经验值约为每 GB 堆 20 个分片。",
    threshold: { label: "每节点分片数", unit: "个", min: 100, max: 5000, step: 50, fallback: 600 },
  },
  {
    id: "ilm-stopped",
    domain: "生命周期",
    severity: "warning",
    title: "ILM 未处于 RUNNING",
    basis: "GET /_ilm/status 的 operation_mode 不是 RUNNING。",
  },
  {
    id: "refresh-disabled",
    domain: "索引设置",
    severity: "warning",
    title: "索引关闭了刷新",
    basis: "index.refresh_interval=-1：新写入的文档在对该索引 refresh 前不可搜索。",
  },
  {
    id: "write-blocked",
    domain: "索引设置",
    severity: "warning",
    title: "索引存在写/读阻断",
    basis: "index.blocks.read_only / index.blocks.write / read_only_allow_delete 为 true。",
  },
  {
    id: "delayed-unassigned",
    domain: "分配",
    severity: "info",
    title: "有等待超时后重新分配的分片",
    basis: "_cluster/health 的 delayed_unassigned_shards > 0。",
  },
];

export const RULE_BY_ID = new Map(RULES.map((rule) => [rule.id, rule]));

export interface RuleConfig {
  enabled: boolean;
  threshold: number;
}

export type RuleConfigs = Record<string, RuleConfig>;

export function defaultConfigs(): RuleConfigs {
  const out: RuleConfigs = {};
  for (const rule of RULES) {
    out[rule.id] = { enabled: true, threshold: rule.threshold?.fallback ?? 0 };
  }
  return out;
}

function clamp(value: number, threshold: RuleThreshold): number {
  if (!Number.isFinite(value)) return threshold.fallback;
  return Math.min(threshold.max, Math.max(threshold.min, value));
}

/** Accepts whatever came out of host.storage; unknown/invalid entries fall back. */
export function normalizeConfigs(raw: unknown): RuleConfigs {
  const base = defaultConfigs();
  const parsed = typeof raw === "string" ? safeJson(raw) : raw;
  const source = record(parsed);
  for (const rule of RULES) {
    const entry = record(source[rule.id]);
    if (typeof entry.enabled === "boolean") base[rule.id].enabled = entry.enabled;
    if (rule.threshold && entry.threshold !== undefined) {
      base[rule.id].threshold = clamp(num(entry.threshold), rule.threshold);
    }
  }
  return base;
}

function safeJson(text: string): unknown {
  try {
    return JSON.parse(text);
  } catch {
    return {};
  }
}

export function serializeConfigs(configs: RuleConfigs): string {
  return JSON.stringify(configs);
}

// ---------------------------------------------------------------------------
// snapshot
// ---------------------------------------------------------------------------

export interface NodeFact {
  name: string;
  roles: string;
  master: boolean;
  heapPercent: number;
  cpu: number;
  diskPercent: number;
}

export interface IndexFact {
  index: string;
  replicas: number;
  refreshInterval: string;
  blocks: string[];
}

export interface UnassignedReason {
  reason: string;
  count: number;
}

export interface Snapshot {
  collectedAt: number;
  status: string;
  nodes: number;
  dataNodes: number;
  primary: number;
  active: number;
  unassigned: number;
  unassignedPrimary: number;
  relocating: number;
  initializing: number;
  delayed: number;
  nodeFacts: NodeFact[];
  indices: IndexFact[];
  reasons: UnassignedReason[];
  watermarks: { low: string; high: string; flood: string };
  ilmMode: string;
}

// `filter_path` cannot select these keys: with flat_settings=true the key is
// literally "index.number_of_replicas", and filter_path treats dots as path
// separators — the response comes back as {}. The `name` parameter filters by
// setting name instead (verified against 8.19), which keeps the payload small.
const SETTINGS_KEYS = [
  "index.number_of_replicas",
  "index.refresh_interval",
  "index.blocks.read_only",
  "index.blocks.write",
  "index.blocks.read_only_allow_delete",
].join(",");

export async function collectDiagnostics(bridge: DbxBridge, connectionId: string): Promise<Snapshot> {
  if (!connectionId) throw new Error("NO_CONNECTION");

  const requests: BatchRequest[] = [
    { path: "/_cluster/health" },
    { path: "/_cat/nodes?format=json&h=name,node.role,master,heap.percent,cpu,disk.used_percent" },
    { path: `/_all/_settings?flat_settings=true&name=${SETTINGS_KEYS}` },
    { path: "/_cat/shards?format=json&h=index,shard,prirep,state,unassigned.reason" },
    { path: "/_cluster/settings?flat_settings=true" },
    { path: "/_ilm/status" },
  ];
  const results = await batch(bridge, connectionId, requests);

  const healthResult = results[0];
  if (!healthResult || isError(healthResult)) {
    const detail = healthResult?.error?.message ?? "empty response";
    throw new Error(`/_cluster/health 失败：${detail}`);
  }
  const health = record(healthResult.body);

  // A failing side endpoint must degrade to "unknown", not break the whole check.
  const catNodes = Array.isArray(results[1]?.body) ? (results[1].body as unknown[]) : [];
  const nodeFacts: NodeFact[] = catNodes.map((entry) => {
    const node = record(entry);
    const roles = str(cat(node, "node.role") || node.roles);
    return {
      name: str(cat(node, "name")),
      roles,
      master: str(cat(node, "master")) === "*" || roles.includes("m"),
      heapPercent: num(cat(node, "heap.percent")),
      cpu: num(cat(node, "cpu")),
      diskPercent: num(cat(node, "disk.used_percent")),
    };
  });

  const indexFacts: IndexFact[] = [];
  for (const [index, raw] of Object.entries(record(results[2]?.body))) {
    const settings = record(record(raw).settings);
    const blocks: string[] = [];
    if (str(settings["index.blocks.read_only"]) === "true") blocks.push("read_only");
    if (str(settings["index.blocks.write"]) === "true") blocks.push("write");
    if (str(settings["index.blocks.read_only_allow_delete"]) === "true") blocks.push("read_only_allow_delete");
    indexFacts.push({
      index,
      replicas: num(settings["index.number_of_replicas"]),
      refreshInterval: str(settings["index.refresh_interval"]),
      blocks,
    });
  }

  const shards = Array.isArray(results[3]?.body) ? (results[3].body as unknown[]) : [];
  const reasonCount = new Map<string, number>();
  for (const entry of shards) {
    const shard = record(entry);
    if (str(cat(shard, "state")) !== "UNASSIGNED") continue;
    const reason = str(cat(shard, "unassigned.reason")) || "未给出原因";
    reasonCount.set(reason, (reasonCount.get(reason) ?? 0) + 1);
  }
  const reasons = [...reasonCount.entries()]
    .map(([reason, count]) => ({ reason, count }))
    .sort((a, b) => b.count - a.count || a.reason.localeCompare(b.reason));

  const persistent = record(record(record(results[4]?.body).persistent));
  const watermark = (key: string): string => str(persistent[key]);

  // /_ilm/status does not exist on every distribution; "" means "not applicable".
  const ilmBody = results[5] && !isError(results[5]) ? record(results[5].body) : {};
  const ilmMode = str(ilmBody.operation_mode);

  const dataNodeCount = nodeFacts.filter((node) => node.roles.includes("d")).length;

  return {
    collectedAt: Date.now(),
    status: str(health.status),
    nodes: num(health.number_of_nodes),
    dataNodes: dataNodeCount || num(health.number_of_data_nodes),
    primary: num(health.active_primary_shards),
    active: num(health.active_shards),
    unassigned: num(health.unassigned_shards),
    unassignedPrimary: num(health.unassigned_primary_shards),
    relocating: num(health.relocating_shards),
    initializing: num(health.initializing_shards),
    delayed: num(health.delayed_unassigned_shards),
    nodeFacts,
    indices: indexFacts,
    reasons,
    watermarks: {
      low: watermark("cluster.routing.allocation.disk.watermark.low") || "85%",
      high: watermark("cluster.routing.allocation.disk.watermark.high") || "90%",
      flood: watermark("cluster.routing.allocation.disk.watermark.flood_stage") || "95%",
    },
    ilmMode,
  };
}

// ---------------------------------------------------------------------------
// rule evaluation
// ---------------------------------------------------------------------------

export interface Finding {
  ruleId: string;
  domain: string;
  severity: Severity;
  title: string;
  evidence: string;
  suggestion: string;
  targets: string[];
}

function percentOf(text: string): number {
  const match = /([\d.]+)\s*%/.exec(text);
  return match ? Number(match[1]) : 0;
}

function suggestFor(ruleId: string): string {
  switch (ruleId) {
    case "health-red":
      return "先看 Allocation 页的 explain 确认主分片为什么起不来，再决定恢复节点或从快照恢复。";
    case "health-yellow":
      return "看「分片未分配」这条的依据，按原因处理；如果只是单节点集群的副本，把副本数设为 0。";
    case "unassigned-shards":
      return "按原因处理：NODE_LEFT 等延迟超时、ALLOCATION_FAILED 看 explain、CLUSTER_RECOVERED 等自动分配。";
    case "single-node-replicas":
      return "单节点环境把这些索引的 number_of_replicas 设为 0（Allocation 页可改），或增加数据节点。";
    case "replicas-exceed-nodes":
      return "把副本数降到节点数 - 1 以内，避免长期存在待分配副本。";
    case "disk-watermark":
      return "清理旧索引/调整保留期，或扩容；必要时临时调高 watermark，但不要长期开着。";
    case "heap-pressure":
      return "结合 Node Metrics 看是否长期高位；短期可降并发查询，长期需减分片或扩堆。";
    case "cpu-saturation":
      return "先确认是否长期（History/Node Metrics），再看是否有大查询或 merge 压力。";
    case "shards-per-node":
      return "合并小索引或使用 ILM 滚动，把单节点分片数压到经验值以内。";
    case "ilm-stopped":
      return "确认是否有人手动停了 ILM；需要时在 Lifecycle 页启动。";
    case "refresh-disabled":
      return "如果写入后需要立刻可搜，把 refresh_interval 改回 1s 或显式 refresh。";
    case "write-blocked":
      return "确认阻断是有意为之（归档/只读索引）；否则移除对应 index.blocks.* 设置。";
    case "delayed-unassigned":
      return "通常会在延迟超时后自动分配；持续增长再看节点是否真的离线。";
    default:
      return "";
  }
}

/**
 * Pure function: same snapshot + same configs ⇒ same findings.
 * Disabled rules produce nothing; unknown rule ids in configs are ignored.
 */
export function evaluate(snapshot: Snapshot, configs: RuleConfigs): Finding[] {
  const out: Finding[] = [];
  const enabled = (id: string): boolean => configs[id]?.enabled !== false;
  const thresholdOf = (id: string): number => {
    const meta = RULE_BY_ID.get(id);
    if (!meta?.threshold) return 0;
    const configured = configs[id]?.threshold;
    return configured === undefined ? meta.threshold.fallback : clamp(configured, meta.threshold);
  };
  const add = (ruleId: string, evidence: string, targets: string[] = []): void => {
    const meta = RULE_BY_ID.get(ruleId);
    if (!meta) return;
    out.push({
      ruleId,
      domain: meta.domain,
      severity: meta.severity,
      title: meta.title,
      evidence,
      suggestion: suggestFor(ruleId),
      targets,
    });
  };

  if (enabled("health-red") && snapshot.status === "red") {
    add("health-red", `status=red · 未分配主分片 ${snapshot.unassignedPrimary} · 活跃分片 ${snapshot.active}`);
  }
  if (enabled("health-yellow") && snapshot.status === "yellow" && snapshot.unassigned > 0) {
    add("health-yellow", `status=yellow · 未分配分片 ${snapshot.unassigned} · 活跃分片 ${snapshot.active}`);
  }
  if (enabled("unassigned-shards") && snapshot.unassigned > 0) {
    const detail = snapshot.reasons.length
      ? snapshot.reasons.map((row) => `${row.reason}×${row.count}`).join(" · ")
      : "未拿到 unassigned.reason";
    add("unassigned-shards", `未分配 ${snapshot.unassigned}（${detail}）`);
  }
  if (enabled("single-node-replicas") && snapshot.nodes === 1) {
    const withReplicas = snapshot.indices.filter((index) => index.replicas > 0);
    if (withReplicas.length > 0) {
      const sample = withReplicas.slice(0, 4).map((index) => `${index.index}(${index.replicas})`).join("、");
      const more = withReplicas.length > 4 ? ` 等 ${withReplicas.length} 个` : "";
      add("single-node-replicas", `1 个节点，${withReplicas.length} 个索引副本数 ≥1：${sample}${more}`,
        withReplicas.map((index) => index.index));
    }
  }
  if (enabled("replicas-exceed-nodes") && snapshot.nodes > 1) {
    const over = snapshot.indices.filter((index) => index.replicas >= snapshot.nodes);
    if (over.length > 0) {
      add("replicas-exceed-nodes", `${snapshot.nodes} 个节点，${over.length} 个索引副本数 ≥ 节点数`,
        over.map((index) => index.index));
    }
  }
  if (enabled("disk-watermark")) {
    const limit = thresholdOf("disk-watermark");
    const hot = snapshot.nodeFacts.filter((node) => node.diskPercent >= limit);
    if (hot.length > 0) {
      const highLimit = percentOf(snapshot.watermarks.high) || 90;
      const worst = Math.max(...hot.map((node) => node.diskPercent));
      const evidence = hot.map((node) => `${node.name} ${node.diskPercent}%`).join(" · ");
      const note = worst >= highLimit
        ? `（已达 high 水位 ${snapshot.watermarks.high}，分片会被外迁）`
        : `（high 水位 ${snapshot.watermarks.high}）`;
      add("disk-watermark", `${evidence} ≥ 阈值 ${limit}%${note}`, hot.map((node) => node.name));
    }
  }
  if (enabled("heap-pressure")) {
    const limit = thresholdOf("heap-pressure");
    const hot = snapshot.nodeFacts.filter((node) => node.heapPercent >= limit);
    if (hot.length > 0) {
      add("heap-pressure", hot.map((node) => `${node.name} ${node.heapPercent}%`).join(" · ") + ` ≥ 阈值 ${limit}%`,
        hot.map((node) => node.name));
    }
  }
  if (enabled("cpu-saturation")) {
    const limit = thresholdOf("cpu-saturation");
    const hot = snapshot.nodeFacts.filter((node) => node.cpu >= limit);
    if (hot.length > 0) {
      add("cpu-saturation", hot.map((node) => `${node.name} ${node.cpu}%`).join(" · ") + ` ≥ 阈值 ${limit}%`,
        hot.map((node) => node.name));
    }
  }
  if (enabled("shards-per-node")) {
    const limit = thresholdOf("shards-per-node");
    const perNode = snapshot.nodes > 0 ? Math.round(snapshot.active / snapshot.nodes) : snapshot.active;
    if (perNode >= limit) {
      add("shards-per-node", `活跃分片 ${snapshot.active} / 节点 ${snapshot.nodes} = ${perNode} ≥ 阈值 ${limit}`);
    }
  }
  if (enabled("ilm-stopped") && snapshot.ilmMode && snapshot.ilmMode !== "RUNNING") {
    add("ilm-stopped", `operation_mode=${snapshot.ilmMode}`);
  }
  if (enabled("refresh-disabled")) {
    const off = snapshot.indices.filter((index) => index.refreshInterval === "-1");
    if (off.length > 0) {
      add("refresh-disabled", `${off.length} 个索引 refresh_interval=-1：${off.slice(0, 4).map((index) => index.index).join("、")}`,
        off.map((index) => index.index));
    }
  }
  if (enabled("write-blocked")) {
    const blocked = snapshot.indices.filter((index) => index.blocks.length > 0);
    if (blocked.length > 0) {
      add("write-blocked",
        blocked.slice(0, 4).map((index) => `${index.index}[${index.blocks.join("+")}]`).join(" · ")
          + (blocked.length > 4 ? ` 等 ${blocked.length} 个` : ""),
        blocked.map((index) => index.index));
    }
  }
  if (enabled("delayed-unassigned") && snapshot.delayed > 0) {
    add("delayed-unassigned", `delayed_unassigned_shards=${snapshot.delayed}`);
  }

  return out.sort((a, b) => SEVERITY_ORDER.indexOf(a.severity) - SEVERITY_ORDER.indexOf(b.severity));
}

export interface SeverityCount {
  critical: number;
  warning: number;
  info: number;
}

export function countBySeverity(findings: Finding[]): SeverityCount {
  const out: SeverityCount = { critical: 0, warning: 0, info: 0 };
  for (const finding of findings) out[finding.severity] += 1;
  return out;
}
