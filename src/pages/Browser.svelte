<script lang="ts">
  import StateBlock from "../components/StateBlock.svelte";
  import JsonView from "../components/JsonView.svelte";
  import ConfirmDanger from "../components/ConfirmDanger.svelte";
  import {
    fetchIndices, searchIndex, getDocument, deleteDocument, isError,
    type IndexRow, type DocHit, type SearchResult,
  } from "../lib/services";
  import type { DbxBridge } from "../lib/bridge";

  interface Props {
    bridge: DbxBridge;
    connectionId: string;
  }
  let { bridge, connectionId }: Props = $props();

  let indices = $state<IndexRow[]>([]);
  let selectedIndex = $state("");
  let size = $state(20);
  let result = $state<SearchResult | null>(null);
  let hits = $state<DocHit[]>([]);
  let loadingIndices = $state(true);
  let loadingSearch = $state(false);
  let searchError = $state("");

  let selected = $state<{ id: string; source: unknown } | null>(null);
  let loadingDoc = $state(false);

  let pendingDelete = $state<string | null>(null);
  let toast = $state("");

  async function loadIndices(id: string): Promise<void> {
    if (!id) return;
    loadingIndices = true;
    try {
      indices = await fetchIndices(bridge, id);
      if (!selectedIndex && indices.length) selectedIndex = indices[0].index;
    } catch {
      indices = [];
    } finally {
      loadingIndices = false;
    }
  }

  $effect(() => {
    void loadIndices(connectionId);
  });

  async function runSearch(): Promise<void> {
    if (!selectedIndex) {
      searchError = "请先选择索引";
      return;
    }
    loadingSearch = true;
    searchError = "";
    selected = null;
    try {
      result = await searchIndex(bridge, connectionId, selectedIndex, { size });
      hits = result.hits;
      if (result.error) searchError = result.error;
    } catch (cause) {
      searchError = cause instanceof Error ? cause.message : String(cause);
      hits = [];
    } finally {
      loadingSearch = false;
    }
  }

  async function openDoc(id: string): Promise<void> {
    loadingDoc = true;
    try {
      const doc = await getDocument(bridge, connectionId, selectedIndex, id);
      selected = { id, source: doc.found ? doc.source : null };
    } finally {
      loadingDoc = false;
    }
  }

  function askDelete(id: string): void {
    pendingDelete = id;
  }
  function cancelDelete(): void {
    pendingDelete = null;
  }
  async function confirmDelete(): Promise<void> {
    if (pendingDelete === null) return;
    const id = pendingDelete;
    toast = "";
    const res = await deleteDocument(bridge, connectionId, selectedIndex, id);
    pendingDelete = null;
    if (isError(res)) {
      toast = `删除失败: ${res.error?.message ?? "HTTP " + res.status}`;
    } else {
      toast = `已删除文档 ${id}`;
      selected = null;
      await runSearch();
    }
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
      <h1>Document Browser</h1>
      <p>Browse documents in an index via <code>_search</code>; inspect and delete by id.</p>
    </div>
  </header>

  {#if toast}<div class="toast">{toast}</div>{/if}

  <section class="bar">
    <label>Index
      <select bind:value={selectedIndex} disabled={loadingIndices}>
        {#if loadingIndices}<option>Loading…</option>{/if}
        {#each indices as ix (ix.index)}<option value={ix.index}>{ix.index}</option>{/each}
      </select>
    </label>
    <label>Size
      <input type="number" min="1" max="1000" bind:value={size} />
    </label>
    <button type="button" class="primary" onclick={runSearch} disabled={!selectedIndex || loadingSearch}>
      {loadingSearch ? "Searching…" : "Search"}
    </button>
  </section>

  {#if searchError}
    <StateBlock kind="error" message="Search failed" detail={searchError} />
  {:else if loadingSearch}
    <StateBlock kind="loading" message="Searching…" detail={`POST /${selectedIndex}/_search`} />
  {:else if hits.length === 0 && result}
    <StateBlock kind="empty" message="No documents matched" />
  {:else if hits.length > 0}
    <div class="grid">
      <div class="results">
        <p class="meta">total {result?.total ?? 0} · took {result?.took ?? 0} ms</p>
        <table class="docs">
          <thead><tr><th>_id</th><th>Score</th><th>_source</th><th></th></tr></thead>
          <tbody>
            {#each hits as hit (hit.id)}
              <tr class:active={selected?.id === hit.id}>
                <td class="mono">{hit.id}</td>
                <td class="mono">{hit.score}</td>
                <td class="src mono">{preview(hit.source)}</td>
                <td class="actions">
                  <button type="button" class="mini" onclick={() => openDoc(hit.id)}>View</button>
                  <button type="button" class="mini danger" onclick={() => askDelete(hit.id)}>Delete</button>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
      <div class="detail">
        {#if loadingDoc}
          <StateBlock kind="loading" message="Loading document…" />
        {:else if selected}
          <header class="detail-head">
            <strong class="mono">{selected?.id}</strong>
            <button type="button" class="mini danger" onclick={() => askDelete(selected?.id ?? "")}>Delete</button>
          </header>
          <JsonView value={selected.source} />
        {:else}
          <StateBlock kind="empty" message="Select a document to inspect" />
        {/if}
      </div>
    </div>
  {/if}
</div>

<ConfirmDanger
  open={pendingDelete !== null}
  title="删除文档"
  message="该文档将通过 DELETE 请求永久删除，操作不可撤销。"
  scope={pendingDelete !== null ? `索引 ${selectedIndex} 文档 ${pendingDelete}` : ""}
  confirmLabel="删除文档"
  onConfirm={confirmDelete}
  onCancel={cancelDelete}
/>

<style>
  .page { max-width: 1200px; }
  header { margin-bottom: 18px; }
  h1 { margin: 0 0 4px; font-size: 18px; }
  header p { margin: 0; color: var(--es-muted); }
  header code { font-family: var(--es-mono); font-size: 11px; }
  button { padding: 6px 14px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); cursor: pointer; }
  button:hover:not(:disabled) { background: var(--es-surface-2); }
  button:disabled { opacity: 0.55; cursor: default; }
  .toast { padding: 8px 12px; margin-bottom: 12px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface-2); font-size: 12px; }
  .bar { display: flex; gap: 12px; align-items: end; margin-bottom: 16px; flex-wrap: wrap; }
  .bar label { display: flex; flex-direction: column; gap: 4px; font-size: 11px; color: var(--es-muted); }
  .bar select, .bar input { padding: 6px 10px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); }
  .bar .primary { background: var(--es-accent); color: #fff; border-color: var(--es-accent); align-self: flex-end; }

  .grid { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; align-items: start; }
  .meta { font-size: 11px; color: var(--es-muted); font-family: var(--es-mono); margin: 0 0 8px; }
  table.docs { width: 100%; border-collapse: collapse; font-size: 12px; border: 1px solid var(--es-border); border-radius: var(--es-radius); overflow: hidden; }
  .docs th, .docs td { padding: 6px 8px; border-bottom: 1px solid var(--es-border); white-space: nowrap; }
  .docs th { background: var(--es-surface-2); color: var(--es-muted); text-align: left; font-weight: 600; }
  .docs td.mono { font-family: var(--es-mono); }
  .docs td.src { max-width: 320px; overflow: hidden; text-overflow: ellipsis; }
  .docs tbody tr:hover { background: var(--es-surface-2); }
  .docs tr.active { background: var(--es-accent-soft); }
  .actions { text-align: right; }
  .mini { padding: 3px 8px; font-size: 11px; }
  .mini.danger { color: var(--es-red); border-color: var(--es-red); }
  .mini.danger:hover { background: var(--es-red); color: #fff; }
  .detail-head { display: flex; justify-content: space-between; align-items: center; margin-bottom: 8px; }
</style>
