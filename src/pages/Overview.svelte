<script lang="ts">
  import StateBlock from "../components/StateBlock.svelte";
  import { fetchOverview, formatBytes, formatCount } from "../lib/api";
  import type { OverviewSnapshot } from "../lib/api";
  import type { DbxBridge } from "../lib/bridge";

  interface Props {
    bridge: DbxBridge;
    connectionId: string;
  }
  let { bridge, connectionId }: Props = $props();

  let data: OverviewSnapshot | null = $state(null);
  let loading = $state(true);
  let error = $state("");
  let inflight = false; // plain value: deliberately NOT reactive, guards overlapping refreshes

  async function load(id: string): Promise<void> {
    if (inflight) return; // requirement 5.2.5: never stack auto-refresh requests
    if (!id) {
      data = null;
      error = "NO_CONNECTION";
      loading = false;
      return;
    }
    inflight = true;
    loading = true;
    error = "";
    try {
      data = await fetchOverview(bridge, id);
    } catch (cause) {
      data = null;
      error = cause instanceof Error ? cause.message : String(cause);
    } finally {
      inflight = false;
      loading = false;
    }
  }

  $effect(() => {
    void load(connectionId);
  });

  interface Metric {
    label: string;
    value: string;
  }

  // Plain functions (instead of $derived) keep TypeScript's narrowing of the
  // nullable $state value intact; Svelte still tracks reads of `data`.
  function clusterMetrics(): Metric[] {
    const snapshot: OverviewSnapshot | null = data;
    if (!snapshot) return [];
    return [
      { label: "Cluster name", value: snapshot.clusterName },
      { label: "Cluster UUID", value: snapshot.clusterUuid },
      { label: "Elasticsearch version", value: snapshot.version },
      { label: "Node name", value: snapshot.nodeName },
      { label: "Nodes / data nodes", value: `${formatCount(snapshot.nodeCount)} / ${formatCount(snapshot.dataNodeCount)}` },
      { label: "Indices", value: formatCount(snapshot.indexCount) },
      { label: "Documents", value: formatCount(snapshot.docCount) },
      { label: "Total storage", value: formatBytes(snapshot.storeBytes) },
    ];
  }

  function shardMetrics(): Metric[] {
    const snapshot: OverviewSnapshot | null = data;
    if (!snapshot) return [];
    return [
      { label: "Active primary shards", value: formatCount(snapshot.activePrimaryShards) },
      { label: "Active shards", value: formatCount(snapshot.activeShards) },
      { label: "Relocating", value: formatCount(snapshot.relocatingShards) },
      { label: "Initializing", value: formatCount(snapshot.initializingShards) },
      { label: "Unassigned", value: formatCount(snapshot.unassignedShards) },
      { label: "Delayed unassigned", value: formatCount(snapshot.delayedUnassignedShards) },
    ];
  }

  function refreshedAt(value: number): string {
    return new Date(value).toLocaleTimeString();
  }
</script>

