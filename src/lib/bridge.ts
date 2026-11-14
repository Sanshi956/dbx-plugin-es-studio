/** Official DBX Host Bridge surface (verified against plugin-cli 0.1.10
 *  dev-runtime/runtime.mjs installBridge). No invented methods. */
export interface ConnectionSummary {
  id: string;
  name: string;
  label?: string;
  dbType?: string;
  dialect?: string;
  databaseType?: string;
  connected?: boolean;
  isConnected?: boolean;
  readOnly?: boolean;
}

export interface DbxContext {
  connectionId?: string;
  providerId?: string;
  connectionType?: string;
  [key: string]: unknown;
}

export interface DbxBridge {
  ready: Promise<DbxContext>;
  readonly context: DbxContext;
  readonly locale: string;
  readonly contributionId?: string;
  readonly theme?: { appearance?: string; tokens?: Record<string, string> };
  invoke<T = unknown>(method: string, params?: unknown, options?: { timeoutMs?: number }): Promise<T>;
  getConnections(): Promise<ConnectionSummary[]>;
  listConnections(options?: unknown): Promise<ConnectionSummary[]>;
  storage: {
    get<T = unknown>(key: string): Promise<T | null>;
    set(key: string, value: unknown): Promise<void>;
    delete(key: string): Promise<void>;
  };
  onContext(fn: (context: DbxContext) => void): () => void;
  workbench: { setTitle(title: string): Promise<void> };
}

declare global {
  interface Window {
    dbxPlugin?: DbxBridge;
  }
}

export function getBridge(): DbxBridge | null {
  return window.dbxPlugin ?? null;
}

/** Wait for the host init message before invoking anything. */
export async function ready(): Promise<DbxBridge> {
  const bridge = getBridge();
  if (!bridge) throw new Error("NOT_IN_DBX_HOST");
  await bridge.ready;
  return bridge;
}
