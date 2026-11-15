<script lang="ts">
  import StateBlock from "../components/StateBlock.svelte";
  import DataTable from "../components/DataTable.svelte";
  import { fetchNodes, type NodeRow } from "../lib/services";
  import type { DbxBridge } from "../lib/bridge";
  import type { TableColumn } from "../lib/types";

  interface Props {
    bridge: DbxBridge;
    connectionId: string;
  }
  let { bridge, connectionId }: Props = $props();

  let nodes = $state<NodeRow[]>([]);
  let loading = $state(true);
  let error = $state("");
  let inflight = false;

  async function load(id: string): Promise<void> {
    if (inflight) return;
    if (!id) {
      nodes = [];
      error = "NO_CONNECTION";
      loading = false;
      return;
    }
    inflight = true;
    loading = true;
    error = "";
    try {
      nodes = await fetchNodes(bridge, id);
    } catch (cause) {
      nodes = [];
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
    { key: "name", label: "Name" },
    { key: "ip", label: "IP", mono: true },
    { key: "roles", label: "Roles", render: (v) => (Array.isArray(v) ? (v as string[]).join(", ") : String(v)) },
    { key: "isMaster", label: "Master", align: "center", render: (v) => (v ? "✓" : "") },
    { key: "version", label: "Version", mono: true },
    { key: "heapPercent", label: "Heap %", align: "right", mono: true, render: (v) => `${v}%` },
    { key: "cpu", label: "CPU %", align: "right", mono: true, render: (v) => `${v}%` },
    { key: "load1m", label: "Load 1m", align: "right", mono: true },
    { key: "ramPercent", label: "RAM %", align: "right", mono: true, render: (v) => `${v}%` },
  ];
</script>

<div class="page">
  <header>
    <div>
      <h1>Nodes</h1>
      <p>Cluster nodes from <code>/_cat/nodes</code> and <code>/_nodes</code>.</p>
    </div>
    <button type="button" onclick={() => load(connectionId)} disabled={loading || !connectionId}>
      {loading ? "Loading…" : "Refresh"}
    </button>
  </header>

  {#if loading && nodes.length === 0}
    <StateBlock kind="loading" message="Loading nodes…" detail="GET /_cat/nodes · /_nodes" />
  {:else if error}
    <StateBlock kind="error" message="Could not load nodes" detail={error === "NO_CONNECTION" ? "No DBX connection is selected." : error} />
  {:else}
    <DataTable
      columns={columns}
      rows={nodes as unknown as Record<string, unknown>[]}
      rowKey={(r) => String(r.id)}
      emptyText="No nodes returned"
    />
  {/if}
</div>

<style>
  .page { max-width: 1100px; }
  header { display: flex; justify-content: space-between; align-items: flex-start; gap: 16px; margin-bottom: 18px; }
  h1 { margin: 0 0 4px; font-size: 18px; }
  header p { margin: 0; color: var(--es-muted); }
  header code { font-family: var(--es-mono); font-size: 11px; }
  button { padding: 6px 14px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); cursor: pointer; }
  button:hover:not(:disabled) { background: var(--es-surface-2); }
  button:disabled { opacity: 0.55; cursor: default; }
</style>
