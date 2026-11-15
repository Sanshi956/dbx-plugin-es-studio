<script lang="ts">
  import StateBlock from "../components/StateBlock.svelte";
  import HealthBadge from "../components/HealthBadge.svelte";
  import ConfirmDanger from "../components/ConfirmDanger.svelte";
  import { fetchIndices, single, isError, type IndexRow } from "../lib/services";
  import type { DbxBridge } from "../lib/bridge";
  import { t } from "../lib/i18n.svelte.js";

  interface Props {
    bridge: DbxBridge;
    connectionId: string;
  }
  let { bridge, connectionId }: Props = $props();

  let indices = $state<IndexRow[]>([]);
  let loading = $state(true);
  let error = $state("");
  let toast = $state("");
  let filter = $state("");
  let inflight = false;

  let pending = $state<{ index: string; method: string; path: string; label: string; scope: string } | null>(null);
  let busy = $state(false);

  async function load(id: string): Promise<void> {
    if (inflight) return;
    if (!id) {
      indices = [];
      error = "NO_CONNECTION";
      loading = false;
      return;
    }
    inflight = true;
    loading = true;
    error = "";
    try {
      indices = await fetchIndices(bridge, id);
    } catch (cause) {
      indices = [];
      error = cause instanceof Error ? cause.message : String(cause);
    } finally {
      inflight = false;
      loading = false;
    }
  }

  $effect(() => {
    void load(connectionId);
  });

  function visible(): IndexRow[] {
    const q = filter.trim().toLowerCase();
    return q ? indices.filter((r) => r.index.toLowerCase().includes(q)) : indices;
  }

  function isClosed(row: IndexRow): boolean {
    return row.status === "close";
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

  function ask(index: string, method: string, path: string, label: string, scope: string): void {
    pending = { index, method, path, label, scope };
  }

  function cancel(): void {
    pending = null;
  }

  async function confirm(): Promise<void> {
    if (!pending) return;
    busy = true;
    toast = "";
    try {
      const res = await single(bridge, connectionId, pending.method, pending.path);
      if (isError(res)) {
        toast = t("index.toast.failed", { action: pending.label, detail: res.error?.message ?? "HTTP " + res.status });
      } else {
        toast = t("index.toast.succeeded", { action: pending.label });
        await load(connectionId);
      }
    } finally {
      busy = false;
      pending = null;
    }
  }

  async function refreshIndex(index: string): Promise<void> {
    toast = "";
    const res = await single(bridge, connectionId, "POST", `/${encodeURIComponent(index)}/_refresh`);
    toast = isError(res)
      ? t("index.toast.refreshFailed", { detail: res.error?.message ?? "HTTP " + res.status })
      : t("index.toast.refreshed", { index });
  }
</script>

<div class="page">
  <header>
    <div>
      <h1>{t("index.title")}</h1>
      <p>{t("index.hintPrefix")}<code>/_cat/indices</code>{t("index.hintSuffix")}</p>
    </div>
    <button type="button" onclick={() => load(connectionId)} disabled={loading || !connectionId}>
      {loading ? t("common.loading") : t("common.refresh")}
    </button>
  </header>

  {#if toast}<div class="toast">{toast}</div>{/if}

  {#if loading && indices.length === 0}
    <StateBlock kind="loading" message={t("index.loadingMessage")} detail="GET /_cat/indices" />
  {:else if error}
    <StateBlock kind="error" message={t("index.loadFailed")} detail={error === "NO_CONNECTION" ? t("common.noConnection") : error} />
  {:else if indices.length === 0}
    <StateBlock kind="empty" message={t("index.empty")} />
  {:else}
    <div class="filter-row">
      <label>{t("index.filterLabel")}<input placeholder={t("index.filterPlaceholder")} bind:value={filter} /></label>
      <span class="count">{visible().length} / {indices.length}</span>
    </div>

    {#if visible().length === 0}
      <StateBlock kind="empty" message={t("index.noMatch")} detail={t("index.noMatchDetail")} />
    {:else}
      <table class="ix">
        <thead>
          <tr>
            <th>{t("index.col.index")}</th>
            <th>{t("index.col.health")}</th>
            <th>{t("index.col.status")}</th>
            <th class="right">{t("index.col.primary")}</th>
            <th class="right">{t("index.col.replicas")}</th>
            <th class="right">{t("index.col.docs")}</th>
            <th class="right">{t("index.col.deleted")}</th>
            <th class="right">{t("index.col.store")}</th>
            <th>{t("index.col.actions")}</th>
          </tr>
        </thead>
        <tbody>
          {#each visible() as row (row.index)}
            <tr>
              <td class="mono">{row.index}</td>
              <td><HealthBadge health={row.health} /></td>
              <td class:closed={isClosed(row)}>{row.status}</td>
              <td class="right mono">{row.primaryShards}</td>
              <td class="right mono">{row.replicaShards}</td>
              <td class="right mono">{row.docsCount}</td>
              <td class="right mono">{row.docsDeleted}</td>
              <td class="right mono">{formatBytes(row.storeBytes)}</td>
              <td class="actions">
                {#if isClosed(row)}
                  <button type="button" class="mini" onclick={() => ask(row.index, "POST", `/${encodeURIComponent(row.index)}/_open`, t("index.act.open"), t("index.scope.index", { index: row.index }))} title={t("index.act.open")}>{t("index.act.open")}</button>
                {:else}
                  <button type="button" class="mini" onclick={() => refreshIndex(row.index)} title={t("index.act.refreshTitle")}>↻</button>
                  <button type="button" class="mini" onclick={() => ask(row.index, "POST", `/${encodeURIComponent(row.index)}/_close`, t("index.act.close"), t("index.scope.index", { index: row.index }))} title={t("index.act.close")}>{t("index.act.close")}</button>
                {/if}
                <button type="button" class="mini danger" onclick={() => ask(row.index, "DELETE", `/${encodeURIComponent(row.index)}`, t("index.act.delete"), t("index.scope.indexData", { index: row.index }))} title={t("index.act.delete")}>{t("index.act.delete")}</button>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  {/if}
</div>

<ConfirmDanger
  open={pending !== null}
  title={pending ? pending.label : ""}
  message={t("index.confirm.message")}
  scope={pending ? pending.scope : ""}
  confirmLabel={pending ? pending.label : t("common.confirm")}
  onConfirm={confirm}
  onCancel={cancel}
/>

<style>
  .page { max-width: 1200px; }
  header { display: flex; justify-content: space-between; align-items: flex-start; gap: 16px; margin-bottom: 18px; }
  h1 { margin: 0 0 4px; font-size: var(--es-fs-3xl); }
  header p { margin: 0; color: var(--es-muted); }
  header code { font-family: var(--es-mono); font-size: var(--es-fs-xs); }
  button { padding: 6px 14px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); cursor: pointer; }
  button:hover:not(:disabled):not(.primary) { background: var(--es-surface-2); }
  button:disabled { opacity: 0.55; cursor: default; }
  .toast { padding: 8px 12px; margin-bottom: 12px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface-2); font-size: var(--es-fs-sm); }
  .filter-row { display: flex; gap: 12px; align-items: end; margin-bottom: 14px; }
  .filter-row label { display: flex; flex-direction: column; gap: 4px; font-size: var(--es-fs-xs); color: var(--es-muted); }
  .filter-row input { padding: 6px 10px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); min-width: 240px; }
  .filter-row .count { font-family: var(--es-mono); font-size: var(--es-fs-xs); color: var(--es-muted); padding-bottom: 8px; }

  table.ix { width: 100%; border-collapse: collapse; font-size: var(--es-fs-sm); border: 1px solid var(--es-border); border-radius: var(--es-radius); overflow: hidden; }
  .ix th, .ix td { padding: 8px 10px; border-bottom: 1px solid var(--es-border); white-space: nowrap; }
  .ix th { background: var(--es-surface-2); color: var(--es-muted); font-weight: 600; text-align: left; }
  .ix th.right, .ix td.right { text-align: right; }
  .ix td.mono { font-family: var(--es-mono); }
  .ix td.closed { color: var(--es-muted); }
  .ix tbody tr:hover { background: var(--es-surface-2); }
  .actions { display: flex; gap: 4px; }
  .mini { padding: 3px 8px; font-size: var(--es-fs-xs); }
  .mini.danger { color: var(--es-red); border-color: var(--es-red); }
  .mini.danger:hover { background: var(--es-red); color: #fff; }
</style>
