<script lang="ts">
  import StateBlock from "../components/StateBlock.svelte";
  import JsonView from "../components/JsonView.svelte";
  import Inspector from "../components/Inspector.svelte";
  import ConfirmDanger from "../components/ConfirmDanger.svelte";
  import Combobox from "../components/Combobox.svelte";
  import { handleEditorKey } from "../lib/editorKeys";
  import {
    fetchTemplates, getTemplate, putTemplate, deleteTemplate, isError, failureText,
    normalizeLegacyTemplate, type TemplateRow, type TemplateKind,
  } from "../lib/services";
  import { clusterCapabilities, type Capabilities } from "../lib/capabilities";
  import type { DbxBridge } from "../lib/bridge";
  import { t } from "../lib/i18n.svelte.js";

  interface Props {
    bridge: DbxBridge;
    connectionId: string;
  }
  let { bridge, connectionId }: Props = $props();

  const COMPOSABLE_DRAFT = `{
  "index_patterns": ["p3-demo-*"],
  "priority": 100,
  "template": {
    "settings": { "number_of_shards": 1, "number_of_replicas": 0 },
    "mappings": { "properties": { "message": { "type": "text" } } }
  }
}`;
  // Legacy bodies are flat (no `template` wrapper) and use `order`, not
  // `priority`; the mapping type layer follows the typeless switch, so 6.x needs
  // the `_doc` wrapper or the PUT answers 400.
  const LEGACY_DRAFT = `{
  "index_patterns": ["legacy-demo-*"],
  "order": 100,
  "settings": { "number_of_shards": 1, "number_of_replicas": 0 },
  "mappings": { "properties": { "message": { "type": "text" } } }
}`;
  const LEGACY_TYPED_DRAFT = `{
  "index_patterns": ["legacy-demo-*"],
  "order": 100,
  "settings": { "number_of_shards": 1, "number_of_replicas": 0 },
  "mappings": { "_doc": { "properties": { "message": { "type": "text" } } } }
}`;

  let rows = $state<TemplateRow[]>([]);
  let loading = $state(true);
  let error = $state("");
  let toast = $state("");
  let filter = $state("");
  let kindFilter = $state<"all" | TemplateKind>("all");
  let caps = $state<Capabilities | null>(null);
  let composableSupported = $state(true);
  let legacySupported = $state(true);

  let detail = $state<{ name: string; kind: TemplateKind; body: unknown } | null>(null);

  let creating = $state(false);
  let newName = $state("");
  let newKind = $state<TemplateKind>("composable");
  let draft = $state(COMPOSABLE_DRAFT);
  let drafts: Record<TemplateKind, string> = { composable: COMPOSABLE_DRAFT, legacy: LEGACY_DRAFT };
  let formError = $state("");
  let pendingDelete = $state<TemplateRow | null>(null);
  let pendingCreate = $state(false);
  let pendingBody = $state<Record<string, unknown> | null>(null);
  let pendingChanges = $state<string[]>([]);

  function defaultDraft(kind: TemplateKind): string {
    if (kind === "composable") return COMPOSABLE_DRAFT;
    return caps?.legacyTypedMappings ? LEGACY_TYPED_DRAFT : LEGACY_DRAFT;
  }

  function switchKind(kind: TemplateKind): void {
    if (kind === newKind) return;
    drafts[newKind] = draft;
    newKind = kind;
    draft = drafts[kind] ?? defaultDraft(kind);
  }

  async function load(): Promise<void> {
    loading = true;
    error = "";
    try {
      caps = await clusterCapabilities(bridge, connectionId);
      const listing = await fetchTemplates(bridge, connectionId);
      rows = listing.rows;
      composableSupported = listing.composableSupported;
      legacySupported = listing.legacySupported;
      // The 6.x draft must carry the `_doc` layer from the start, otherwise the
      // first create attempt fails on a body the page itself suggested.
      if (caps?.legacyTypedMappings) {
        if (drafts.legacy === LEGACY_DRAFT) drafts.legacy = LEGACY_TYPED_DRAFT;
        if (draft === LEGACY_DRAFT) draft = LEGACY_TYPED_DRAFT;
      }
      if (!composableSupported && newKind === "composable") {
        switchKind("legacy");
      }
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
    let parsed: Record<string, unknown>;
    try {
      parsed = JSON.parse(draft) as Record<string, unknown>;
    } catch (cause) {
      formError = t("common.jsonParseFailed", { detail: cause instanceof Error ? cause.message : String(cause) });
      return;
    }
    if (newKind === "legacy") {
      // Rewrite composable-shaped bodies (and the wrong mapping layout for this
      // cluster's typeless switch) instead of letting ES answer a bare 400.
      const normalized = normalizeLegacyTemplate(parsed, caps?.legacyTypedMappings ?? false);
      pendingBody = normalized.body;
      pendingChanges = normalized.changes;
    } else {
      pendingBody = parsed;
      pendingChanges = [];
    }
    pendingCreate = true;
  }

  async function confirmCreate(): Promise<void> {
    pendingCreate = false;
    const payload = pendingBody ?? {};
    const res = await putTemplate(bridge, connectionId, newName.trim(), payload, newKind);
    if (isError(res)) {
      toast = t("common.toast.writeFailed", { detail: failureText(res) });
      return;
    }
    if (pendingChanges.length > 0) {
      const applied = pendingChanges.map((key) => t(key)).join("；");
      toast = t("tpl.toast.writtenNormalized", { kind: newKind, name: newName.trim(), changes: applied });
    } else {
      toast = t("tpl.toast.written", { kind: newKind, name: newName.trim() });
    }
    creating = false;
    newName = "";
    pendingBody = null;
    pendingChanges = [];
    await load();
  }

  async function confirmDelete(): Promise<void> {
    if (!pendingDelete) return;
    const target = pendingDelete;
    pendingDelete = null;
    const res = await deleteTemplate(bridge, connectionId, target.name, target.kind);
    toast = isError(res)
      ? t("common.toast.deleteFailed", { detail: failureText(res) })
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
  {#if !composableSupported}
    <p class="gate">{caps?.version
      ? t("tpl.gate.composable", { version: caps.version })
      : t("tpl.gate.composableNoVersion")}</p>
  {/if}
  {#if !legacySupported}
    <p class="gate">{caps?.version
      ? t("tpl.gate.legacy", { version: caps.version })
      : t("tpl.gate.legacyNoVersion")}</p>
  {/if}

  {#if creating}
    <section class="editor">
      <div class="fields">
        <label>{t("tpl.name")}<input placeholder="demo-template" bind:value={newName} /></label>
        <label>{t("tpl.kind")}
          <Combobox
            listId="tpl-kinds"
            options={[
              ...(composableSupported ? [{ value: "composable", label: "composable (_index_template)" }] : []),
              { value: "legacy", label: "legacy (_template)" },
            ]}
            value={newKind}
            onValueChange={(v) => switchKind(v as TemplateKind)}
            searchable={false}
          />
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
      <Combobox
        listId="tpl-kind-filter"
        options={[
          { value: "all", label: t("tpl.kindAll") },
          { value: "composable", label: "composable" },
          { value: "legacy", label: "legacy" },
        ]}
        value={kindFilter}
        onValueChange={(v) => (kindFilter = v as "all" | TemplateKind)}
        searchable={false}
      />
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
  .gate { margin: 0 0 12px; padding: 8px 12px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface-2); color: var(--es-muted); font-size: var(--es-fs-sm); }
  .editor { border: 1px solid var(--es-border); border-radius: var(--es-radius); padding: 14px; margin-bottom: 14px; background: var(--es-surface); }
  .fields { display: flex; gap: 12px; align-items: end; flex-wrap: wrap; margin-bottom: 10px; }
  .fields label { display: flex; flex-direction: column; gap: 4px; font-size: var(--es-fs-xs); color: var(--es-muted); }
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
