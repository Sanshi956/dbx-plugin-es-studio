/**
 * Cluster settings metadata: which keys are common, and what value candidates
 * each one accepts. The page uses this to offer a picker for both the key and
 * the value, while always keeping a free-text escape hatch.
 *
 * Nothing here is authoritative — ES validates on write. The point is to stop
 * people from hand-typing `cluster.routing.allocation.enable` and then having
 * to remember whether the value is `new_primaries` or `new-primary`.
 */

export type ValueKind = "boolean" | "enum" | "number" | "duration" | "size" | "text";

export interface SettingMeta {
  kind: ValueKind;
  /** Clickable candidates: the full enum, or the common values for the kind. */
  options: string[];
  /** One-line meaning, shown as a tooltip only (never as page copy). */
  hintKey: string;
}

const BOOL = ["true", "false"];
const ALLOC_ENABLE = ["all", "primaries", "new_primaries", "none"];
const REBALANCE_ENABLE = ["all", "primaries", "replicas", "none"];
const ALLOW_REBALANCE = ["always", "indices_primaries_active", "indices_all_active"];
const DURATIONS = ["10s", "30s", "1m", "5m"];
const WATERMARKS = ["85%", "90%", "95%", "5gb"];

/** Curated keys, in the order they are offered in the picker. */
const CURATED: { key: string; meta: SettingMeta }[] = [
  { key: "cluster.routing.allocation.enable", meta: { kind: "enum", options: ALLOC_ENABLE, hintKey: "cs.hint.cluster.routing.allocation.enable" } },
  { key: "cluster.routing.allocation.allow_rebalance", meta: { kind: "enum", options: ALLOW_REBALANCE, hintKey: "cs.hint.cluster.routing.allocation.allow_rebalance" } },
  { key: "cluster.routing.rebalance.enable", meta: { kind: "enum", options: REBALANCE_ENABLE, hintKey: "cs.hint.cluster.routing.rebalance.enable" } },
  { key: "cluster.routing.allocation.type", meta: { kind: "enum", options: ["balanced", "desired_balance"], hintKey: "cs.hint.cluster.routing.allocation.type" } },
  { key: "cluster.routing.allocation.disk.threshold_enabled", meta: { kind: "boolean", options: BOOL, hintKey: "cs.hint.cluster.routing.allocation.disk.threshold_enabled" } },
  { key: "cluster.routing.allocation.disk.include_relocations", meta: { kind: "boolean", options: BOOL, hintKey: "cs.hint.cluster.routing.allocation.disk.include_relocations" } },
  { key: "cluster.routing.allocation.disk.watermark.low", meta: { kind: "size", options: WATERMARKS, hintKey: "cs.hint.cluster.routing.allocation.disk.watermark.low" } },
  { key: "cluster.routing.allocation.disk.watermark.high", meta: { kind: "size", options: WATERMARKS, hintKey: "cs.hint.cluster.routing.allocation.disk.watermark.high" } },
  { key: "cluster.routing.allocation.disk.watermark.flood_stage", meta: { kind: "size", options: WATERMARKS, hintKey: "cs.hint.cluster.routing.allocation.disk.watermark.flood_stage" } },
  { key: "cluster.routing.allocation.node_concurrent_recoveries", meta: { kind: "number", options: ["1", "2", "4"], hintKey: "cs.hint.cluster.routing.allocation.node_concurrent_recoveries" } },
  { key: "cluster.routing.allocation.node_initial_primaries_recoveries", meta: { kind: "number", options: ["4", "8"], hintKey: "cs.hint.cluster.routing.allocation.node_initial_primaries_recoveries" } },
  { key: "cluster.routing.allocation.cluster_concurrent_rebalance", meta: { kind: "number", options: ["2", "4", "8"], hintKey: "cs.hint.cluster.routing.allocation.cluster_concurrent_rebalance" } },
  { key: "cluster.routing.allocation.balance.shard", meta: { kind: "number", options: ["0.45"], hintKey: "cs.hint.cluster.routing.allocation.balance.shard" } },
  { key: "cluster.routing.allocation.balance.index", meta: { kind: "number", options: ["0.55"], hintKey: "cs.hint.cluster.routing.allocation.balance.index" } },
  { key: "cluster.routing.allocation.balance.threshold", meta: { kind: "number", options: ["1.0"], hintKey: "cs.hint.cluster.routing.allocation.balance.threshold" } },
  { key: "cluster.routing.use_adaptive_replica_selection", meta: { kind: "boolean", options: BOOL, hintKey: "cs.hint.cluster.routing.use_adaptive_replica_selection" } },
  { key: "cluster.blocks.read_only", meta: { kind: "boolean", options: BOOL, hintKey: "cs.hint.cluster.blocks.read_only" } },
  { key: "cluster.blocks.read_only_allow_delete", meta: { kind: "boolean", options: BOOL, hintKey: "cs.hint.cluster.blocks.read_only_allow_delete" } },
  { key: "cluster.info.update.interval", meta: { kind: "duration", options: DURATIONS, hintKey: "cs.hint.cluster.info.update.interval" } },
  { key: "cluster.indices.close.enable", meta: { kind: "boolean", options: BOOL, hintKey: "cs.hint.cluster.indices.close.enable" } },
  { key: "cluster.max_shards_per_node", meta: { kind: "number", options: ["1000", "2000"], hintKey: "cs.hint.cluster.max_shards_per_node" } },
  { key: "cluster.max_shards_per_node.frozen", meta: { kind: "number", options: ["3000"], hintKey: "cs.hint.cluster.max_shards_per_node.frozen" } },
  { key: "cluster.publish.timeout", meta: { kind: "duration", options: ["30s", "60s"], hintKey: "cs.hint.cluster.publish.timeout" } },
  { key: "cluster.auto_shrink_voting_configuration", meta: { kind: "boolean", options: BOOL, hintKey: "cs.hint.cluster.auto_shrink_voting_configuration" } },
  { key: "cluster.deprecation_indexing.enabled", meta: { kind: "boolean", options: BOOL, hintKey: "cs.hint.cluster.deprecation_indexing.enabled" } },
  { key: "cluster.fault_detection.follower.ping_interval", meta: { kind: "duration", options: DURATIONS, hintKey: "cs.hint.cluster.fault_detection.follower.ping_interval" } },
  { key: "cluster.fault_detection.leader_check.interval", meta: { kind: "duration", options: DURATIONS, hintKey: "cs.hint.cluster.fault_detection.leader_check.interval" } },
  { key: "cluster.name", meta: { kind: "text", options: [], hintKey: "cs.hint.cluster.name" } }
];

