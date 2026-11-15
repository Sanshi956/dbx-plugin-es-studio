// P2 domain service layer. Every call goes through the generic, permission-gated
// `es/request` / `es/batch` transport (see backend/internal/transport/sidecar.go).
// This module only builds requests, parses the raw Elasticsearch JSON into typed
// structs, and classifies write operations so the UI can ask for confirmation.
// Business safety (read-only rejection, unsafe-path guard) stays in Go.

import type { DbxBridge } from "./bridge";
import { invoke, type EsResult } from "./api";

export type { EsResult } from "./api";

// ---------------------------------------------------------------------------
// small parsing helpers (defensive: ES field names vary across versions)
// ---------------------------------------------------------------------------

function record(value: unknown): Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {};
}

function str(value: unknown): string {
  return typeof value === "string" ? value : value === undefined || value === null ? "" : String(value);
}

function num(value: unknown): number {
  // `_cat/*` returns every field as a string ("2", "12915", "0.19"), so a
  // number-only check silently turned docs / store / cpu / heap into 0.
  if (typeof value === "number") return Number.isFinite(value) ? value : 0;
  if (typeof value === "string") {
    const raw = value.trim();
    if (raw === "") return 0;
    const parsed = Number(raw);
    return Number.isFinite(parsed) ? parsed : 0;
  }
  return 0;
}

/** Read a cat-API dotted key or a nested object key without crashing. */
function cat(value: Record<string, unknown>, ...keys: string[]): unknown {
  let current: unknown = value;
  for (const key of keys) {
    const obj = record(current);
    if (obj[key] !== undefined) current = obj[key];
    else if (obj[key.replace(/\./g, "_")] !== undefined) current = obj[key.replace(/\./g, "_")];
    else return undefined;
  }
  return current;
}

/** Read a nested object path safely (used for /_nodes structured JSON). */
function deep(value: unknown, ...keys: string[]): unknown {
  let cur: unknown = value;
  for (const k of keys) {
    if (cur === null || typeof cur !== "object") return undefined;
    cur = (cur as Record<string, unknown>)[k];
  }
  return cur;
}

// ---------------------------------------------------------------------------
// transport wrappers
// ---------------------------------------------------------------------------

export interface BatchRequest {
  method?: string;
  path: string;
  query?: Record<string, string[]>;
  body?: unknown;
}

export async function batch(
  bridge: DbxBridge,
  connectionId: string,
  requests: BatchRequest[],
): Promise<EsResult[]> {
  const result = await invoke<{ results: EsResult[] }>(bridge, "es/batch", {
    connection_id: connectionId,
    requests: requests.map((r) => ({
      method: (r.method ?? "GET").toUpperCase(),
      path: r.path,
      ...(r.query ? { query: r.query } : {}),
      ...(r.body !== undefined ? { body: r.body } : {}),
    })),
  });
  return Array.isArray(result?.results) ? result.results : [];
}

export async function single(
  bridge: DbxBridge,
  connectionId: string,
  method: string,
  path: string,
  body?: unknown,
): Promise<EsResult> {
  try {
    const result = await invoke<EsResult>(bridge, "es/request", {
      connection_id: connectionId,
      method: method.toUpperCase(),
      path,
      ...(body !== undefined ? { body } : {}),
    });
    return result ?? { status: 0 };
  } catch (cause) {
    const message = cause instanceof Error ? cause.message : String(cause);
    // Keep the Sidecar code (-32003 read-only, -32004 unsafe path, ...) so the UI
    // can tell a permission denial apart from a transport failure.
    const code = typeof (cause as { code?: unknown })?.code === "number" ? (cause as { code: number }).code : -32000;
    return { status: 0, error: { code, message } };
  }
}

export function isError(r: EsResult): boolean {
  return !!r.error || (r.status !== undefined && r.status >= 400);
}

// ---------------------------------------------------------------------------
// Nodes
// ---------------------------------------------------------------------------

