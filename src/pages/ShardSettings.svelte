<script lang="ts">
  import { untrack } from "svelte";
  import StateBlock from "../components/StateBlock.svelte";
  import ConfirmDanger from "../components/ConfirmDanger.svelte";
  import {
    fetchAllocation, setReplicas, setAllocationRule, isError,
    type AllocationRow, type EsResult,
  } from "../lib/services";
  import type { DbxBridge } from "../lib/bridge";
  import { t } from "../lib/i18n.svelte.js";

  interface Props {
    bridge: DbxBridge;
    connectionId: string;
  }
  let { bridge, connectionId }: Props = $props();

  let allocRows = $state<AllocationRow[]>([]);
  let loading = $state(true);
  let error = $state("");
  let inflight = false;
  let indexFilter = $state("");
  let editing = $state<AllocationRow | null>(null);
  let form = $state({ replicas: 0, include: "", exclude: "", require: "" });
  let pending = $state(false);
  let toast = $state("");

  const query = $derived(indexFilter.trim().toLowerCase());
  const filtered = $derived(
    query ? allocRows.filter((r) => r.index.toLowerCase().includes(query)) : allocRows,
  );

  async function load(id: string): Promise<void> {
    if (inflight) return;
    if (!id) {
      allocRows = [];
      error = "NO_CONNECTION";
      loading = false;
      return;
    }
    inflight = true;
    loading = true;
    error = "";
    try {
      allocRows = await fetchAllocation(bridge, id);
    } catch (cause) {
      allocRows = [];
      error = cause instanceof Error ? cause.message : String(cause);
    } finally {
      inflight = false;
      loading = false;
    }
  }

  // untrack: load() 同步读写 inflight/loading，被 effect 追踪会无限循环。
  $effect(() => {
    untrack(() => void load(connectionId));
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
      toast = t("common.toast.failed", { detail: failed.map((r) => r.error?.message ?? "HTTP " + r.status).join("; ") });
    } else {
      toast = t("alloc.toast.updated", { index: target.index });
    }
    await load(connectionId);
  }
</script>

