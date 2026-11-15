<script lang="ts">
  import { tick } from "svelte";
  import StateBlock from "../components/StateBlock.svelte";
  import JsonView from "../components/JsonView.svelte";
  import { searchIndex, single, fetchIndices, type DocHit, type IndexRow } from "../lib/services";
  import type { DbxBridge } from "../lib/bridge";

  interface Props {
    bridge: DbxBridge;
    connectionId: string;
  }
  let { bridge, connectionId }: Props = $props();

  let index = $state("");
  let indices = $state<IndexRow[]>([]);
  let loadingIndices = $state(true);
  let body = $state('{\n  "query": { "match_all": {} },\n  "size": 20\n}');
  let loading = $state(false);
  let error = $state("");
  let hits = $state<DocHit[]>([]);
  let raw = $state<unknown>(null);
  let showRaw = $state(false);
  let formatError = $state("");
  let history = $state<string[]>([]);
  let escapeArmed = $state(false);

  const HISTORY_KEY = "es.studio.dsls";

  $effect(() => {
    void bridge.storage.get<string[]>(HISTORY_KEY).then((v) => {
      if (Array.isArray(v)) history = v;
    });
  });

  $effect(() => {
    const id = connectionId;
    if (!id) {
      loadingIndices = false;
      return;
    }
    loadingIndices = true;
    void fetchIndices(bridge, id)
      .then((rows) => {
        indices = rows;
      })
      .catch(() => {
        indices = [];
      })
      .finally(() => {
        loadingIndices = false;
      });
  });

  /**
   * A JSON editor without Tab-to-indent is painful: Tab used to move focus into
   * the results table mid-typing. Now Tab inserts two spaces, and Esc first if
   * you actually want to leave the editor by keyboard.
   */
  async function onEditorKey(event: KeyboardEvent): Promise<void> {
    if (event.key === "Escape") {
      escapeArmed = true;
      return;
    }
    if (event.key === "Enter" && (event.ctrlKey || event.metaKey)) {
      event.preventDefault();
      void run();
      return;
    }
    if (event.key !== "Tab") return;
    if (event.shiftKey || escapeArmed) {
      escapeArmed = false;
      return;
    }
    event.preventDefault();
    const el = event.currentTarget as HTMLTextAreaElement;
    const start = el.selectionStart;
    const end = el.selectionEnd;
    const indent = "  ";
    body = body.slice(0, start) + indent + body.slice(end);
    await tick();
    el.selectionStart = el.selectionEnd = start + indent.length;
  }

  function format(): void {
    formatError = "";
    try {
      const parsed = JSON.parse(body);
      body = JSON.stringify(parsed, null, 2);
    } catch (cause) {
      formatError = cause instanceof Error ? cause.message : String(cause);
    }
  }

  async function run(): Promise<void> {
    error = "";
    formatError = "";
    let parsed: unknown;
    try {
      parsed = JSON.parse(body);
    } catch (cause) {
      formatError = cause instanceof Error ? cause.message : String(cause);
      return;
    }
    loading = true;
    try {
      let res;
      if (index.trim()) {
        res = await searchIndex(bridge, connectionId, index.trim(), parsed as Record<string, unknown>);
      } else {
        const r = await single(bridge, connectionId, "POST", "/_search", parsed);
        if (r.error) {
          error = r.error?.message ?? "search failed";
          raw = r;
          hits = [];
          return;
        }
        const rec = r.body as any;
        res = {
          took: rec?.took ?? 0,
          total: rec?.hits?.total?.value ?? rec?.hits?.total ?? 0,
          hits: (rec?.hits?.hits ?? []).map((h: any) => ({ index: h._index, id: h._id, score: h._score ?? 0, source: h._source })),
          raw: r.body,
        };
      }
      if (res.error) {
        error = res.error;
        hits = [];
        raw = res.raw;
      } else {
        hits = res.hits;
        raw = res.raw;
        pushHistory();
      }
    } catch (cause) {
      error = cause instanceof Error ? cause.message : String(cause);
    } finally {
      loading = false;
    }
  }

  function pushHistory(): void {
    history = [body, ...history.filter((h) => h !== body)].slice(0, 10);
    void bridge.storage.set(HISTORY_KEY, history);
  }

  function useHistory(item: string): void {
    body = item;
  }

  function preview(source: unknown): string {
    try {
      const s = JSON.stringify(source);
      return s.length > 80 ? s.slice(0, 80) + "…" : s;
    } catch {
      return String(source);
    }
  }
</script>