export interface NodeRow {
  id: string;
  name: string;
  ip: string;
  roles: string[];
  isMaster: boolean;
  version: string;
  heapPercent: number;
  cpu: number;
  load1m: number;
  ramPercent: number;
}

export async function fetchNodes(bridge: DbxBridge, connectionId: string): Promise<NodeRow[]> {
  const rows = await batch(bridge, connectionId, [
    { path: "/_cat/nodes?format=json" },
    { path: "/_nodes" },
  ]);
  const catNodes = Array.isArray(rows[0]?.body) ? (rows[0].body as unknown[]) : [];
  const nodesInfo = record(record(rows[1]?.body).nodes);

  const byName = new Map<string, Record<string, unknown>>();
  for (const entry of catNodes) {
    const node = record(entry);
    const name = str(cat(node, "name"));
    byName.set(name, node);
  }

  const out: NodeRow[] = [];
  for (const [id, raw] of Object.entries(nodesInfo)) {
    const node = record(raw);
    const name = str(node.name);
    const catNode = byName.get(name) ?? {};
    const rolesRaw = node.roles;
    const roles = Array.isArray(rolesRaw) ? (rolesRaw as unknown[]).map((r) => str(r)) : str(rolesRaw).split(",");
    out.push({
      id,
      name,
      ip: str(node.ip),
      roles: roles.filter(Boolean),
      isMaster: str(node.master) === "*" || roles.includes("master"),
      version: str(node.version),
      heapPercent: num(cat(catNode, "heap.percent")) || num(deep(node, "jvm", "mem", "heap_used_percent")),
      cpu: num(cat(catNode, "cpu")) || num(deep(node, "os", "cpu", "percent")),
      load1m: num(cat(catNode, "load_1m")),
      ramPercent: num(cat(catNode, "ram.percent")),
    });
  }
  if (out.length === 0) {
    // Fall back to cat nodes only if /_nodes was empty.
    for (const entry of catNodes) {
      const node = record(entry);
      const roles = str(node.roles).split(",");
      out.push({
        id: str(node.name),
        name: str(node.name),
        ip: str(node.ip),
        roles: roles.filter(Boolean),
        isMaster: str(node.master) === "*",
        version: "",
        heapPercent: num(node["heap.percent"]),
        cpu: num(node.cpu),
        load1m: num(node["load_1m"]),
        ramPercent: num(node["ram.percent"]),
      });
    }
  }
  return out;
}

// ---------------------------------------------------------------------------
// Shards / topology
// ---------------------------------------------------------------------------

export interface ShardRow {
  index: string;
  shard: number;
  prirep: "p" | "r";
  state: string;
  docs: number;
  storeBytes: number;
  node: string;
  ip: string;
  unassigned: boolean;
}

export async function fetchShards(bridge: DbxBridge, connectionId: string): Promise<ShardRow[]> {
  const rows = await batch(bridge, connectionId, [{ path: "/_cat/shards?format=json&bytes=b" }]);
  const body = Array.isArray(rows[0]?.body) ? (rows[0].body as unknown[]) : [];
  return body.map((entry) => {
    const s = record(entry);
    const state = str(s.state);
    return {
      index: str(s.index),
      shard: num(s.shard),
      prirep: str(s.prirep) === "p" ? "p" : "r",
      state,
      docs: num(s.docs),
      storeBytes: num(s.store),
      node: str(s.node),
      ip: str(s.ip),
      unassigned: state === "UNASSIGNED",
    };
  });
}

/** Build a node x shard topology matrix for the "cluster topology" view. */
/**
 * A single shard chip. `label` is what the user sees ("0r:UNASSIGNED");
 * `key` is guaranteed unique even when one shard has several replicas in the
 * same state (Svelte keyed each blocks throw on duplicate keys).
 */
export interface ShardTag {
  key: string;
  label: string;
}

export interface TopologyMatrix {
  nodes: string[];
  indices: string[];
  // matrix[indexName][nodeName] = list of shard chips
  cells: Record<string, Record<string, ShardTag[]>>;
  unassignedByIndex: Record<string, ShardTag[]>;
}

