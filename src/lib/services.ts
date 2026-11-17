// P2 domain service layer. Every call goes through the generic, permission-gated
// `es/request` / `es/batch` transport (see backend/internal/transport/sidecar.go).
// This module only builds requests, parses the raw Elasticsearch JSON into typed
// structs, and classifies write operations so the UI can ask for confirmation.
// Business safety (read-only rejection, unsafe-path guard) stays in Go.

import type { DbxBridge } from "./bridge";
import { t } from "./i18n.svelte.js";
import { invoke, type EsResult } from "./api";
import { mapProfileShards } from "./profile-parse";
import type { ProfileNode, ProfileShard } from "./profile-parse";

export type { EsResult } from "./api";
export type { ProfileNode, ProfileShard } from "./profile-parse";

// ---------------------------------------------------------------------------
// small parsing helpers (defensive: ES field names vary across versions)
// ---------------------------------------------------------------------------

export function record(value: unknown): Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {};
}

export function str(value: unknown): string {
  return typeof value === "string" ? value : value === undefined || value === null ? "" : String(value);
}

export function num(value: unknown): number {
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
export function cat(value: Record<string, unknown>, ...keys: string[]): unknown {
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

/**
 * Message for a failed response. Elasticsearch explains itself in
 * `body.error.reason` (and `root_cause`), which is far more useful than the
 * bare status our transport also reports; a 429 without an explanation gets
 * the backpressure hint, because "HTTP 429" alone tells nobody what to do.
 */
export function failureText(res: EsResult): string {
  const err = record(record(res.body).error);
  const causes = Array.isArray(err.root_cause) ? err.root_cause : [];
  const reason = str(err.reason) || str(record(causes[0]).reason);
  if (reason) return `${reason} · HTTP ${res.status}`;
  if (res.error?.message) return `${res.error.message} · HTTP ${res.status}`;
  if (res.status === 429) return t("err.tooManyRequests");
  return `HTTP ${res.status}`;
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
  /** `unassigned.reason` — INDEX_CREATED / NODE_LEFT / ALLOCATION_FAILED … */
  reason: string;
  /** how long the copy has been waiting (e.g. "3.2m"), empty when assigned */
  waiting: string;
}

// `_cat/shards` only returns the short default column set, and the reason for an
// unassigned copy is the single most useful field here — ask for it explicitly.
const SHARD_COLUMNS = [
  "index",
  "shard",
  "prirep",
  "state",
  "docs",
  "store",
  "ip",
  "node",
  "unassigned.reason",
  "unassigned.at",
  "unassigned.for",
].join(",");

export async function fetchShards(bridge: DbxBridge, connectionId: string): Promise<ShardRow[]> {
  const rows = await batch(bridge, connectionId, [
    { path: `/_cat/shards?format=json&bytes=b&h=${SHARD_COLUMNS}` },
  ]);
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
      reason: str(s["unassigned.reason"]),
      waiting: str(s["unassigned.for"]),
    };
  });
}

/** Counts per shard copy state, so the page can surface in-flight allocation. */
export interface ShardSummary {
  total: number;
  started: number;
  initializing: number;
  relocating: number;
  unassigned: number;
  /** copies ES is actively moving/creating right now */
  inFlight: number;
}

export function summarizeShards(shards: ShardRow[]): ShardSummary {
  const count = (state: string): number => shards.filter((s) => s.state === state).length;
  const initializing = count("INITIALIZING");
  const relocating = count("RELOCATING");
  return {
    total: shards.length,
    started: count("STARTED"),
    initializing,
    relocating,
    unassigned: count("UNASSIGNED"),
    inFlight: initializing + relocating,
  };
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
  /** For leaves inside a `nested` object: the innermost `nested` parent path.
   *  A plain `term` on such a leaf silently matches nothing — the query must be
   *  wrapped in `nested: { path }`, which `buildDsl` does when this is set. */
  nestedPath?: string;
}

function collectFields(
  props: Record<string, unknown>,
  prefix: string,
  out: Map<string, { type: string; nestedPath: string }>,
  depth: number,
  nestedPath = "",
): void {
  if (depth > 8) return;
  for (const [name, raw] of Object.entries(props)) {
    const def = record(raw);
    const path = prefix ? `${prefix}.${name}` : name;
    const type = str(def.type);
    if (type) out.set(path, { type, nestedPath });
    // multi-fields: name.kw -> keyword
    for (const [sub, subRaw] of Object.entries(record(def.fields))) {
      out.set(`${path}.${sub}`, { type: str(record(subRaw).type) || "text", nestedPath });
    }
    // Nested parent: everything below it queries through that path.
    const nested = record(def.properties);
    if (Object.keys(nested).length) {
      collectFields(nested, path, out, depth + 1, type === "nested" ? path : nestedPath);
    }
  }
}

/**
 * A 400/405/501 from a versioned endpoint means "this build does not have the
 * API", which must read differently from "the cluster has nothing here".
 * Silently returning an empty list is how a 7.x cluster looked broken instead
 * of simply not supporting data streams. 404 stays a legitimate empty result.
 */
function throwIfUnsupported(res: EsResult, api: string): void {
  // A transport failure (sidecar could not reach the cluster) must not be folded
  // into "no rows": `status: 0` / a plugin error would otherwise render as an
  // empty table, which reads as "this cluster has none".
  if (res.error && res.status < 400) {
    throw new Error(failureText(res));
  }
  if (res.status === 400 || res.status === 405 || res.status === 501) {
    throw new Error(t("err.unsupportedApi", { api }));
  }
}

/** True for a plain object — guards the typed/typeless mapping branch. */
function isRecord(value: unknown): boolean {
  return typeof value === "object" && value !== null && !Array.isArray(value);
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
  const found = new Map<string, { type: string; nestedPath: string }>();
  const body = record(res.body);
  for (const indexKey of Object.keys(body)) {
    const mappings = record(record(body[indexKey]).mappings);
    // Typeless (8.x/9.x): properties live directly under mappings. Typed
    // (6.x, and 7.x before types were removed): properties sit one level
    // deeper, under the type name. Reading only `mappings.properties` is why
    // the field picker used to come up empty on a 6.x cluster.
    if (isRecord(mappings.properties)) {
      collectFields(record(mappings.properties), "", found, 0);
      continue;
    }
    for (const typeName of Object.keys(mappings)) {
      const typed = record(mappings[typeName]);
      if (isRecord(typed.properties)) collectFields(record(typed.properties), "", found, 0);
    }
  }
  return [...found.entries()]
    .map(([name, { type, nestedPath }]) => ({
      name, type, ...(nestedPath ? { nestedPath } : {}),
    }))
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
  /** alias filter expression (JSON) — empty when the alias has no filter */
  filter: string;
  /** `_cat/aliases` reports "-" for "not the write index" */
  writeIndex: boolean;
  /** set with `is_hidden: true`; hidden aliases are skipped by wildcards */
  hidden: boolean;
  indexRouting: string;
  searchRouting: string;
}

/**
 * Reads `GET /_alias` rather than `_cat/aliases`: the cat API has no column for
 * `is_hidden`, and it collapses both routings into `routing.index` /
 * `routing.search` with "-" for empty, which is what made the Filter column
 * look meaningless.
 */
