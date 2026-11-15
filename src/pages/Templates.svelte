<script lang="ts">
  import StateBlock from "../components/StateBlock.svelte";
  import JsonView from "../components/JsonView.svelte";
  import Inspector from "../components/Inspector.svelte";
  import ConfirmDanger from "../components/ConfirmDanger.svelte";
  import { handleEditorKey } from "../lib/editorKeys";
  import {
    fetchTemplates, getTemplate, putTemplate, deleteTemplate, isError,
    type TemplateRow, type TemplateKind,
  } from "../lib/services";
  import type { DbxBridge } from "../lib/bridge";
  import { t } from "../lib/i18n.svelte.js";

  interface Props {
    bridge: DbxBridge;
    connectionId: string;
  }
  let { bridge, connectionId }: Props = $props();

  let rows = $state<TemplateRow[]>([]);
  let loading = $state(true);
  let error = $state("");
  let toast = $state("");
  let filter = $state("");
  let kindFilter = $state<"all" | TemplateKind>("all");

  let detail = $state<{ name: string; kind: TemplateKind; body: unknown } | null>(null);

  let creating = $state(false);
  let newName = $state("");
  let newKind = $state<TemplateKind>("composable");
  let draft = $state(`{
  "index_patterns": ["p3-demo-*"],
  "priority": 100,
  "template": {
    "settings": { "number_of_shards": 1, "number_of_replicas": 0 },
    "mappings": { "properties": { "message": { "type": "text" } } }
  }
}`);
  let formError = $state("");
  let pendingDelete = $state<TemplateRow | null>(null);
  let pendingCreate = $state(false);

  async function load(): Promise<void> {
    loading = true;
    error = "";
    try {
      rows = await fetchTemplates(bridge, connectionId);
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

  function visible(): TemplateRow[] {
    const q = filter.trim().toLowerCase();
    return rows.filter((r) =>
      (kindFilter === "all" || r.kind === kindFilter) &&
      (q ? r.name.toLowerCase().includes(q) : true));
  }

  async function view(row: TemplateRow): Promise<void> {
    const res = await getTemplate(bridge, connectionId, row.name, row.kind);
    if (!res.ok) {
      toast = t("common.toast.readFailed", { detail: String(res.error) });
      return;
    }
    detail = { name: row.name, kind: row.kind, body: res.body };
  }

  function askCreate(): void {
    formError = "";
    if (!newName.trim()) {
      formError = t("tpl.err.noName");
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
    const res = await putTemplate(bridge, connectionId, newName.trim(), JSON.parse(draft), newKind);
    if (isError(res)) {
      toast = t("common.toast.writeFailed", { detail: res.error?.message ?? "HTTP " + res.status });
    } else {
      toast = t("tpl.toast.written", { kind: newKind, name: newName.trim() });
      creating = false;
      newName = "";
      await load();
    }
  }

  async function confirmDelete(): Promise<void> {
    if (!pendingDelete) return;
    const target = pendingDelete;
    pendingDelete = null;
    const res = await deleteTemplate(bridge, connectionId, target.name, target.kind);
    toast = isError(res)
      ? t("common.toast.deleteFailed", { detail: res.error?.message ?? "HTTP " + res.status })
      : t("tpl.toast.deleted", { kind: target.kind, name: target.name });
    if (detail?.name === target.name) detail = null;
    await load();
  }
</script>

<div class="page">
  <header>
    <div>
      <h1>{t("tpl.title")}</h1>
      <p>{t("tpl.hintPrefix")}<code>_index_template</code>{t("tpl.hintMid")}<code>_template</code>{t("tpl.hintSuffix")}</p>
    </div>
    <button type="button" class="primary" onclick={() => (creating = !creating)}>{creating ? t("tpl.closeEditor") : t("tpl.newTemplate")}</button>
  </header>

  {#if toast}<div class="toast">{toast}</div>{/if}
  {#if error}<StateBlock kind="error" message={t("tpl.failed")} detail={error} />{/if}

  {#if creating}
    <section class="editor">
      <div class="fields">
        <label>{t("tpl.name")}<input placeholder="demo-template" bind:value={newName} /></label>
        <label>{t("tpl.kind")}
          <select bind:value={newKind}>
            <option value="composable">composable (_index_template)</option>
            <option value="legacy">legacy (_template)</option>
          </select>
        </label>
        <button type="button" class="primary" onclick={askCreate}>{t("common.write")}</button>
      </div>
      <textarea
        spellcheck="false"
        rows="12"
        bind:value={draft}
        onkeydown={(event) => handleEditorKey(event, () => draft, (next) => (draft = next))}
      ></textarea>
      {#if formError}<p class="form-error">{formError}</p>{/if}
    </section>
  {/if}

  <div class="filter-row">
    <label>{t("index.filterLabel")}<input placeholder={t("tpl.filterPlaceholder")} bind:value={filter} /></label>
    <label>{t("tpl.kind")}
      <select bind:value={kindFilter}>
        <option value="all">{t("tpl.kindAll")}</option>
        <option value="composable">composable</option>
        <option value="legacy">legacy</option>
      </select>
    </label>
  </div>

  {#if loading}
    <StateBlock kind="loading" message={t("tpl.reading")} />
  {:else if visible().length === 0}
    <StateBlock kind="empty" message={t("tpl.empty")} detail={t("tpl.emptyDetail")} />
  {:else}
    <table>
      <thead><tr><th>{t("tpl.col.name")}</th><th>{t("tpl.col.kind")}</th><th>{t("tpl.col.patterns")}</th><th>{t("tpl.col.priority")}</th><th>{t("tpl.col.composedOf")}</th><th>{t("tpl.col.ver")}</th><th></th></tr></thead>
      <tbody>
        {#each visible() as row (`${row.kind}:${row.name}`)}
          <tr class:active={detail?.name === row.name && detail?.kind === row.kind}>
            <td class="mono">{row.name}</td>
            <td><span class="tag {row.kind}">{row.kind}</span></td>
            <td class="mono small">{row.patterns.join(", ")}</td>
            <td class="mono">{row.priority && row.priority < 1_000_000 ? row.priority : "-"}</td>
            <td class="mono small">{row.composedOf.join(", ") || "-"}</td>
            <td class="mono">{row.version && row.version < 1_000_000 ? row.version : "-"}</td>
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
    subtitle={detail?.kind ?? ""}
    onClose={() => (detail = null)}
  >
    {#if detail}<JsonView value={detail.body} />{/if}
  </Inspector>
</div>

<ConfirmDanger
  open={pendingDelete !== null}
  title={t("tpl.confirm.deleteTitle")}
  message={t("tpl.confirm.deleteMessage")}
  scope={pendingDelete ? t("tpl.confirm.deleteScope", { kind: pendingDelete.kind, name: pendingDelete.name }) : ""}
  confirmLabel={t("tpl.confirm.deleteLabel")}
  onConfirm={confirmDelete}
  onCancel={() => (pendingDelete = null)}
/>

<ConfirmDanger
  open={pendingCreate}
  title={t("tpl.confirm.writeTitle")}
  message={t("tpl.confirm.writeMessage")}
  scope={newName ? t("tpl.confirm.deleteScope", { kind: newKind, name: newName }) : ""}
  confirmLabel={t("tpl.confirm.writeLabel")}
  onConfirm={confirmCreate}
  onCancel={() => (pendingCreate = false)}
/>

<style>
  .page { max-width: 1120px; }
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
  .fields input, .fields select, .filter-row input, .filter-row select { padding: 6px 10px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); }
  textarea { width: 100%; box-sizing: border-box; font-family: var(--es-mono); font-size: var(--es-fs-sm); padding: 10px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface-2); color: var(--es-text); }
  .filter-row { display: flex; gap: 12px; align-items: end; margin-bottom: 14px; }
  .filter-row label { display: flex; flex-direction: column; gap: 4px; font-size: var(--es-fs-xs); color: var(--es-muted); }
  .form-error { margin: 8px 0 0; font-size: var(--es-fs-xs); color: var(--es-red); }
  table { width: 100%; border-collapse: collapse; font-size: var(--es-fs-sm); border: 1px solid var(--es-border); border-radius: var(--es-radius); overflow: hidden; }
  th, td { padding: 6px 8px; border-bottom: 1px solid var(--es-border); text-align: left; }
  th { background: var(--es-surface-2); color: var(--es-muted); font-size: var(--es-fs-xs); font-weight: 600; }
  .mono { font-family: var(--es-mono); }
  .small { font-size: var(--es-fs-xs); color: var(--es-muted); }
  .act { text-align: right; white-space: nowrap; }
  tr.active { background: var(--es-accent-soft); }
  .tag { padding: 1px 6px; border-radius: 4px; font-size: var(--es-fs-2xs); font-family: var(--es-mono); border: 1px solid var(--es-border); }
  .tag.composable { color: var(--es-accent); border-color: var(--es-accent); }
</style>
