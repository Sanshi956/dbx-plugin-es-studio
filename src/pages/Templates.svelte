<script lang="ts">
  import StateBlock from "../components/StateBlock.svelte";
  import JsonView from "../components/JsonView.svelte";
  import ConfirmDanger from "../components/ConfirmDanger.svelte";
  import {
    fetchTemplates, getTemplate, putTemplate, deleteTemplate, isError,
    type TemplateRow, type TemplateKind,
  } from "../lib/services";
  import type { DbxBridge } from "../lib/bridge";

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
      toast = `读取失败：${res.error}`;
      return;
    }
    detail = { name: row.name, kind: row.kind, body: res.body };
  }

  function askCreate(): void {
    formError = "";
    if (!newName.trim()) {
      formError = "请填写模板名";
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
    const res = await putTemplate(bridge, connectionId, newName.trim(), JSON.parse(draft), newKind);
    if (isError(res)) {
      toast = `写入失败：${res.error?.message ?? "HTTP " + res.status}`;
    } else {
      toast = `已写入 ${newKind} 模板 ${newName.trim()}`;
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
      ? `删除失败：${res.error?.message ?? "HTTP " + res.status}`
      : `已删除 ${target.kind} 模板 ${target.name}`;
    if (detail?.name === target.name) detail = null;
    await load();
  }
</script>

<div class="page">
  <header>
    <div>
      <h1>Templates</h1>
      <p>Composable <code>_index_template</code> and legacy <code>_template</code>, read from the real API.</p>
    </div>
    <button type="button" class="primary" onclick={() => (creating = !creating)}>{creating ? "关闭编辑器" : "新建模板"}</button>
  </header>

  {#if toast}<div class="toast">{toast}</div>{/if}
  {#if error}<StateBlock kind="error" message="Failed to read templates" detail={error} />{/if}

  {#if creating}
    <section class="editor">
      <div class="fields">
        <label>模板名<input placeholder="p3-demo-template" bind:value={newName} /></label>
        <label>类型
          <select bind:value={newKind}>
            <option value="composable">composable (_index_template)</option>
            <option value="legacy">legacy (_template)</option>
          </select>
        </label>
        <button type="button" class="primary" onclick={askCreate}>写入</button>
      </div>
      <textarea spellcheck="false" bind:value={draft} rows="12"></textarea>
      {#if formError}<p class="form-error">{formError}</p>{/if}
    </section>
  {/if}

  <div class="filter-row">
    <label>Filter<input placeholder="按名称过滤" bind:value={filter} /></label>
    <label>Kind
      <select bind:value={kindFilter}>
        <option value="all">all</option>
        <option value="composable">composable</option>
        <option value="legacy">legacy</option>
      </select>
    </label>
  </div>

  {#if loading}
    <StateBlock kind="loading" message="Reading /_index_template and /_template…" />
  {:else if visible().length === 0}
    <StateBlock kind="empty" message="No templates match" detail="可用「新建模板」创建一个 composable 模板" />
  {:else}
    <table>
      <thead><tr><th>Name</th><th>Kind</th><th>Patterns</th><th>Priority</th><th>Composed of</th><th>Ver</th><th></th></tr></thead>
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
              <button type="button" class="mini" onclick={() => view(row)}>View</button>
              <button type="button" class="mini danger" onclick={() => (pendingDelete = row)}>Delete</button>
            </td>
          </tr>
        {/each}
      </tbody>
    </table>
  {/if}

  {#if detail}
    <section class="detail">
      <h2>{detail.name} <span class="tag {detail.kind}">{detail.kind}</span></h2>
      <JsonView value={detail.body} />
    </section>
  {/if}
</div>

<ConfirmDanger
  open={pendingDelete !== null}
  title="删除索引模板"
  message="删除模板不会影响已存在的索引，但此后新建的匹配索引将不再继承该模板的 settings / mappings / aliases。"
  scope={pendingDelete ? `${pendingDelete.kind} 模板 ${pendingDelete.name}` : ""}
  confirmLabel="删除模板"
  onConfirm={confirmDelete}
  onCancel={() => (pendingDelete = null)}
/>

<ConfirmDanger
  open={pendingCreate}
  title="写入索引模板"
  message="模板会被后续匹配 index_patterns 的新索引自动继承；参数错误会导致新索引创建失败。"
  scope={newName ? `${newKind} 模板 ${newName}` : ""}
  confirmLabel="写入模板"
  onConfirm={confirmCreate}
  onCancel={() => (pendingCreate = false)}
/>

<style>
  .page { max-width: 1120px; }
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
  .editor, .detail { border: 1px solid var(--es-border); border-radius: var(--es-radius); padding: 14px; margin-bottom: 14px; background: var(--es-surface); }
  .detail h2 { margin: 0 0 10px; font-size: 13px; display: flex; gap: 8px; align-items: center; }
  .fields { display: flex; gap: 12px; align-items: end; flex-wrap: wrap; margin-bottom: 10px; }
  .fields label { display: flex; flex-direction: column; gap: 4px; font-size: 11px; color: var(--es-muted); }
  .fields input, .fields select, .filter-row input, .filter-row select { padding: 6px 10px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); }
  textarea { width: 100%; box-sizing: border-box; font-family: var(--es-mono); font-size: 12px; padding: 10px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface-2); color: var(--es-text); }
  .filter-row { display: flex; gap: 12px; align-items: end; margin-bottom: 14px; }
  .filter-row label { display: flex; flex-direction: column; gap: 4px; font-size: 11px; color: var(--es-muted); }
  .form-error { margin: 8px 0 0; font-size: 11px; color: var(--es-red); }
  table { width: 100%; border-collapse: collapse; font-size: 12px; border: 1px solid var(--es-border); border-radius: var(--es-radius); overflow: hidden; }
  th, td { padding: 6px 8px; border-bottom: 1px solid var(--es-border); text-align: left; }
  th { background: var(--es-surface-2); color: var(--es-muted); font-size: 11px; font-weight: 600; }
  .mono { font-family: var(--es-mono); }
  .small { font-size: 11px; color: var(--es-muted); }
  .act { text-align: right; white-space: nowrap; }
  tr.active { background: var(--es-accent-soft); }
  .tag { padding: 1px 6px; border-radius: 4px; font-size: 10px; font-family: var(--es-mono); border: 1px solid var(--es-border); }
  .tag.composable { color: var(--es-accent); border-color: var(--es-accent); }
</style>