export async function fetchAliases(bridge: DbxBridge, connectionId: string): Promise<AliasRow[]> {
  const rows = await batch(bridge, connectionId, [{ path: "/_alias" }]);
  const body = record(rows[0]?.body);
  const out: AliasRow[] = [];
  for (const [index, node] of Object.entries(body)) {
    const definitions = record(record(node).aliases);
    for (const [alias, raw] of Object.entries(definitions)) {
      const def = record(raw);
      const shared = str(def.routing).trim();
      const filter = def.filter;
      out.push({
        index,
        alias,
        filter: filter && typeof filter === "object" && Object.keys(record(filter)).length ? JSON.stringify(filter) : "",
        writeIndex: def.is_write_index === true,
        hidden: def.is_hidden === true,
        indexRouting: shared || str(def.index_routing).trim(),
        searchRouting: shared || str(def.search_routing).trim(),
      });
    }
  }
  return out.sort((a, b) => a.alias.localeCompare(b.alias) || a.index.localeCompare(b.index));
}

export interface AliasAdd {
  index: string;
  alias: string;
  filter?: unknown;
  routing?: string;
  index_routing?: string;
  search_routing?: string;
  is_write_index?: boolean;
  is_hidden?: boolean;
}

export interface AliasRemove {
  index: string;
  alias: string;
}

export function aliasActionsBody(actions: ({ add: AliasAdd } | { remove: AliasRemove })[]) {
  return { actions };
}

/** The filter box on the Aliases page takes raw query DSL; empty means "no filter". */
export function parseAliasFilter(text: string): { ok: true; value?: Record<string, unknown> } | { ok: false; error: string } {
  const trimmed = text.trim();
  if (!trimmed) return { ok: true };
  let parsed: unknown;
  try {
    parsed = JSON.parse(trimmed);
  } catch (cause) {
    return { ok: false, error: t("filter.badJson", { detail: cause instanceof Error ? cause.message : String(cause) }) };
  }
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
    return { ok: false, error: t("filter.notObject") };
  }
  return { ok: true, value: parsed as Record<string, unknown> };
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
  if (isError(res)) return { took: 0, total: 0, hits: [], raw: res.error, error: failureText(res) };
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

/** Where a clause lives inside a bool query. */
export type ClauseOccur = "must" | "filter" | "should" | "must_not";

export type QueryClause =
  | { type: "match_all"; boost?: number }
  | { type: "term"; field: string; value: string; occur?: ClauseOccur }
  | { type: "match"; field: string; value: string; occur?: ClauseOccur }
  | { type: "match_phrase"; field: string; value: string; occur?: ClauseOccur }
  | { type: "prefix"; field: string; value: string; occur?: ClauseOccur }
  | { type: "wildcard"; field: string; value: string; occur?: ClauseOccur }
  | { type: "exists"; field: string; occur?: ClauseOccur }
  | { type: "range"; field: string; gte?: string; lte?: string; gt?: string; lt?: string; occur?: ClauseOccur };

/** Leaf clause kinds a user can pick; `match_all` is not one of them. */
export type LeafClauseType = Exclude<QueryClause["type"], "match_all">;

export interface DslOptions {
  size?: number;
  from?: number;
  sortField?: string;
  sortOrder?: "asc" | "desc";
  /** only meaningful when there is at least one `should` clause */
  minimumShouldMatch?: string;
  /**
   * Leaf field -> innermost `nested` path (from the index mapping). A term on
   * `tags.name` without the wrapper matches nothing on real nested docs, so
   * clauses whose field is listed here are wrapped in
   * `{ nested: { path, query } }` — the silent-zero-hits trap, closed.
   */
  nestedPaths?: Record<string, string>;
}

/**
 * Turn the clause list into a bool query. Each clause declares its own
 * occurrence, so `must` / `filter` / `should` / `must_not` can be combined —
 * the previous version silently mapped term/range to `filter` and match to
 * `must`, which made "exclude these" impossible to express.
 *
 * Defaults keep term/range/prefix/wildcard/exists in `filter` (cached, no
 * scoring) and match/match_phrase in `must`.
 */
export function buildDsl(clauses: QueryClause[], opts: DslOptions = {}): Record<string, unknown> {
  const groups: Record<ClauseOccur, Record<string, unknown>[]> = {
    must: [],
    filter: [],
    should: [],
    must_not: [],
  };

  const defaultOccur = (c: QueryClause): ClauseOccur => {
    if (c.type === "match" || c.type === "match_phrase") return "must";
    return "filter";
  };

  for (const c of clauses) {
    if (c.type === "match_all") {
      groups.must.push({ match_all: c.boost ? { boost: c.boost } : {} });
      continue;
    }
    const occur = c.occur ?? defaultOccur(c);
    const field = c.field;
    // Leaf clauses on a `nested` field go through the nested query wrapper.
    const wrapPath = field && opts.nestedPaths?.[field];
    const push = (leaf: Record<string, unknown>): void => {
      if (wrapPath) {
        groups[occur].push({ nested: { path: wrapPath, query: leaf } });
      } else {
        groups[occur].push(leaf);
      }
    };
    switch (c.type) {
      case "term":
        if (field && c.value !== "") push({ term: { [field]: c.value } });
        break;
      case "match":
        if (field && c.value !== "") push({ match: { [field]: c.value } });
        break;
      case "match_phrase":
        if (field && c.value !== "") push({ match_phrase: { [field]: c.value } });
        break;
      case "prefix":
        if (field && c.value !== "") push({ prefix: { [field]: c.value } });
        break;
      case "wildcard":
        if (field && c.value !== "") push({ wildcard: { [field]: c.value } });
        break;
      case "exists":
        if (field) push({ exists: { field } });
        break;
      case "range": {
        const r: Record<string, string> = {};
        if (c.gte) r.gte = c.gte;
        if (c.lte) r.lte = c.lte;
        if (c.gt) r.gt = c.gt;
        if (c.lt) r.lt = c.lt;
        if (field && Object.keys(r).length) push({ range: { [field]: r } });
        break;
      }
    }
  }

  const bool: Record<string, unknown> = {};
  if (groups.must.length) bool.must = groups.must;
  if (groups.filter.length) bool.filter = groups.filter;
  if (groups.should.length) {
    // A bool with only `should` clauses requires one match by default; when the
    // user combines should with must/filter, ES defaults to "optional", which is
    // rarely what "I added a should" means — be explicit. It is assigned before
    // `should` so it shows up above the clause list in the DSL preview.
    const msm = (opts.minimumShouldMatch ?? "").trim();
    bool.minimum_should_match = msm === "" ? 1 : msm;
    bool.should = groups.should;
  }
  if (groups.must_not.length) bool.must_not = groups.must_not;

  const dsl: Record<string, unknown> = { query: Object.keys(bool).length ? { bool } : { match_all: {} } };
  if (opts.size !== undefined) dsl.size = opts.size;
  if (opts.from !== undefined) dsl.from = opts.from;
  if (opts.sortField) dsl.sort = [{ [opts.sortField]: opts.sortOrder ?? "asc" }];
  return dsl;
}

// ---------------------------------------------------------------------------
// Field-type driven operators (Query Builder should not offer `match` on a
// `long`, nor `range` on a `boolean`)
// ---------------------------------------------------------------------------

const NUMERIC_TYPES = new Set([
  "long", "integer", "short", "byte", "double", "float", "half_float",
  "scaled_float", "unsigned_long",
]);
const DATE_TYPES = new Set(["date", "date_nanos"]);
const TEXT_TYPES = new Set(["text", "match_only_text", "search_as_you_type", "annotated_text"]);
const KEYWORD_TYPES = new Set(["keyword", "constant_keyword", "wildcard"]);
const GEO_TYPES = new Set(["geo_point", "geo_shape", "point", "shape", "ip"]);

export interface OperatorOption {
  value: LeafClauseType;
  label: string;
}