<div class="overview">
  <header>
    <div>
      <h1>Cluster Overview</h1>
      <p>Live metrics from the selected Elasticsearch cluster.</p>
    </div>
    <button type="button" onclick={() => load(connectionId)} disabled={loading || !connectionId}>
      {loading ? "Refreshing…" : "Refresh"}
    </button>
  </header>

  {#if loading && !data}
    <StateBlock kind="loading" message="Loading cluster metrics…" detail="GET / · /_cluster/health · /_cluster/stats · /_cat/nodes · /_cat/indices" />
  {:else if error}
    <StateBlock
      kind="error"
      message="Could not load cluster metrics"
      detail={error === "NO_CONNECTION"
        ? "No DBX connection is selected. Create and connect an Elasticsearch connection in DBX first."
        : error}
    />
  {:else if data}
    <section class="summary">
      <div class="identity">
        <span class="badge {data.health}">{data.health}</span>
        <div>
          <strong>{data.clusterName}</strong>
          <small>ES {data.version} · refreshed {refreshedAt(data.fetchedAt)}</small>
        </div>
      </div>
      <p class="health-note">
        {#if data.health === "green"}
          All primary and replica shards are allocated. No action required.
        {:else if data.health === "yellow"}
          All primary shards are allocated but {formatCount(data.unassignedShards)} replica shard(s) are unassigned.
        {:else if data.health === "red"}
          {formatCount(data.unassignedShards)} primary shard(s) are unassigned; some index data is unavailable.
        {:else}
          Cluster health is unknown; check the connection and Elasticsearch logs.
        {/if}
        {#if data.relocatingShards > 0}
          {formatCount(data.relocatingShards)} shard(s) are currently relocating.
        {/if}
        {#if data.initializingShards > 0}
          {formatCount(data.initializingShards)} shard(s) are initializing.
        {/if}
      </p>
    </section>

    <h2>Cluster</h2>
    <div class="grid">
      {#each clusterMetrics() as metric (metric.label)}
        <div class="card">
          <span>{metric.label}</span>
          <strong>{metric.value}</strong>
        </div>
      {/each}
    </div>

    <h2>Shards</h2>
    <div class="grid">
      {#each shardMetrics() as metric (metric.label)}
        <div class="card">
          <span>{metric.label}</span>
          <strong>{metric.value}</strong>
        </div>
      {/each}
    </div>

    {#if data.partialErrors.length > 0}
      <div class="partial" role="note">
        <strong>Some optional metrics were unavailable:</strong>
        <ul>
          {#each data.partialErrors as message (message)}<li>{message}</li>{/each}
        </ul>
      </div>
    {/if}
  {:else}
    <StateBlock kind="empty" message="No cluster data" detail="Connect a cluster in DBX to see live metrics." />
  {/if}
</div>

<style>
  .overview { max-width: 1100px; }
  header { display: flex; justify-content: space-between; align-items: flex-start; gap: 16px; margin-bottom: 18px; }
  h1 { margin: 0 0 4px; font-size: 18px; }
  header p { margin: 0; color: var(--es-muted); }
  h2 { margin: 22px 0 10px; font-size: 12px; text-transform: uppercase; letter-spacing: 0.08em; color: var(--es-muted); }
  button {
    padding: 6px 14px;
    border: 1px solid var(--es-border);
    border-radius: 6px;
    background: var(--es-surface);
    color: var(--es-text);
    cursor: pointer;
  }
  button:hover:not(:disabled) { background: var(--es-surface-2); }
  button:disabled { opacity: 0.55; cursor: default; }
  .summary {
    padding: 16px;
    border: 1px solid var(--es-border);
    border-radius: var(--es-radius);
    background: var(--es-surface);
  }
  .identity { display: flex; align-items: center; gap: 12px; }
  .identity strong { display: block; font-size: 15px; }
  .identity small { color: var(--es-muted); }
  .badge {
    padding: 4px 10px;
    border-radius: 999px;
    font-size: 11px;
    font-weight: 700;
    text-transform: uppercase;
    border: 1px solid var(--es-border);
  }
  .badge.green { color: var(--es-green); border-color: var(--es-green); }
  .badge.yellow { color: var(--es-yellow); border-color: var(--es-yellow); }
  .badge.red { color: var(--es-red); border-color: var(--es-red); }
  .health-note { margin: 12px 0 0; color: var(--es-muted); line-height: 1.6; }
  .grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(210px, 1fr)); gap: 10px; }
  .card {
    padding: 12px 14px;
    border: 1px solid var(--es-border);
    border-radius: var(--es-radius);
    background: var(--es-surface);
  }
  .card span { display: block; color: var(--es-muted); font-size: 11px; margin-bottom: 6px; }
  .card strong { font-family: var(--es-mono); font-size: 14px; word-break: break-all; }
  .partial {
    margin-top: 18px;
    padding: 12px 14px;
    border: 1px solid var(--es-border);
    border-radius: var(--es-radius);
    background: var(--es-surface-2);
    color: var(--es-muted);
  }
  .partial ul { margin: 6px 0 0; padding-left: 18px; font-family: var(--es-mono); font-size: 11px; }
</style>
