<script lang="ts">
  import StateBlock from "../components/StateBlock.svelte";
  import {
    fetchShards, buildTopology, summarizeShards,
    type ShardRow, type ShardTag, type TopologyMatrix, type ShardSummary,
  } from "../lib/services";
  import type { DbxBridge } from "../lib/bridge";
  import { t } from "../lib/i18n.svelte.js";

  interface Props {
    bridge: DbxBridge;
    connectionId: string;
  }
  let { bridge, connectionId }: Props = $props();

  let shards = $state<ShardRow[]>([]);
  let summary = $state<ShardSummary>({ total: 0, started: 0, initializing: 0, relocating: 0, unassigned: 0, inFlight: 0 });
  let loading = $state(true);
  let error = $state("");
  let autoRefresh = $state(true);
  let lastUpdated = $state("");
  let inflight = false;
  let indexFilter = $state("");

  /** i18n keys for the four `_cat/shards` states ES actually reports. */
  const STATE_LABEL: Record<string, string> = {
    STARTED: "shards.state.started",
    INITIALIZING: "shards.state.initializing",
    RELOCATING: "shards.state.relocating",
    UNASSIGNED: "shards.state.unassigned",
  };

  function stateText(state: string): string {
    const key = STATE_LABEL[state];
    return key ? t(key) : state;
  }

  async function load(id: string): Promise<void> {
    if (inflight) return;
    if (!id) {
      shards = [];
      summary = { total: 0, started: 0, initializing: 0, relocating: 0, unassigned: 0, inFlight: 0 };
      error = "NO_CONNECTION";
      loading = false;
      return;
    }
    inflight = true;
    loading = true;
    error = "";
    try {
      const next = await fetchShards(bridge, id);
      shards = next;
      summary = summarizeShards(next);
      lastUpdated = new Date().toLocaleTimeString();
    } catch (cause) {
      shards = [];
      error = cause instanceof Error ? cause.message : String(cause);
    } finally {
      inflight = false;
      loading = false;
    }
  }

  $effect(() => {
    void load(connectionId);
  });

  // While ES is actively creating or moving copies the page is only useful if it
  // follows along, so refresh on a short timer until nothing is in flight.
  $effect(() => {
    if (!autoRefresh || summary.inFlight === 0 || !connectionId) return;
    const timer = setInterval(() => void load(connectionId), 2000);
    return () => clearInterval(timer);
  });

  // ------------------------------------------------------------------
  // 分配设置已拆到独立「分片设置」页（0.5.1）；本页只做拓扑与明细。
  // ------------------------------------------------------------------

  // Index-name filter narrows the matrix and the shard list together — one box.
  const query = $derived(indexFilter.trim().toLowerCase());
  const filteredShards = $derived(
    query ? shards.filter((s) => s.index.toLowerCase().includes(query)) : shards,
  );
  const matrix = $derived.by(() => buildTopology(filteredShards));

  // Detail table pagination — the matrix above stays whole (one row per index),
  // but thousands of shard copies must not all mount into the DOM at once.
  const DETAIL_PAGE_SIZE = 100;
  let detailPage = $state(1);
  const detailPages = $derived(Math.max(1, Math.ceil(filteredShards.length / DETAIL_PAGE_SIZE)));
  $effect(() => {
    if (detailPage > detailPages) detailPage = detailPages;
  });
  const detailRows = $derived(filteredShards.slice((detailPage - 1) * DETAIL_PAGE_SIZE, detailPage * DETAIL_PAGE_SIZE));

  // Matrix pagination — one row per index, but a big cluster has hundreds of
  // indices; cap mounted rows (and therefore shard chips) to keep the DOM small.
  // buildTopology still runs over all filtered shards so cellTags can look up any
  // index on the current page without rebuilding per page.
  const MATRIX_PAGE_SIZE = 50;
  let matrixPage = $state(1);
  const matrixPages = $derived(Math.max(1, Math.ceil(matrix.indices.length / MATRIX_PAGE_SIZE)));
  $effect(() => {
    if (matrixPage > matrixPages) matrixPage = matrixPages;
  });
  const matrixIndexRows = $derived(
    matrix.indices.slice((matrixPage - 1) * MATRIX_PAGE_SIZE, matrixPage * MATRIX_PAGE_SIZE),
  );

  function stateClass(state: string): string {
    if (state === "UNASSIGNED") return "unassigned";
    if (state === "INITIALIZING") return "initializing";
    if (state === "RELOCATING") return "relocating";
    if (state === "STARTED") return "started";
    return "other";
  }

  function formatBytes(value: number): string {
    if (!Number.isFinite(value) || value <= 0) return "0 B";
    const units = ["B", "KB", "MB", "GB", "TB"];
    let i = 0;
    let n = value;
    while (n >= 1024 && i < units.length - 1) {
      n /= 1024;
      i += 1;
    }
    return `${n.toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
  }

  function cellTags(index: string, node: string): ShardTag[] {
    return matrix.cells[index]?.[node] ?? [];
  }
  function unassignedTags(index: string): ShardTag[] {
    return matrix.unassignedByIndex[index] ?? [];
  }
  function tagState(label: string): string {
    const parts = label.split(":");
    return parts.length > 1 ? parts[parts.length - 1] : label;
  }
</script>

<div class="page">
  <header>
    <div>
      <h1>{t("shards.title")}</h1>
      <p>{t("shards.hintPrefix")}<code>/_cat/shards</code>{t("shards.hintSuffix")}</p>
    </div>
    <div class="head-actions">
      <label class="auto"><input type="checkbox" bind:checked={autoRefresh} />{t("shards.autoRefresh")}</label>
      <button type="button" onclick={() => load(connectionId)} disabled={loading || !connectionId}>
        {loading ? t("common.loading") : t("common.refresh")}
      </button>
    </div>
  </header>

  {#if loading && shards.length === 0}
    <StateBlock kind="loading" message={t("shards.loadingMessage")} detail="GET /_cat/shards" />
  {:else if error}
    <StateBlock kind="error" message={t("shards.loadFailed")} detail={error === "NO_CONNECTION" ? t("common.noConnection") : error} />
  {:else if shards.length === 0}
    <StateBlock kind="empty" message={t("shards.empty")} />
  {:else}
    <section class="summary">
      <div class="card started">
        <span class="num">{summary.started}</span>
        <span class="lbl">{t("shards.card.started")}</span>
      </div>
      <div class="card initializing" class:active={summary.initializing > 0}>
        <span class="num">{summary.initializing}</span>
        <span class="lbl">{t("shards.card.initializing")}</span>
      </div>
      <div class="card relocating" class:active={summary.relocating > 0}>
        <span class="num">{summary.relocating}</span>
        <span class="lbl">{t("shards.card.relocating")}</span>
      </div>
      <div class="card unassigned" class:active={summary.unassigned > 0}>
        <span class="num">{summary.unassigned}</span>
        <span class="lbl">{t("shards.card.unassigned")}</span>
      </div>
    </section>

    {#if summary.inFlight > 0}
      <p class="inflight">
        {t("shards.inflight", { count: summary.inFlight, initializing: summary.initializing, relocating: summary.relocating })}
        {#if autoRefresh}<span class="muted">{t("shards.autoRefreshNote")}</span>{/if}
      </p>
    {:else if summary.unassigned > 0}
      <p class="inflight note">{t("shards.unassignedNote", { count: summary.unassigned })}</p>
    {/if}

    <div class="filter-bar">
      <input
        class="filter"
        type="search"
        placeholder={t("shards.filter")}
        bind:value={indexFilter}
      />
    </div>

    <h2>{t("shards.matrix.title")} <span class="asof">{lastUpdated ? t("shards.matrix.updated", { time: lastUpdated }) : ""}</span></h2>
    {#if matrix.indices.length === 0}
      <p class="no-match">{t("shards.noMatch", { filter: indexFilter })}</p>
    {:else}
      <div class="matrix-wrap">
        <table class="matrix">
          <thead>
            <tr>
              <th class="corner">{t("shards.matrix.index")}</th>
              {#each matrix.nodes as node (node)}<th>{node}</th>{/each}
              <th class="unassigned-col">{t("shards.matrix.unassigned")}</th>
            </tr>
          </thead>
          <tbody>
            {#each matrixIndexRows as index (index)}
              <tr>
                <th class="rowhead">{index}</th>
                {#each matrix.nodes as node (node)}
                  <td>
                    {#each cellTags(index, node) as tag (tag.key)}
                      <span class="shard {stateClass(tagState(tag.label))}" title={stateText(tagState(tag.label))}>{tag.label}</span>
                    {/each}
                  </td>
                {/each}
                <td class="unassigned-col">
                  {#each unassignedTags(index) as tag (tag.key)}
                    <span class="shard unassigned" title={t("shards.matrix.unassigned")}>{tag.label}</span>
                  {/each}
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
      {#if matrixPages > 1}
        <div class="pager matrix-pager">
          <button type="button" onclick={() => (matrixPage = Math.max(1, matrixPage - 1))} disabled={matrixPage <= 1}>{t("common.prev")}</button>
          <span>{t("common.pageInfo", { page: matrixPage, pages: matrixPages, total: matrix.indices.length })}</span>
          <button type="button" onclick={() => (matrixPage = Math.min(matrixPages, matrixPage + 1))} disabled={matrixPage >= matrixPages}>{t("common.next")}</button>
        </div>
      {/if}
    {/if}

    <h2>{t("shards.table.title")} <span class="asof">{t("shards.table.copies", { count: summary.total })}</span></h2>
    <div class="table-wrap">
      <table>
        <thead>
          <tr>
            <th>{t("shards.col.index")}</th>
            <th class="right">{t("shards.col.shard")}</th>
            <th class="center">{t("shards.col.type")}</th>
            <th>{t("shards.col.state")}</th>
            <th class="right">{t("shards.col.docs")}</th>
            <th class="right">{t("shards.col.store")}</th>
            <th>{t("shards.col.node")}</th>
            <th>{t("shards.col.reason")}</th>
            <th>{t("shards.col.waiting")}</th>
          </tr>
        </thead>
        <tbody>
          {#each detailRows as s, i ((detailPage - 1) * DETAIL_PAGE_SIZE + i)}
            <tr>
              <td class="mono">{s.index}</td>
              <td class="right mono">{s.shard}</td>
              <td class="center mono">{s.prirep}</td>
              <td class="mono {stateClass(s.state)}">{s.state} · {stateText(s.state)}</td>
              <td class="right mono">{s.docs}</td>
              <td class="right mono">{formatBytes(Number(s.storeBytes))}</td>
              <td>{s.node}</td>
              <td class="mono">{s.reason || "—"}</td>
              <td class="mono">{s.waiting || "—"}</td>
            </tr>
          {/each}
        </tbody>
      </table>
      {#if detailPages > 1}
        <div class="pager">
          <button type="button" onclick={() => (detailPage = Math.max(1, detailPage - 1))} disabled={detailPage <= 1}>{t("common.prev")}</button>
          <span>{t("common.pageInfo", { page: detailPage, pages: detailPages, total: filteredShards.length })}</span>
          <button type="button" onclick={() => (detailPage = Math.min(detailPages, detailPage + 1))} disabled={detailPage >= detailPages}>{t("common.next")}</button>
        </div>
      {/if}
    </div>

  {/if}
</div>


<style>
  .page { max-width: 1240px; }
  header { display: flex; justify-content: space-between; align-items: flex-start; gap: 16px; margin-bottom: 16px; }
  h1 { margin: 0 0 6px; font-size: var(--es-fs-3xl); }
  h2 { margin: 22px 0 10px; font-size: var(--es-fs-sm); text-transform: uppercase; letter-spacing: 0.08em; color: var(--es-muted); }
  h2 .asof { margin-left: 8px; font-size: var(--es-fs-2xs); letter-spacing: 0; text-transform: none; font-family: var(--es-mono); }
  header p { margin: 0; color: var(--es-muted); }
  header code { font-family: var(--es-mono); font-size: var(--es-fs-xs); }
  .head-actions { display: flex; align-items: center; gap: 12px; flex: 0 0 auto; }
  .auto { display: flex; align-items: center; gap: 6px; font-size: var(--es-fs-xs); color: var(--es-muted); white-space: nowrap; }
  button { padding: 6px 14px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); cursor: pointer; white-space: nowrap; }
  button:hover:not(:disabled):not(.primary) { background: var(--es-surface-2); }
  .primary { background: var(--es-accent); color: #fff; border-color: var(--es-accent); }
  .primary:hover:not(:disabled) { filter: brightness(0.92); }
  button:disabled { opacity: 0.55; cursor: default; }

  .filter-bar { margin: 14px 0 0; }
  .filter { width: 320px; max-width: 100%; padding: 7px 12px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); font-size: var(--es-fs-sm); }
  .no-match { margin: 0; padding: 10px 12px; border: 1px dashed var(--es-border); border-radius: var(--es-radius); color: var(--es-muted); font-size: var(--es-fs-sm); }

  .summary { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 10px; }
  .card { display: flex; flex-direction: column; gap: 3px; padding: 10px 12px; border: 1px solid var(--es-border); border-radius: var(--es-radius); background: var(--es-surface); border-left-width: 3px; }
  .card .num { font-family: var(--es-mono); font-size: var(--es-fs-4xl); font-weight: 600; line-height: 1.1; }
  .card .lbl { font-size: var(--es-fs-xs); color: var(--es-muted); font-family: var(--es-mono); }
  .card.started { border-left-color: var(--es-green); }
  .card.started .num { color: var(--es-green); }
  .card.initializing { border-left-color: var(--es-accent); }
  .card.initializing .num { color: var(--es-muted); }
  .card.initializing.active .num { color: var(--es-accent); }
  .card.relocating { border-left-color: var(--es-yellow); }
  .card.relocating .num { color: var(--es-muted); }
  .card.relocating.active .num { color: var(--es-yellow); }
  .card.unassigned { border-left-color: var(--es-red); }
  .card.unassigned .num { color: var(--es-muted); }
  .card.unassigned.active { background: color-mix(in srgb, var(--es-red) 6%, var(--es-surface)); }
  .card.unassigned.active .num { color: var(--es-red); }

  .inflight { margin: 12px 0 0; padding: 6px 12px; font-size: var(--es-fs-sm); border: 1px solid var(--es-accent); border-radius: var(--es-radius); background: var(--es-accent-soft); color: var(--es-text); }
  .inflight.note { border-color: var(--es-border); background: var(--es-surface-2); color: var(--es-muted); }
  .inflight .muted { color: var(--es-muted); }

  .table-wrap { overflow-x: auto; border: 1px solid var(--es-border); border-radius: var(--es-radius); }
  table { width: 100%; border-collapse: collapse; font-size: var(--es-fs-sm); }
  th, td { padding: 8px 12px; text-align: left; border-bottom: 1px solid var(--es-border); white-space: nowrap; }
  th { background: var(--es-surface-2); color: var(--es-muted); font-weight: 600; position: sticky; top: 0; }
  th.right, td.right { text-align: right; }
  th.center, td.center { text-align: center; }
  td.mono { font-family: var(--es-mono); }
  tbody tr:hover { background: var(--es-surface-2); }
  td.started { color: var(--es-green); }
  td.unassigned { color: var(--es-red); font-weight: 700; }
  td.initializing { color: var(--es-accent); font-weight: 700; }
  td.relocating { color: var(--es-yellow); font-weight: 700; }

  .matrix-wrap { overflow-x: auto; border: 1px solid var(--es-border); border-radius: var(--es-radius); }
  table.matrix { border-collapse: collapse; font-size: var(--es-fs-xs); width: 100%; }
  .matrix th, .matrix td { border: 1px solid var(--es-border); padding: 6px 8px; vertical-align: top; }
  .matrix .corner, .matrix .rowhead { background: var(--es-surface-2); text-align: left; font-weight: 600; white-space: nowrap; }
  .matrix .unassigned-col { background: var(--es-surface-2); }
  .shard { display: inline-block; margin: 1px; padding: 1px 5px; border-radius: 4px; font-family: var(--es-mono); font-size: var(--es-fs-2xs); border: 1px solid var(--es-border); }
  .shard.started { color: var(--es-green); }
  .shard.initializing { color: var(--es-accent); border-color: var(--es-accent); background: var(--es-accent-soft); }
  .shard.relocating { color: var(--es-yellow); border-color: var(--es-yellow); }
  .shard.other { color: var(--es-muted); }
  .shard.unassigned { color: var(--es-red); border-color: var(--es-red); }

  .pager { display: flex; align-items: center; justify-content: flex-end; gap: 12px; padding: 8px 12px; border-top: 1px solid var(--es-border); font-size: var(--es-fs-xs); color: var(--es-muted); }
  .pager button { padding: 3px 10px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); cursor: pointer; }
  .pager button:hover:not(:disabled) { background: var(--es-surface-2); }
  .pager button:disabled { opacity: 0.45; cursor: default; }

</style>
