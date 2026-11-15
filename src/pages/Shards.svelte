<script lang="ts">
  import StateBlock from "../components/StateBlock.svelte";
  import DataTable from "../components/DataTable.svelte";
  import PrirepBadge from "../components/PrirepBadge.svelte";
  import { fetchShards, buildTopology, type ShardRow, type TopologyMatrix } from "../lib/services";
  import type { DbxBridge } from "../lib/bridge";
  import type { TableColumn } from "../lib/types";

  interface Props {
    bridge: DbxBridge;
    connectionId: string;
  }
  let { bridge, connectionId }: Props = $props();

  let shards = $state<ShardRow[]>([]);
  let matrix = $state<TopologyMatrix>({ nodes: [], indices: [], cells: {}, unassignedByIndex: {} });
  let loading = $state(true);
  let error = $state("");
  let inflight = false;

  async function load(id: string): Promise<void> {
    if (inflight) return;
    if (!id) {
      shards = [];
      error = "NO_CONNECTION";
      loading = false;
      return;
    }
    inflight = true;
    loading = true;
    error = "";
    try {
      shards = await fetchShards(bridge, id);
      matrix = buildTopology(shards);
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

  const columns: TableColumn[] = [
    { key: "index", label: "Index" },
    { key: "shard", label: "Shard", align: "right", mono: true },
    { key: "prirep", label: "Type", align: "center", mono: true },
    { key: "state", label: "State", mono: true, cellClass: (v) => (String(v) === "UNASSIGNED" ? "unassigned" : "") },
    { key: "docs", label: "Docs", align: "right", mono: true },
    { key: "storeBytes", label: "Store", align: "right", mono: true, render: (v) => formatBytes(Number(v)) },
    { key: "node", label: "Node" },
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

  function cellTags(index: string, node: string): string[] {
    return matrix.cells[index]?.[node] ?? [];
  }
  function unassignedTags(index: string): string[] {
    return matrix.unassignedByIndex[index] ?? [];
  }
  function shardClass(tag: string): string {
    return tag.includes("UNASSIGNED") ? "unassigned" : tag.includes(":STARTED") ? "started" : "other";
  }
</script>

<div class="page">
  <header>
    <div>
      <h1>Shards & Topology</h1>
      <p>Allocation detail from <code>/_cat/shards</code> and a node × shard topology matrix.</p>
    </div>
    <button type="button" onclick={() => load(connectionId)} disabled={loading || !connectionId}>
      {loading ? "Loading…" : "Refresh"}
    </button>
  </header>

  {#if loading && shards.length === 0}
    <StateBlock kind="loading" message="Loading shards…" detail="GET /_cat/shards" />
  {:else if error}
    <StateBlock kind="error" message="Could not load shards" detail={error === "NO_CONNECTION" ? "No DBX connection is selected." : error} />
  {:else if shards.length === 0}
    <StateBlock kind="empty" message="No shards returned" />
  {:else}
    <h2>Topology matrix</h2>
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
                  {#each cellTags(index, node) as tag (tag)}
                    <span class="shard {shardClass(tag)}">{tag}</span>
                  {/each}
                </td>
              {/each}
              <td class="unassigned-col">
                {#each unassignedTags(index) as tag (tag)}
                  <span class="shard unassigned">{tag}</span>
                {/each}
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>

    <h2>Shard allocation</h2>
    <DataTable
      columns={columns}
      rows={shards as unknown as Record<string, unknown>[]}
      rowKey={(r) => `${r.index}-${r.shard}-${r.prirep}`}
    >
    </DataTable>
  {/if}
</div>

<style>
  .page { max-width: 1200px; }
  header { display: flex; justify-content: space-between; align-items: flex-start; gap: 16px; margin-bottom: 18px; }
  h1 { margin: 0 0 4px; font-size: 18px; }
  h2 { margin: 22px 0 10px; font-size: 12px; text-transform: uppercase; letter-spacing: 0.08em; color: var(--es-muted); }
  header p { margin: 0; color: var(--es-muted); }
  header code { font-family: var(--es-mono); font-size: 11px; }
  button { padding: 6px 14px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); cursor: pointer; }
  button:hover:not(:disabled) { background: var(--es-surface-2); }
  button:disabled { opacity: 0.55; cursor: default; }

  .matrix-wrap { overflow-x: auto; border: 1px solid var(--es-border); border-radius: var(--es-radius); }
  table.matrix { border-collapse: collapse; font-size: 11px; width: 100%; }
  .matrix th, .matrix td { border: 1px solid var(--es-border); padding: 6px 8px; vertical-align: top; }
  .matrix .corner, .matrix .rowhead { background: var(--es-surface-2); text-align: left; font-weight: 600; white-space: nowrap; }
  .matrix .unassigned-col { background: var(--es-surface-2); }
  .shard { display: inline-block; margin: 1px; padding: 1px 5px; border-radius: 4px; font-family: var(--es-mono); font-size: 10px; border: 1px solid var(--es-border); }
  .shard.started { color: var(--es-green); }
  .shard.other { color: var(--es-muted); }
  .shard.unassigned { color: var(--es-red); border-color: var(--es-red); }
  :global(td.unassigned) { color: var(--es-red); font-weight: 700; }
</style>
