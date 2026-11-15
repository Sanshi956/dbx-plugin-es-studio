<script lang="ts">
  import StateBlock from "../components/StateBlock.svelte";
  import JsonView from "../components/JsonView.svelte";
  import Inspector from "../components/Inspector.svelte";
  import ConfirmDanger from "../components/ConfirmDanger.svelte";
  import { handleEditorKey } from "../lib/editorKeys";
  import {
    fetchComponentTemplates, getComponentTemplate, putComponentTemplate, deleteComponentTemplate, isError,
    type ComponentTemplateRow,
  } from "../lib/services";
  import type { DbxBridge } from "../lib/bridge";
  import { t } from "../lib/i18n.svelte.js";
  import { clusterCapabilities } from "../lib/capabilities";

  interface Props {
    bridge: DbxBridge;
    connectionId: string;
  }
  let { bridge, connectionId }: Props = $props();

  let rows = $state<ComponentTemplateRow[]>([]);
  let loading = $state(true);
  /** Set when the cluster is too old for this API — an empty table would lie. */
  let unsupported = $state("");
  let error = $state("");
  let toast = $state("");
  let filter = $state("");
  let detail = $state<{ name: string; body: unknown } | null>(null);

  let creating = $state(false);
  let newName = $state("");
  let draft = $state(`{
  "template": {
    "settings": { "index.refresh_interval": "5s" },
    "mappings": { "properties": { "host": { "type": "keyword" } } }
  }
}`);
  let formError = $state("");
  let pendingCreate = $state(false);
  let pendingDelete = $state<ComponentTemplateRow | null>(null);

  async function load(): Promise<void> {
    loading = true;
    error = "";
    unsupported = "";
    try {
      const caps = await clusterCapabilities(bridge, connectionId);
      if (caps && !caps.componentTemplates) {
        unsupported = t("ct.unsupported", { version: caps.version });
        rows = [];
        return;
      }
      rows = await fetchComponentTemplates(bridge, connectionId);
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

  function visible(): ComponentTemplateRow[] {
    const q = filter.trim().toLowerCase();
    return rows.filter((r) => (q ? r.name.toLowerCase().includes(q) : true));
  }

  async function view(row: ComponentTemplateRow): Promise<void> {
    const res = await getComponentTemplate(bridge, connectionId, row.name);
    if (!res.ok) {
      toast = t("common.toast.readFailed", { detail: String(res.error) });
      return;
    }
    detail = { name: row.name, body: res.body };
  }

  function askCreate(): void {
    formError = "";
    if (!newName.trim()) {
      formError = t("ct.err.noName");
      return;
    }
    try {
      JSON.parse(draft);
    } catch (cause) {
      formError = t("common.jsonParseFailed", { detail: cause instanceof Error ? cause.message : String(cause) });
      return;
    }
    pendingCreate = true;
  }

  async function confirmCreate(): Promise<void> {
    pendingCreate = false;
    const res = await putComponentTemplate(bridge, connectionId, newName.trim(), JSON.parse(draft));
    if (isError(res)) {
      toast = t("common.toast.writeFailed", { detail: res.error?.message ?? "HTTP " + res.status });
    } else {
      toast = t("ct.toast.written", { name: newName.trim() });
      creating = false;
      newName = "";
      await load();
    }
  }

  async function confirmDelete(): Promise<void> {
    if (!pendingDelete) return;
    const target = pendingDelete;
    pendingDelete = null;
    const res = await deleteComponentTemplate(bridge, connectionId, target.name);
    toast = isError(res)
      ? t("common.toast.deleteFailed", { detail: res.error?.message ?? "HTTP " + res.status })
      : t("ct.toast.deleted", { name: target.name });
    if (detail?.name === target.name) detail = null;
    await load();
  }
</script>

<div class="page">
  <header>
    <div>
      <h1>{t("ct.title")}</h1>
      <p>{t("ct.hintPrefix")}<code>composed_of</code>{t("ct.hintSuffix")}</p>
    </div>
    <button type="button" class="primary" onclick={() => (creating = !creating)}>{creating ? t("tpl.closeEditor") : t("ct.newComponent")}</button>
  </header>

  {#if toast}<div class="toast">{toast}</div>{/if}
  {#if error}<StateBlock kind="error" message={t("ct.failed")} detail={error} />
  {:else if unsupported}<StateBlock kind="empty" message={t("ct.unsupportedTitle")} detail={unsupported} />{/if}

  {#if creating}
    <section class="editor">
      <div class="fields">
        <label>{t("ct.name")}<input placeholder="demo-component" bind:value={newName} /></label>
        <button type="button" class="primary" onclick={askCreate}>{t("common.write")}</button>
      </div>
      <textarea
        spellcheck="false"
        rows="10"
        bind:value={draft}
        onkeydown={(event) => handleEditorKey(event, () => draft, (next) => (draft = next))}
      ></textarea>
      {#if formError}<p class="form-error">{formError}</p>{/if}
    </section>
  {/if}

  <div class="filter-row">
    <label>{t("index.filterLabel")}<input placeholder={t("tpl.filterPlaceholder")} bind:value={filter} /></label>
  </div>

  {#if loading}
    <StateBlock kind="loading" message={t("ct.reading")} />
  {:else if visible().length === 0}
    <StateBlock kind="empty" message={t("ct.empty")} detail={t("ct.emptyDetail")} />
  {:else}
    <table>
      <thead><tr><th>{t("ct.col.name")}</th><th>{t("ct.col.ver")}</th><th>settings</th><th>mappings</th><th>aliases</th><th></th></tr></thead>
      <tbody>
        {#each visible() as row (row.name)}
          <tr class:active={detail?.name === row.name}>
            <td class="mono">{row.name}</td>
            <td class="mono">{row.version && row.version < 1_000_000 ? row.version : "-"}</td>
            <td>{row.hasSettings ? "✓" : "-"}</td>
            <td>{row.hasMappings ? "✓" : "-"}</td>
            <td>{row.hasAliases ? "✓" : "-"}</td>
            <td class="act">
              <button type="button" class="mini" onclick={() => view(row)}>{t("common.view")}</button>
              <button type="button" class="mini danger" onclick={() => (pendingDelete = row)}>{t("common.delete")}</button>
            </td>
          </tr>
        {/each}
      </tbody>
    </table>
  {/if}

  <Inspector
    open={detail !== null}
    title={detail?.name ?? ""}
    onClose={() => (detail = null)}
  >
    {#if detail}<JsonView value={detail.body} />{/if}
  </Inspector>
</div>

<ConfirmDanger
  open={pendingCreate}
  title={t("ct.confirm.writeTitle")}
  message={t("ct.confirm.writeMessage")}
  scope={newName ? t("ct.confirm.scope", { name: newName }) : ""}
  confirmLabel={t("ct.confirm.writeLabel")}
  onConfirm={confirmCreate}
  onCancel={() => (pendingCreate = false)}
/>

<ConfirmDanger
  open={pendingDelete !== null}
  title={t("ct.confirm.deleteTitle")}
  message={t("ct.confirm.deleteMessage")}
  scope={pendingDelete ? t("ct.confirm.scope", { name: pendingDelete.name }) : ""}
  confirmLabel={t("ct.confirm.deleteLabel")}
  onConfirm={confirmDelete}
  onCancel={() => (pendingDelete = null)}
/>

<style>
  .page { max-width: 1000px; }
  header { display: flex; justify-content: space-between; align-items: start; gap: 16px; margin-bottom: 16px; }
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
  .editor { border: 1px solid var(--es-border); border-radius: var(--es-radius); padding: 14px; margin-bottom: 14px; background: var(--es-surface); }
  .fields { display: flex; gap: 12px; align-items: end; flex-wrap: wrap; margin-bottom: 10px; }
  .fields label { display: flex; flex-direction: column; gap: 4px; font-size: var(--es-fs-xs); color: var(--es-muted); }
  .fields input, .filter-row input { padding: 6px 10px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); }
  textarea { width: 100%; box-sizing: border-box; font-family: var(--es-mono); font-size: var(--es-fs-sm); padding: 10px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface-2); color: var(--es-text); }
  .filter-row { margin-bottom: 14px; }
  .filter-row label { display: flex; flex-direction: column; gap: 4px; font-size: var(--es-fs-xs); color: var(--es-muted); }
  .form-error { margin: 8px 0 0; font-size: var(--es-fs-xs); color: var(--es-red); }
  table { width: 100%; border-collapse: collapse; font-size: var(--es-fs-sm); border: 1px solid var(--es-border); border-radius: var(--es-radius); overflow: hidden; }
  th, td { padding: 6px 8px; border-bottom: 1px solid var(--es-border); text-align: left; }
  th { background: var(--es-surface-2); color: var(--es-muted); font-size: var(--es-fs-xs); font-weight: 600; }
  .mono { font-family: var(--es-mono); }
  .act { text-align: right; white-space: nowrap; }
  tr.active { background: var(--es-accent-soft); }
</style>
