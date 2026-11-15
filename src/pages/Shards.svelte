<script lang="ts">
  import StateBlock from "../components/StateBlock.svelte";
  import DataTable from "../components/DataTable.svelte";
  import {
    fetchShards, buildTopology, summarizeShards,
    type ShardRow, type ShardTag, type TopologyMatrix, type ShardSummary,
  } from "../lib/services";
  import type { DbxBridge } from "../lib/bridge";
  import type { TableColumn } from "../lib/types";

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

  /** Chinese label for the four `_cat/shards` states ES actually reports. */
  const STATE_LABEL: Record<string, string> = {
    STARTED: "已就绪",
    INITIALIZING: "分配中",
    RELOCATING: "迁移中",
    UNASSIGNED: "未分配",
  };

  function stateText(state: string): string {
    return STATE_LABEL[state] ?? state;
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
    { key: "index", label: "Index" },
    { key: "shard", label: "Shard", align: "right", mono: true },
    { key: "prirep", label: "Type", align: "center", mono: true },
    {
      key: "state",
      label: "State",
      mono: true,
      render: (v) => `${String(v)} · ${stateText(String(v))}`,
      cellClass: (v) => stateClass(String(v)),
    },
    { key: "docs", label: "Docs", align: "right", mono: true },
    { key: "storeBytes", label: "Store", align: "right", mono: true, render: (v) => formatBytes(Number(v)) },
    { key: "node", label: "Node" },
    { key: "reason", label: "未分配原因", mono: true, render: (v) => (String(v) ? String(v) : "—") },
    { key: "waiting", label: "已等待", mono: true, render: (v) => (String(v) ? String(v) : "—") },
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
      <h1>Shards &amp; Topology</h1>
      <p>
        分片副本明细来自 <code>/_cat/shards</code>，拓扑矩阵按「索引 × 节点」摆放松弛的分片副本。
        副本除了「已就绪 / 未分配」，还有两种<strong>正在搬运</strong>的状态：分配中（ES 正在创建副本、
        补数据）与迁移中（分片正从一个节点搬到另一个节点）。
      </p>
    </div>
    <div class="head-actions">
      <label class="auto"><input type="checkbox" bind:checked={autoRefresh} />有在途分片时每 2s 自动刷新</label>
      <button type="button" onclick={() => load(connectionId)} disabled={loading || !connectionId}>
        {loading ? "Loading…" : "Refresh"}
      </button>
    </div>
  </header>

  {#if loading && shards.length === 0}
    <StateBlock kind="loading" message="Loading shards…" detail="GET /_cat/shards" />
  {:else if error}
    <StateBlock kind="error" message="Could not load shards" detail={error === "NO_CONNECTION" ? "No DBX connection is selected." : error} />
  {:else if shards.length === 0}
    <StateBlock kind="empty" message="No shards returned" />
  {:else}
    <section class="summary">
      <div class="card started">
        <span class="num">{summary.started}</span>
        <span class="lbl">已就绪 STARTED</span>
        <span class="hint">副本可正常读写</span>
      </div>
      <div class="card initializing" class:active={summary.initializing > 0}>
        <span class="num">{summary.initializing}</span>
        <span class="lbl">分配中 INITIALIZING</span>
        <span class="hint">{summary.initializing > 0 ? "ES 正在创建副本并回放数据" : "当前没有正在分配的分片"}</span>
      </div>
      <div class="card relocating" class:active={summary.relocating > 0}>
        <span class="num">{summary.relocating}</span>
        <span class="lbl">迁移中 RELOCATING</span>
        <span class="hint">{summary.relocating > 0 ? "分片正从源节点搬到目标节点" : "当前没有迁移中的分片"}</span>
      </div>
      <div class="card unassigned" class:active={summary.unassigned > 0}>
        <span class="num">{summary.unassigned}</span>
        <span class="lbl">未分配 UNASSIGNED</span>
        <span class="hint">{summary.unassigned > 0 ? "没有节点接收，见下表「未分配原因」" : "全部分片已有归属"}</span>
      </div>
    </section>

    {#if summary.inFlight > 0}
      <p class="inflight">
        集群正在搬运 <strong>{summary.inFlight}</strong> 个分片副本（分配中 {summary.initializing} / 迁移中 {summary.relocating}），
        {autoRefresh ? "每 2 秒自动刷新；" : "已关闭自动刷新；"}拓扑里的
        <span class="shard initializing inline">分配中</span> 与
        <span class="shard relocating inline">迁移中</span> 会随进度变化。
      </p>
    {:else if summary.unassigned > 0}
      <p class="inflight note">
        没有正在搬运的分片，但有 <strong>{summary.unassigned}</strong> 个副本未分配 —— 这通常是单节点集群上的副本数 &gt; 0，
        或节点/路由规则不满足。点 Shard allocation 表的「未分配原因」看 ES 给的判定依据，
        需要完整推理时去 Allocation 页用「Explain unassigned shard」。
      </p>
    {/if}

    <h2>Topology matrix <span class="asof">{lastUpdated ? `updated ${lastUpdated}` : ""}</span></h2>
    <div class="matrix-wrap">
      <table class="matrix">
        <thead>
          <tr>
            <th class="corner">Index</th>
            {#each matrix.nodes as node (node)}<th>{node}</th>{/each}
            <th class="unassigned-col">Unassigned</th>
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
                  <span class="shard unassigned" title="未分配">{tag.label}</span>
                {/each}
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>

    <h2>Shard allocation <span class="asof">{summary.total} copies</span></h2>
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
  h1 { margin: 0 0 6px; font-size: 18px; }
  h2 { margin: 22px 0 10px; font-size: 12px; text-transform: uppercase; letter-spacing: 0.08em; color: var(--es-muted); }
  h2 .asof { margin-left: 8px; font-size: 10px; letter-spacing: 0; text-transform: none; font-family: var(--es-mono); }
  header p { margin: 0; color: var(--es-muted); max-width: 760px; line-height: 1.6; }
  header code { font-family: var(--es-mono); font-size: 11px; }
  header strong { color: var(--es-text); }
  .head-actions { display: flex; align-items: center; gap: 12px; flex: 0 0 auto; }
  .auto { display: flex; align-items: center; gap: 6px; font-size: 11px; color: var(--es-muted); white-space: nowrap; }
  button { padding: 6px 14px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); cursor: pointer; }
  button:hover:not(:disabled):not(.primary) { background: var(--es-surface-2); }
  .primary:hover:not(:disabled) { filter: brightness(0.92); }
  button:disabled { opacity: 0.55; cursor: default; }

  .summary { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 10px; }
  .card { display: flex; flex-direction: column; gap: 3px; padding: 10px 12px; border: 1px solid var(--es-border); border-radius: var(--es-radius); background: var(--es-surface); border-left-width: 3px; }
  .card .num { font-family: var(--es-mono); font-size: 20px; font-weight: 600; line-height: 1.1; }
  .card .lbl { font-size: 11px; color: var(--es-muted); font-family: var(--es-mono); }
  .card .hint { font-size: 11px; color: var(--es-muted); }
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

  .inflight { margin: 12px 0 0; padding: 8px 12px; font-size: 12px; border: 1px solid var(--es-accent); border-radius: var(--es-radius); background: var(--es-accent-soft); color: var(--es-text); line-height: 1.7; }
  .inflight.note { border-color: var(--es-border); background: var(--es-surface-2); }

  .matrix-wrap { overflow-x: auto; border: 1px solid var(--es-border); border-radius: var(--es-radius); }
  table.matrix { border-collapse: collapse; font-size: 11px; width: 100%; }
  .matrix th, .matrix td { border: 1px solid var(--es-border); padding: 6px 8px; vertical-align: top; }
  .matrix .corner, .matrix .rowhead { background: var(--es-surface-2); text-align: left; font-weight: 600; white-space: nowrap; }
  .matrix .unassigned-col { background: var(--es-surface-2); }
  .shard { display: inline-block; margin: 1px; padding: 1px 5px; border-radius: 4px; font-family: var(--es-mono); font-size: 10px; border: 1px solid var(--es-border); }
  .shard.inline { font-size: 10px; }
  .shard.started { color: var(--es-green); }
  .shard.initializing { color: var(--es-accent); border-color: var(--es-accent); background: var(--es-accent-soft); }
  .shard.relocating { color: var(--es-yellow); border-color: var(--es-yellow); }
  .shard.other { color: var(--es-muted); }
  .shard.unassigned { color: var(--es-red); border-color: var(--es-red); }
  :global(td.unassigned) { color: var(--es-red); font-weight: 700; }
  :global(td.initializing) { color: var(--es-accent); font-weight: 700; }
  :global(td.relocating) { color: var(--es-yellow); font-weight: 700; }
</style>