export function buildTopology(shards: ShardRow[]): TopologyMatrix {
  const nodeSet = new Set<string>();
  const indexSet = new Set<string>();
  for (const s of shards) {
    if (s.node) nodeSet.add(s.node);
    indexSet.add(s.index);
  }
  const nodes = [...nodeSet].sort();
  const indices = [...indexSet].sort();
  const cells: Record<string, Record<string, ShardTag[]>> = {};
  const unassignedByIndex: Record<string, ShardTag[]> = {};
  const seen = new Map<string, number>();
  for (const s of shards) {
    // "/_cat/shards" has one row per copy, so the same shard id + prirep + state
    // can legitimately repeat (e.g. 2 unassigned replicas of shard 0). Suffix a
    // counter to keep the render key unique while showing the identical label.
    const base = `${s.shard}${s.prirep}:${s.state}`;
    const n = (seen.get(base) ?? 0) + 1;
    seen.set(base, n);
    const tag: ShardTag = { key: n === 1 ? `${s.index}|${base}` : `${s.index}|${base}#${n}`, label: base };
    if (!s.node) {
      (unassignedByIndex[s.index] ??= []).push(tag);
      continue;
    }
    (cells[s.index] ??= {})[s.node] ??= [];
    cells[s.index][s.node].push(tag);
  }
  return { nodes, indices, cells, unassignedByIndex };
}

// ---------------------------------------------------------------------------
// Indices
// ---------------------------------------------------------------------------

export interface IndexRow {
  index: string;
  health: string;
  status: string;
  uuid: string;
  primaryShards: number;
  replicaShards: number;
  docsCount: number;
  docsDeleted: number;
  storeBytes: number;
}

export async function fetchIndices(bridge: DbxBridge, connectionId: string): Promise<IndexRow[]> {
  const rows = await batch(bridge, connectionId, [{ path: "/_cat/indices?format=json&bytes=b" }]);
  const body = Array.isArray(rows[0]?.body) ? (rows[0].body as unknown[]) : [];
  return body.map((entry) => {
    const i = record(entry);
    return {
      index: str(i.index),
      health: str(i.health),
      status: str(i.status),
      uuid: str(i.uuid),
      primaryShards: num(i.pri),
      replicaShards: num(i.rep),
      docsCount: num(cat(i, "docs.count")),
      docsDeleted: num(cat(i, "docs.deleted")),
      storeBytes: num(cat(i, "store.size")),
    };
  });
}

// ---------------------------------------------------------------------------
// Fields (from _mapping) — so the UI can offer real field names instead of
// asking the user to remember them.
// ---------------------------------------------------------------------------

export interface FieldInfo {
  /** Dotted path, e.g. `name`, `name.kw`, `address.city`. */
  name: string;
  type: string;
}

function collectFields(
  props: Record<string, unknown>,
  prefix: string,
  out: Map<string, string>,
  depth: number,
): void {
  if (depth > 8) return;
  for (const [name, raw] of Object.entries(props)) {
    const def = record(raw);
    const path = prefix ? `${prefix}.${name}` : name;
    const type = str(def.type);
    if (type) out.set(path, type);
    // multi-fields: name.kw -> keyword
    for (const [sub, subRaw] of Object.entries(record(def.fields))) {
      out.set(`${path}.${sub}`, str(record(subRaw).type) || "text");
    }
    const nested = record(def.properties);
    if (Object.keys(nested).length) collectFields(nested, path, out, depth + 1);
  }
}

/**
 * Read field names + types from `GET /{index}/_mapping`.
 * Works for aliases (mapping spans several indices, names are deduped) and
 * returns `[]` for a closed/missing index instead of throwing.
 */
