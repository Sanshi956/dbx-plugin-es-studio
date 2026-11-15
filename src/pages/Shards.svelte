<script lang="ts">
  import StateBlock from "../components/StateBlock.svelte";
  import DataTable from "../components/DataTable.svelte";
  import {
    fetchShards, buildTopology, summarizeShards,
    type ShardRow, type ShardTag, type TopologyMatrix, type ShardSummary,
  } from "../lib/services";
  import type { DbxBridge } from "../lib/bridge";
  import type { TableColumn } from "../lib/types";
  import { t } from "../lib/i18n.svelte.js";

  interface Props {
    bridge: DbxBridge;
    connectionId: string;
  }
  let { bridge, connectionId }: Props = $props();

  let shards = $state<ShardRow[]>([]);
  let matrix = $state<TopologyMatrix>({ nodes: [], indices: [], cells: {}, unassignedByIndex: {} });
  let summary = $state<ShardSummary>({ total: 0, started: 0, initializing: 0, relocating: 0, unassigned: 0, inFlight: 0 });
  let loading = $state(true);
  let error = $state("");
  let autoRefresh = $state(true);
  let lastUpdated = $state("");
  let inflight = false;

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
      matrix = buildTopology(next);
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

  // One row per shard copy: index+shard+prirep is NOT unique when an index has
  // several same-state replicas, so hand DataTable a positional row id.
  const tableRows = $derived<Record<string, unknown>[]>(
    shards.map((s, i) => ({ ...s, __rowId: i })),
  );

  const columns: TableColumn[] = [
    { key: "index", label: "shards.col.index" },
    { key: "shard", label: "shards.col.shard", align: "right", mono: true },
    { key: "prirep", label: "shards.col.type", align: "center", mono: true },
    {
      key: "state",
      label: "shards.col.state",
      mono: true,
      render: (v) => `${String(v)} · ${stateText(String(v))}`,
      cellClass: (v) => stateClass(String(v)),
    },
    { key: "docs", label: "shards.col.docs", align: "right", mono: true },
    { key: "storeBytes", label: "shards.col.store", align: "right", mono: true, render: (v) => formatBytes(Number(v)) },
    { key: "node", label: "shards.col.node" },
    { key: "reason", label: "shards.col.reason", mono: true, render: (v) => (String(v) ? String(v) : "—") },
    { key: "waiting", label: "shards.col.waiting", mono: true, render: (v) => (String(v) ? String(v) : "—") },
  ];

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
  function stateClass(state: string): string {
    if (state === "UNASSIGNED") return "unassigned";
    if (state === "INITIALIZING") return "initializing";
    if (state === "RELOCATING") return "relocating";
    if (state === "STARTED") return "started";
    return "other";
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

    <h2>{t("shards.matrix.title")} <span class="asof">{lastUpdated ? t("shards.matrix.updated", { time: lastUpdated }) : ""}</span></h2>
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
          {#each matrix.indices as index (index)}
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

    <h2>{t("shards.table.title")} <span class="asof">{t("shards.table.copies", { count: summary.total })}</span></h2>
    <DataTable
      columns={columns}
      rows={tableRows}
      rowKey={(r) => String(r.__rowId)}
    >
    </DataTable>
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
  button { padding: 6px 14px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); cursor: pointer; }
  button:hover:not(:disabled):not(.primary) { background: var(--es-surface-2); }
  .primary:hover:not(:disabled) { filter: brightness(0.92); }
  button:disabled { opacity: 0.55; cursor: default; }

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

  .matrix-wrap { overflow-x: auto; border: 1px solid var(--es-border); border-radius: var(--es-radius); }
  table.matrix { border-collapse: collapse; font-size: var(--es-fs-xs); width: 100%; }
  .matrix th, .matrix td { border: 1px solid var(--es-border); padding: 6px 8px; vertical-align: top; }
  .matrix .corner, .matrix .rowhead { background: var(--es-surface-2); text-align: left; font-weight: 600; white-space: nowrap; }
  .matrix .unassigned-col { background: var(--es-surface-2); }
  .shard { display: inline-block; margin: 1px; padding: 1px 5px; border-radius: 4px; font-family: var(--es-mono); font-size: var(--es-fs-2xs); border: 1px solid var(--es-border); }
  .shard.inline { font-size: var(--es-fs-2xs); }
  .shard.started { color: var(--es-green); }
  .shard.initializing { color: var(--es-accent); border-color: var(--es-accent); background: var(--es-accent-soft); }
  .shard.relocating { color: var(--es-yellow); border-color: var(--es-yellow); }
  .shard.other { color: var(--es-muted); }
  .shard.unassigned { color: var(--es-red); border-color: var(--es-red); }
  :global(td.unassigned) { color: var(--es-red); font-weight: 700; }
  :global(td.initializing) { color: var(--es-accent); font-weight: 700; }
  :global(td.relocating) { color: var(--es-yellow); font-weight: 700; }
</style>
