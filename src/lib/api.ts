import type { DbxBridge, ConnectionSummary } from "./bridge";

export interface EsResult {
  status: number;
  body?: unknown;
  headers?: Record<string, string[]>;
  duration_ms?: number;
  error?: { code: number; message: string };
}

export interface BatchResult {
  results: EsResult[];
}

export interface OverviewSnapshot {
  version: string;
  clusterName: string;
  clusterUuid: string;
  nodeName: string;
  health: string;
  nodeCount: number;
  dataNodeCount: number;
  indexCount: number;
  docCount: number;
  storeBytes: number;
  activePrimaryShards: number;
  activeShards: number;
  relocatingShards: number;
  initializingShards: number;
  unassignedShards: number;
  delayedUnassignedShards: number;
  fetchedAt: number;
  partialErrors: string[];
}

export function invoke<T>(bridge: DbxBridge, method: string, params: unknown): Promise<T> {
  return bridge.invoke<T>(method, params, { timeoutMs: 30000 });
}

export async function listConnections(bridge: DbxBridge): Promise<ConnectionSummary[]> {
  const rows = await bridge.getConnections();
  return Array.isArray(rows) ? rows : [];
}

function record(value: unknown): Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {};
}

function str(value: unknown): string {
  return typeof value === "string" ? value : "—";
}

function num(value: unknown): number {
  return typeof value === "number" && Number.isFinite(value) ? value : 0;
}

function nested(obj: Record<string, unknown>, ...keys: string[]): unknown {
  let current: unknown = obj;
  for (const key of keys) {
    current = record(current)[key];
  }
  return current;
}

/** Load every P1 overview metric from REAL Elasticsearch APIs via es/batch. */
export async function fetchOverview(bridge: DbxBridge, connectionId: string): Promise<OverviewSnapshot> {
  const batch = await invoke<BatchResult>(bridge, "es/batch", {
    connection_id: connectionId,
    requests: [
      { method: "GET", path: "/" },
      { method: "GET", path: "/_cluster/health" },
      { method: "GET", path: "/_cluster/stats" },
      { method: "GET", path: "/_cat/nodes?format=json" },
      { method: "GET", path: "/_cat/indices?format=json&bytes=b" },
    ],
  });
  const rows = Array.isArray(batch?.results) ? batch.results : [];
  const labels = ["GET /", "GET /_cluster/health", "GET /_cluster/stats", "GET /_cat/nodes", "GET /_cat/indices"];
  const partialErrors: string[] = [];
  rows.forEach((row, index) => {
    if (row?.error) partialErrors.push(`${labels[index] ?? "request"}: ${row.error.message}`);
    else if (!row || row.status >= 400) partialErrors.push(`${labels[index] ?? "request"}: HTTP ${row?.status ?? "?"}`);
  });

  const root = record(rows[0]?.body);
  const health = record(rows[1]?.body);
  const stats = record(rows[2]?.body);
  const nodes = Array.isArray(rows[3]?.body) ? (rows[3].body as unknown[]) : [];
  const indices = Array.isArray(rows[4]?.body) ? (rows[4].body as unknown[]) : [];

  if (!root.version || !health.status) {
    throw new Error(
      partialErrors[0] ?? "CLUSTER_METADATA_UNAVAILABLE",
    );
  }

  const version = record(root.version);
  const indicesStats = record(stats.indices);
  return {
    version: str(version.number),
    clusterName: str(root.cluster_name),
    clusterUuid: str(root.cluster_uuid),
    nodeName: str(root.name),
    health: str(health.status),
    nodeCount: num(health.number_of_nodes) || nodes.length,
    dataNodeCount: num(health.number_of_data_nodes),
    indexCount: num(indicesStats.count) || indices.length,
    docCount: num(nested(indicesStats, "docs", "count")),
    storeBytes: num(nested(indicesStats, "store", "size_in_bytes")),
    activePrimaryShards: num(health.active_primary_shards),
    activeShards: num(health.active_shards),
    relocatingShards: num(health.relocating_shards),
    initializingShards: num(health.initializing_shards),
    unassignedShards: num(health.unassigned_shards),
    delayedUnassignedShards: num(health.delayed_unassigned_shards),
    fetchedAt: Date.now(),
    partialErrors,
  };
}

export function formatBytes(value: number): string {
  let n = value;
  if (!Number.isFinite(n) || n <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let i = 0;
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024;
    i += 1;
  }
  return `${n.toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
}

export function formatCount(value: number): string {
  return new Intl.NumberFormat("en-US").format(value);
}