export async function fetchFields(
  bridge: DbxBridge,
  connectionId: string,
  index: string,
): Promise<FieldInfo[]> {
  const res = await single(bridge, connectionId, "GET", `/${encodeURIComponent(index)}/_mapping`);
  if (isError(res)) return [];
  const found = new Map<string, string>();
  const body = record(res.body);
  for (const indexKey of Object.keys(body)) {
    const mappings = record(record(body[indexKey]).mappings);
    collectFields(record(mappings.properties), "", found, 0);
  }
  return [...found.entries()]
    .map(([name, type]) => ({ name, type }))
    .sort((a, b) => a.name.localeCompare(b.name));
}

/**
 * Search body for the plain "keyword box" flow.
 * Empty keyword -> match_all. Empty field -> multi_match across all fields
 * (verified against ES 8.19: `fields: ["*"]` expands to every searchable field).
 */
export function simpleSearchBody(
  keyword: string,
  field: string,
  size: number,
): Record<string, unknown> {
  const q = keyword.trim();
  if (!q) return { size, query: { match_all: {} } };
  return {
    size,
    query: field
      ? { match: { [field]: q } }
      : { multi_match: { query: q, fields: ["*"] } },
  };
}

// ---------------------------------------------------------------------------
// Aliases
// ---------------------------------------------------------------------------

export interface AliasRow {
  alias: string;
  index: string;
  filter: string;
}

export async function fetchAliases(bridge: DbxBridge, connectionId: string): Promise<AliasRow[]> {
  const rows = await batch(bridge, connectionId, [{ path: "/_cat/aliases?format=json" }]);
  const body = Array.isArray(rows[0]?.body) ? (rows[0].body as unknown[]) : [];
  return body.map((entry) => {
    const a = record(entry);
    return {
      alias: str(a.alias),
      index: str(a.index),
      filter: str(a.filter),
    };
  });
}

export function aliasActionsBody(actions: { add?: { index: string; alias: string }; remove?: { index: string; alias: string } }[]) {
  return { actions };
}

// ---------------------------------------------------------------------------
// Documents (Browser)
// ---------------------------------------------------------------------------

export interface DocHit {
  index: string;
  id: string;
  score: number;
  source: unknown;
}

export interface SearchResult {
  took: number;
  total: number;
  hits: DocHit[];
  raw: unknown;
  error?: string;
}

function parseSearch(body: unknown): SearchResult {
  const r = record(body);
  const hits = record(r.hits);
  const total = record(hits.total);
  const items = Array.isArray(hits.hits) ? (hits.hits as unknown[]) : [];
  return {
    took: num(r.took),
    total: num(total.value ?? total),
    hits: items.map((h) => {
      const hit = record(h);
      return {
        index: str(hit._index),
        id: str(hit._id),
        score: typeof hit._score === "number" ? (hit._score as number) : 0,
        source: hit._source,
      };
    }),
    raw: body,
  };
}

export async function searchIndex(
  bridge: DbxBridge,
  connectionId: string,
  index: string,
  body: Record<string, unknown>,
): Promise<SearchResult> {
  const res = await single(bridge, connectionId, "POST", `/${encodeURIComponent(index)}/_search`, body);
  if (isError(res)) return { took: 0, total: 0, hits: [], raw: res.error, error: res.error?.message ?? `HTTP ${res.status}` };
  return parseSearch(res.body);
}

export async function getDocument(
  bridge: DbxBridge,
  connectionId: string,
  index: string,
  id: string,
): Promise<{ found: boolean; source: unknown; raw: unknown }> {
  const res = await single(bridge, connectionId, "GET", `/${encodeURIComponent(index)}/_doc/${encodeURIComponent(id)}`);
  if (isError(res)) return { found: false, source: null, raw: res.error };
  const r = record(res.body);
  return { found: r.found !== false, source: r._source, raw: res.body };
}

export async function deleteDocument(
  bridge: DbxBridge,
  connectionId: string,
  index: string,
  id: string,
): Promise<EsResult> {
  return single(bridge, connectionId, "DELETE", `/${encodeURIComponent(index)}/_doc/${encodeURIComponent(id)}`);
}

