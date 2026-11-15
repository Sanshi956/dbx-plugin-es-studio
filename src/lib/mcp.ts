// P5 · MCP Tool Service client.
//
// Everything here is a thin wrapper over the Sidecar's mcp/* methods. The page
// never builds an Elasticsearch request itself: it lists tools, calls one tool
// through the exact same path an AI client uses, and reads the audit log.

import { invoke } from "./api";
import type { DbxBridge } from "./bridge";

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
  "-32002": "该连接尚未在 DBX 中打开（先点连接节点，再授权）",
  "-32003": "MCP 只读：写操作被拒绝",
  "-32602": "参数不符合工具声明",
};

/** Turn a Sidecar error into a sentence the page can show. */
export function mcpErrorText(cause: unknown): string {
  const message = cause instanceof Error ? cause.message : String(cause);
  const match = /(-32\d{3})/.exec(message);
  if (match && ERRORS[match[1]]) return `${ERRORS[match[1]]}（${match[1]}）`;
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
  cluster: "集群",
  index: "索引",
  doc: "文档与查询",
  template: "模板",
  stream: "数据流",
  ilm: "生命周期",
  snapshot: "快照",
  task: "任务",
  advanced: "高级",
};
