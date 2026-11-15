<script lang="ts">
  import StateBlock from "../components/StateBlock.svelte";
  import DataTable from "../components/DataTable.svelte";
  import { fetchNodes, type NodeRow } from "../lib/services";
  import type { DbxBridge } from "../lib/bridge";
  import type { TableColumn } from "../lib/types";
  import { t } from "../lib/i18n.svelte.js";

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
    { key: "name", label: "nodes.col.name" },
    { key: "ip", label: "nodes.col.ip", mono: true },
    { key: "roles", label: "nodes.col.roles", render: (v) => (Array.isArray(v) ? (v as string[]).join(", ") : String(v)) },
    { key: "isMaster", label: "nodes.col.master", align: "center", render: (v) => (v ? "✓" : "") },
    { key: "version", label: "nodes.col.version", mono: true },
    { key: "heapPercent", label: "nodes.col.heap", align: "right", mono: true, render: (v) => `${v}%` },
    { key: "cpu", label: "nodes.col.cpu", align: "right", mono: true, render: (v) => `${v}%` },
    { key: "load1m", label: "nodes.col.load", align: "right", mono: true },
    { key: "ramPercent", label: "nodes.col.ram", align: "right", mono: true, render: (v) => `${v}%` },
  ];
</script>

<div class="page">
  <header>
    <div>
      <h1>{t("nodes.title")}</h1>
      <p>{t("nodes.hintPrefix")}<code>/_cat/nodes</code>{t("nodes.hintMid")}<code>/_nodes</code>{t("nodes.hintSuffix")}</p>
    </div>
    <button type="button" onclick={() => load(connectionId)} disabled={loading || !connectionId}>
      {loading ? t("common.loading") : t("common.refresh")}
    </button>
  </header>

  {#if loading && nodes.length === 0}
    <StateBlock kind="loading" message={t("nodes.loadingMessage")} detail="GET /_cat/nodes · /_nodes" />
  {:else if error}
    <StateBlock kind="error" message={t("nodes.loadFailed")} detail={error === "NO_CONNECTION" ? t("common.noConnection") : error} />
  {:else}
    <DataTable
      columns={columns}
      rows={nodes as unknown as Record<string, unknown>[]}
      rowKey={(r) => String(r.id)}
      emptyText={t("nodes.empty")}
    />
  {/if}
</div>

<style>
  .page { max-width: 1100px; }
  header { display: flex; justify-content: space-between; align-items: flex-start; gap: 16px; margin-bottom: 18px; }
  h1 { margin: 0 0 4px; font-size: var(--es-fs-3xl); }
  header p { margin: 0; color: var(--es-muted); }
  header code { font-family: var(--es-mono); font-size: var(--es-fs-xs); }
  button { padding: 6px 14px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); cursor: pointer; }
  button:hover:not(:disabled):not(.primary) { background: var(--es-surface-2); }
  button:disabled { opacity: 0.55; cursor: default; }
</style>