/** Operators that actually make sense for a mapping type, most useful first. */
export function operatorsForType(type: string): OperatorOption[] {
  const trimmed = type.trim();
  if (NUMERIC_TYPES.has(trimmed) || DATE_TYPES.has(trimmed)) {
    return [
      { value: "range", label: t("qb.op.range") },
      { value: "term", label: t("qb.op.term") },
      { value: "exists", label: t("qb.op.exists") },
    ];
  }
  if (trimmed === "boolean") {
    return [
      { value: "term", label: t("qb.op.termBool") },
      { value: "exists", label: t("qb.op.exists") },
    ];
  }
  if (KEYWORD_TYPES.has(trimmed)) {
    return [
      { value: "term", label: t("qb.op.term") },
      { value: "wildcard", label: t("qb.op.wildcard") },
      { value: "prefix", label: t("qb.op.prefix") },
      { value: "exists", label: t("qb.op.exists") },
    ];
  }
  if (TEXT_TYPES.has(trimmed)) {
    return [
      { value: "match", label: t("qb.op.match") },
      { value: "match_phrase", label: t("qb.op.matchPhrase") },
      { value: "term", label: t("qb.op.termKeyword") },
      { value: "prefix", label: t("qb.op.prefix") },
      { value: "wildcard", label: t("qb.op.wildcard") },
      { value: "exists", label: t("qb.op.exists") },
    ];
  }
  if (GEO_TYPES.has(trimmed)) {
    return [
      { value: "term", label: t("qb.op.term") },
      { value: "exists", label: t("qb.op.exists") },
    ];
  }
  // The nested object itself: only existence is meaningful, and it still goes
  // through the nested wrapper (see nestedPathsOf) — a plain term on the
  // object cannot match anything.
  if (trimmed === "nested") {
    return [{ value: "exists", label: t("qb.op.exists") }];
  }
  // object / flattened / unknown: only existence is safe to build
  // without knowing the shape.
  return [
    { value: "exists", label: t("qb.op.exists") },
    { value: "term", label: t("qb.op.term") },
  ];
}

/**
 * Leaf field -> innermost `nested` path, plus the nested parents themselves.
 * Feed this into `buildDsl` so clauses on nested fields get wrapped instead of
 * silently matching zero documents.
 */
export function nestedPathsOf(fields: FieldInfo[]): Record<string, string> {
  const map: Record<string, string> = {};
  for (const f of fields) {
    if (f.type === "nested") map[f.name] = f.name;
    else if (f.nestedPath) map[f.name] = f.nestedPath;
  }
  return Object.keys(map).length ? map : {};
}

/** Default operator for a field, used when the user picks a field. */
export function defaultOperatorForType(type: string): LeafClauseType {
  return operatorsForType(type)[0].value;
}

