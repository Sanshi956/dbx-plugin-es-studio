<script lang="ts">
  import StateBlock from "../components/StateBlock.svelte";
  import ConfirmDanger from "../components/ConfirmDanger.svelte";
  import {
    fetchDataStreams, rolloverDataStream, deleteDataStream, createDataStream, isError,
    type DataStreamRow,
  } from "../lib/services";
  import type { DbxBridge } from "../lib/bridge";
  import { t } from "../lib/i18n.svelte.js";
  import { clusterCapabilities } from "../lib/capabilities";

  interface Props {
    bridge: DbxBridge;
    connectionId: string;
  }
  let { bridge, connectionId }: Props = $props();

  let rows = $state<DataStreamRow[]>([]);
  let loading = $state(true);
  /** Set when the cluster is too old for this API — an empty table would lie. */
  let unsupported = $state("");
  let error = $state("");
  let toast = $state("");
  let newName = $state("");
  let formError = $state("");

  let pendingRollover = $state<DataStreamRow | null>(null);
  let pendingDelete = $state<DataStreamRow | null>(null);
  let pendingCreate = $state(false);

  async function load(): Promise<void> {
    loading = true;
    error = "";
    unsupported = "";
    try {
      const caps = await clusterCapabilities(bridge, connectionId);
      if (caps && !caps.dataStreams) {
        unsupported = t("ds.unsupported", { version: caps.version });
        rows = [];
        return;
      }
      rows = await fetchDataStreams(bridge, connectionId);
    } catch (cause) {
      error = cause instanceof Error ? cause.message : String(cause);
      rows = [];
    } finally {
      loading = false;
    }
  }
  $effect(() => {
    void load();
  });

  function askCreate(): void {
    formError = "";
    const name = newName.trim();
    if (!name) {
      formError = t("ds.err.noName");
      return;
    }
    if (!/^[a-z0-9][a-z0-9_.-]*$/.test(name)) {
      formError = t("ds.err.nameCharset");
      return;
    }
    pendingCreate = true;
  }

  async function confirmCreate(): Promise<void> {
    pendingCreate = false;
    const res = await createDataStream(bridge, connectionId, newName.trim());
    if (isError(res)) {
      toast = t("ds.toast.createFailed", { detail: res.error?.message ?? "HTTP " + res.status });
    } else {
      toast = t("ds.toast.created", { name: newName.trim() });
      newName = "";
      await load();
    }
  }

  async function confirmRollover(): Promise<void> {
    if (!pendingRollover) return;
    const target = pendingRollover;
    pendingRollover = null;
    const res = await rolloverDataStream(bridge, connectionId, target.name);
    toast = isError(res)
      ? t("ds.toast.rolloverFailed", { detail: res.error?.message ?? "HTTP " + res.status })
      : t("ds.toast.rolledOver", { name: target.name });
    await load();
  }

  async function confirmDelete(): Promise<void> {
    if (!pendingDelete) return;
    const target = pendingDelete;
    pendingDelete = null;
    const res = await deleteDataStream(bridge, connectionId, target.name);
    toast = isError(res)
      ? t("common.toast.deleteFailed", { detail: res.error?.message ?? "HTTP " + res.status })
      : t("ds.toast.deleted", { name: target.name, count: target.indices.length });
    await load();
  }
</script>