const BY_KEY = new Map(CURATED.map((entry) => [entry.key, entry.meta]));

function guess(key: string): SettingMeta {
  if (/\.(enable|enabled)$/.test(key)) {
    return { kind: "boolean", options: BOOL, hintKey: "cs.guess.enable" };
  }
  if (/watermark/.test(key)) {
    return { kind: "size", options: WATERMARKS, hintKey: "cs.guess.watermark" };
  }
  if (/\.(timeout|interval|delay|ttl|keep_alive|period)$/.test(key)) {
    return { kind: "duration", options: DURATIONS, hintKey: "cs.guess.duration" };
  }
  if (/(concurrent|recoveries|rebalance|retries|shards_per_node|threshold|\.size$|\.max$|\.min$|\.weight$)/.test(key)) {
    return { kind: "number", options: [], hintKey: "cs.guess.number" };
  }
  return { kind: "text", options: [], hintKey: "cs.guess.text" };
}

export function settingMeta(key: string): SettingMeta {
  const trimmed = key.trim();
  return BY_KEY.get(trimmed) ?? guess(trimmed);
}

/** Keys offered for the picker: curated list plus whatever the cluster has set. */
export function settingKeyCandidates(extra: string[] = []): string[] {
  const keys = new Set<string>(CURATED.map((entry) => entry.key));
  for (const key of extra) {
    if (key.trim()) keys.add(key.trim());
  }
  return [...keys].sort((a, b) => a.localeCompare(b));
}