/** Clause value kinds that need no free-text input. */
export function clauseHasNoValue(type: LeafClauseType): boolean {
  return type === "exists";
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
  index: string,
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
  // Field/tokenizer context is only valid against an index: POST /_analyze with
  // a `field` is rejected with "requires an index", which is why every
  // field-scoped analysis used to fail no matter what the page selected.
  const path = index.trim() ? `/${encodeURIComponent(index.trim())}/_analyze` : "/_analyze";
  const res = await single(bridge, connectionId, "POST", path, body);
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

// ---------------------------------------------------------------------------
// P3 · Allocation (per-index routing rules + explain for unassigned shards)
// ---------------------------------------------------------------------------

export interface AllocationRow {
  index: string;
  primaryShards: number;
  replicaShards: number;
  status: string;
  include: string;
  exclude: string;
  require: string;
  totalShardsPerNode: string;
}

export async function fetchAllocation(bridge: DbxBridge, connectionId: string): Promise<AllocationRow[]> {
  const rows = await batch(bridge, connectionId, [
    { path: "/_cat/indices?format=json&h=index,pri,rep,status" },
    { path: "/_all/_settings?flat_settings=true" },
  ]);
  const list = Array.isArray(rows[0]?.body) ? (rows[0].body as unknown[]) : [];
  const settings = record(rows[1]?.body);
  const pick = (s: Record<string, unknown>, ...keys: string[]): string => {
    for (const key of keys) if (s[key] !== undefined && s[key] !== null) return str(s[key]);
    return "";
  };
  return list.map((entry) => {
    const i = record(entry);
    const index = str(i.index);
    const s = record(record(settings[index]).settings);
    return {
      index,
      primaryShards: num(i.pri),
      replicaShards: num(i.rep),
      status: str(i.status),
      include: pick(s, "index.routing.allocation.include._name", "index.routing.allocation.include._ip"),
      exclude: pick(s, "index.routing.allocation.exclude._name", "index.routing.allocation.exclude._ip"),
      require: pick(s, "index.routing.allocation.require._name", "index.routing.allocation.require._ip"),
      totalShardsPerNode: pick(s, "index.routing.allocation.total_shards_per_node"),
    };
  });
}

export async function setReplicas(
  bridge: DbxBridge,
  connectionId: string,
  index: string,
  replicas: number,
): Promise<EsResult> {
  return single(bridge, connectionId, "PUT", `/${encodeURIComponent(index)}/_settings`, {
    index: { number_of_replicas: replicas },
  });
}

export type AllocationKind = "include" | "exclude" | "require";

/** Empty value removes the rule (`null` in ES settings). */
export async function setAllocationRule(
  bridge: DbxBridge,
  connectionId: string,
  index: string,
  kind: AllocationKind,
  value: string,
): Promise<EsResult> {
  const trimmed = value.trim();
  return single(bridge, connectionId, "PUT", `/${encodeURIComponent(index)}/_settings`, {
    index: { routing: { allocation: { [kind]: { _name: trimmed === "" ? null : trimmed } } } },
  });
}

export interface AllocationDecision {
  node: string;
  decision: string;
  /** Decider name (e.g. same_shard, filter, disk_threshold) — finite enum, translatable. */
  decider: string;
  reason: string;
}

export interface AllocationExplain {
  ok: boolean;
  index: string;
  shard: string;
  primary: boolean;
  state: string;
  canAllocate: string;
  reason: string;
  nodes: AllocationDecision[];
  note: string;
  error?: string;
  raw: unknown;
}

/**
 * `GET /_cluster/allocation/explain`. Without a target ES explains the first
 * unassigned shard it picks; with a target the shard is addressed exactly,
 * which is what the diagnosis page does per row.
 */
export async function explainAllocation(
  bridge: DbxBridge,
  connectionId: string,
  target?: { index: string; shard: number; primary: boolean },
): Promise<AllocationExplain> {
  const empty: AllocationExplain = {
    ok: false, index: "", shard: "", primary: false, state: "", canAllocate: "",
    reason: "", nodes: [], note: "", raw: null,
  };
  let res;
  if (target) {
    res = await single(bridge, connectionId, "POST", "/_cluster/allocation/explain", {
      index: target.index,
      shard: target.shard,
      primary: target.primary,
    });
  } else {
    res = await single(bridge, connectionId, "GET", "/_cluster/allocation/explain");
  }
  if (isError(res)) {
    return { ...empty, error: failureText(res), raw: res.error };
  }
  const body = record(res.body);
  // Response shape (8.x verified): index / shard / primary live at the top
  // level; current_state is a STRING ("unassigned"), not an object.
  const state = str(body.current_state);
  const decisions = Array.isArray(body.node_allocation_decisions)
    ? (body.node_allocation_decisions as unknown[]).map((d) => {
        const node = record(d);
        const deciders = Array.isArray(node.deciders) ? (node.deciders as unknown[]) : [];
        const first = record(deciders[0]);
        return {
          node: str(node.node_name),
          decision: str(node.node_decision),
          decider: str(first.decider),
          reason: str(first.explanation) || str(node.transport_address),
        };
      })
    : [];
  return {
    ok: true,
    index: str(body.index),
    shard: str(body.shard),
    primary: body.primary === true,
    state,
    canAllocate: str(body.can_allocate),
    reason: str(body.allocate_explanation),
    nodes: decisions,
    note: str(body.note),
    raw: body,
  };
}

// ---------------------------------------------------------------------------
// P3 · Cluster settings (persistent / transient)
// ---------------------------------------------------------------------------

export interface ClusterSettingsSnapshot {
  persistent: Record<string, string>;
  transient: Record<string, string>;
  defaults: Record<string, string>;
}

function flatSettings(value: unknown): Record<string, string> {
  const out: Record<string, string> = {};
  const walk = (node: unknown, prefix: string): void => {
    const obj = record(node);
    for (const [key, raw] of Object.entries(obj)) {
      const path = prefix ? `${prefix}.${key}` : key;
      if (raw !== null && typeof raw === "object" && !Array.isArray(raw)) walk(raw, path);
      else out[path] = Array.isArray(raw) ? raw.join(",") : str(raw);
    }
  };
  walk(value, "");
  return out;
}

export async function fetchClusterSettings(
  bridge: DbxBridge,
  connectionId: string,
  includeDefaults = false,
): Promise<ClusterSettingsSnapshot> {
  const path = `/_cluster/settings?flat_settings=true${includeDefaults ? "&include_defaults=true" : ""}`;
  const res = await single(bridge, connectionId, "GET", path);
  if (isError(res)) return { persistent: {}, transient: {}, defaults: {} };
  const body = record(res.body);
  return {
    persistent: flatSettings(body.persistent),
    transient: flatSettings(body.transient),
    defaults: flatSettings(body.defaults),
  };
}

function coerceSettingValue(raw: string): unknown {
  const t = raw.trim();
  if (t === "" || t === "null") return null;
  if (t === "true") return true;
  if (t === "false") return false;
  if (/^-?\d+(\.\d+)?$/.test(t)) return Number(t);
  if ((t.startsWith("{") && t.endsWith("}")) || (t.startsWith("[") && t.endsWith("]"))) {
    try {
      return JSON.parse(t);
    } catch {
      return t;
    }
  }
  return t;
}

/** Passing an empty value removes the key (ES semantics: `null`). */
export async function updateClusterSettings(
  bridge: DbxBridge,
  connectionId: string,
  scope: "persistent" | "transient",
  key: string,
  value: string,
): Promise<EsResult> {
  return single(bridge, connectionId, "PUT", "/_cluster/settings", {
    [scope]: { [key.trim()]: coerceSettingValue(value) },
  });
}

// ---------------------------------------------------------------------------
// P3 · Index templates / component templates
// ---------------------------------------------------------------------------

export type TemplateKind = "composable" | "legacy";

export interface TemplateRow {
  name: string;
  kind: TemplateKind;
  patterns: string[];
  priority: number;
  composedOf: string[];
  version: number;
}

export interface ComponentTemplateRow {
  name: string;
  version: number;
  hasSettings: boolean;
  hasMappings: boolean;
  hasAliases: boolean;
}

export interface TemplateListing {
  rows: TemplateRow[];
  /** `/_index_template` answered — composable templates exist from 7.8. */
  composableSupported: boolean;
  /** `/_template` answered — legacy templates exist on every version tested (6.8–9.5). */
  legacySupported: boolean;
}

export async function fetchTemplates(bridge: DbxBridge, connectionId: string): Promise<TemplateListing> {
  const results = await batch(bridge, connectionId, [{ path: "/_index_template" }, { path: "/_template" }]);
  const out: TemplateRow[] = [];

  // The two endpoints are gated differently: `/_index_template` is 400 on
  // 6.x / 7.0–7.7 ("invalid index name"), while `/_template` still answers on
  // 6.8 through 9.5. Whichever refuses has to be reported to the page — folding
  // it into "no rows" makes a missing API look like an empty cluster.
  const composableRes = results[0];
  const legacyRes = results[1];
  const composableSupported = !composableRes || !isError(composableRes);
  const legacySupported = !legacyRes || !isError(legacyRes);
  if (!composableSupported && !legacySupported) {
    // Both refused: that is not "two gates", it is an unusable cluster (or a
    // transport failure). Surface the reason instead of two misleading notes.
    throw new Error(failureText(composableRes ?? legacyRes ?? { status: 0 }));
  }

  const composable = composableSupported ? record(composableRes?.body) : {};
  for (const entry of Array.isArray(composable.index_templates) ? (composable.index_templates as unknown[]) : []) {
    const item = record(entry);
    const def = record(item.index_template);
    out.push({
      name: str(item.name),
      kind: "composable",
      patterns: Array.isArray(def.index_patterns) ? (def.index_patterns as unknown[]).map(str) : [],
      priority: num(def.priority),
      composedOf: Array.isArray(def.composed_of) ? (def.composed_of as unknown[]).map(str) : [],
      version: num(def.version),
    });
  }

  // `GET /_template` answers with a name -> definition map, and an ERROR body is
  // also an object: iterating it blindly renders phantom rows literally named
  // "error" and "status".
  const legacy = legacySupported ? record(legacyRes?.body) : {};
  for (const [name, raw] of Object.entries(legacy)) {
    if (name === "error" || name === "status") continue;
    const def = record(raw);
    out.push({
      name,
      kind: "legacy",
      patterns: Array.isArray(def.index_patterns) ? (def.index_patterns as unknown[]).map(str) : [],
      priority: num(def.order),
      composedOf: [],
      version: num(def.version),
    });
  }
  return {
    rows: out.sort((a, b) => a.name.localeCompare(b.name)),
    composableSupported,
    legacySupported,
  };
}


export interface LegacyNormalization {
  body: Record<string, unknown>;
  /** Ids of the rewrites that were applied, so the UI can say what it changed. */
  changes: string[];
}

/** Keys that mean "this mapping object is the mapping itself", not a type name. */
const MAPPING_BODY_KEYS = new Set([
  "properties", "dynamic", "dynamic_date_formats", "dynamic_templates", "date_detection",
  "numeric_detection", "runtime", "_source", "_meta", "_routing", "enabled", "_field_names",
]);

/**
 * Rewrite a user-authored legacy `_template` body into the shape this cluster
 * accepts. Both facts below were verified against real 6.8.23 / 7.7.1 / 8.19.23 /
 * 9.5.4 clusters:
 *
 * 1. Composable and legacy bodies are NOT interchangeable. Legacy templates take
 *    `order` (not `priority`), no `composed_of`, and put `settings` /
 *    `mappings` / `aliases` at the top level (not under `template`). Sending a
 *    copied composable body answers 400 "unknown key [priority] in the template".
 * 2. The mapping type layer switches at 7.0, not at 8.0. Measured on real
 *    clusters: 6.8.23 needs `mappings._doc.properties` (a bare `properties`
 *    answers 400 "Root mapping definition has unsupported parameters"), while
 *    7.7.1 — like 8.x/9.x — answers 400 "The mapping definition cannot be nested
 *    under a type [_doc]" for the typed form. Callers must pass the 7.0 flag,
 *    not the 8.0 typeless-API flag.
 */
export function normalizeLegacyTemplate(
  raw: Record<string, unknown>,
  typedMappings: boolean,
): LegacyNormalization {
  const body: Record<string, unknown> = JSON.parse(JSON.stringify(raw));
  const changes: string[] = [];

  const inner = record(body.template);
  if (Object.keys(inner).length > 0) {
    for (const [key, value] of Object.entries(inner)) {
      if (body[key] === undefined) body[key] = value;
    }
    delete body.template;
    changes.push("tpl.norm.template");
  }
  if (body.priority !== undefined && body.order === undefined) {
    body.order = body.priority;
    delete body.priority;
    changes.push("tpl.norm.priority");
  }
  if (body.composed_of !== undefined) {
    delete body.composed_of;
    changes.push("tpl.norm.composedOf");
  }

  const mappings = record(body.mappings);
  if (Object.keys(mappings).length > 0) {
    const looksTypeless = Object.keys(mappings).some((key) => MAPPING_BODY_KEYS.has(key));
    if (typedMappings && looksTypeless) {
      body.mappings = { _doc: mappings };
      changes.push("tpl.norm.typed");
    } else if (!typedMappings && !looksTypeless && Object.keys(mappings).length === 1) {
      // Single wrapper key and no mapping-body keys => it is a type layer.
      const [typeName] = Object.keys(mappings);
      body.mappings = record(mappings[typeName]);
      changes.push("tpl.norm.untyped");
    }
  }
  return { body, changes };
}

export async function getTemplate(
  bridge: DbxBridge,
  connectionId: string,
  name: string,
  kind: TemplateKind,
): Promise<{ ok: boolean; body: unknown; error?: string }> {
  const res = kind === "composable"
    ? await single(bridge, connectionId, "GET", `/_index_template/${encodeURIComponent(name)}`)
    : await single(bridge, connectionId, "GET", `/_template/${encodeURIComponent(name)}`);
  if (isError(res)) return { ok: false, body: null, error: failureText(res) };
  if (kind === "legacy") return { ok: true, body: record(res.body)[name] ?? res.body };
  const body = record(res.body);
  const first = Array.isArray(body.index_templates) ? record((body.index_templates as unknown[])[0]) : {};
  return { ok: true, body: first.index_template ?? res.body };
}

export async function putTemplate(
  bridge: DbxBridge,
  connectionId: string,
  name: string,
  body: unknown,
  kind: TemplateKind = "composable",
): Promise<EsResult> {
  const path = kind === "composable"
    ? `/_index_template/${encodeURIComponent(name)}`
    : `/_template/${encodeURIComponent(name)}`;
  return single(bridge, connectionId, "PUT", path, body);
}

export async function deleteTemplate(
  bridge: DbxBridge,
  connectionId: string,
  name: string,
  kind: TemplateKind,
): Promise<EsResult> {
  const path = kind === "composable"
    ? `/_index_template/${encodeURIComponent(name)}`
    : `/_template/${encodeURIComponent(name)}`;
  return single(bridge, connectionId, "DELETE", path);
}

export async function fetchComponentTemplates(
  bridge: DbxBridge,
  connectionId: string,
): Promise<ComponentTemplateRow[]> {
  const res = await single(bridge, connectionId, "GET", "/_component_template");
  if (isError(res)) {
    throwIfUnsupported(res, "/_component_template");
    return [];
  }
  const body = record(res.body);
  const list = Array.isArray(body.component_templates) ? (body.component_templates as unknown[]) : [];
  return list
    .map((entry) => {
      const item = record(entry);
      const def = record(item.component_template);
      const template = record(def.template);
      return {
        name: str(item.name),
        version: num(def.version),
        hasSettings: Object.keys(record(template.settings)).length > 0,
        hasMappings: Object.keys(record(template.mappings)).length > 0,
        hasAliases: Object.keys(record(template.aliases)).length > 0,
      };
    })
    .sort((a, b) => a.name.localeCompare(b.name));
}

export async function getComponentTemplate(
  bridge: DbxBridge,
  connectionId: string,
  name: string,
): Promise<{ ok: boolean; body: unknown; error?: string }> {
  const res = await single(bridge, connectionId, "GET", `/_component_template/${encodeURIComponent(name)}`);
  if (isError(res)) return { ok: false, body: null, error: failureText(res) };
  const body = record(res.body);
  const first = Array.isArray(body.component_templates) ? record((body.component_templates as unknown[])[0]) : {};
  return { ok: true, body: first.component_template ?? res.body };
}

export async function putComponentTemplate(
  bridge: DbxBridge,
  connectionId: string,
  name: string,
  body: unknown,
): Promise<EsResult> {
  return single(bridge, connectionId, "PUT", `/_component_template/${encodeURIComponent(name)}`, body);
}

export async function deleteComponentTemplate(
  bridge: DbxBridge,
  connectionId: string,
  name: string,
): Promise<EsResult> {
  return single(bridge, connectionId, "DELETE", `/_component_template/${encodeURIComponent(name)}`);
}

// ---------------------------------------------------------------------------
// P3 · Data streams
// ---------------------------------------------------------------------------

export interface DataStreamRow {
  name: string;
  status: string;
  template: string;
  ilmPolicy: string;
  generation: number;
  indices: string[];
}

export async function fetchDataStreams(bridge: DbxBridge, connectionId: string): Promise<DataStreamRow[]> {
  const res = await single(bridge, connectionId, "GET", "/_data_stream");
  if (isError(res)) {
    throwIfUnsupported(res, "/_data_stream");
    return [];
  }
  const body = record(res.body);
  const list = Array.isArray(body.data_streams) ? (body.data_streams as unknown[]) : [];
  return list
    .map((entry) => {
      const item = record(entry);
      return {
        name: str(item.name),
        status: str(item.status),
        template: str(item.template),
        ilmPolicy: str(item.ilm_policy),
        generation: num(item.generation),
        indices: Array.isArray(item.indices)
          ? (item.indices as unknown[]).map((i) => str(record(i).index_name))
          : [],
      };
    })
    .sort((a, b) => a.name.localeCompare(b.name));
}

export async function rolloverDataStream(
  bridge: DbxBridge,
  connectionId: string,
  name: string,
): Promise<EsResult> {
  return single(bridge, connectionId, "POST", `/${encodeURIComponent(name)}/_rollover`);
}

export async function createDataStream(
  bridge: DbxBridge,
  connectionId: string,
  name: string,
): Promise<EsResult> {
  return single(bridge, connectionId, "PUT", `/_data_stream/${encodeURIComponent(name)}`);
}

export async function deleteDataStream(
  bridge: DbxBridge,
  connectionId: string,
  name: string,
): Promise<EsResult> {
  return single(bridge, connectionId, "DELETE", `/_data_stream/${encodeURIComponent(name)}`);
}

// ---------------------------------------------------------------------------
// P3 · ILM (Index Lifecycle Management)
// ---------------------------------------------------------------------------

export interface IlmStatus {
  mode: string;
  error?: string;
}

export async function fetchIlmStatus(bridge: DbxBridge, connectionId: string): Promise<IlmStatus> {
  const res = await single(bridge, connectionId, "GET", "/_ilm/status");
  if (isError(res)) return { mode: "unknown", error: failureText(res) };
  return { mode: str(record(res.body).operation_mode) || "unknown" };
}

export interface IlmPolicyRow {
  name: string;
  version: number;
  modified: string;
  phases: string[];
  minAges: string[];
}

export async function fetchIlmPolicies(bridge: DbxBridge, connectionId: string): Promise<IlmPolicyRow[]> {
  const res = await single(bridge, connectionId, "GET", "/_ilm/policy");
  if (isError(res)) {
    throwIfUnsupported(res, "/_ilm/policy");
    return [];
  }
  const body = record(res.body);
  const out: IlmPolicyRow[] = [];
  for (const [name, raw] of Object.entries(body)) {
    const policy = record(record(raw).policy);
    const phases = record(policy.phases);
    const entries = Object.entries(phases);
    out.push({
      name,
      version: num(record(raw).version),
      modified: str(record(raw).modified_date),
      phases: entries.map(([phase]) => phase),
      minAges: entries.map(([, v]) => str(record(v).min_age) || "-"),
    });
  }
  return out.sort((a, b) => a.name.localeCompare(b.name));
}

export interface IlmExplainRow {
  index: string;
  managed: boolean;
  policy: string;
  phase: string;
  action: string;
  step: string;
  error: string;
}

export async function fetchIlmExplain(bridge: DbxBridge, connectionId: string): Promise<IlmExplainRow[]> {
  // ES 8.x has no bare `/_ilm/explain` handler (405/404); the target form is
  // required and `_all` covers every index in one read-only GET.
  const res = await single(bridge, connectionId, "GET", "/_all/_ilm/explain");
  if (isError(res)) {
    throwIfUnsupported(res, "/_all/_ilm/explain");
    return [];
  }
  const body = record(res.body);
  const indices = record(body.indices);
  return Object.entries(indices)
    .map(([index, raw]) => {
      const item = record(raw);
      const stepInfo = record(item.step_info);
      return {
        index,
        managed: item.managed === true,
        policy: str(item.policy),
        phase: str(item.phase),
        action: str(item.action),
        step: str(item.step),
        error: str(stepInfo.reason) || str(item.error),
      };
    })
    .sort((a, b) => a.index.localeCompare(b.index));
}

export async function setIlmStatus(
  bridge: DbxBridge,
  connectionId: string,
  mode: "start" | "stop",
): Promise<EsResult> {
  return single(bridge, connectionId, "POST", `/_ilm/${mode}`);
}

export async function deleteIlmPolicy(
  bridge: DbxBridge,
  connectionId: string,
  name: string,
): Promise<EsResult> {
  return single(bridge, connectionId, "DELETE", `/_ilm/policy/${encodeURIComponent(name)}`);
}

export async function putIlmPolicy(
  bridge: DbxBridge,
  connectionId: string,
  name: string,
  body: unknown,
): Promise<EsResult> {
  return single(bridge, connectionId, "PUT", `/_ilm/policy/${encodeURIComponent(name)}`, body);
}

// ---------------------------------------------------------------------------
// P3 · Query profiler
// ---------------------------------------------------------------------------

export async function runProfile(
  bridge: DbxBridge,
  connectionId: string,
  index: string,
  body: Record<string, unknown>,
): Promise<{ ok: boolean; took: number; hits: number; shards: ProfileShard[]; error?: string; raw: unknown }> {
  const res = await single(
    bridge,
    connectionId,
    "POST",
    `/${encodeURIComponent(index)}/_search`,
    { ...body, profile: true },
  );
  if (isError(res)) {
    return { ok: false, took: 0, hits: 0, shards: [], error: failureText(res), raw: res.error };
  }
  const payload = record(res.body);
  const profile = record(payload.profile);
  const rawShards = Array.isArray(profile.shards) ? (profile.shards as unknown[]) : [];
  const shards: ProfileShard[] = mapProfileShards(rawShards);
  const hitsTotal = record(record(payload.hits).total).value ?? record(payload.hits).total ?? 0;
  return { ok: true, took: num(payload.took), hits: num(hitsTotal), shards, raw: payload };
}

// ---------------------------------------------------------------------------
// P3 · Snapshots
// ---------------------------------------------------------------------------

export interface RepositoryRow {
  name: string;
  type: string;
  settings: Record<string, unknown>;
}

export async function fetchRepositories(bridge: DbxBridge, connectionId: string): Promise<RepositoryRow[]> {
  const res = await single(bridge, connectionId, "GET", "/_snapshot");
  if (isError(res)) {
    throwIfUnsupported(res, "/_snapshot");
    return [];
  }
  const body = record(res.body);
  return Object.entries(body)
    .filter(([name]) => name !== "error")
    .map(([name, raw]) => {
      const item = record(raw);
      return { name, type: str(item.type), settings: record(item.settings) };
    })
    .sort((a, b) => a.name.localeCompare(b.name));
}

export interface SnapshotRow {
  repository: string;
  snapshot: string;
  state: string;
  indices: string[];
  startTime: string;
  duration: string;
  total: number;
  successful: number;
  failed: number;
}

export async function fetchSnapshots(
  bridge: DbxBridge,
  connectionId: string,
  repository: string,
): Promise<SnapshotRow[]> {
  const res = await single(bridge, connectionId, "GET", `/_snapshot/${encodeURIComponent(repository)}/_all`);
  if (isError(res)) {
    throwIfUnsupported(res, "/_snapshot/{repository}/_all");
    return [];
  }
  const body = record(res.body);
  return (Array.isArray(body.snapshots) ? (body.snapshots as unknown[]) : [])
    .map((entry) => {
      const item = record(entry);
      const shards = record(item.shards);
      return {
        repository,
        snapshot: str(item.snapshot),
        state: str(item.state),
        indices: Array.isArray(item.indices) ? (item.indices as unknown[]).map(str) : [],
        startTime: str(item.start_time_in_millis),
        duration: str(item.duration_in_millis),
        total: num(shards.total),
        successful: num(shards.successful),
        failed: num(shards.failed),
      };
    })
    .sort((a, b) => a.snapshot.localeCompare(b.snapshot));
}

export async function createRepository(
  bridge: DbxBridge,
  connectionId: string,
  name: string,
  type: string,
  settings: Record<string, unknown>,
): Promise<EsResult> {
  return single(bridge, connectionId, "PUT", `/_snapshot/${encodeURIComponent(name)}`, { type, settings });
}

export async function deleteRepository(
  bridge: DbxBridge,
  connectionId: string,
  name: string,
): Promise<EsResult> {
  return single(bridge, connectionId, "DELETE", `/_snapshot/${encodeURIComponent(name)}`);
}

export async function createSnapshot(
  bridge: DbxBridge,
  connectionId: string,
  repository: string,
  snapshot: string,
  indices: string,
  includeGlobalState: boolean,
): Promise<EsResult> {
  const list = indices.split(",").map((s) => s.trim()).filter(Boolean);
  return single(
    bridge,
    connectionId,
    "PUT",
    `/_snapshot/${encodeURIComponent(repository)}/${encodeURIComponent(snapshot)}?wait_for_completion=true`,
    { indices: list.length ? list.join(",") : "*", include_global_state: includeGlobalState },
  );
}

export async function deleteSnapshot(
  bridge: DbxBridge,
  connectionId: string,
  repository: string,
  snapshot: string,
): Promise<EsResult> {
  return single(
    bridge,
    connectionId,
    "DELETE",
    `/_snapshot/${encodeURIComponent(repository)}/${encodeURIComponent(snapshot)}`,
  );
}

export async function restoreSnapshot(
  bridge: DbxBridge,
  connectionId: string,
  repository: string,
  snapshot: string,
  indices: string,
  renamePattern: string,
  renameReplacement: string,
): Promise<EsResult> {
  const list = indices.split(",").map((s) => s.trim()).filter(Boolean);
  const body: Record<string, unknown> = { include_global_state: false, ignore_unavailable: true };
  if (list.length) body.indices = list.join(",");
  if (renamePattern.trim()) {
    body.rename_pattern = renamePattern.trim();
    body.rename_replacement = renameReplacement.trim() || "restored_$1";
  }
  return single(
    bridge,
    connectionId,
    "POST",
    `/_snapshot/${encodeURIComponent(repository)}/${encodeURIComponent(snapshot)}/_restore?wait_for_completion=false`,
    body,
  );
}

// ---------------------------------------------------------------------------
// P3 · Tasks
// ---------------------------------------------------------------------------

export interface TaskRow {
  taskId: string;
  action: string;
  type: string;
  node: string;
  startTime: number;
  runningNanos: number;
  cancellable: boolean;
  description: string;
}

export async function fetchTasks(bridge: DbxBridge, connectionId: string): Promise<TaskRow[]> {
  const res = await single(bridge, connectionId, "GET", "/_tasks?group_by=none&detailed=true");
  if (isError(res)) {
    throwIfUnsupported(res, "/_tasks");
    return [];
  }
  const body = record(res.body);
  const out: TaskRow[] = [];

  const push = (taskId: string, task: Record<string, unknown>, nodeFallback: string): void => {
    const action = str(task.action);
    // `cluster:monitor/tasks/lists` is created by this very request, so it is
    // always present and always noise.
    if (action.startsWith("cluster:monitor/tasks/lists")) return;
    out.push({
      taskId,
      action,
      type: str(task.type),
      node: str(task.node) || nodeFallback,
      startTime: num(task.start_time_in_millis) || num(task.start_time),
      runningNanos: num(task.running_time_in_nanos),
      cancellable: task.cancellable === true,
      description: str(task.description),
    });
  };

  // ES 8.x answers `?group_by=none` with a flat `tasks` array; the default
  // (`group_by=nodes`) nests them under `nodes.<nodeId>.tasks`. Accept both.
  if (Array.isArray(body.tasks)) {
    for (const raw of body.tasks as unknown[]) {
      const task = record(raw);
      const node = str(task.node);
      const id = task.id === undefined ? "" : String(task.id);
      push(id ? `${node}:${id}` : str(task.task_id), task, node);
    }
    return out.sort((a, b) => b.runningNanos - a.runningNanos);
  }

  for (const [nodeId, raw] of Object.entries(record(body.nodes))) {
    const node = record(raw);
    for (const [taskId, taskRaw] of Object.entries(record(node.tasks))) {
      push(taskId, record(taskRaw), nodeId);
    }
  }
  return out.sort((a, b) => b.runningNanos - a.runningNanos);
}

export async function cancelTask(
  bridge: DbxBridge,
  connectionId: string,
  taskId: string,
): Promise<EsResult> {
  return single(bridge, connectionId, "POST", `/_tasks/${encodeURIComponent(taskId)}/_cancel`);
}

export type RiskKind = "delete-index" | "delete-doc" | "close-index" | "open-index" | "aliases" | "delete-by-query" | "forcemerge" | "refresh" | "flush" | "create-index" | "bulk" | "reindex" | "settings" | "template" | "data-stream" | "ilm" | "snapshot" | "task" | "allocation" | "other";

export interface OperationRisk {
  dangerous: boolean;
  kind: RiskKind;
  label: string;
  scope: string;
}

const DESTRUCTIVE_ENDS: Record<string, { kind: RiskKind; label: string }> = {
  "_close": { kind: "close-index", label: "risk.closeIndex.label" },
  "_open": { kind: "open-index", label: "risk.openIndex.label" },
  "_delete_by_query": { kind: "delete-by-query", label: "risk.deleteByQueryEnds.label" },
  "_forcemerge": { kind: "forcemerge", label: "risk.forcemerge.label" },
  "_flush": { kind: "flush", label: "risk.flush.label" },
};

export function classifyOperation(method: string, path: string): OperationRisk {
  const m = method.toUpperCase();
  const p = path.trim();
  const segs = p.split("/").filter(Boolean);

  if (m === "POST" || m === "PUT" || m === "DELETE") {
    const has = (token: string): boolean => segs.includes(token);
    const scopeOf = (fallback: string): string => segs.filter((s) => !s.startsWith("_")).join("/") || fallback;

    if (has("_cluster") && has("settings")) {
      return { dangerous: true, kind: "settings", label: t("risk.settings.label"), scope: t("risk.settings.scope") };
    }
    if (has("_snapshot")) {
      if (has("_restore")) return { dangerous: true, kind: "snapshot", label: t("risk.restore.label"), scope: t("risk.restore.scope", { name: scopeOf("") }) };
      if (m === "DELETE") return { dangerous: true, kind: "snapshot", label: t("risk.snapshotDelete.label"), scope: t("risk.scope.name", { name: scopeOf(t("risk.scope.unspecified")) }) };
      return { dangerous: true, kind: "snapshot", label: t("risk.snapshotCreate.label"), scope: t("risk.scope.name", { name: scopeOf(t("risk.scope.unspecified")) }) };
    }
    if (has("_data_stream")) {
      return m === "DELETE"
        ? { dangerous: true, kind: "data-stream", label: t("risk.dsDelete.label"), scope: t("risk.dsDelete.scope", { name: scopeOf("") }) }
        : { dangerous: true, kind: "data-stream", label: t("risk.dsRollover.label"), scope: t("risk.dsRollover.scope", { name: scopeOf("") }) };
    }
    if (has("_ilm")) {
      if (has("_start") || has("_stop")) {
        return { dangerous: false, kind: "ilm", label: t("risk.ilmToggle.label"), scope: t("risk.ilmToggle.scope") };
      }
      return m === "DELETE"
        ? { dangerous: true, kind: "ilm", label: t("risk.ilmDelete.label"), scope: t("risk.ilm.scope", { name: scopeOf(t("risk.scope.unspecified")) }) }
        : { dangerous: true, kind: "ilm", label: t("risk.ilmWrite.label"), scope: t("risk.ilm.scope", { name: scopeOf(t("risk.scope.unspecified")) }) };
    }
    if (has("_index_template")) {
      return m === "DELETE"
        ? { dangerous: true, kind: "template", label: t("risk.tplDelete.label"), scope: t("risk.tpl.scope", { name: scopeOf(t("risk.scope.unspecified")) }) }
        : { dangerous: true, kind: "template", label: t("risk.tplWrite.label"), scope: t("risk.tpl.scope", { name: scopeOf(t("risk.scope.unspecified")) }) };
    }
    if (has("_component_template")) {
      return m === "DELETE"
        ? { dangerous: true, kind: "template", label: t("risk.ctDelete.label"), scope: t("risk.ct.scope", { name: scopeOf(t("risk.scope.unspecified")) }) }
        : { dangerous: true, kind: "template", label: t("risk.ctWrite.label"), scope: t("risk.ct.scope", { name: scopeOf(t("risk.scope.unspecified")) }) };
    }
    if (has("_tasks") && has("_cancel")) {
      return { dangerous: true, kind: "task", label: t("risk.taskCancel.label"), scope: t("risk.task.scope", { name: segs[1] ?? "" }) };
    }
    if (has("_settings")) {
      return { dangerous: true, kind: "settings", label: t("risk.indexSettings.label"), scope: t("risk.index.scope", { name: segs[0] ?? "*" }) };
    }
  }

  if (m === "DELETE") {
    // DELETE /{index}/_doc/{id} or /{index}/_doc/{id}/_create etc.
    if (segs.length >= 2 && segs[1] === "_doc") {
      return { dangerous: true, kind: "delete-doc", label: t("risk.deleteDoc.label"), scope: t("risk.deleteDoc.scope", { name: segs[0], doc: segs[2] ?? "" }) };
    }
    return { dangerous: true, kind: "delete-index", label: t("risk.deleteIndex.label"), scope: t("risk.index.scope", { name: segs[0] ?? "*" }) };
  }

  if (m === "POST" || m === "PUT") {
    if (segs.length >= 1 && segs[segs.length - 1].startsWith("_")) {
      const tail = segs[segs.length - 1];
      if (tail === "_aliases") {
        return { dangerous: true, kind: "aliases", label: t("risk.aliases.label"), scope: t("risk.aliases.scope") };
      }
      if (tail === "_delete_by_query") {
        return { dangerous: true, kind: "delete-by-query", label: t("risk.deleteByQuery.label"), scope: t("risk.index.scope", { name: segs[0] ?? "*" }) };
      }
      const mapped = DESTRUCTIVE_ENDS[tail];
      if (mapped) {
        return { dangerous: mapped.kind !== "refresh" && mapped.kind !== "flush", kind: mapped.kind, label: t(mapped.label), scope: t("risk.index.scope", { name: segs[0] ?? "*" }) };
      }
    }
    if (m === "PUT" && segs.length === 1 && !segs[0].startsWith("_")) {
      return { dangerous: true, kind: "create-index", label: t("risk.createIndex.label"), scope: t("risk.index.scope", { name: segs[0] }) };
    }
    if (m === "POST" && segs.length === 1 && !segs[0].startsWith("_")) {
      return { dangerous: true, kind: "bulk", label: t("risk.bulk.label"), scope: t("risk.bulk.scope", { name: segs[0] }) };
    }
    if (segs.length === 1 && segs[0] === "_reindex") {
      return { dangerous: true, kind: "reindex", label: t("risk.reindex.label"), scope: t("risk.reindex.scope") };
    }
  }

  if (m === "POST" && p === "/_analyze") {
    return { dangerous: false, kind: "other", label: t("risk.analyze.label"), scope: t("risk.scope.none") };
  }
  if (m === "POST" && (p.endsWith("/_refresh") || p === "/_refresh")) {
    return { dangerous: false, kind: "refresh", label: t("risk.refresh.label"), scope: t("risk.refresh.scope") };
  }
  if (m === "GET" || m === "HEAD" || m === "POST" && (p.endsWith("/_search") || p === "/_search") || p.endsWith("/_doc/") ) {
    return { dangerous: false, kind: "other", label: t("risk.read.label"), scope: t("risk.scope.none") };
  }
  return { dangerous: false, kind: "other", label: m, scope: t("risk.scope.none") };
}

// ---------------------------------------------------------------------------
// P4 · Monitoring
//
// Unlike every other domain here, the series is NOT fetched through es/request:
// the Sidecar collects it on its own schedule and persists it to the plugin data
// dir, so the chart keeps filling while the user is on another page. These
// wrappers therefore call dedicated mon/* RPCs (see backend/internal/transport/
// monitor.go) and only map snake_case → camelCase.
// ---------------------------------------------------------------------------

export interface MonitorClusterPoint {
  status: string;
  nodes: number;
  primary: number;
  active: number;
  unassigned: number;
  relocating: number;
  initializing: number;
  indices: number;
  docs: number;
  storeBytes: number;
  heapPercent: number;
  cpuPercent: number;
  diskFreeBytes: number;
  diskTotalBytes: number;
}

export interface MonitorNodePoint {
  name: string;
  heapPercent: number;
  cpuPercent: number;
  load1: number;
  diskPercent: number;
}

export interface MonitorSample {
  at: number;
  cluster: MonitorClusterPoint;
  nodes: Record<string, MonitorNodePoint>;
}

export interface MonitorStatus {
  connectionId: string;
  running: boolean;
  intervalSec: number;
  collected: number;
  lastAt: number;
  lastError: string;
  stored: number;
  bytes: number;
  file: string;
}

export interface MonitorHistory {
  samples: MonitorSample[];
  count: number;
  stored: number;
  firstAt: number;
  lastAt: number;
  bytes: number;
  file: string;
}

function parseClusterPoint(value: unknown): MonitorClusterPoint {
  const row = record(value);
  return {
    status: str(row.status),
    nodes: num(row.nodes),
    primary: num(row.primary),
    active: num(row.active),
    unassigned: num(row.unassigned),
    relocating: num(row.relocating),
    initializing: num(row.initializing),
    indices: num(row.indices),
    docs: num(row.docs),
    storeBytes: num(row.store_bytes),
    heapPercent: num(row.heap_percent),
    cpuPercent: num(row.cpu_percent),
    diskFreeBytes: num(row.disk_free_bytes),
    diskTotalBytes: num(row.disk_total_bytes),
  };
}

function parseSample(value: unknown): MonitorSample {
  const row = record(value);
  const nodes: Record<string, MonitorNodePoint> = {};
  for (const [name, raw] of Object.entries(record(row.nodes))) {
    const node = record(raw);
    nodes[name] = {
      name: str(node.name) || name,
      heapPercent: num(node.heap_percent),
      cpuPercent: num(node.cpu_percent),
      load1: num(node.load_1m),
      diskPercent: num(node.disk_percent),
    };
  }
  return { at: num(row.t), cluster: parseClusterPoint(row.cluster), nodes };
}

export function parseMonitorStatus(value: unknown): MonitorStatus {
  const row = record(value);
  return {
    connectionId: str(row.connection_id),
    running: row.running === true,
    intervalSec: num(row.interval_sec),
    collected: num(row.collected),
    lastAt: num(row.last_at),
    lastError: str(row.last_error),
    stored: num(row.stored),
    bytes: num(row.bytes),
    file: str(row.file),
  };
}

export async function startMonitor(bridge: DbxBridge, connectionId: string, intervalSec: number): Promise<MonitorStatus> {
  const res = await invoke<unknown>(bridge, "mon/start", { connection_id: connectionId, interval_sec: intervalSec });
  return parseMonitorStatus(res);
}

export async function stopMonitor(bridge: DbxBridge, connectionId: string): Promise<MonitorStatus> {
  const res = record(await invoke<unknown>(bridge, "mon/stop", { connection_id: connectionId }));
  return parseMonitorStatus(res.status);
}

export async function monitorStatus(bridge: DbxBridge, connectionId: string): Promise<MonitorStatus> {
  const res = record(await invoke<unknown>(bridge, "mon/status", { connection_id: connectionId }));
  return parseMonitorStatus(res.status);
}

export async function sampleNow(bridge: DbxBridge, connectionId: string): Promise<MonitorStatus> {
  const res = record(await invoke<unknown>(bridge, "mon/sample", { connection_id: connectionId }));
  return parseMonitorStatus(res.status);
}

export async function fetchMonitorHistory(
  bridge: DbxBridge,
  connectionId: string,
  options: { sinceMs?: number; limit?: number } = {},
): Promise<MonitorHistory> {
  const res = record(
    await invoke<unknown>(bridge, "mon/history", {
      connection_id: connectionId,
      since: options.sinceMs ?? 0,
      limit: options.limit ?? 0,
    }),
  );
  const rows = Array.isArray(res.samples) ? (res.samples as unknown[]) : [];
  return {
    samples: rows.map(parseSample),
    count: num(res.count),
    stored: num(res.stored),
    firstAt: num(res.first_at),
    lastAt: num(res.last_at),
    bytes: num(res.bytes),
    file: str(res.file),
  };
}