// ---------------------------------------------------------------------------
// Query Builder (form -> DSL)
// ---------------------------------------------------------------------------

export type QueryClause =
  | { type: "match_all"; boost?: number }
  | { type: "term"; field: string; value: string }
  | { type: "match"; field: string; value: string }
  | { type: "range"; field: string; gte?: string; lte?: string; gt?: string; lt?: string }
  | { type: "wildcard"; field: string; value: string };

export function buildDsl(
  clauses: QueryClause[],
  opts: { size?: number; from?: number; sortField?: string; sortOrder?: "asc" | "desc" } = {},
): Record<string, unknown> {
  const must: Record<string, unknown>[] = [];
  const filter: Record<string, unknown>[] = [];
  for (const c of clauses) {
    switch (c.type) {
      case "match_all":
        must.push({ match_all: c.boost ? { boost: c.boost } : {} });
        break;
      case "term":
        if (c.field && c.value !== "")
          filter.push({ term: { [c.field]: c.value } });
        break;
      case "match":
        if (c.field && c.value !== "") must.push({ match: { [c.field]: c.value } });
        break;
      case "range": {
        const r: Record<string, string> = {};
        if (c.gte) r.gte = c.gte;
        if (c.lte) r.lte = c.lte;
        if (c.gt) r.gt = c.gt;
        if (c.lt) r.lt = c.lt;
        if (c.field && Object.keys(r).length) filter.push({ range: { [c.field]: r } });
        break;
      }
      case "wildcard":
        if (c.field && c.value !== "") filter.push({ wildcard: { [c.field]: c.value } });
        break;
    }
  }
  const bool: Record<string, unknown> = {};
  if (must.length) bool.must = must;
  if (filter.length) bool.filter = filter;
  const dsl: Record<string, unknown> = { query: Object.keys(bool).length ? { bool } : { match_all: {} } };
  if (opts.size !== undefined) dsl.size = opts.size;
  if (opts.from !== undefined) dsl.from = opts.from;
  if (opts.sortField) dsl.sort = [{ [opts.sortField]: opts.sortOrder ?? "asc" }];
  return dsl;
}

// ---------------------------------------------------------------------------
// Analyzer
// ---------------------------------------------------------------------------

export interface AnalyzeToken {
  token: string;
  start: number;
  end: number;
  type: string;
  position: number;
}

export interface AnalyzeResult {
  tokens: AnalyzeToken[];
  detail: unknown;
}

export async function analyzeText(
  bridge: DbxBridge,
  connectionId: string,
  params: {
    text: string;
    analyzer?: string;
    field?: string;
    tokenizer?: string;
    filter?: string[];
    char_filter?: string[];
  },
): Promise<AnalyzeResult> {
  const body: Record<string, unknown> = { text: params.text };
  if (params.analyzer) body.analyzer = params.analyzer;
  if (params.field) body.field = params.field;
  if (params.tokenizer) body.tokenizer = params.tokenizer;
  if (params.filter?.length) body.filter = params.filter;
  if (params.char_filter?.length) body.char_filter = params.char_filter;
  const res = await single(bridge, connectionId, "POST", "/_analyze", body);
  if (isError(res)) return { tokens: [], detail: res.error };
  const r = record(res.body);
  const tokens = Array.isArray(r.tokens) ? (r.tokens as unknown[]).map((t) => {
    const tk = record(t);
    return {
      token: str(tk.token),
      start: num(tk.start_offset),
      end: num(tk.end_offset),
      type: str(tk.type),
      position: num(tk.position),
    };
  }) : [];
  return { tokens, detail: res.body };
}

// ---------------------------------------------------------------------------
// REST Console
// ---------------------------------------------------------------------------

export async function restCall(
  bridge: DbxBridge,
  connectionId: string,
  method: string,
  path: string,
  body?: string,
): Promise<EsResult> {
  let parsed: unknown = undefined;
  if (body !== undefined && body.trim() !== "") {
    try {
      parsed = JSON.parse(body);
    } catch {
      return { status: 0, error: { code: -32602, message: "Request body is not valid JSON" } };
  }
  }
  return single(bridge, connectionId, method, path, parsed);
}

