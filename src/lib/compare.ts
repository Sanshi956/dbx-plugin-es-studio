// P4-b · Multi-Cluster Compare.
//
// Two clusters are reduced to a flat list of facts, then diffed row by row. The
// page never claims a difference it cannot point at: every row keeps the raw
// value from each side, and `same` is computed from those raw values, so a
// formatting change can never fabricate a diff.
//
// Read-only by construction: four GETs per side, no new RPC, no writes.

import { batch, isError, num, record, str, type BatchRequest } from "./services";
import { formatBytes, formatCount } from "./api";
import type { DbxBridge } from "./bridge";
import { t } from "./i18n.svelte.js";

export interface ClusterFacts {
  connectionId: string;
  name: string;
  version: string;
  health: string;
  nodes: number;
  dataNodes: number;
  masterNodes: number;
  indices: number;
  docs: number;
  storeBytes: number;
  primary: number;
  active: number;
  unassigned: number;
  heapUsedPercent: number;
  heapMaxBytes: number;
  diskTotalBytes: number;
  diskFreeBytes: number;
  settings: Record<string, string>;
  collectedAt: number;
}

/** Keys worth comparing: the ones an operator actually changes on purpose. */
export const SETTING_KEYS = [
  "cluster.routing.allocation.enable",
  "cluster.routing.allocation.disk.watermark.low",
  "cluster.routing.allocation.disk.watermark.high",
  "cluster.routing.allocation.disk.watermark.flood_stage",
  "cluster.blocks.read_only",
  "cluster.max_shards_per_node",
];

const SETTING_LABEL: Record<string, string> = {
  "cluster.routing.allocation.enable": "cmp.setting.enable",
  "cluster.routing.allocation.disk.watermark.low": "cmp.setting.wmLow",
  "cluster.routing.allocation.disk.watermark.high": "cmp.setting.wmHigh",
  "cluster.routing.allocation.disk.watermark.flood_stage": "cmp.setting.wmFlood",
  "cluster.blocks.read_only": "cmp.setting.readOnly",
  "cluster.max_shards_per_node": "cmp.setting.maxShards",
};

export const SETTING_DEFAULT: Record<string, string> = {
  "cluster.routing.allocation.enable": "all",
  "cluster.routing.allocation.disk.watermark.low": "85%",
  "cluster.routing.allocation.disk.watermark.high": "90%",
  "cluster.routing.allocation.disk.watermark.flood_stage": "95%",
  "cluster.blocks.read_only": "false",
  "cluster.max_shards_per_node": "1000",
};

export async function collectFacts(bridge: DbxBridge, connectionId: string): Promise<ClusterFacts> {
  if (!connectionId) throw new Error("NO_CONNECTION");

  const requests: BatchRequest[] = [
    { path: "/_cluster/health" },
    { path: "/_cluster/stats" },
    { path: "/_cat/nodes?format=json&h=name,node.role,master,heap.percent" },
    { path: "/_cluster/settings?flat_settings=true" },
  ];
  const results = await batch(bridge, connectionId, requests);

  const healthResult = results[0];
  if (!healthResult || isError(healthResult)) {
    const detail = healthResult?.error?.message ?? "empty response";
    throw new Error(t("cmp.healthFailed", { detail }));
  }
  const health = record(healthResult.body);
  const stats = record(results[1]?.body);
  const nodesCount = record(stats.nodes);
  const nodeCount = record(nodesCount.count);
  const indices = record(stats.indices);
  const jvmMem = record(record(nodesCount.jvm).mem);
  const fs = record(nodesCount.fs);

  const catNodes = Array.isArray(results[2]?.body) ? (results[2].body as unknown[]) : [];
  const roles = catNodes.map((entry) => str(record(entry)["node.role"] ?? record(entry).roles));
  // `master` is "*" / "-" and is authoritative; the role letters are only a
  // fallback, because a voting-only node also carries the `m` letter.
  const masterFlags = catNodes.map((entry) => str(record(entry).master));
  const hasMasterColumn = masterFlags.some((flag) => flag === "*" || flag === "-");
  const dataNodes = roles.length > 0
    ? roles.filter((role) => role.includes("d")).length
    : num(nodeCount.data);
  const masterNodes = hasMasterColumn
    ? masterFlags.filter((flag) => flag === "*").length
    : num(nodeCount.master);

  const persistent = record(record(record(results[3]?.body).persistent));
  const settings: Record<string, string> = {};
  for (const key of SETTING_KEYS) settings[key] = str(persistent[key]);

  const heapUsed = num(jvmMem.heap_used_in_bytes);
  const heapMax = num(jvmMem.heap_max_in_bytes);

  return {
    connectionId,
    name: str(stats.cluster_name) || str(health.cluster_name),
    version: Array.isArray(nodesCount.versions) ? (nodesCount.versions as unknown[]).map(str).join(" / ") : "",
    health: str(health.status),
    nodes: num(health.number_of_nodes) || num(nodeCount.total),
    dataNodes,
    masterNodes,
    indices: num(indices.count),
    docs: num(record(indices.docs).count),
    storeBytes: num(record(indices.store).size_in_bytes),
    primary: num(health.active_primary_shards),
    active: num(health.active_shards),
    unassigned: num(health.unassigned_shards),
    heapUsedPercent: heapMax > 0 ? (heapUsed / heapMax) * 100 : 0,
    heapMaxBytes: heapMax,
    diskTotalBytes: num(fs.total_in_bytes),
    diskFreeBytes: num(fs.available_in_bytes),
    settings,
    collectedAt: Date.now(),
  };
}