<div class="page">
  <header>
    <div>
      <h1>{t("settings.title")}</h1>
      <p>{t("settings.hint")}</p>
    </div>
    <div class="head-actions">
      <input
        class="filter"
        type="search"
        placeholder={t("settings.filter")}
        bind:value={indexFilter}
      />
      <button type="button" onclick={() => load(connectionId)} disabled={loading || !connectionId}>
        {loading ? t("common.loading") : t("common.refresh")}
      </button>
    </div>
  </header>

  {#if toast}<div class="toast">{toast}</div>{/if}
  {#if loading && allocRows.length === 0}
    <StateBlock kind="loading" message={t("alloc.reading")} detail="GET /_all/_settings" />
  {:else if error}
    <StateBlock kind="error" message={t("alloc.failed")} detail={error === "NO_CONNECTION" ? t("common.noConnection") : error} />
  {:else if filtered.length === 0}
    {#if query}
      <StateBlock kind="empty" message={t("shards.noMatch", { filter: indexFilter })} />
    {:else}
      <StateBlock kind="empty" message={t("alloc.empty")} />
    {/if}
  {:else}
    {#if editing}
      <section class="editor">
        <h3>{t("alloc.editHeading")} <code>{editing.index}</code></h3>
        <div class="fields">
          <label>number_of_replicas<input type="number" min="0" max="10" bind:value={form.replicas} /></label>
          <label>include._name<input placeholder={t("alloc.rulePlaceholder")} bind:value={form.include} /></label>
          <label>exclude._name<input placeholder={t("alloc.rulePlaceholder")} bind:value={form.exclude} /></label>
          <label>require._name<input placeholder={t("alloc.rulePlaceholder")} bind:value={form.require} /></label>
        </div>
        <div class="actions">
          <span class="diff">{changes(editing).join("; ") || t("alloc.noChange")}</span>
          <button type="button" onclick={() => (editing = null)}>{t("common.cancel")}</button>
          <button type="button" class="primary" disabled={changes(editing).length === 0} onclick={() => (pending = true)}>{t("common.apply")}</button>
        </div>
      </section>
    {/if}

    <div class="table-wrap">
      <table>
        <thead>
          <tr>
            <th>{t("index.col.index")}</th>
            <th class="right">{t("alloc.col.pri")}</th>
            <th class="right">{t("alloc.col.rep")}</th>
            <th title={t("alloc.statusTip")}>{t("alloc.col.status")}</th>
            <th class="mono">include._name</th>
            <th class="mono">exclude._name</th>
            <th class="mono">require._name</th>
            <th class="right">{t("alloc.col.actions")}</th>
          </tr>
        </thead>
        <tbody>
          {#each filtered as row (row.index)}
            <tr>
              <td class="mono">{row.index}</td>
              <td class="right mono">{row.primaryShards}</td>
              <td class="right mono">{row.replicaShards}</td>
              <td>{row.status}</td>
              <td class="mono">{row.include || "—"}</td>
              <td class="mono">{row.exclude || "—"}</td>
              <td class="mono">{row.require || "—"}</td>
              <td class="right">
                <button type="button" class="mini" onclick={() => openEdit(row)} title={t("alloc.editTip")}>{t("alloc.editShort")}</button>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
</div>

<ConfirmDanger
  open={pending && editing !== null}
  title={t("alloc.confirm.title")}
  message={t("alloc.confirm.message")}
  scope={editing ? t("alloc.confirm.scope", { index: editing.index, changes: changes(editing).join("; ") }) : ""}
  confirmLabel={t("alloc.confirm.label")}
  onConfirm={save}
  onCancel={() => (pending = false)}
/>

<style>
  .page { max-width: 1240px; }
  header { display: flex; justify-content: space-between; align-items: flex-start; gap: 16px; margin-bottom: 16px; }
  h1 { margin: 0 0 6px; font-size: var(--es-fs-3xl); }
  header p { margin: 0; color: var(--es-muted); }
  .head-actions { display: flex; align-items: center; gap: 12px; flex: 0 0 auto; }
  .filter { width: 320px; max-width: 100%; padding: 7px 12px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); font-size: var(--es-fs-sm); }
  button { padding: 6px 14px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); cursor: pointer; white-space: nowrap; }
  button:hover:not(:disabled):not(.primary) { background: var(--es-surface-2); }
  .primary { background: var(--es-accent); color: #fff; border-color: var(--es-accent); }
  .primary:hover:not(:disabled) { filter: brightness(0.92); }
  button:disabled { opacity: 0.55; cursor: default; }
  .mini { padding: 3px 8px; font-size: var(--es-fs-xs); }

  .toast { padding: 8px 12px; margin-bottom: 12px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface-2); font-size: var(--es-fs-sm); }
  .table-wrap { overflow-x: auto; border: 1px solid var(--es-border); border-radius: var(--es-radius); }
  table { width: 100%; border-collapse: collapse; font-size: var(--es-fs-sm); }
  th, td { padding: 8px 12px; text-align: left; border-bottom: 1px solid var(--es-border); white-space: nowrap; }
  th { background: var(--es-surface-2); color: var(--es-muted); font-weight: 600; }
  th.right, td.right { text-align: right; }
  td.mono, th.mono { font-family: var(--es-mono); }
  tbody tr:hover { background: var(--es-surface-2); }

  .editor { border: 1px solid var(--es-border); border-radius: var(--es-radius); padding: 14px; margin-bottom: 16px; background: var(--es-surface); }
  .editor h3 { margin: 0 0 8px; font-size: var(--es-fs-md); }
  .fields { display: flex; gap: 12px; flex-wrap: wrap; margin-bottom: 10px; }
  .fields label { display: flex; flex-direction: column; gap: 4px; font-size: var(--es-fs-xs); color: var(--es-muted); }
  .fields input { padding: 6px 10px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); min-width: 180px; }
  .actions { display: flex; align-items: center; gap: 10px; justify-content: flex-end; }
  .diff { margin-right: auto; font-size: var(--es-fs-xs); color: var(--es-muted); font-family: var(--es-mono); }
</style>
