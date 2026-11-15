<script lang="ts">
  import StateBlock from "../components/StateBlock.svelte";
  import DataTable from "../components/DataTable.svelte";
  import ConfirmDanger from "../components/ConfirmDanger.svelte";
  import {
    fetchAllocation, explainAllocation, setReplicas, setAllocationRule, isError,
    type AllocationRow, type AllocationExplain, type EsResult,
  } from "../lib/services";
  import type { TableColumn } from "../lib/types";
  import type { DbxBridge } from "../lib/bridge";

  interface Props {
    bridge: DbxBridge;
    connectionId: string;
  }
  let { bridge, connectionId }: Props = $props();

  let rows = $state<AllocationRow[]>([]);
  let loading = $state(true);
  let error = $state("");
  let toast = $state("");

  let editing = $state<AllocationRow | null>(null);
  let form = $state({ replicas: 0, include: "", exclude: "", require: "" });
  let pending = $state(false);

  let explain = $state<AllocationExplain | null>(null);
  let explaining = $state(false);

  async function load(): Promise<void> {
    loading = true;
    error = "";
    try {
      rows = await fetchAllocation(bridge, connectionId);
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

  function openEdit(row: AllocationRow): void {
    editing = row;
    form = { replicas: row.replicaShards, include: row.include, exclude: row.exclude, require: row.require };
    toast = "";
  }

  function changes(row: AllocationRow): string[] {
    const out: string[] = [];
    if (form.replicas !== row.replicaShards) out.push(`number_of_replicas ${row.replicaShards} → ${form.replicas}`);
    if (form.include !== row.include) out.push(`include._name "${row.include}" → "${form.include}"`);
    if (form.exclude !== row.exclude) out.push(`exclude._name "${row.exclude}" → "${form.exclude}"`);
    if (form.require !== row.require) out.push(`require._name "${row.require}" → "${form.require}"`);
    return out;
  }

  async function save(): Promise<void> {
    if (!editing) return;
    const target = editing;
    pending = false;
    const calls: Promise<EsResult>[] = [];
    if (form.replicas !== target.replicaShards) {
      calls.push(setReplicas(bridge, connectionId, target.index, form.replicas));
    }
    if (form.include !== target.include) calls.push(setAllocationRule(bridge, connectionId, target.index, "include", form.include));
    if (form.exclude !== target.exclude) calls.push(setAllocationRule(bridge, connectionId, target.index, "exclude", form.exclude));
    if (form.require !== target.require) calls.push(setAllocationRule(bridge, connectionId, target.index, "require", form.require));
    const results = await Promise.all(calls);
    const failed = results.filter((r) => isError(r));
    editing = null;
    if (failed.length) {
      toast = `失败：${failed.map((r) => r.error?.message ?? "HTTP " + r.status).join("；")}`;
    } else {
      toast = `已更新 ${target.index} 的分配设置`;
    }
    await load();
  }

  async function runExplain(): Promise<void> {
    explaining = true;
    try {
      explain = await explainAllocation(bridge, connectionId);
    } finally {
      explaining = false;
    }
  }

  const columns: TableColumn[] = [
    { key: "index", label: "Index", mono: true },
    { key: "primaryShards", label: "Pri", align: "right" },
    { key: "replicaShards", label: "Rep", align: "right" },
    { key: "status", label: "Status", render: (v) => (String(v) === "open" ? "open" : "closed") },
    { key: "include", label: "include._name", mono: true },
    { key: "exclude", label: "exclude._name", mono: true },
    { key: "require", label: "require._name", mono: true },
  ];
</script>

<div class="page">
  <header>
    <div>
      <h1>Allocation</h1>
      <p>Per-index shard allocation settings plus the real reason unassigned shards cannot be placed.</p>
    </div>
    <button type="button" onclick={runExplain} disabled={explaining}>
      {explaining ? "Explaining…" : "Explain unassigned shard"}
    </button>
  </header>

  {#if toast}<div class="toast">{toast}</div>{/if}
  {#if error}<StateBlock kind="error" message="Failed to read allocation settings" detail={error} />{/if}

  {#if explain}
    <section class="explain">
      {#if explain.ok}
        <h2>{explain.index} · shard {explain.shard}{explain.primary ? " (primary)" : " (replica)"}</h2>
        <p class="verdict"><span class={explain.canAllocate === "yes" ? "ok" : "bad"}>{explain.canAllocate}</span> {explain.reason}</p>
        {#if explain.nodes.length}
          <table class="mini">
            <thead><tr><th>Node</th><th>Decision</th><th>Reason</th></tr></thead>
            <tbody>
              {#each explain.nodes as d (d.node)}
                <tr><td class="mono">{d.node}</td><td>{d.decision}</td><td class="reason">{d.reason}</td></tr>
              {/each}
            </tbody>
          </table>
        {/if}
        {#if explain.note}<p class="note">{explain.note}</p>{/if}
      {:else}
        <p class="note">无法解释：{explain.error}</p>
      {/if}
    </section>
  {/if}

  {#if editing}
    <section class="editor">
      <h2>编辑 <code>{editing.index}</code></h2>
      <div class="fields">
        <label>number_of_replicas<input type="number" min="0" max="10" bind:value={form.replicas} /></label>
        <label>include._name<input placeholder="留空 = 移除规则" bind:value={form.include} /></label>
        <label>exclude._name<input placeholder="留空 = 移除规则" bind:value={form.exclude} /></label>
        <label>require._name<input placeholder="留空 = 移除规则" bind:value={form.require} /></label>
      </div>
      <div class="actions">
        <span class="diff">{changes(editing).join("；") || "无变更"}</span>
        <button type="button" onclick={() => (editing = null)}>取消</button>
        <button type="button" class="primary" disabled={changes(editing).length === 0} onclick={() => (pending = true)}>应用</button>
      </div>
    </section>
  {/if}

  {#if loading}
    <StateBlock kind="loading" message="Reading /_all/_settings…" />
  {:else if rows.length === 0}
    <StateBlock kind="empty" message="No indices" />
  {:else}
    <DataTable
      columns={columns}
      rows={rows as unknown as Record<string, unknown>[]}
      rowKey={(r) => String(r.index)}
    />
    <div class="row-actions">
      {#each rows as row (row.index)}
        <button type="button" class="mini" onclick={() => openEdit(row)}>编辑 {row.index}</button>
      {/each}
    </div>
  {/if}
</div>

<ConfirmDanger
  open={pending && editing !== null}
  title="修改索引分配设置"
  message="写入 index.number_of_replicas / index.routing.allocation.* 会立即触发分片重新分配，可能造成短暂的副本缺失与数据搬迁。"
  scope={editing ? `${editing.index}：${changes(editing).join("；")}` : ""}
  confirmLabel="应用设置"
  onConfirm={save}
  onCancel={() => (pending = false)}
/>

<style>
  .page { max-width: 1080px; }
  header { display: flex; justify-content: space-between; align-items: start; gap: 16px; margin-bottom: 16px; }
  h1 { margin: 0 0 4px; font-size: 18px; }
  header p { margin: 0; color: var(--es-muted); }
  button { padding: 6px 14px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); cursor: pointer; white-space: nowrap; }
  button:hover:not(:disabled):not(.primary) { background: var(--es-surface-2); }
  .primary { background: var(--es-accent); color: #fff; border-color: var(--es-accent); }
  .primary:hover:not(:disabled) { filter: brightness(0.92); }
  button:disabled { opacity: 0.55; cursor: default; }
  .mini { padding: 3px 8px; font-size: 11px; }
  .toast { padding: 8px 12px; margin-bottom: 12px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface-2); font-size: 12px; }
  .explain, .editor { border: 1px solid var(--es-border); border-radius: var(--es-radius); padding: 14px; margin-bottom: 16px; background: var(--es-surface); }
  .explain h2, .editor h2 { margin: 0 0 8px; font-size: 13px; }
  .verdict { margin: 0 0 10px; font-size: 12px; }
  .ok { color: var(--es-green); font-weight: 700; }
  .bad { color: var(--es-red); font-weight: 700; }
  table.mini { width: 100%; border-collapse: collapse; font-size: 11px; }
  table.mini th, table.mini td { padding: 5px 8px; border-bottom: 1px solid var(--es-border); text-align: left; }
  .reason { white-space: normal; color: var(--es-muted); }
  .note { margin: 8px 0 0; font-size: 11px; color: var(--es-muted); line-height: 1.6; }
  .fields { display: flex; gap: 12px; flex-wrap: wrap; margin-bottom: 10px; }
  .fields label { display: flex; flex-direction: column; gap: 4px; font-size: 11px; color: var(--es-muted); }
  .fields input { padding: 6px 10px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); min-width: 180px; }
  .actions { display: flex; align-items: center; gap: 10px; justify-content: flex-end; }
  .diff { margin-right: auto; font-size: 11px; color: var(--es-muted); font-family: var(--es-mono); }
  .row-actions { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 10px; }
</style>
