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
  hint: string;
}

const BOOL = ["true", "false"];
const ALLOC_ENABLE = ["all", "primaries", "new_primaries", "none"];
const REBALANCE_ENABLE = ["all", "primaries", "replicas", "none"];
const ALLOW_REBALANCE = ["always", "indices_primaries_active", "indices_all_active"];
const DURATIONS = ["10s", "30s", "1m", "5m"];
const WATERMARKS = ["85%", "90%", "95%", "5gb"];

/** Curated keys, in the order they are offered in the picker. */
const CURATED: { key: string; meta: SettingMeta }[] = [
  { key: "cluster.routing.allocation.enable", meta: { kind: "enum", options: ALLOC_ENABLE, hint: "分片分配开关；滚动重启前设为 none（默认 all）" } },
  { key: "cluster.routing.allocation.allow_rebalance", meta: { kind: "enum", options: ALLOW_REBALANCE, hint: "允许重平衡的时机（默认 indices_all_active）" } },
  { key: "cluster.routing.rebalance.enable", meta: { kind: "enum", options: REBALANCE_ENABLE, hint: "允许哪类分片参与重平衡（默认 all）" } },
  { key: "cluster.routing.allocation.type", meta: { kind: "enum", options: ["balanced", "desired_balance"], hint: "分配算法：balanced 按权重，desired_balance 按目标分片数（默认 balanced）" } },
  { key: "cluster.routing.allocation.disk.threshold_enabled", meta: { kind: "boolean", options: BOOL, hint: "是否启用磁盘水位线检查（默认 true）" } },
  { key: "cluster.routing.allocation.disk.include_relocations", meta: { kind: "boolean", options: BOOL, hint: "水位线统计是否计入正在迁出的分片（默认 true）" } },
  { key: "cluster.routing.allocation.disk.watermark.low", meta: { kind: "size", options: WATERMARKS, hint: "低水位：不再往该节点分配新分片（默认 85%）" } },
  { key: "cluster.routing.allocation.disk.watermark.high", meta: { kind: "size", options: WATERMARKS, hint: "高水位：尝试把分片迁走（默认 90%）" } },
  { key: "cluster.routing.allocation.disk.watermark.flood_stage", meta: { kind: "size", options: WATERMARKS, hint: "洪泛水位：命中索引被置为只读（默认 95%）" } },
  { key: "cluster.routing.allocation.node_concurrent_recoveries", meta: { kind: "number", options: ["1", "2", "4"], hint: "单节点并发分片恢复数（默认 2）" } },
  { key: "cluster.routing.allocation.node_initial_primaries_recoveries", meta: { kind: "number", options: ["4", "8"], hint: "单节点并发主分片恢复数（默认 4）" } },
  { key: "cluster.routing.allocation.cluster_concurrent_rebalance", meta: { kind: "number", options: ["2", "4", "8"], hint: "全集群并发重平衡数（默认 2）" } },
  { key: "cluster.routing.allocation.balance.shard", meta: { kind: "number", options: ["0.45"], hint: "重平衡时 shard 数量的权重（默认 0.45）" } },
  { key: "cluster.routing.allocation.balance.index", meta: { kind: "number", options: ["0.55"], hint: "重平衡时索引数量的权重（默认 0.55）" } },
  { key: "cluster.routing.allocation.balance.threshold", meta: { kind: "number", options: ["1.0"], hint: "触发重平衡的权重差阈值（默认 1.0）" } },
  { key: "cluster.routing.use_adaptive_replica_selection", meta: { kind: "boolean", options: BOOL, hint: "读请求是否走自适应副本选择（默认 true）" } },
  { key: "cluster.blocks.read_only", meta: { kind: "boolean", options: BOOL, hint: "全集群只读（元数据与写入都拒绝，默认 false）" } },
  { key: "cluster.blocks.read_only_allow_delete", meta: { kind: "boolean", options: BOOL, hint: "只读但允许删除；磁盘满时 ES 会自动设置（默认 false）" } },
  { key: "cluster.info.update.interval", meta: { kind: "duration", options: DURATIONS, hint: "集群元信息（磁盘等）刷新间隔（默认 30s）" } },
  { key: "cluster.indices.close.enable", meta: { kind: "boolean", options: BOOL, hint: "是否允许关闭索引（默认 true）" } },
  { key: "cluster.max_shards_per_node", meta: { kind: "number", options: ["1000", "2000"], hint: "单节点分片数上限（默认 1000）" } },
  { key: "cluster.max_shards_per_node.frozen", meta: { kind: "number", options: ["3000"], hint: "单节点 frozen 分片数上限（默认 3000）" } },
  { key: "cluster.publish.timeout", meta: { kind: "duration", options: ["30s", "60s"], hint: "集群状态发布超时（默认 30s）" } },
  { key: "cluster.auto_shrink_voting_configuration", meta: { kind: "boolean", options: BOOL, hint: "是否自动收缩投票节点集合（默认 true）" } },
  { key: "cluster.deprecation_indexing.enabled", meta: { kind: "boolean", options: BOOL, hint: "是否写入弃用日志（默认 true）" } },
  { key: "cluster.fault_detection.follower.ping_interval", meta: { kind: "duration", options: DURATIONS, hint: "follower 向 leader 发 ping 的间隔（默认 10s）" } },
  { key: "cluster.fault_detection.leader_check.interval", meta: { kind: "duration", options: DURATIONS, hint: "leader 检查 follower 的间隔（默认 10s）" } },
  { key: "cluster.name", meta: { kind: "text", options: [], hint: "集群名；改了会触发全集群重新加入" } }
];

const BY_KEY = new Map(CURATED.map((entry) => [entry.key, entry.meta]));

function guess(key: string): SettingMeta {
  if (/\.(enable|enabled)$/.test(key)) {
    return { kind: "boolean", options: BOOL, hint: "开关值：true / false" };
  }
  if (/watermark/.test(key)) {
    return { kind: "size", options: WATERMARKS, hint: "百分比（85%）或字节值（5gb）" };
  }
  if (/\.(timeout|interval|delay|ttl|keep_alive|period)$/.test(key)) {
    return { kind: "duration", options: DURATIONS, hint: "时间值需带单位：30s / 1m" };
  }
  if (/(concurrent|recoveries|rebalance|retries|shards_per_node|threshold|\.size$|\.max$|\.min$|\.weight$)/.test(key)) {
    return { kind: "number", options: [], hint: "数值（可为小数）" };
  }
  return { kind: "text", options: [], hint: "自由文本；留空表示移除该项" };
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