// ---------------------------------------------------------------------------
// Danger classification (UI confirmation only; Go is authoritative for safety)
// ---------------------------------------------------------------------------

export type RiskKind = "delete-index" | "delete-doc" | "close-index" | "open-index" | "aliases" | "delete-by-query" | "forcemerge" | "refresh" | "flush" | "create-index" | "bulk" | "reindex" | "other";

export interface OperationRisk {
  dangerous: boolean;
  kind: RiskKind;
  label: string;
  scope: string;
}

const DESTRUCTIVE_ENDS: Record<string, { kind: RiskKind; label: string }> = {
  "_close": { kind: "close-index", label: "关闭索引" },
  "_open": { kind: "open-index", label: "打开索引" },
  "_delete_by_query": { kind: "delete-by-query", label: "按查询删除" },
  "_forcemerge": { kind: "forcemerge", label: "强制合并段" },
  "_flush": { kind: "flush", label: "刷新事务日志" },
};

export function classifyOperation(method: string, path: string): OperationRisk {
  const m = method.toUpperCase();
  const p = path.trim();
  const segs = p.split("/").filter(Boolean);

  if (m === "DELETE") {
    // DELETE /{index}/_doc/{id} or /{index}/_doc/{id}/_create etc.
    if (segs.length >= 2 && segs[1] === "_doc") {
      return { dangerous: true, kind: "delete-doc", label: "删除文档", scope: `索引 ${segs[0]} 中的文档 ${segs[2] ?? ""}` };
    }
    return { dangerous: true, kind: "delete-index", label: "删除索引", scope: `索引 ${segs[0] ?? "*"}` };
  }

  if (m === "POST" || m === "PUT") {
    if (segs.length >= 1 && segs[segs.length - 1].startsWith("_")) {
      const tail = segs[segs.length - 1];
      if (tail === "_aliases") {
        return { dangerous: true, kind: "aliases", label: "修改别名", scope: "集群别名映射" };
      }
      if (tail === "_delete_by_query") {
        return { dangerous: true, kind: "delete-by-query", label: "按查询删除文档", scope: `索引 ${segs[0] ?? "*"}` };
      }
      const mapped = DESTRUCTIVE_ENDS[tail];
      if (mapped) {
        return { dangerous: mapped.kind !== "refresh" && mapped.kind !== "flush", kind: mapped.kind, label: mapped.label, scope: `索引 ${segs[0] ?? "*"}` };
      }
    }
    if (m === "PUT" && segs.length === 1 && !segs[0].startsWith("_")) {
      return { dangerous: true, kind: "create-index", label: "创建索引", scope: `索引 ${segs[0]}` };
    }
    if (m === "POST" && segs.length === 1 && !segs[0].startsWith("_")) {
      return { dangerous: true, kind: "bulk", label: "批量写入", scope: `索引 ${segs[0]}` };
    }
    if (segs.length === 1 && segs[0] === "_reindex") {
      return { dangerous: true, kind: "reindex", label: "重建索引", scope: "跨索引重建" };
    }
  }

  if (m === "POST" && p === "/_analyze") {
    return { dangerous: false, kind: "other", label: "分析文本", scope: "无状态变更" };
  }
  if (m === "POST" && (p.endsWith("/_refresh") || p === "/_refresh")) {
    return { dangerous: false, kind: "refresh", label: "刷新", scope: "使新文档可搜索" };
  }
  if (m === "GET" || m === "HEAD" || m === "POST" && (p.endsWith("/_search") || p === "/_search") || p.endsWith("/_doc/") ) {
    return { dangerous: false, kind: "other", label: "只读查询", scope: "无状态变更" };
  }
  return { dangerous: false, kind: "other", label: m, scope: "无状态变更" };
}
