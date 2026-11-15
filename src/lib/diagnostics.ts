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
import { t } from "./i18n.svelte.js";

export type Severity = "critical" | "warning" | "info";

export const SEVERITY_ORDER: Severity[] = ["critical", "warning", "info"];

/** Severity label for display; resolved per render so a language switch applies. */
export function severityLabel(severity: Severity): string {
  return t(`diag.sev.${severity}`);
}

export interface RuleThreshold {
  labelKey: string;
  unitKey: string;
  min: number;
  max: number;
  step: number;
  fallback: number;
}

export interface RuleMeta {
  id: string;
  domainKey: string;
  severity: Severity;
  titleKey: string;
  /** What the rule looks at — shown as a tooltip, not as body copy. */
  basisKey: string;
  threshold?: RuleThreshold;
}

// Rule ids are persisted in host.storage; never rename one without a migration.
export const RULES: RuleMeta[] = [
  {
    id: "health-red",
    domainKey: "diag.domain.health",
    severity: "critical",
    titleKey: "diag.rule.health-red.title",
    basisKey: "diag.rule.health-red.basis",
  },
  {
    id: "health-yellow",
    domainKey: "diag.domain.health",
    severity: "warning",
    titleKey: "diag.rule.health-yellow.title",
    basisKey: "diag.rule.health-yellow.basis",
  },
  {
    id: "unassigned-shards",
    domainKey: "diag.domain.allocation",
    severity: "warning",
    titleKey: "diag.rule.unassigned-shards.title",
    basisKey: "diag.rule.unassigned-shards.basis",
  },
  {
    id: "single-node-replicas",
    domainKey: "diag.domain.allocation",
    severity: "warning",
    titleKey: "diag.rule.single-node-replicas.title",
    basisKey: "diag.rule.single-node-replicas.basis",
  },
  {
    id: "replicas-exceed-nodes",
    domainKey: "diag.domain.allocation",
    severity: "info",
    titleKey: "diag.rule.replicas-exceed-nodes.title",
    basisKey: "diag.rule.replicas-exceed-nodes.basis",
  },
  {
    id: "disk-watermark",
    domainKey: "diag.domain.capacity",
    severity: "warning",
    titleKey: "diag.rule.disk-watermark.title",
    basisKey: "diag.rule.disk-watermark.basis",
    threshold: { labelKey: "diag.rule.disk-watermark.label", unitKey: "diag.unit.pct", min: 50, max: 99, step: 1, fallback: 85 },
  },
  {
    id: "heap-pressure",
    domainKey: "diag.domain.capacity",
    severity: "warning",
    titleKey: "diag.rule.heap-pressure.title",
    basisKey: "diag.rule.heap-pressure.basis",
    threshold: { labelKey: "diag.rule.heap-pressure.label", unitKey: "diag.unit.pct", min: 50, max: 99, step: 1, fallback: 85 },
  },
  {
    id: "cpu-saturation",
    domainKey: "diag.domain.capacity",
    severity: "warning",
    titleKey: "diag.rule.cpu-saturation.title",
    basisKey: "diag.rule.cpu-saturation.basis",
    threshold: { labelKey: "diag.rule.cpu-saturation.label", unitKey: "diag.unit.pct", min: 50, max: 100, step: 1, fallback: 90 },
  },
  {
    id: "shards-per-node",
    domainKey: "diag.domain.capacity",
    severity: "info",
    titleKey: "diag.rule.shards-per-node.title",
    basisKey: "diag.rule.shards-per-node.basis",
    threshold: { labelKey: "diag.rule.shards-per-node.label", unitKey: "diag.unit.count", min: 100, max: 5000, step: 50, fallback: 600 },
  },
  {
    id: "ilm-stopped",
    domainKey: "diag.domain.lifecycle",
    severity: "warning",
    titleKey: "diag.rule.ilm-stopped.title",
    basisKey: "diag.rule.ilm-stopped.basis",
  },
  {
    id: "refresh-disabled",
    domainKey: "diag.domain.indexSettings",
    severity: "warning",
    titleKey: "diag.rule.refresh-disabled.title",
    basisKey: "diag.rule.refresh-disabled.basis",
  },
  {
    id: "write-blocked",
    domainKey: "diag.domain.indexSettings",
    severity: "warning",
    titleKey: "diag.rule.write-blocked.title",
    basisKey: "diag.rule.write-blocked.basis",
  },
  {
    id: "delayed-unassigned",
    domainKey: "diag.domain.allocation",
    severity: "info",
    titleKey: "diag.rule.delayed-unassigned.title",
    basisKey: "diag.rule.delayed-unassigned.basis",
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
    throw new Error(t("diag.healthFailed", { detail }));
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
    const reason = str(cat(shard, "unassigned.reason")) || t("diag.noReason");
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
  return t(`diag.rule.${ruleId}.suggestion`);
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
      domain: t(meta.domainKey),
      severity: meta.severity,
      title: t(meta.titleKey),
      evidence,
      suggestion: suggestFor(ruleId),
      targets,
    });
  };

  if (enabled("health-red") && snapshot.status === "red") {
    add("health-red", t("diag.ev.healthRed", { primary: snapshot.unassignedPrimary, active: snapshot.active }));
  }
  if (enabled("health-yellow") && snapshot.status === "yellow" && snapshot.unassigned > 0) {
    add("health-yellow", t("diag.ev.healthYellow", { unassigned: snapshot.unassigned, active: snapshot.active }));
  }
  if (enabled("unassigned-shards") && snapshot.unassigned > 0) {
    const detail = snapshot.reasons.length
      ? snapshot.reasons.map((row) => `${row.reason}×${row.count}`).join(" · ")
      : t("diag.ev.noReason");
    add("unassigned-shards", t("diag.ev.unassigned", { count: snapshot.unassigned, detail }));
  }
  if (enabled("single-node-replicas") && snapshot.nodes === 1) {
    const withReplicas = snapshot.indices.filter((index) => index.replicas > 0);
    if (withReplicas.length > 0) {
      const sample = withReplicas.slice(0, 4).map((index) => `${index.index}(${index.replicas})`).join(t("common.listSep"));
      const more = withReplicas.length > 4 ? t("diag.ev.more", { count: withReplicas.length }) : "";
      add("single-node-replicas", t("diag.ev.singleNodeReplicas", { count: withReplicas.length, sample, more }),
        withReplicas.map((index) => index.index));
    }
  }
  if (enabled("replicas-exceed-nodes") && snapshot.nodes > 1) {
    const over = snapshot.indices.filter((index) => index.replicas >= snapshot.nodes);
    if (over.length > 0) {
      add("replicas-exceed-nodes", t("diag.ev.replicasExceed", { nodes: snapshot.nodes, count: over.length }),
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
        ? t("diag.ev.diskHigh", { high: snapshot.watermarks.high })
        : t("diag.ev.diskNormal", { high: snapshot.watermarks.high });
      add("disk-watermark", t("diag.ev.disk", { nodes: evidence, limit, note }), hot.map((node) => node.name));
    }
  }
  if (enabled("heap-pressure")) {
    const limit = thresholdOf("heap-pressure");
    const hot = snapshot.nodeFacts.filter((node) => node.heapPercent >= limit);
    if (hot.length > 0) {
      add("heap-pressure", t("diag.ev.threshold", { nodes: hot.map((node) => `${node.name} ${node.heapPercent}%`).join(" · "), limit }),
        hot.map((node) => node.name));
    }
  }
  if (enabled("cpu-saturation")) {
    const limit = thresholdOf("cpu-saturation");
    const hot = snapshot.nodeFacts.filter((node) => node.cpu >= limit);
    if (hot.length > 0) {
      add("cpu-saturation", t("diag.ev.threshold", { nodes: hot.map((node) => `${node.name} ${node.cpu}%`).join(" · "), limit }),
        hot.map((node) => node.name));
    }
  }
  if (enabled("shards-per-node")) {
    const limit = thresholdOf("shards-per-node");
    const perNode = snapshot.nodes > 0 ? Math.round(snapshot.active / snapshot.nodes) : snapshot.active;
    if (perNode >= limit) {
      add("shards-per-node", t("diag.ev.shardsPerNode", { active: snapshot.active, nodes: snapshot.nodes, perNode, limit }));
    }
  }
  if (enabled("ilm-stopped") && snapshot.ilmMode && snapshot.ilmMode !== "RUNNING") {
    add("ilm-stopped", `operation_mode=${snapshot.ilmMode}`);
  }
  if (enabled("refresh-disabled")) {
    const off = snapshot.indices.filter((index) => index.refreshInterval === "-1");
    if (off.length > 0) {
      add("refresh-disabled", t("diag.ev.refresh", { count: off.length, sample: off.slice(0, 4).map((index) => index.index).join(t("common.listSep")) }),
        off.map((index) => index.index));
    }
  }
  if (enabled("write-blocked")) {
    const blocked = snapshot.indices.filter((index) => index.blocks.length > 0);
    if (blocked.length > 0) {
      add("write-blocked",
        blocked.slice(0, 4).map((index) => `${index.index}[${index.blocks.join("+")}]`).join(" · ")
          + (blocked.length > 4 ? t("diag.ev.more", { count: blocked.length }) : ""),
        blocked.map((index) => index.index));
    }
  }
  if (enabled("delayed-unassigned") && snapshot.delayed > 0) {
    add("delayed-unassigned", t("diag.ev.delayed", { count: snapshot.delayed }));
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
