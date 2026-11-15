<script lang="ts">
  import StateBlock from "../components/StateBlock.svelte";
  import ConfirmDanger from "../components/ConfirmDanger.svelte";
  import {
    fetchShards, buildTopology, summarizeShards,
    fetchAllocation, explainAllocation, setReplicas, setAllocationRule, isError,
    type ShardRow, type ShardTag, type TopologyMatrix, type ShardSummary,
    type AllocationRow, type AllocationExplain, type EsResult,
  } from "../lib/services";
  import type { DbxBridge } from "../lib/bridge";
  import { t } from "../lib/i18n.svelte.js";

  interface Props {
    bridge: DbxBridge;
    connectionId: string;
  }
  let { bridge, connectionId }: Props = $props();

  let shards = $state<ShardRow[]>([]);
  let summary = $state<ShardSummary>({ total: 0, started: 0, initializing: 0, relocating: 0, unassigned: 0, inFlight: 0 });
  let loading = $state(true);
  let error = $state("");
  let autoRefresh = $state(true);
  let lastUpdated = $state("");
  let inflight = false;
  let indexFilter = $state("");

  /** i18n keys for the four `_cat/shards` states ES actually reports. */
  const STATE_LABEL: Record<string, string> = {
    STARTED: "shards.state.started",
    INITIALIZING: "shards.state.initializing",
    RELOCATING: "shards.state.relocating",
    UNASSIGNED: "shards.state.unassigned",
  };

  function stateText(state: string): string {
    const key = STATE_LABEL[state];
    return key ? t(key) : state;
  }

  async function load(id: string): Promise<void> {
    if (inflight) return;
    if (!id) {
      shards = [];
      summary = { total: 0, started: 0, initializing: 0, relocating: 0, unassigned: 0, inFlight: 0 };
      error = "NO_CONNECTION";
      loading = false;
      return;
    }
    inflight = true;
    loading = true;
    error = "";
    try {
      const next = await fetchShards(bridge, id);
      shards = next;
      summary = summarizeShards(next);
      lastUpdated = new Date().toLocaleTimeString();
    } catch (cause) {
      shards = [];
      error = cause instanceof Error ? cause.message : String(cause);
    } finally {
      inflight = false;
      loading = false;
    }
    await loadAllocation();
  }

  $effect(() => {
    void load(connectionId);
  });

  // While ES is actively creating or moving copies the page is only useful if it
  // follows along, so refresh on a short timer until nothing is in flight.
  $effect(() => {
    if (!autoRefresh || summary.inFlight === 0 || !connectionId) return;
    const timer = setInterval(() => void load(connectionId), 2000);
    return () => clearInterval(timer);
  });

  // ------------------------------------------------------------------
  // 分配设置（原「分片分配」页并入：规则读取 + 行内编辑 + 未分配诊断）
  // ------------------------------------------------------------------
  let allocRows = $state<AllocationRow[]>([]);
  let allocError = $state("");
  let editing = $state<AllocationRow | null>(null);
  let form = $state({ replicas: 0, include: "", exclude: "", require: "" });
  let pending = $state(false);
  let toast = $state("");
  let explain = $state<AllocationExplain | null>(null);
  let explaining = $state(false);
  let showExplain = $state(false);

  // Index-name filter narrows the matrix, the shard list and the allocation
  // settings together — one box instead of three.
  const query = $derived(indexFilter.trim().toLowerCase());
  const filteredShards = $derived(
    query ? shards.filter((s) => s.index.toLowerCase().includes(query)) : shards,
  );
  const matrix = $derived.by(() => buildTopology(filteredShards));
  const filteredAlloc = $derived(
    query ? allocRows.filter((r) => r.index.toLowerCase().includes(query)) : allocRows,
  );

  async function loadAllocation(): Promise<void> {
    allocError = "";
    try {
      allocRows = await fetchAllocation(bridge, connectionId);
    } catch (cause) {
      allocError = cause instanceof Error ? cause.message : String(cause);
      allocRows = [];
    }
  }

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
    await loadAllocation();
  }

  async function runExplain(): Promise<void> {
    explaining = true;
    try {
      explain = await explainAllocation(bridge, connectionId);
      showExplain = true;
    } finally {
      explaining = false;
    }
  }

  function stateClass(state: string): string {
    if (state === "UNASSIGNED") return "unassigned";
    if (state === "INITIALIZING") return "initializing";
    if (state === "RELOCATING") return "relocating";
    if (state === "STARTED") return "started";
    return "other";
  }

  function formatBytes(value: number): string {
    if (!Number.isFinite(value) || value <= 0) return "0 B";
    const units = ["B", "KB", "MB", "GB", "TB"];
    let i = 0;
    let n = value;
    while (n >= 1024 && i < units.length - 1) {
      n /= 1024;
      i += 1;
    }
    return `${n.toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
  }

  function cellTags(index: string, node: string): ShardTag[] {
    return matrix.cells[index]?.[node] ?? [];
  }
  function unassignedTags(index: string): ShardTag[] {
    return matrix.unassignedByIndex[index] ?? [];
  }
  function tagState(label: string): string {
    const parts = label.split(":");
    return parts.length > 1 ? parts[parts.length - 1] : label;
  }
</script>

<div class="page">
  <header>
    <div>
      <h1>{t("shards.title")}</h1>
      <p>{t("shards.hintPrefix")}<code>/_cat/shards</code>{t("shards.hintSuffix")}</p>
    </div>
    <div class="head-actions">
      <label class="auto"><input type="checkbox" bind:checked={autoRefresh} />{t("shards.autoRefresh")}</label>
      <button type="button" onclick={() => load(connectionId)} disabled={loading || !connectionId}>
        {loading ? t("common.loading") : t("common.refresh")}
      </button>
    </div>
  </header>

  {#if loading && shards.length === 0}
    <StateBlock kind="loading" message={t("shards.loadingMessage")} detail="GET /_cat/shards" />
  {:else if error}
    <StateBlock kind="error" message={t("shards.loadFailed")} detail={error === "NO_CONNECTION" ? t("common.noConnection") : error} />
  {:else if shards.length === 0}
    <StateBlock kind="empty" message={t("shards.empty")} />
  {:else}
    <section class="summary">
      <div class="card started">
        <span class="num">{summary.started}</span>
        <span class="lbl">{t("shards.card.started")}</span>
      </div>
      <div class="card initializing" class:active={summary.initializing > 0}>
        <span class="num">{summary.initializing}</span>
        <span class="lbl">{t("shards.card.initializing")}</span>
      </div>
      <div class="card relocating" class:active={summary.relocating > 0}>
        <span class="num">{summary.relocating}</span>
        <span class="lbl">{t("shards.card.relocating")}</span>
      </div>
      <div class="card unassigned" class:active={summary.unassigned > 0}>
        <span class="num">{summary.unassigned}</span>
        <span class="lbl">{t("shards.card.unassigned")}</span>
      </div>
    </section>

    {#if summary.inFlight > 0}
      <p class="inflight">
        {t("shards.inflight", { count: summary.inFlight, initializing: summary.initializing, relocating: summary.relocating })}
        {#if autoRefresh}<span class="muted">{t("shards.autoRefreshNote")}</span>{/if}
      </p>
    {:else if summary.unassigned > 0}
      <p class="inflight note">{t("shards.unassignedNote", { count: summary.unassigned })}</p>
    {/if}

    <div class="filter-bar">
      <input
        class="filter"
        type="search"
        placeholder={t("shards.filter")}
        bind:value={indexFilter}
      />
    </div>

    <h2>{t("shards.matrix.title")} <span class="asof">{lastUpdated ? t("shards.matrix.updated", { time: lastUpdated }) : ""}</span></h2>
    {#if matrix.indices.length === 0}
      <p class="no-match">{t("shards.noMatch", { filter: indexFilter })}</p>
    {:else}
      <div class="matrix-wrap">
        <table class="matrix">
          <thead>
            <tr>
              <th class="corner">{t("shards.matrix.index")}</th>
              {#each matrix.nodes as node (node)}<th>{node}</th>{/each}
              <th class="unassigned-col">{t("shards.matrix.unassigned")}</th>
            </tr>
          </thead>
          <tbody>
            {#each matrix.indices as index (index)}
              <tr>
                <th class="rowhead">{index}</th>
                {#each matrix.nodes as node (node)}
                  <td>
                    {#each cellTags(index, node) as tag (tag.key)}
                      <span class="shard {stateClass(tagState(tag.label))}" title={stateText(tagState(tag.label))}>{tag.label}</span>
                    {/each}
                  </td>
                {/each}
                <td class="unassigned-col">
                  {#each unassignedTags(index) as tag (tag.key)}
                    <span class="shard unassigned" title={t("shards.matrix.unassigned")}>{tag.label}</span>
                  {/each}
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {/if}

    <h2>{t("shards.table.title")} <span class="asof">{t("shards.table.copies", { count: summary.total })}</span></h2>
    <div class="table-wrap">
      <table>
        <thead>
          <tr>
            <th>{t("shards.col.index")}</th>
            <th class="right">{t("shards.col.shard")}</th>
            <th class="center">{t("shards.col.type")}</th>
            <th>{t("shards.col.state")}</th>
            <th class="right">{t("shards.col.docs")}</th>
            <th class="right">{t("shards.col.store")}</th>
            <th>{t("shards.col.node")}</th>
            <th>{t("shards.col.reason")}</th>
            <th>{t("shards.col.waiting")}</th>
          </tr>
        </thead>
        <tbody>
          {#each filteredShards as s, i (i)}
            <tr>
              <td class="mono">{s.index}</td>
              <td class="right mono">{s.shard}</td>
              <td class="center mono">{s.prirep}</td>
              <td class="mono {stateClass(s.state)}">{s.state} · {stateText(s.state)}</td>
              <td class="right mono">{s.docs}</td>
              <td class="right mono">{formatBytes(Number(s.storeBytes))}</td>
              <td>{s.node}</td>
              <td class="mono">{s.reason || "—"}</td>
              <td class="mono">{s.waiting || "—"}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>

    <h2>{t("alloc.title")}</h2>
    <p class="section-hint">{t("alloc.hint")}</p>

    {#if toast}<div class="toast">{toast}</div>{/if}
    {#if allocError}<StateBlock kind="error" message={t("alloc.failed")} detail={allocError} />{/if}

    {#if showExplain && explain}
      <section class="explain">
        <div class="explain-head">
          {#if explain.ok}
            <strong>{explain.index} · shard {explain.shard}{explain.primary ? t("alloc.role.primary") : t("alloc.role.replica")}</strong>
            <span class="verdict"><span class={explain.canAllocate === "yes" ? "ok" : "bad"}>{explain.canAllocate}</span> {explain.reason}</span>
          {:else}
            <strong class="note">{t("alloc.noExplain", { detail: explain.error ?? "" })}</strong>
          {/if}
          <div class="inline-actions">
            <button type="button" class="mini" onclick={() => void runExplain()} disabled={explaining}>
              {explaining ? t("alloc.explaining") : t("alloc.explainAgain")}
            </button>
            <button type="button" class="mini" onclick={() => (showExplain = false)}>{t("alloc.hideExplain")}</button>
          </div>
        </div>
        {#if explain.ok && explain.nodes.length}
          <table class="mini">
            <thead><tr><th>{t("alloc.col.node")}</th><th>{t("alloc.col.decision")}</th><th>{t("alloc.col.reason")}</th></tr></thead>
            <tbody>
              {#each explain.nodes as d (d.node)}
                <tr><td class="mono">{d.node}</td><td>{d.decision}</td><td class="reason">{d.reason}</td></tr>
              {/each}
            </tbody>
          </table>
        {/if}
        {#if explain.ok && explain.note}<p class="note">{explain.note}</p>{/if}
      </section>
    {/if}

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

    {#if loading === false && filteredAlloc.length === 0 && !allocError}
      <StateBlock kind="empty" message={t("alloc.empty")} />
    {:else if filteredAlloc.length > 0}
      <div class="table-wrap">
        <table>
          <thead>
            <tr>
              <th>{t("index.col.index")}</th>
              <th class="right">{t("alloc.col.pri")}</th>
              <th class="right">{t("alloc.col.rep")}</th>
              <th>{t("alloc.col.status")}</th>
              <th class="mono">include._name</th>
              <th class="mono">exclude._name</th>
              <th class="mono">require._name</th>
              <th class="right">{t("alloc.col.actions")}</th>
            </tr>
          </thead>
          <tbody>
            {#each filteredAlloc as row (row.index)}
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
      <div class="explain-launch">
        <button type="button" onclick={() => (showExplain ? (showExplain = false) : void runExplain())} disabled={explaining}>
          {explaining ? t("alloc.explaining") : (showExplain ? t("alloc.hideExplain") : t("alloc.explain"))}
        </button>
      </div>
    {/if}
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
  h2 { margin: 22px 0 10px; font-size: var(--es-fs-sm); text-transform: uppercase; letter-spacing: 0.08em; color: var(--es-muted); }
  h2 .asof { margin-left: 8px; font-size: var(--es-fs-2xs); letter-spacing: 0; text-transform: none; font-family: var(--es-mono); }
  header p { margin: 0; color: var(--es-muted); }
  header code { font-family: var(--es-mono); font-size: var(--es-fs-xs); }
  .head-actions { display: flex; align-items: center; gap: 12px; flex: 0 0 auto; }
  .auto { display: flex; align-items: center; gap: 6px; font-size: var(--es-fs-xs); color: var(--es-muted); white-space: nowrap; }
  button { padding: 6px 14px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); cursor: pointer; white-space: nowrap; }
  button:hover:not(:disabled):not(.primary) { background: var(--es-surface-2); }
  .primary { background: var(--es-accent); color: #fff; border-color: var(--es-accent); }
  .primary:hover:not(:disabled) { filter: brightness(0.92); }
  button:disabled { opacity: 0.55; cursor: default; }
  .mini { padding: 3px 8px; font-size: var(--es-fs-xs); }

  .filter-bar { margin: 14px 0 0; }
  .filter { width: 320px; max-width: 100%; padding: 7px 12px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); font-size: var(--es-fs-sm); }
  .no-match { margin: 0; padding: 10px 12px; border: 1px dashed var(--es-border); border-radius: var(--es-radius); color: var(--es-muted); font-size: var(--es-fs-sm); }

  .summary { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 10px; }
  .card { display: flex; flex-direction: column; gap: 3px; padding: 10px 12px; border: 1px solid var(--es-border); border-radius: var(--es-radius); background: var(--es-surface); border-left-width: 3px; }
  .card .num { font-family: var(--es-mono); font-size: var(--es-fs-4xl); font-weight: 600; line-height: 1.1; }
  .card .lbl { font-size: var(--es-fs-xs); color: var(--es-muted); font-family: var(--es-mono); }
  .card.started { border-left-color: var(--es-green); }
  .card.started .num { color: var(--es-green); }
  .card.initializing { border-left-color: var(--es-accent); }
  .card.initializing .num { color: var(--es-muted); }
  .card.initializing.active .num { color: var(--es-accent); }
  .card.relocating { border-left-color: var(--es-yellow); }
  .card.relocating .num { color: var(--es-muted); }
  .card.relocating.active .num { color: var(--es-yellow); }
  .card.unassigned { border-left-color: var(--es-red); }
  .card.unassigned .num { color: var(--es-muted); }
  .card.unassigned.active { background: color-mix(in srgb, var(--es-red) 6%, var(--es-surface)); }
  .card.unassigned.active .num { color: var(--es-red); }

  .inflight { margin: 12px 0 0; padding: 6px 12px; font-size: var(--es-fs-sm); border: 1px solid var(--es-accent); border-radius: var(--es-radius); background: var(--es-accent-soft); color: var(--es-text); }
  .inflight.note { border-color: var(--es-border); background: var(--es-surface-2); color: var(--es-muted); }
  .inflight .muted { color: var(--es-muted); }

  .table-wrap { overflow-x: auto; border: 1px solid var(--es-border); border-radius: var(--es-radius); }
  table { width: 100%; border-collapse: collapse; font-size: var(--es-fs-sm); }
  th, td { padding: 8px 12px; text-align: left; border-bottom: 1px solid var(--es-border); white-space: nowrap; }
  th { background: var(--es-surface-2); color: var(--es-muted); font-weight: 600; position: sticky; top: 0; }
  th.right, td.right { text-align: right; }
  th.center, td.center { text-align: center; }
  td.mono, th.mono { font-family: var(--es-mono); }
  tbody tr:hover { background: var(--es-surface-2); }
  td.started { color: var(--es-green); }
  td.unassigned { color: var(--es-red); font-weight: 700; }
  td.initializing { color: var(--es-accent); font-weight: 700; }
  td.relocating { color: var(--es-yellow); font-weight: 700; }

  .matrix-wrap { overflow-x: auto; border: 1px solid var(--es-border); border-radius: var(--es-radius); }
  table.matrix { border-collapse: collapse; font-size: var(--es-fs-xs); width: 100%; }
  .matrix th, .matrix td { border: 1px solid var(--es-border); padding: 6px 8px; vertical-align: top; }
  .matrix .corner, .matrix .rowhead { background: var(--es-surface-2); text-align: left; font-weight: 600; white-space: nowrap; }
  .matrix .unassigned-col { background: var(--es-surface-2); }
  .shard { display: inline-block; margin: 1px; padding: 1px 5px; border-radius: 4px; font-family: var(--es-mono); font-size: var(--es-fs-2xs); border: 1px solid var(--es-border); }
  .shard.started { color: var(--es-green); }
  .shard.initializing { color: var(--es-accent); border-color: var(--es-accent); background: var(--es-accent-soft); }
  .shard.relocating { color: var(--es-yellow); border-color: var(--es-yellow); }
  .shard.other { color: var(--es-muted); }
  .shard.unassigned { color: var(--es-red); border-color: var(--es-red); }

  .section-hint { margin: 0 0 12px; color: var(--es-muted); font-size: var(--es-fs-sm); }
  .toast { padding: 8px 12px; margin-bottom: 12px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface-2); font-size: var(--es-fs-sm); }
  .explain, .editor { border: 1px solid var(--es-border); border-radius: var(--es-radius); padding: 14px; margin-bottom: 16px; background: var(--es-surface); }
  .explain-head { display: flex; align-items: baseline; gap: 14px; flex-wrap: wrap; margin-bottom: 8px; }
  .explain-head .verdict { font-size: var(--es-fs-sm); }
  .inline-actions { margin-left: auto; display: flex; gap: 6px; }
  .ok { color: var(--es-green); font-weight: 700; }
  .bad { color: var(--es-red); font-weight: 700; }
  table.mini { width: 100%; border-collapse: collapse; font-size: var(--es-fs-xs); }
  table.mini th, table.mini td { padding: 5px 8px; border-bottom: 1px solid var(--es-border); text-align: left; }
  .reason { white-space: normal; color: var(--es-muted); }
  .note { margin: 8px 0 0; font-size: var(--es-fs-xs); color: var(--es-muted); line-height: 1.6; }
  .editor h3 { margin: 0 0 8px; font-size: var(--es-fs-md); }
  .fields { display: flex; gap: 12px; flex-wrap: wrap; margin-bottom: 10px; }
  .fields label { display: flex; flex-direction: column; gap: 4px; font-size: var(--es-fs-xs); color: var(--es-muted); }
  .fields input { padding: 6px 10px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); min-width: 180px; }
  .actions { display: flex; align-items: center; gap: 10px; justify-content: flex-end; }
  .diff { margin-right: auto; font-size: var(--es-fs-xs); color: var(--es-muted); font-family: var(--es-mono); }
  .explain-launch { margin-top: 12px; }
</style>
