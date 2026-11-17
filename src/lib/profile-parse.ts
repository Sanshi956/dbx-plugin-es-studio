// Pure, dependency-free parsing of the Elasticsearch `_search?profile=true` response.
// Kept separate from services.ts (which imports svelte/i18n) so this logic can be
// unit-tested directly under plain Node with `node --experimental-strip-types`.

export interface ProfileNode {
  type: string;
  description: string;
  timeNanos: number;
  depth: number;
}

export interface ProfileShard {
  node: string;
  index: string;
  shard: string;
  queryNanos: number;
  rewriteNanos: number;
  collectorNanos: number;
  nodes: ProfileNode[];
}

function record(value: unknown): Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {};
}

function str(value: unknown): string {
  return typeof value === "string" ? value : value === undefined || value === null ? "" : String(value);
}

function num(value: unknown): number {
  if (typeof value === "number") return Number.isFinite(value) ? value : 0;
  if (typeof value === "string") {
    const raw = value.trim();
    if (raw === "") return 0;
    const parsed = Number(raw);
    return Number.isFinite(parsed) ? parsed : 0;
  }
  return 0;
}

/** Walks the ES 6/7/8/9 profile query tree (`{type, description, time_in_nanos, children}`). */
function flattenProfile(entries: unknown, depth: number, out: ProfileNode[]): void {
  for (const entry of Array.isArray(entries) ? entries : []) {
    const node = record(entry);
    const type = str(node.type) || Object.keys(node).find((k) => !["children", "description"].includes(k)) || "query";
    const inner = node.type === undefined ? record(node[type]) : {};
    out.push({
      type,
      description: str(node.description) || str(inner.description),
      timeNanos: num(node.time_in_nanos) || num(inner.time_in_nanos),
      depth,
    });
    if (depth < 24) flattenProfile(node.children ?? inner.children, depth + 1, out);
  }
}

/**
 * Map raw `profile.shards[]` into typed rows for every ES version (6/7/8/9).
 *
 * ES 6/7/8/9 flat structure: { id, searches, ... } (8/9 also carry standalone node_id/index/shard_id).
 * Legacy 7.x wrapper (if any): { "<nodeId>": { searches: [...] } }.
 * Key: only treat as wrapper when there is a single key AND its value really holds
 * a searches array — otherwise 6/7.x "id" would be misread as a node ID.
 */
export function mapProfileShards(rawShards: unknown[]): ProfileShard[] {
  return rawShards.map((entry, i) => {
    const wrapped = record(entry);
    const keys = Object.keys(wrapped);
    let node = wrapped;
    if (
      keys.length === 1 &&
      !Array.isArray(wrapped.searches) &&
      Array.isArray(record(wrapped[keys[0]]).searches)
    ) {
      node = record(wrapped[keys[0]]);
    }

    // Standalone fields first; for 6/7.x restore from the composite "[nodeId][index][shard]" id.
    const idMatch = /^\[([^\]]+)\]\[([^\]]+)\]\[(\d+)\]$/.exec(str(wrapped.id));
    const nodeId = str(wrapped.node_id) || (idMatch ? idMatch[1] : keys[0] || "");
    const index = str(wrapped.index) || (idMatch ? idMatch[2] : "");
    const shardId = wrapped.shard_id !== undefined
      ? num(wrapped.shard_id)
      : (idMatch ? num(idMatch[3]) : i);

    const searches = Array.isArray(node.searches) ? (node.searches as unknown[]) : [];
    const first = record(searches[0]);
    const nodes: ProfileNode[] = [];
    flattenProfile(first.query, 0, nodes);
    const collectorList = Array.isArray(first.collector) ? (first.collector as unknown[]) : [];
    const rootCollector = record(collectorList[0]);
    return {
      node: nodeId,
      index,
      shard: `#${shardId}`,
      // The root query node's time already includes its children.
      queryNanos: nodes.length ? nodes[0].timeNanos : 0,
      rewriteNanos: num(first.rewrite_time),
      collectorNanos: num(rootCollector.time_in_nanos),
      nodes,
    };
  });
}