<div class="page">
  <header>
    <div>
      <h1>DSL Editor</h1>
      <p>Write raw Elasticsearch query DSL. A lightweight editor (no Monaco) with format + history.</p>
    </div>
  </header>

  <section class="bar">
    <label>Index
      <select bind:value={index} disabled={loadingIndices}>
        <option value="">All indices (*)</option>
        {#each indices as ix (ix.index)}<option value={ix.index}>{ix.index}</option>{/each}
      </select>
    </label>
    <button type="button" onclick={format}>Format</button>
    <button type="button" class="primary" onclick={run} disabled={loading || !connectionId}>
      {loading ? "Running…" : "Run"}
    </button>
    <span class="keys">Tab 插入 2 空格 · Esc 后 Tab 跳出编辑器 · Ctrl/⌘+Enter 运行</span>
  </section>

  {#if formatError}<div class="err">JSON error: {formatError}</div>{/if}

  <textarea
    class="editor"
    spellcheck="false"
    bind:value={body}
    onkeydown={onEditorKey}
    placeholder={'{\n  "query": { "match_all": {} }\n}'}
  ></textarea>

  <div class="cols">
    <div class="results">
      {#if error}
        <StateBlock kind="error" message="Query failed" detail={error} />
      {:else if loading}
        <StateBlock kind="loading" message="Running query…" detail="POST /_search" />
      {:else if hits.length > 0}
        <p class="meta">total {raw && typeof raw === "object" && "hits" in (raw as object) ? ((raw as any).hits.total?.value ?? (raw as any).hits.total ?? 0) : 0} hits</p>
        <table class="docs">
          <thead><tr><th>_id</th><th>Score</th><th>_source</th></tr></thead>
          <tbody>
            {#each hits as hit (hit.id)}<tr><td class="mono">{hit.id}</td><td class="mono">{hit.score}</td><td class="mono src">{preview(hit.source)}</td></tr>{/each}
          </tbody>
        </table>
        <button type="button" class="link" onclick={() => (showRaw = !showRaw)}>{showRaw ? "Hide" : "Show"} raw response</button>
        {#if showRaw}<JsonView value={raw} />{/if}
      {:else if raw}
        <StateBlock kind="empty" message="No documents matched" />
      {/if}
    </div>
    <aside class="history">
      <h2>History</h2>
      {#if history.length === 0}<p class="muted">No saved queries yet.</p>{/if}
      {#each history as item (item)}
        <button type="button" class="hist" onclick={() => useHistory(item)}>{preview(JSON.parse(item))}</button>
      {/each}
    </aside>
  </div>
</div>

<style>
  .page { max-width: 1100px; }
  header { margin-bottom: 18px; }
  h1 { margin: 0 0 4px; font-size: 18px; }
  header p { margin: 0; color: var(--es-muted); }
  button { padding: 6px 14px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); cursor: pointer; }
  button:hover:not(:disabled):not(.primary) { background: var(--es-surface-2); }
  .primary:hover:not(:disabled) { filter: brightness(0.92); }
  button:disabled { opacity: 0.55; cursor: default; }
  .bar { display: flex; gap: 12px; align-items: end; margin-bottom: 10px; flex-wrap: wrap; }
  .bar label { display: flex; flex-direction: column; gap: 4px; font-size: 11px; color: var(--es-muted); }
  .bar select { padding: 6px 10px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); min-width: 190px; }
  .keys { font-size: 10px; color: var(--es-muted); font-family: var(--es-mono); padding-bottom: 9px; }
  .primary { background: var(--es-accent); color: #fff; border-color: var(--es-accent); }
  .err { padding: 8px 12px; margin-bottom: 10px; border: 1px solid var(--es-red); border-radius: 6px; background: var(--es-surface-2); color: var(--es-red); font-size: 12px; font-family: var(--es-mono); }
  .editor {
    width: 100%; min-height: 220px; resize: vertical;
    padding: 12px 14px; border: 1px solid var(--es-border); border-radius: var(--es-radius);
    background: var(--es-surface-2); color: var(--es-text);
    font-family: var(--es-mono); font-size: 12px; line-height: 1.6; box-sizing: border-box;
  }
  .cols { display: grid; grid-template-columns: 1fr 240px; gap: 16px; margin-top: 14px; align-items: start; }
  .meta { font-size: 11px; color: var(--es-muted); font-family: var(--es-mono); }
  table.docs { width: 100%; border-collapse: collapse; font-size: 12px; border: 1px solid var(--es-border); border-radius: var(--es-radius); overflow: hidden; }
  .docs th, .docs td { padding: 6px 8px; border-bottom: 1px solid var(--es-border); }
  .docs th { background: var(--es-surface-2); color: var(--es-muted); text-align: left; font-weight: 600; }
  .docs td.mono { font-family: var(--es-mono); }
  .docs td.src { max-width: 420px; overflow: hidden; text-overflow: ellipsis; }
  .link { background: none; border: none; color: var(--es-accent); cursor: pointer; padding: 8px 0; }
  .history h2 { font-size: 12px; text-transform: uppercase; letter-spacing: 0.08em; color: var(--es-muted); margin: 0 0 8px; }
  .history .muted { color: var(--es-muted); font-size: 12px; }
  .hist { display: block; width: 100%; text-align: left; font-family: var(--es-mono); font-size: 11px; margin-bottom: 6px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
</style>
