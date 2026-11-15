<script lang="ts">
  import StateBlock from "../components/StateBlock.svelte";
  import JsonView from "../components/JsonView.svelte";
  import { restCall, classifyOperation, isError, type EsResult } from "../lib/services";
  import type { DbxBridge } from "../lib/bridge";

  interface Props {
    bridge: DbxBridge;
    connectionId: string;
  }
  let { bridge, connectionId }: Props = $props();

  const METHODS = ["GET", "POST", "PUT", "DELETE", "HEAD", "PATCH"];
  let method = $state("GET");
  let path = $state("/_cluster/health");
  let body = $state("");

  let loading = $state(false);
  let error = $state("");
  let result = $state<EsResult | null>(null);
  let duration = $state(0);
  let risk = $state(classifyOperation("GET", "/_cluster/health"));
  let history = $state<{ method: string; path: string; body: string }[]>([]);

  const HISTORY_KEY = "es.studio.rest";

  $effect(() => {
    risk = classifyOperation(method, path);
  });
  $effect(() => {
    void bridge.storage.get<{ method: string; path: string; body: string }[]>(HISTORY_KEY).then((v) => {
      if (Array.isArray(v)) history = v;
    });
  });

  async function run(): Promise<void> {
    if (!path.trim().startsWith("/")) {
      error = "路径必须以 / 开头（相对路径，不能含 scheme 或主机名）";
      return;
    }
    loading = true;
    error = "";
    const t0 = performance.now();
    try {
      const res = await restCall(bridge, connectionId, method, path.trim(), body);
      duration = Math.round(performance.now() - t0);
      result = res;
      if (isError(res)) error = res.error?.message ?? `HTTP ${res.status}`;
      else pushHistory();
    } catch (cause) {
      error = cause instanceof Error ? cause.message : String(cause);
    } finally {
      loading = false;
    }
  }

  function pushHistory(): void {
    history = [{ method, path: path.trim(), body }, ...history.filter((h) => !(h.method === method && h.path === path.trim()))].slice(0, 12);
    void bridge.storage.set(HISTORY_KEY, history);
  }

  function useHistory(item: { method: string; path: string; body: string }): void {
    method = item.method;
    path = item.path;
    body = item.body;
  }

  function statusClass(s: number): string {
    if (s >= 200 && s < 300) return "ok";
    if (s >= 400) return "bad";
    return "warn";
  }
</script>

<div class="page">
  <header>
    <div>
      <h1>REST Console</h1>
      <p>Send any Elasticsearch REST request through the Sidecar (permission-gated).</p>
    </div>
  </header>

  <section class="bar">
    <select bind:value={method}>
      {#each METHODS as m (m)}<option value={m}>{m}</option>{/each}
    </select>
    <input class="path" placeholder="/_cat/indices?format=json" bind:value={path} />
    <button type="button" class="primary" onclick={run} disabled={loading || !connectionId}>{loading ? "Sending…" : "Send"}</button>
  </section>

  {#if risk.dangerous}
    <div class="warn-banner">
      ⚠ 该操作被标记为 <strong>{risk.label}</strong>。在只读连接上会被 Go Sidecar 拒绝（-32003）；确认前请知悉影响范围：{risk.scope}。
    </div>
  {/if}

  <textarea class="body" spellcheck="false" placeholder="Request body (JSON, optional)" bind:value={body}></textarea>

  {#if error}
    <StateBlock kind="error" message="Request failed" detail={error} />
  {:else if loading}
    <StateBlock kind="loading" message="Sending request…" detail={`${method} ${path}`} />
  {:else if result}
    <div class="resp">
      <div class="resp-head">
        <span class="status {statusClass(result.status ?? 0)}">HTTP {result.status ?? "—"}</span>
        <span class="dur">{duration} ms</span>
      </div>
      {#if result.headers}
        <details class="headers">
          <summary>Response headers</summary>
          <JsonView value={result.headers} compact />
        </details>
      {/if}
      <JsonView value={result.body} />
    </div>
  {/if}

  <aside class="history">
    <h2>History</h2>
    {#if history.length === 0}<p class="muted">No requests yet.</p>{/if}
    {#each history as item (item.method + item.path + item.body)}
      <button type="button" class="hist" onclick={() => useHistory(item)}>
        <span class="m">{item.method}</span> <span class="p">{item.path}</span>
      </button>
    {/each}
  </aside>
</div>

<style>
  .page { max-width: 1100px; }
  header { margin-bottom: 18px; }
  h1 { margin: 0 0 4px; font-size: 18px; }
  header p { margin: 0; color: var(--es-muted); }
  button { padding: 6px 14px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); cursor: pointer; }
  button:disabled { opacity: 0.55; cursor: default; }
  .primary { background: var(--es-accent); color: #fff; border-color: var(--es-accent); }
  .bar { display: flex; gap: 8px; margin-bottom: 10px; }
  .bar select, .bar input { padding: 6px 10px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); }
  .bar .path { flex: 1; font-family: var(--es-mono); }
  .warn-banner { padding: 10px 12px; margin-bottom: 10px; border: 1px solid var(--es-yellow); border-radius: 6px; background: var(--es-surface-2); color: var(--es-yellow); font-size: 12px; line-height: 1.6; }
  .body { width: 100%; min-height: 140px; resize: vertical; padding: 12px 14px; border: 1px solid var(--es-border); border-radius: var(--es-radius); background: var(--es-surface-2); color: var(--es-text); font-family: var(--es-mono); font-size: 12px; box-sizing: border-box; margin-bottom: 14px; }
  .resp-head { display: flex; gap: 12px; align-items: center; margin-bottom: 10px; font-family: var(--es-mono); font-size: 12px; }
  .status { padding: 2px 10px; border-radius: 999px; font-weight: 700; }
  .status.ok { color: var(--es-green); border: 1px solid var(--es-green); }
  .status.bad { color: var(--es-red); border: 1px solid var(--es-red); }
  .status.warn { color: var(--es-yellow); border: 1px solid var(--es-yellow); }
  .dur { color: var(--es-muted); }
  .headers { margin-bottom: 10px; }
  .headers summary { cursor: pointer; color: var(--es-muted); font-size: 12px; }
  .history { margin-top: 18px; border-top: 1px solid var(--es-border); padding-top: 12px; }
  .history h2 { font-size: 12px; text-transform: uppercase; letter-spacing: 0.08em; color: var(--es-muted); margin: 0 0 8px; }
  .history .muted { color: var(--es-muted); font-size: 12px; }
  .hist { display: block; width: 100%; text-align: left; font-family: var(--es-mono); font-size: 11px; margin-bottom: 6px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .hist .m { color: var(--es-accent); }
</style>