<div class="page">
  <header>
    <div>
      <h1>{t("ds.title")}</h1>
      <p>{t("ds.hintPrefix")}<code>GET /_data_stream</code>{t("ds.hintSuffix")}</p>
    </div>
  </header>

  {#if toast}<div class="toast">{toast}</div>{/if}
  {#if error}<StateBlock kind="error" message={t("ds.failed")} detail={error} />
  {:else if unsupported}<StateBlock kind="empty" message={t("ds.unsupportedTitle")} detail={unsupported} />{/if}

  {#if !unsupported}
  <section class="creator">
    <div class="fields">
      <label>{t("ds.newName")}<input placeholder="logs-demo" bind:value={newName} /></label>
      <button type="button" class="primary" onclick={askCreate}>{t("ds.create")}</button>
    </div>
    {#if formError}<p class="form-error">{formError}</p>{/if}
    <p class="hint">{t("ds.hint")}</p>
  </section>
  {/if}

  {#if loading}
    <StateBlock kind="loading" message={t("ds.reading")} />
  {:else if rows.length === 0}
    <StateBlock kind="empty" message={t("ds.empty")} detail={t("ds.emptyDetail")} />
  {:else}
    <table>
      <thead><tr><th>{t("ds.col.name")}</th><th>{t("ds.col.status")}</th><th>{t("ds.col.gen")}</th><th>{t("ds.col.template")}</th><th>{t("ds.col.ilm")}</th><th>{t("ds.col.backing")}</th><th></th></tr></thead>
      <tbody>
        {#each rows as row (row.name)}
          <tr>
            <td class="mono">{row.name}</td>
            <td><span class="state {row.status}">{row.status || "-"}</span></td>
            <td class="mono">{row.generation}</td>
            <td class="mono small">{row.template || "-"}</td>
            <td class="mono small">{row.ilmPolicy || "-"}</td>
            <td class="mono small">{row.indices.join(", ") || "-"}</td>
            <td class="act">
              <button type="button" class="mini" onclick={() => (pendingRollover = row)}>Rollover</button>
              <button type="button" class="mini danger" onclick={() => (pendingDelete = row)}>{t("common.delete")}</button>
            </td>
          </tr>
        {/each}
      </tbody>
    </table>
  {/if}
</div>

<ConfirmDanger
  open={pendingCreate}
  title={t("ds.confirm.createTitle")}
  message={t("ds.confirm.createMessage")}
  scope={newName ? t("ds.confirm.scope", { name: newName }) : ""}
  confirmLabel={t("ds.confirm.createLabel")}
  onConfirm={confirmCreate}
  onCancel={() => (pendingCreate = false)}
/>

<ConfirmDanger
  open={pendingRollover !== null}
  title={t("ds.confirm.rolloverTitle")}
  message={t("ds.confirm.rolloverMessage")}
  scope={pendingRollover ? t("ds.confirm.rolloverScope", { name: pendingRollover.name, count: pendingRollover.indices.length }) : ""}
  confirmLabel={t("ds.confirm.rolloverLabel")}
  onConfirm={confirmRollover}
  onCancel={() => (pendingRollover = null)}
/>

<ConfirmDanger
  open={pendingDelete !== null}
  title={t("ds.confirm.deleteTitle")}
  message={t("ds.confirm.deleteMessage")}
  scope={pendingDelete
    ? t("ds.confirm.deleteScope", {
        name: pendingDelete.name,
        count: pendingDelete.indices.length,
        indices: pendingDelete.indices.join(", "),
      })
    : ""}
  confirmLabel={t("ds.confirm.deleteLabel")}
  onConfirm={confirmDelete}
  onCancel={() => (pendingDelete = null)}
/>

<style>
  .page { max-width: 1120px; }
  header { margin-bottom: 16px; }
  h1 { margin: 0 0 4px; font-size: var(--es-fs-3xl); }
  header p { margin: 0; color: var(--es-muted); }
  header code { font-family: var(--es-mono); font-size: var(--es-fs-xs); }
  button { padding: 6px 14px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); cursor: pointer; white-space: nowrap; }
  button:hover:not(:disabled):not(.primary) { background: var(--es-surface-2); }
  .primary { background: var(--es-accent); color: #fff; border-color: var(--es-accent); }
  .primary:hover:not(:disabled) { filter: brightness(0.92); }
  .mini { padding: 3px 8px; font-size: var(--es-fs-xs); }
  .mini.danger { color: var(--es-red); border-color: var(--es-red); }
  .mini.danger:hover { background: var(--es-red); color: #fff; }
  .toast { padding: 8px 12px; margin-bottom: 12px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface-2); font-size: var(--es-fs-sm); }
  .creator { border: 1px solid var(--es-border); border-radius: var(--es-radius); padding: 14px; margin-bottom: 16px; background: var(--es-surface); }
  .fields { display: flex; gap: 12px; align-items: end; flex-wrap: wrap; }
  .fields label { display: flex; flex-direction: column; gap: 4px; font-size: var(--es-fs-xs); color: var(--es-muted); }
  .fields input { min-width: 240px; }
  .hint { margin: 8px 0 0; font-size: var(--es-fs-xs); color: var(--es-muted); }
  .form-error { margin: 8px 0 0; font-size: var(--es-fs-xs); color: var(--es-red); }
  table { width: 100%; border-collapse: collapse; font-size: var(--es-fs-sm); border: 1px solid var(--es-border); border-radius: var(--es-radius); overflow: hidden; }
  th, td { padding: 6px 8px; border-bottom: 1px solid var(--es-border); text-align: left; }
  th { background: var(--es-surface-2); color: var(--es-muted); font-size: var(--es-fs-xs); font-weight: 600; }
  .mono { font-family: var(--es-mono); }
  .small { font-size: var(--es-fs-xs); }
  .act { text-align: right; white-space: nowrap; }
  .state { font-size: var(--es-fs-xs); color: var(--es-muted); }
  .state.YELLOW { color: var(--es-amber, #b45309); }
  .state.RED { color: var(--es-red); }
</style>
