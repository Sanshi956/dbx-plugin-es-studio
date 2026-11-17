<script lang="ts">
  import StateBlock from "../components/StateBlock.svelte";
  import { fetchOverview, formatBytes, formatCount } from "../lib/api";
  import type { OverviewSnapshot } from "../lib/api";
  import type { DbxBridge } from "../lib/bridge";
  import { t } from "../lib/i18n.svelte.js";

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
      { label: t("overview.metric.clusterName"), value: snapshot.clusterName },
      { label: t("overview.metric.clusterUuid"), value: snapshot.clusterUuid },
      { label: t("overview.metric.version"), value: snapshot.version },
      { label: t("overview.metric.nodeName"), value: snapshot.nodeName },
      { label: t("overview.metric.nodes"), value: `${formatCount(snapshot.nodeCount)} / ${formatCount(snapshot.dataNodeCount)}` },
      { label: t("overview.metric.indices"), value: formatCount(snapshot.indexCount) },
      { label: t("overview.metric.documents"), value: formatCount(snapshot.docCount) },
      { label: t("overview.metric.storage"), value: formatBytes(snapshot.storeBytes) },
    ];
  }

  function shardMetrics(): Metric[] {
    const snapshot: OverviewSnapshot | null = data;
    if (!snapshot) return [];
    return [
      { label: t("overview.metric.activePrimary"), value: formatCount(snapshot.activePrimaryShards) },
      { label: t("overview.metric.activeShards"), value: formatCount(snapshot.activeShards) },
      { label: t("overview.metric.relocating"), value: formatCount(snapshot.relocatingShards) },
      { label: t("overview.metric.initializing"), value: formatCount(snapshot.initializingShards) },
      { label: t("overview.metric.unassigned"), value: formatCount(snapshot.unassignedShards) },
      { label: t("overview.metric.delayedUnassigned"), value: formatCount(snapshot.delayedUnassignedShards) },
    ];
  }

  function refreshedAt(value: number): string {
    return new Date(value).toLocaleTimeString();
  }
</script>

<div class="overview">
  <header>
    <div>
      <h1>{t("overview.title")}</h1>
      <p>{t("overview.hint")}</p>
    </div>
    <button type="button" onclick={() => load(connectionId)} disabled={loading || !connectionId}>
      {loading ? t("common.refreshing") : t("common.refresh")}
    </button>
  </header>

  {#if loading && !data}
    <StateBlock kind="loading" message={t("overview.loadingMessage")} detail="GET / · /_cluster/health · /_cluster/stats · /_cat/nodes" />
  {:else if error}
    <StateBlock
      kind="error"
      message={t("overview.loadFailed")}
      detail={error === "NO_CONNECTION" ? t("overview.noConnectionDetail") : error}
    />
  {:else if data}
    <section class="summary">
      <div class="identity">
        <span class="badge {data.health}">{data.health}</span>
        <div>
          <strong>{data.clusterName}</strong>
          <small>{t("overview.refreshedAt", { version: data.version, time: refreshedAt(data.fetchedAt) })}</small>
        </div>
      </div>
      <p class="health-note">
        {#if data.health === "green"}
          {t("overview.health.green")}
        {:else if data.health === "yellow"}
          {t("overview.health.yellow", { count: formatCount(data.unassignedShards) })}
        {:else if data.health === "red"}
          {t("overview.health.red", { count: formatCount(data.unassignedShards) })}
        {:else}
          {t("overview.health.unknown")}
        {/if}
        {#if data.relocatingShards > 0}
          {t("overview.health.relocating", { count: formatCount(data.relocatingShards) })}
        {/if}
        {#if data.initializingShards > 0}
          {t("overview.health.initializing", { count: formatCount(data.initializingShards) })}
        {/if}
      </p>
    </section>

    <h2>{t("overview.section.cluster")}</h2>
    <div class="grid">
      {#each clusterMetrics() as metric (metric.label)}
        <div class="card">
          <span>{metric.label}</span>
          <strong>{metric.value}</strong>
        </div>
      {/each}
    </div>

    <h2>{t("overview.section.shards")}</h2>
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
        <strong>{t("overview.partial")}</strong>
        <ul>
          {#each data.partialErrors as message (message)}<li>{message}</li>{/each}
        </ul>
      </div>
    {/if}
  {:else}
    <StateBlock kind="empty" message={t("overview.empty.title")} detail={t("overview.empty.detail")} />
  {/if}
</div>

<style>
  .overview { max-width: 1100px; }
  header { display: flex; justify-content: space-between; align-items: flex-start; gap: 16px; margin-bottom: 18px; }
  h1 { margin: 0 0 4px; font-size: var(--es-fs-3xl); }
  header p { margin: 0; color: var(--es-muted); }
  h2 { margin: 22px 0 10px; font-size: var(--es-fs-sm); text-transform: uppercase; letter-spacing: 0.08em; color: var(--es-muted); }
  button {
    padding: 6px 14px;
    border: 1px solid var(--es-border);
    border-radius: 6px;
    background: var(--es-surface);
    color: var(--es-text);
    cursor: pointer;
  }
  button:hover:not(:disabled):not(.primary) { background: var(--es-surface-2); }
  .primary:hover:not(:disabled) { filter: brightness(0.92); }
  button:disabled { opacity: 0.55; cursor: default; }
  .summary {
    padding: 16px;
    border: 1px solid var(--es-border);
    border-radius: var(--es-radius);
    background: var(--es-surface);
  }
  .identity { display: flex; align-items: center; gap: 12px; }
  .identity strong { display: block; font-size: var(--es-fs-xl); }
  .identity small { color: var(--es-muted); }
  .badge {
    padding: 4px 10px;
    border-radius: 999px;
    font-size: var(--es-fs-xs);
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
  .card span { display: block; color: var(--es-muted); font-size: var(--es-fs-xs); margin-bottom: 6px; }
  .card strong { font-family: var(--es-mono); font-size: var(--es-fs-lg); word-break: break-all; }
  .partial {
    margin-top: 18px;
    padding: 12px 14px;
    border: 1px solid var(--es-border);
    border-radius: var(--es-radius);
    background: var(--es-surface-2);
    color: var(--es-muted);
  }
  .partial ul { margin: 6px 0 0; padding-left: 18px; font-family: var(--es-mono); font-size: var(--es-fs-xs); }
</style>
