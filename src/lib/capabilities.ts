/**
 * Which version-gated APIs this cluster actually has.
 *
 * The Go side already computes this matrix for `connection/test`, but a page
 * needs it before it renders an empty table, and it must not pay for a second
 * round-trip each time it mounts. `GET /` is the cheapest call Elasticsearch
 * offers, so it is fetched once per connection and cached for the session.
 *
 * Keep the thresholds identical to backend/internal/core/es/capability.go.
 * They are duplicated because the page has to decide *before* calling, while
 * the sidecar decides when it answers; a test asserts both agree on 6.8/7.17/9.
 */
import { invoke } from "./api";
import type { DbxBridge } from "./bridge";
import { record, str } from "./services";

export interface Capabilities {
  version: string;
  major: number;
  minor: number;
  typedApi: boolean;
  ilm: boolean;
  componentTemplates: boolean;
  dataStreams: boolean;
  pit: boolean;
  esql: boolean;
}

const CACHE = new Map<string, Promise<Capabilities | null>>();

function atLeast(major: number, minor: number, m: number, n: number): boolean {
  return major === m ? minor >= n : major > m;
}

/** Pure matrix, mirroring the Go implementation. `null` = version unknown. */
export function capabilitiesFor(raw: string): Capabilities | null {
  const match = /^(\d+)\.(\d+)/.exec(raw.trim());
  if (!match) return null;
  const major = Number(match[1]);
  const minor = Number(match[2]);
  if (!major) return null;
  return {
    version: raw.trim(),
    major,
    minor,
    typedApi: major < 8,
    ilm: atLeast(major, minor, 6, 6),
    componentTemplates: atLeast(major, minor, 7, 8),
    dataStreams: atLeast(major, minor, 7, 9),
    pit: atLeast(major, minor, 7, 10),
    esql: atLeast(major, minor, 8, 11),
  };
}

/** Cached per connection; `null` when the version could not be read. */
export function clusterCapabilities(bridge: DbxBridge, connectionId: string): Promise<Capabilities | null> {
  if (!connectionId) return Promise.resolve(null);
  const cached = CACHE.get(connectionId);
  if (cached) return cached;
  const pending = invoke<{ body?: unknown }>(bridge, "es/request", {
    connection_id: connectionId,
    method: "GET",
    path: "/",
  })
    .then((res) => capabilitiesFor(str(record(record(res.body).version).number)))
    .catch(() => null);
  CACHE.set(connectionId, pending);
  return pending;
}

/** Drop the cache when a connection reconnects to a different cluster. */
export function forgetCapabilities(connectionId?: string): void {
  if (connectionId) CACHE.delete(connectionId);
  else CACHE.clear();
}
