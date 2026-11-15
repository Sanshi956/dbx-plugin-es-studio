<script lang="ts">
  import StateBlock from "../components/StateBlock.svelte";
  import ConfirmDanger from "../components/ConfirmDanger.svelte";
  import { fetchTasks, cancelTask, isError, type TaskRow } from "../lib/services";
  import type { DbxBridge } from "../lib/bridge";
  import { t } from "../lib/i18n.svelte.js";

  interface Props {
    bridge: DbxBridge;
    connectionId: string;
  }
  let { bridge, connectionId }: Props = $props();

  let tasks = $state<TaskRow[]>([]);
  let loading = $state(true);
  let error = $state("");
  let toast = $state("");
  let filter = $state("");
  let longOnly = $state(false);
  let pendingCancel = $state<TaskRow | null>(null);

  async function load(): Promise<void> {
    loading = true;
    error = "";
    try {
      tasks = await fetchTasks(bridge, connectionId);
    } catch (cause) {
      error = cause instanceof Error ? cause.message : String(cause);
      tasks = [];
    } finally {
      loading = false;
    }
  }
  $effect(() => {
    void load();
  });

  function visible(): TaskRow[] {
    const q = filter.trim().toLowerCase();
    return tasks.filter((t) => {
      if (longOnly && t.runningNanos < 1_000_000_000) return false;
      if (!q) return true;
      return t.action.toLowerCase().includes(q) || t.taskId.toLowerCase().includes(q);
    });
  }

  function secs(nanos: number): string {
    return (nanos / 1_000_000_000).toFixed(2);
  }

  async function confirmCancel(): Promise<void> {
    if (!pendingCancel) return;
    const target = pendingCancel;
    pendingCancel = null;
    const res = await cancelTask(bridge, connectionId, target.taskId);
    toast = isError(res)
      ? t("tasks.toast.cancelFailed", { detail: res.error?.message ?? "HTTP " + res.status })
      : t("tasks.toast.cancelled", { action: target.action, id: target.taskId });
    await load();
  }
</script>

<div class="page">
  <header>
    <div>
      <h1>{t("tasks.title")}</h1>
      <p>{t("tasks.hintPrefix")}<code>GET /_tasks?group_by=none&amp;detailed=true</code>{t("tasks.hintSuffix")}</p>
    </div>
    <button type="button" onclick={load} disabled={loading}>{loading ? t("common.loading") : t("common.refresh")}</button>
  </header>

  {#if toast}<div class="toast">{toast}</div>{/if}
  {#if error}<StateBlock kind="error" message={t("tasks.failed")} detail={error} />{/if}

  <div class="filter-row">
    <label>{t("index.filterLabel")}<input placeholder={t("tasks.filterPlaceholder")} bind:value={filter} /></label>
    <label class="check"><input type="checkbox" bind:checked={longOnly} /> {t("tasks.longOnly")}</label>
  </div>

  {#if loading && tasks.length === 0}
    <StateBlock kind="loading" message={t("tasks.reading")} />
  {:else if visible().length === 0}
    <StateBlock kind="empty" message={t("tasks.empty")} detail={t("tasks.emptyDetail")} />
  {:else}
    <table>
      <thead><tr><th>{t("tasks.col.action")}</th><th>{t("tasks.col.type")}</th><th>{t("tasks.col.node")}</th><th>{t("tasks.col.running")}</th><th>{t("tasks.col.started")}</th><th>{t("tasks.col.cancellable")}</th><th>{t("tasks.col.taskId")}</th><th></th></tr></thead>
      <tbody>
        {#each visible() as task (task.taskId)}
          <tr>
            <td class="mono">{task.action}</td>
            <td class="small">{task.type}</td>
            <td class="small">{task.node}</td>
            <td class="mono">{secs(task.runningNanos)}</td>
            <td class="mono small">{task.startTime ? new Date(task.startTime).toLocaleTimeString() : "-"}</td>
            <td>{task.cancellable ? "yes" : "no"}</td>
            <td class="mono small">{task.taskId}</td>
            <td class="act">
              <button type="button" class="mini danger" disabled={!task.cancellable} onclick={() => (pendingCancel = task)}>{t("tasks.cancel")}</button>
            </td>
          </tr>
        {/each}
      </tbody>
    </table>
  {/if}
</div>

<ConfirmDanger
  open={pendingCancel !== null}
  title={t("tasks.confirm.title")}
  message={t("tasks.confirm.message")}
  scope={pendingCancel
    ? t("tasks.confirm.scope", {
        action: pendingCancel.action,
        id: pendingCancel.taskId,
        secs: secs(pendingCancel.runningNanos),
      })
    : ""}
  confirmLabel={t("tasks.confirm.label")}
  onConfirm={confirmCancel}
  onCancel={() => (pendingCancel = null)}
/>

<style>
  .page { max-width: 1140px; }
  header { display: flex; justify-content: space-between; align-items: start; gap: 16px; margin-bottom: 16px; }
  h1 { margin: 0 0 4px; font-size: var(--es-fs-3xl); }
  header p { margin: 0; color: var(--es-muted); }
  header code { font-family: var(--es-mono); font-size: var(--es-fs-xs); }
  button { padding: 6px 14px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); cursor: pointer; white-space: nowrap; }
  button:hover:not(:disabled) { background: var(--es-surface-2); }
  button:disabled { opacity: 0.55; cursor: default; }
  .mini { padding: 3px 8px; font-size: var(--es-fs-xs); }
  .mini.danger { color: var(--es-red); border-color: var(--es-red); }
  .mini.danger:hover:not(:disabled) { background: var(--es-red); color: #fff; }
  .toast { padding: 8px 12px; margin-bottom: 12px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface-2); font-size: var(--es-fs-sm); }
  .filter-row { display: flex; gap: 14px; align-items: end; margin-bottom: 14px; flex-wrap: wrap; }
  .filter-row label { display: flex; flex-direction: column; gap: 4px; font-size: var(--es-fs-xs); color: var(--es-muted); }
  .filter-row .check { flex-direction: row; align-items: center; gap: 6px; padding-bottom: 6px; }
  .filter-row input:not([type]) { padding: 6px 10px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); }
  table { width: 100%; border-collapse: collapse; font-size: var(--es-fs-sm); border: 1px solid var(--es-border); border-radius: var(--es-radius); overflow: hidden; }
  th, td { padding: 6px 8px; border-bottom: 1px solid var(--es-border); text-align: left; }
  th { background: var(--es-surface-2); color: var(--es-muted); font-size: var(--es-fs-xs); font-weight: 600; }
  .mono { font-family: var(--es-mono); }
  .small { font-size: var(--es-fs-xs); color: var(--es-muted); }
  .act { text-align: right; white-space: nowrap; }
</style>
