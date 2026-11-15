// P5 · MCP Tool Service client.
//
// Everything here is a thin wrapper over the Sidecar's mcp/* methods. The page
// never builds an Elasticsearch request itself: it lists tools, calls one tool
// through the exact same path an AI client uses, and reads the audit log.

import { invoke } from "./api";
import type { DbxBridge } from "./bridge";
import { t } from "./i18n.svelte.js";

export interface McpArg {
  name: string;
  type: string;
  description: string;
  required?: boolean;
  enum?: string[];
  default?: unknown;
}

export interface McpToolSummary {
  name: string;
  title: string;
  category: string;
  description: string;
  read_only: boolean;
  args: McpArg[];
}

export interface McpCatalogue {
  count: number;
  catalogue: McpToolSummary[];
  categories: Record<string, number>;
  read_only: boolean;
}

export interface McpCallResult {
  tool: string;
  status: number;
  text: string;
  bytes: number;
  truncated: boolean;
  duration_ms: number;
  request: { method: string; path: string; query?: Record<string, string> };
  read_only: boolean;
}

export interface McpCallRow {
  at: number;
  tool: string;
  connection?: string;
  ok: boolean;
  status?: number;
  ms?: number;
  bytes?: number;
  truncated?: boolean;
  error?: string;
  args?: string[];
}

export interface McpLog {
  calls: McpCallRow[];
  count: number;
  file?: string;
  samples?: number;
  bytes?: number;
  storage?: string;
}

export interface McpGrant {
  connection_id: string;
  label: string;
  host: string;
  port: number;
  fingerprint: string;
  created_at: number;
  expires_at: number;
  expired: boolean;
  read_only: boolean;
  transport: string;
}

export interface McpGrants {
  grants: McpGrant[];
  count: number;
  file?: string;
  dir?: string;
}

export interface McpGrantResult {
  token: string;
  public: McpGrant;
  expires_at: number;
  command: string;
  args: string[];
  env: Record<string, string>;
  snippet: string;
}

const ERRORS: Record<string, string> = {
  "-32002": "mcp.error.noConnection",
  "-32003": "mcp.error.readOnly",
  "-32602": "mcp.error.badArgs",
};

/** Turn a Sidecar error into a sentence the page can show. */
export function mcpErrorText(cause: unknown): string {
  const message = cause instanceof Error ? cause.message : String(cause);
  const match = /(-32\d{3})/.exec(message);
  if (match && ERRORS[match[1]]) return `${t(ERRORS[match[1]])} (${match[1]})`;
  return message;
}

export function listMcpTools(bridge: DbxBridge): Promise<McpCatalogue> {
  return invoke<McpCatalogue>(bridge, "mcp/tools", {});
}

export function callMcpTool(
  bridge: DbxBridge,
  connectionId: string,
  tool: string,
  args: Record<string, unknown>,
): Promise<McpCallResult> {
  return invoke<McpCallResult>(bridge, "mcp/call", { connection_id: connectionId, tool, arguments: args });
}

export function fetchMcpLog(bridge: DbxBridge, limit = 50): Promise<McpLog> {
  return invoke<McpLog>(bridge, "mcp/log", { limit });
}

export function listGrants(bridge: DbxBridge): Promise<McpGrants> {
  return invoke<McpGrants>(bridge, "mcp/grants", {});
}

export function grantConnection(bridge: DbxBridge, connectionId: string, ttlDays = 30): Promise<McpGrantResult> {
  return invoke<McpGrantResult>(bridge, "mcp/grant", { connection_id: connectionId, ttl_days: ttlDays });
}

export function revokeGrant(bridge: DbxBridge, connectionId: string): Promise<{ revoked: boolean }> {
  return invoke<{ revoked: boolean }>(bridge, "mcp/revoke", { connection_id: connectionId });
}

/** Category labels for the tool catalogue groups. */
export const CATEGORY_LABEL: Record<string, string> = {
  cluster: "mcp.cat.cluster",
  index: "mcp.cat.index",
  doc: "mcp.cat.doc",
  template: "mcp.cat.template",
  stream: "mcp.cat.stream",
  ilm: "mcp.cat.ilm",
  snapshot: "mcp.cat.snapshot",
  task: "mcp.cat.task",
  advanced: "mcp.cat.advanced",
};
