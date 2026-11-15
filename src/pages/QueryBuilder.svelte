<script lang="ts">
  import StateBlock from "../components/StateBlock.svelte";
  import JsonView from "../components/JsonView.svelte";
  import {
    fetchIndices, fetchFields, searchIndex, buildDsl,
    type IndexRow, type DocHit, type QueryClause, type FieldInfo,
  } from "../lib/services";
  import type { DbxBridge } from "../lib/bridge";

  interface Props {
    bridge: DbxBridge;
    connectionId: string;
  }
  let { bridge, connectionId }: Props = $props();

  type ClauseRow = { type: QueryClause["type"]; field: string; value: string; gte: string; lte: string };

  let indices = $state<IndexRow[]>([]);
  let selectedIndex = $state("");
  let clauses = $state<ClauseRow[]>([{ type: "match_all", field: "", value: "", gte: "", lte: "" }]);
  let size = $state(20);
  let sortField = $state("");
  let sortOrder = $state<"asc" | "desc">("asc");

  let loadingIndices = $state(true);
  let loading = $state(false);
  let fields = $state<FieldInfo[]>([]);
  let loadingFields = $state(false);
  let hits = $state<DocHit[]>([]);
  let raw = $state<unknown>(null);
  let error = $state("");
  let showRaw = $state(false);

  async function loadIndices(id: string): Promise<void> {
    if (!id) return;
    loadingIndices = true;
    try {
      indices = await fetchIndices(bridge, id);
      if (!selectedIndex && indices.length) selectedIndex = indices[0].index;
    } catch {
      indices = [];
    } finally {
      loadingIndices = false;
    }
  }
  $effect(() => {
    void loadIndices(connectionId);
  });

  async function loadFields(id: string): Promise<void> {
    if (!id) return;
    loadingFields = true;
    try {
      const next = await fetchFields(bridge, connectionId, id);
      if (selectedIndex === id) fields = next;
    } catch {
      if (selectedIndex === id) fields = [];
    } finally {
      if (selectedIndex === id) loadingFields = false;
    }
  }

  // Field names come from the index mapping so the user does not have to
  // remember them; existing clause values are kept (they may still be valid).
  $effect(() => {
    const ix = selectedIndex;
    if (ix) void loadFields(ix);
  });

  function fieldType(name: string): string {
    return fields.find((f) => f.name === name)?.type ?? "";
  }

  function addClause(): void {
    clauses = [...clauses, { type: "term", field: "", value: "", gte: "", lte: "" }];
  }
  function removeClause(i: number): void {
    clauses = clauses.filter((_, idx) => idx !== i);
  }

  function toQueryClause(c: ClauseRow): QueryClause {
    switch (c.type) {
      case "match_all": return { type: "match_all" };
      case "term": return { type: "term", field: c.field, value: c.value };
      case "match": return { type: "match", field: c.field, value: c.value };
      case "range": return { type: "range", field: c.field, gte: c.gte || undefined, lte: c.lte || undefined };
      case "wildcard": return { type: "wildcard", field: c.field, value: c.value };
    }
  }

  async function run(): Promise<void> {
    if (!selectedIndex) {
      error = "请先选择索引";
      return;
    }
    const dsl = buildDsl(clauses.map(toQueryClause), {
      size,
      ...(sortField.trim() ? { sortField: sortField.trim(), sortOrder } : {}),
    });
    loading = true;
    error = "";
    try {
      const res = await searchIndex(bridge, connectionId, selectedIndex, dsl);
      if (res.error) {
        error = res.error;
        hits = [];
        raw = res.raw;
      } else {
        hits = res.hits;
        raw = res.raw;
      }
    } catch (cause) {
      error = cause instanceof Error ? cause.message : String(cause);
    } finally {
      loading = false;
    }
  }

  function preview(source: unknown): string {
    try {
      const s = JSON.stringify(source);
      return s.length > 80 ? s.slice(0, 80) + "…" : s;
    } catch {
      return String(source);
    }
  }
</script>