export interface DiffRow {
  group: string;
  item: string;
  left: string;
  right: string;
  same: boolean;
  hint?: string;
}

function push(
  out: DiffRow[],
  group: string,
  item: string,
  left: unknown,
  right: unknown,
  format: (value: unknown) => string,
  hint?: string,
): void {
  const same = String(left ?? "") === String(right ?? "");
  out.push({ group, item, left: format(left), right: format(right), same, hint });
}

const text = (value: unknown): string => (value === undefined || value === null || value === "" ? "—" : String(value));
const count = (value: unknown): string => formatCount(num(value));
const bytes = (value: unknown): string => formatBytes(num(value));
const percent = (value: unknown): string => `${num(value).toFixed(1)}%`;

export function diffFacts(left: ClusterFacts, right: ClusterFacts): DiffRow[] {
  const out: DiffRow[] = [];

  push(out, "cmp.g.overview", "cmp.i.name", left.name, right.name, text);
  push(out, "cmp.g.overview", "cmp.i.version", left.version, right.version, text);
  push(out, "cmp.g.overview", "cmp.i.health", left.health, right.health, text, "cmp.hint.health");

  push(out, "cmp.g.nodes", "cmp.i.nodes", left.nodes, right.nodes, count);
  push(out, "cmp.g.nodes", "cmp.i.dataNodes", left.dataNodes, right.dataNodes, count);
  push(out, "cmp.g.nodes", "cmp.i.masterNodes", left.masterNodes, right.masterNodes, count);
  push(out, "cmp.g.nodes", "cmp.i.heapMax", left.heapMaxBytes, right.heapMaxBytes, bytes);
  push(out, "cmp.g.nodes", "cmp.i.heapUsed", left.heapUsedPercent, right.heapUsedPercent, percent);

  push(out, "cmp.g.shards", "cmp.i.indices", left.indices, right.indices, count, "cmp.hint.indices");
  push(out, "cmp.g.shards", "cmp.i.primary", left.primary, right.primary, count);
  push(out, "cmp.g.shards", "cmp.i.active", left.active, right.active, count);
  push(out, "cmp.g.shards", "cmp.i.unassigned", left.unassigned, right.unassigned, count);
  push(out, "cmp.g.shards", "cmp.i.docs", left.docs, right.docs, count);
  push(out, "cmp.g.shards", "cmp.i.store", left.storeBytes, right.storeBytes, bytes);
  push(out, "cmp.g.shards", "cmp.i.diskTotal", left.diskTotalBytes, right.diskTotalBytes, bytes);
  push(out, "cmp.g.shards", "cmp.i.diskFree", left.diskFreeBytes, right.diskFreeBytes, bytes);

  for (const key of SETTING_KEYS) {
    const leftValue = left.settings[key] || SETTING_DEFAULT[key] || "";
    const rightValue = right.settings[key] || SETTING_DEFAULT[key] || "";
    out.push({
      group: "cmp.g.settings",
      item: SETTING_LABEL[key] ?? key,
      left: leftValue || t("cmp.default"),
      right: rightValue || t("cmp.default"),
      same: leftValue === rightValue,
      hint: key,
    });
  }

  return out;
}

export interface DiffSummary {
  total: number;
  different: number;
  same: number;
}

export function summarize(rows: DiffRow[]): DiffSummary {
  const different = rows.filter((row) => !row.same).length;
  return { total: rows.length, different, same: rows.length - different };
}

export function toCsv(rows: DiffRow[], leftLabel: string, rightLabel: string): string {
  const escape = (value: string): string => (/[",\n]/.test(value) ? `"${value.replace(/"/g, '""')}"` : value);
  const header = ["group", "item", escape(leftLabel), escape(rightLabel), "same"].join(",");
  const lines = rows.map((row) =>
    [escape(t(row.group)), escape(t(row.item)), escape(row.left), escape(row.right), row.same ? "yes" : "no"].join(","),
  );
  return [header, ...lines].join("\n");
}
