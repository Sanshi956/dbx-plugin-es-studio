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

  interface Props {
    bridge: DbxBridge;
    connectionId: string;
  }
  let { bridge, connectionId }: Props = $props();

  let rows = $state<ComponentTemplateRow[]>([]);
  let loading = $state(true);
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
    try {
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
      toast = `读取失败：${res.error}`;
      return;
    }
    detail = { name: row.name, body: res.body };
  }

  function askCreate(): void {
    formError = "";
    if (!newName.trim()) {
      formError = "请填写组件模板名";
      return;
    }
    try {
      JSON.parse(draft);
    } catch (cause) {
      formError = `JSON 解析失败：${cause instanceof Error ? cause.message : String(cause)}`;
      return;
    }
    pendingCreate = true;
  }

  async function confirmCreate(): Promise<void> {
    pendingCreate = false;
    const res = await putComponentTemplate(bridge, connectionId, newName.trim(), JSON.parse(draft));
    if (isError(res)) {
      toast = `写入失败：${res.error?.message ?? "HTTP " + res.status}`;
    } else {
      toast = `已写入组件模板 ${newName.trim()}`;
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
      ? `删除失败：${res.error?.message ?? "HTTP " + res.status}`
      : `已删除组件模板 ${target.name}`;
    if (detail?.name === target.name) detail = null;
    await load();
  }
</script>

<div class="page">
  <header>
    <div>
      <h1>Component Templates</h1>
      <p>Reusable building blocks referenced by composable index templates via <code>composed_of</code>.</p>
    </div>
    <button type="button" class="primary" onclick={() => (creating = !creating)}>{creating ? "关闭编辑器" : "新建组件模板"}</button>
  </header>

  {#if toast}<div class="toast">{toast}</div>{/if}
  {#if error}<StateBlock kind="error" message="Failed to read component templates" detail={error} />{/if}

  {#if creating}
    <section class="editor">
      <div class="fields">
        <label>名称<input placeholder="p3-demo-component" bind:value={newName} /></label>
        <button type="button" class="primary" onclick={askCreate}>写入</button>
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
    <label>Filter<input placeholder="按名称过滤" bind:value={filter} /></label>
  </div>

  {#if loading}
    <StateBlock kind="loading" message="Reading /_component_template…" />
  {:else if visible().length === 0}
    <StateBlock kind="empty" message="No component templates" detail="可用「新建组件模板」创建一个" />
  {:else}
    <table>
      <thead><tr><th>Name</th><th>Ver</th><th>settings</th><th>mappings</th><th>aliases</th><th></th></tr></thead>
      <tbody>
        {#each visible() as row (row.name)}
          <tr class:active={detail?.name === row.name}>
            <td class="mono">{row.name}</td>
            <td class="mono">{row.version && row.version < 1_000_000 ? row.version : "-"}</td>
            <td>{row.hasSettings ? "✓" : "-"}</td>
            <td>{row.hasMappings ? "✓" : "-"}</td>
            <td>{row.hasAliases ? "✓" : "-"}</td>
            <td class="act">
              <button type="button" class="mini" onclick={() => view(row)}>View</button>
              <button type="button" class="mini danger" onclick={() => (pendingDelete = row)}>Delete</button>
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
  title="写入组件模板"
  message="组件模板被 composable 索引模板通过 composed_of 引用；删除或改错会让引用它的模板失效，进而影响新索引创建。"
  scope={newName ? `组件模板 ${newName}` : ""}
  confirmLabel="写入组件模板"
  onConfirm={confirmCreate}
  onCancel={() => (pendingCreate = false)}
/>

<ConfirmDanger
  open={pendingDelete !== null}
  title="删除组件模板"
  message="若仍有 composable 索引模板通过 composed_of 引用它，这些模板将无法再创建新索引。"
  scope={pendingDelete ? `组件模板 ${pendingDelete.name}` : ""}
  confirmLabel="删除组件模板"
  onConfirm={confirmDelete}
  onCancel={() => (pendingDelete = null)}
/>

<style>
  .page { max-width: 1000px; }
  header { display: flex; justify-content: space-between; align-items: start; gap: 16px; margin-bottom: 16px; }
  h1 { margin: 0 0 4px; font-size: 18px; }
  header p { margin: 0; color: var(--es-muted); }
  header code { font-family: var(--es-mono); font-size: 11px; }
  button { padding: 6px 14px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); cursor: pointer; white-space: nowrap; }
  button:hover:not(:disabled):not(.primary) { background: var(--es-surface-2); }
  .primary { background: var(--es-accent); color: #fff; border-color: var(--es-accent); }
  .primary:hover:not(:disabled) { filter: brightness(0.92); }
  .mini { padding: 3px 8px; font-size: 11px; }
  .mini.danger { color: var(--es-red); border-color: var(--es-red); }
  .mini.danger:hover { background: var(--es-red); color: #fff; }
  .toast { padding: 8px 12px; margin-bottom: 12px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface-2); font-size: 12px; }
  .editor { border: 1px solid var(--es-border); border-radius: var(--es-radius); padding: 14px; margin-bottom: 14px; background: var(--es-surface); }
  .fields { display: flex; gap: 12px; align-items: end; flex-wrap: wrap; margin-bottom: 10px; }
  .fields label { display: flex; flex-direction: column; gap: 4px; font-size: 11px; color: var(--es-muted); }
  .fields input, .filter-row input { padding: 6px 10px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); }
  textarea { width: 100%; box-sizing: border-box; font-family: var(--es-mono); font-size: 12px; padding: 10px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface-2); color: var(--es-text); }
  .filter-row { margin-bottom: 14px; }
  .filter-row label { display: flex; flex-direction: column; gap: 4px; font-size: 11px; color: var(--es-muted); }
  .form-error { margin: 8px 0 0; font-size: 11px; color: var(--es-red); }
  table { width: 100%; border-collapse: collapse; font-size: 12px; border: 1px solid var(--es-border); border-radius: var(--es-radius); overflow: hidden; }
  th, td { padding: 6px 8px; border-bottom: 1px solid var(--es-border); text-align: left; }
  th { background: var(--es-surface-2); color: var(--es-muted); font-size: 11px; font-weight: 600; }
  .mono { font-family: var(--es-mono); }
  .act { text-align: right; white-space: nowrap; }
  tr.active { background: var(--es-accent-soft); }
</style>