<div class="page">
  <header>
    <div>
      <h1>Query Builder</h1>
      <p>Compose an Elasticsearch query without writing JSON. Built from <code>buildDsl()</code>.</p>
    </div>
  </header>

  <section class="bar">
    <label>Index
      <select bind:value={selectedIndex} disabled={loadingIndices}>
        {#if loadingIndices}<option>Loading…</option>{/if}
        {#each indices as ix (ix.index)}<option value={ix.index}>{ix.index}</option>{/each}
      </select>
    </label>
    <label>Size<input type="number" min="1" max="1000" bind:value={size} /></label>
    <label>Sort field<input placeholder="(optional)" list="qb-fields" bind:value={sortField} /></label>
    <label>Order
      <select bind:value={sortOrder}><option value="asc">asc</option><option value="desc">desc</option></select>
    </label>
  </section>

  <section class="clauses">
    <h2>Clauses <span class="count">{loadingFields ? "loading fields…" : `${fields.length} fields`}</span></h2>
    {#each clauses as c, i (i)}
      <div class="clause">
        <select bind:value={c.type}>
          <option value="match_all">match_all</option>
          <option value="term">term</option>
          <option value="match">match</option>
          <option value="range">range</option>
          <option value="wildcard">wildcard</option>
        </select>
        {#if c.type !== "match_all"}
          <input class="field" placeholder="field（可从下拉选）" list="qb-fields" bind:value={c.field} />
          {#if fieldType(c.field)}<span class="ftype">{fieldType(c.field)}</span>{/if}
        {/if}
        {#if c.type === "range"}
          <input class="value" placeholder="gte" bind:value={c.gte} />
          <input class="value" placeholder="lte" bind:value={c.lte} />
        {:else if c.type !== "match_all"}
          <input class="value" placeholder="value" bind:value={c.value} />
        {/if}
        <button type="button" class="mini" onclick={() => removeClause(i)} disabled={clauses.length === 1}>✕</button>
      </div>
    {/each}
    <button type="button" class="add" onclick={addClause}>+ Add clause</button>
    <button type="button" class="primary" onclick={run} disabled={!selectedIndex || loading}>
      {loading ? "Running…" : "Run query"}
    </button>
    <datalist id="qb-fields">
      {#each fields as f (f.name)}<option value={f.name}>{f.name} · {f.type}</option>{/each}
    </datalist>
  </section>

  {#if error}
    <StateBlock kind="error" message="Query failed" detail={error} />
  {:else if loading}
    <StateBlock kind="loading" message="Running query…" detail={`POST /${selectedIndex}/_search`} />
  {:else if hits.length > 0}
    <p class="meta">total {raw && typeof raw === "object" && "hits" in (raw as object) ? ((raw as any).hits.total?.value ?? (raw as any).hits.total ?? 0) : 0} hits</p>
    <table class="docs">
      <thead><tr><th>_id</th><th>Score</th><th>_source</th></tr></thead>
      <tbody>
        {#each hits as hit (hit.id)}
          <tr><td class="mono">{hit.id}</td><td class="mono">{hit.score}</td><td class="mono src">{preview(hit.source)}</td></tr>
        {/each}
      </tbody>
    </table>
    <button type="button" class="link" onclick={() => (showRaw = !showRaw)}>{showRaw ? "Hide" : "Show"} raw response</button>
    {#if showRaw}<JsonView value={raw} />{/if}
  {:else if raw}
    <StateBlock kind="empty" message="No documents matched" />
  {/if}
</div>

<style>
  .page { max-width: 1000px; }
  header { margin-bottom: 18px; }
  h1 { margin: 0 0 4px; font-size: 18px; }
  header p { margin: 0; color: var(--es-muted); }
  header code { font-family: var(--es-mono); font-size: 11px; }
  button { padding: 6px 14px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); cursor: pointer; white-space: nowrap; }
  button:hover:not(:disabled):not(.primary) { background: var(--es-surface-2); }
  .primary:hover:not(:disabled) { filter: brightness(0.92); }
  button:disabled { opacity: 0.55; cursor: default; }
  .bar { display: flex; gap: 12px; align-items: end; flex-wrap: wrap; margin-bottom: 16px; }
  .bar label { display: flex; flex-direction: column; gap: 4px; font-size: 11px; color: var(--es-muted); }
  .bar select, .bar input { padding: 6px 10px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); }
  .clauses { border: 1px solid var(--es-border); border-radius: var(--es-radius); padding: 14px; margin-bottom: 16px; }
  .clauses h2 { margin: 0 0 10px; font-size: 12px; text-transform: uppercase; letter-spacing: 0.08em; color: var(--es-muted); }
  .clauses h2 .count { margin-left: 8px; font-family: var(--es-mono); font-size: 10px; letter-spacing: 0; text-transform: none; }
  .ftype { font-family: var(--es-mono); font-size: 10px; color: var(--es-muted); border: 1px solid var(--es-border); border-radius: 4px; padding: 2px 6px; white-space: nowrap; }
  .clause { display: flex; gap: 8px; margin-bottom: 8px; align-items: center; }
  .clause select, .clause input { padding: 6px 8px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); }
  .clause .field { flex: 1; }
  .clause .value { width: 140px; }
  .add { margin-right: 10px; }
  .primary { background: var(--es-accent); color: #fff; border-color: var(--es-accent); min-width: 104px; }
  .mini { padding: 3px 8px; font-size: 11px; }
  .meta { font-size: 11px; color: var(--es-muted); font-family: var(--es-mono); }
  table.docs { width: 100%; border-collapse: collapse; font-size: 12px; border: 1px solid var(--es-border); border-radius: var(--es-radius); overflow: hidden; }
  .docs th, .docs td { padding: 6px 8px; border-bottom: 1px solid var(--es-border); }
  .docs th { background: var(--es-surface-2); color: var(--es-muted); text-align: left; font-weight: 600; }
  .docs td.mono { font-family: var(--es-mono); }
  .docs td.src { max-width: 420px; overflow: hidden; text-overflow: ellipsis; }
  .link { background: none; border: none; color: var(--es-accent); cursor: pointer; padding: 8px 0; }
</style>
