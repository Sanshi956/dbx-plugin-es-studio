<script lang="ts">
  import StateBlock from "../components/StateBlock.svelte";
  import ConfirmDanger from "../components/ConfirmDanger.svelte";
  import {
    fetchDataStreams, rolloverDataStream, deleteDataStream, createDataStream, isError,
    type DataStreamRow,
  } from "../lib/services";
  import type { DbxBridge } from "../lib/bridge";

  interface Props {
    bridge: DbxBridge;
    connectionId: string;
  }
  let { bridge, connectionId }: Props = $props();

  let rows = $state<DataStreamRow[]>([]);
  let loading = $state(true);
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
    try {
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
      formError = "请填写数据流名";
      return;
    }
    if (!/^[a-z0-9][a-z0-9_.-]*$/.test(name)) {
      formError = "数据流名只能是小写字母、数字、点、下划线或连字符，且不能以 _ - + 开头";
      return;
    }
    pendingCreate = true;
  }

  async function confirmCreate(): Promise<void> {
    pendingCreate = false;
    const res = await createDataStream(bridge, connectionId, newName.trim());
    if (isError(res)) {
      toast = `创建失败：${res.error?.message ?? "HTTP " + res.status}`;
    } else {
      toast = `已创建数据流 ${newName.trim()}`;
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
      ? `rollover 失败：${res.error?.message ?? "HTTP " + res.status}`
      : `已对 ${target.name} 执行 rollover（写入新后备索引）`;
    await load();
  }

  async function confirmDelete(): Promise<void> {
    if (!pendingDelete) return;
    const target = pendingDelete;
    pendingDelete = null;
    const res = await deleteDataStream(bridge, connectionId, target.name);
    toast = isError(res)
      ? `删除失败：${res.error?.message ?? "HTTP " + res.status}`
      : `已删除数据流 ${target.name}（含 ${target.indices.length} 个后备索引）`;
    await load();
  }
</script>

<div class="page">
  <header>
    <div>
      <h1>Data Streams</h1>
      <p>Append-only streams backed by hidden indices, from <code>GET /_data_stream</code>.</p>
    </div>
  </header>

  {#if toast}<div class="toast">{toast}</div>{/if}
  {#if error}<StateBlock kind="error" message="Failed to read data streams" detail={error} />{/if}

  <section class="creator">
    <div class="fields">
      <label>新建数据流<input placeholder="logs-p3-demo" bind:value={newName} /></label>
      <button type="button" class="primary" onclick={askCreate}>创建</button>
    </div>
    {#if formError}<p class="form-error">{formError}</p>{/if}
    <p class="hint">数据流通常由带 <code>data_stream</code> 的索引模板自动创建；手工创建需要先有匹配的模板，否则会被拒绝。</p>
  </section>

  {#if loading}
    <StateBlock kind="loading" message="Reading /_data_stream…" />
  {:else if rows.length === 0}
    <StateBlock kind="empty" message="No data streams" detail="当前集群没有数据流；可用上面的「创建」或写入带 data_stream 的索引模板" />
  {:else}
    <table>
      <thead><tr><th>Name</th><th>Status</th><th>Gen</th><th>Template</th><th>ILM</th><th>Backing indices</th><th></th></tr></thead>
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
              <button type="button" class="mini danger" onclick={() => (pendingDelete = row)}>Delete</button>
            </td>
          </tr>
        {/each}
      </tbody>
    </table>
  {/if}
</div>

<ConfirmDanger
  open={pendingCreate}
  title="创建数据流"
  message="创建数据流会立即生成一个隐藏的后备索引（.ds-*），并需要匹配的索引模板存在。"
  scope={newName ? `数据流 ${newName}` : ""}
  confirmLabel="创建数据流"
  onConfirm={confirmCreate}
  onCancel={() => (pendingCreate = false)}
/>

<ConfirmDanger
  open={pendingRollover !== null}
  title="数据流 rollover"
  message="rollover 会创建新的后备索引并把写入切过去，旧索引变为只读；若已有 ILM 策略可能同时触发删除阶段。"
  scope={pendingRollover ? `数据流 ${pendingRollover.name}（当前 ${pendingRollover.indices.length} 个后备索引）` : ""}
  confirmLabel="执行 rollover"
  onConfirm={confirmRollover}
  onCancel={() => (pendingRollover = null)}
/>

<ConfirmDanger
  open={pendingDelete !== null}
  title="删除数据流"
  message="删除数据流会同时删除它背后的全部后备索引，数据不可恢复。"
  scope={pendingDelete ? `数据流 ${pendingDelete.name} 及 ${pendingDelete.indices.length} 个索引（${pendingDelete.indices.join(", ")}）` : ""}
  confirmLabel="删除数据流"
  onConfirm={confirmDelete}
  onCancel={() => (pendingDelete = null)}
/>

<style>
  .page { max-width: 1120px; }
  header { margin-bottom: 16px; }
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
  .creator { border: 1px solid var(--es-border); border-radius: var(--es-radius); padding: 14px; margin-bottom: 16px; background: var(--es-surface); }
  .fields { display: flex; gap: 12px; align-items: end; flex-wrap: wrap; }
  .fields label { display: flex; flex-direction: column; gap: 4px; font-size: 11px; color: var(--es-muted); }
  .fields input { padding: 6px 10px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); min-width: 240px; }
  .hint { margin: 8px 0 0; font-size: 11px; color: var(--es-muted); }
  .hint code { font-family: var(--es-mono); }
  .form-error { margin: 8px 0 0; font-size: 11px; color: var(--es-red); }
  table { width: 100%; border-collapse: collapse; font-size: 12px; border: 1px solid var(--es-border); border-radius: var(--es-radius); overflow: hidden; }
  th, td { padding: 6px 8px; border-bottom: 1px solid var(--es-border); text-align: left; }
  th { background: var(--es-surface-2); color: var(--es-muted); font-size: 11px; font-weight: 600; }
  .mono { font-family: var(--es-mono); }
  .small { font-size: 11px; }
  .act { text-align: right; white-space: nowrap; }
  .state { font-size: 11px; color: var(--es-muted); }
  .state.YELLOW { color: var(--es-amber, #b45309); }
  .state.RED { color: var(--es-red); }
</style>
