<script lang="ts">
  import StateBlock from "../components/StateBlock.svelte";
  import JsonView from "../components/JsonView.svelte";
  import {
    fetchIndices, fetchFields, searchIndex, buildDsl,
    operatorsForType, defaultOperatorForType, clauseHasNoValue,
    type IndexRow, type DocHit, type QueryClause, type FieldInfo,
    type ClauseOccur, type LeafClauseType, type OperatorOption,
  } from "../lib/services";
  import type { DbxBridge } from "../lib/bridge";
  import { t } from "../lib/i18n.svelte.js";

  interface Props {
    bridge: DbxBridge;
    connectionId: string;
  }
  let { bridge, connectionId }: Props = $props();

  interface ClauseRow {
    id: number;
    occur: ClauseOccur;
    type: LeafClauseType;
    field: string;
    value: string;
    gte: string;
    lte: string;
  }

  interface Tab {
    id: number;
    index: string;
    clauses: ClauseRow[];
    size: number;
    sortField: string;
    sortOrder: "asc" | "desc";
    minimumShouldMatch: string;
    hits: DocHit[];
    raw: unknown;
    error: string;
    ran: boolean;
  }

  let seq = 1;
  function newClause(type: LeafClauseType, field = ""): ClauseRow {
    return { id: seq++, occur: "must", type, field, value: "", gte: "", lte: "" };
  }
  function newTab(index: string): Tab {
    return {
      id: seq++,
      index,
      clauses: [],
      size: 20,
      sortField: "",
      sortOrder: "asc",
      minimumShouldMatch: "",
      hits: [],
      raw: null,
      error: "",
      ran: false,
    };
  }

  let indices = $state<IndexRow[]>([]);
  let loadingIndices = $state(true);
  // One tab per index is the intended workflow: comparisons shouldn't clobber
  // each other's conditions and results.
  const initialTab = newTab("");
  let tabs = $state<Tab[]>([initialTab]);
  let activeId = $state(initialTab.id);
  let loading = $state(false);

  let fieldsByIndex = $state<Record<string, FieldInfo[]>>({});
  let loadingFields = $state("");

  let showDsl = $state(true);
  let showRaw = $state(false);

  const active = $derived<Tab>(tabs.find((t) => t.id === activeId) ?? tabs[0]);

  async function loadIndices(id: string): Promise<void> {
    if (!id) {
      loadingIndices = false;
      return;
    }
    loadingIndices = true;
    try {
      indices = await fetchIndices(bridge, id);
      if (indices.length) {
        // Fill every tab that still has no index, so the first tab is usable
        // without hunting for a dropdown.
        const first = indices[0].index;
        for (const tab of tabs) if (!tab.index) tab.index = first;
        void ensureFields(tabs[0].index);
      }
    } catch {
      indices = [];
    } finally {
      loadingIndices = false;
    }
  }
  $effect(() => {
    void loadIndices(connectionId);
  });

  async function ensureFields(index: string): Promise<void> {
    if (!index || fieldsByIndex[index]) return;
    loadingFields = index;
    try {
      const next = await fetchFields(bridge, connectionId, index);
      fieldsByIndex = { ...fieldsByIndex, [index]: next };
    } catch {
      fieldsByIndex = { ...fieldsByIndex, [index]: [] };
    } finally {
      if (loadingFields === index) loadingFields = "";
    }
  }

  function fieldsOf(tab: Tab): FieldInfo[] {
    return fieldsByIndex[tab.index] ?? [];
  }
  function fieldTypeOf(tab: Tab, name: string): string {
    if (name === "_id" || name === "_index") return "keyword";
    return fieldsOf(tab).find((f) => f.name === name)?.type ?? "";
  }
  function operatorsOf(tab: Tab, clause: ClauseRow): OperatorOption[] {
    return operatorsForType(fieldTypeOf(tab, clause.field));
  }
  function isBooleanField(tab: Tab, name: string): boolean {
    return fieldTypeOf(tab, name) === "boolean";
  }
  function hasShould(tab: Tab): boolean {
    return tab.clauses.some((c) => c.occur === "should");
  }

  // ---- tabs ---------------------------------------------------------------
  function activate(id: number): void {
    activeId = id;
    const tab = tabs.find((t) => t.id === id);
    if (tab?.index) void ensureFields(tab.index);
  }
  function addTab(): void {
    // A new tab always starts on the first index: predictable, and one click
    // away from switching. Guessing "the next unused index" surprised people.
    const next = indices[0]?.index ?? "";
    const tab = newTab(next);
    tabs = [...tabs, tab];
    activeId = tab.id;
    if (next) void ensureFields(next);
  }
  function closeTab(id: number): void {
    if (tabs.length === 1) return;
    const at = tabs.findIndex((t) => t.id === id);
    tabs = tabs.filter((t) => t.id !== id);
    if (activeId === id) activeId = tabs[Math.max(0, at - 1)].id;
  }
  function tabLabel(tab: Tab): string {
    return tab.index || `Query ${tab.id}`;
  }

  /**
   * Switching index must drop the conditions: a clause built for `products`
   * (`category = x`) silently returns 0 hits against `orders`, which looks like
   * a bug in the query instead of a stale form.
   */
  function changeIndex(tab: Tab, value: string): void {
    if (tab.index === value) return;
    tab.index = value;
    tab.clauses = [];
    tab.sortField = "";
    tab.minimumShouldMatch = "";
    tab.hits = [];
    tab.raw = null;
    tab.error = "";
    tab.ran = false;
    void ensureFields(value);
  }

  // ---- clauses ------------------------------------------------------------
  function addClause(tab: Tab): void {
    const field = tab.sortField || fieldsOf(tab)[0]?.name || "_id";
    const type = defaultOperatorForType(fieldTypeOf(tab, field));
    tab.clauses = [...tab.clauses, newClause(type, field)];
  }
  function removeClause(tab: Tab, id: number): void {
    tab.clauses = tab.clauses.filter((c) => c.id !== id);
  }

  /**
   * Picking a field also picks the operator that suits it — a `term` left over
   * from a keyword field would silently return 0 hits on an analyzed `text`
   * field, which looks like a broken query instead of a stale operator.
   */
  function changeField(tab: Tab, clause: ClauseRow, field: string): void {
    clause.field = field;
    clause.type = defaultOperatorForType(fieldTypeOf(tab, field));
    if (clause.type === "term" && isBooleanField(tab, field) && !clause.value) clause.value = "true";
  }
  function changeType(clause: ClauseRow, type: LeafClauseType): void {
    clause.type = type;
    if (clauseHasNoValue(type)) clause.value = "";
  }

  function toQueryClause(c: ClauseRow): QueryClause {
    switch (c.type) {
      case "term": return { type: "term", field: c.field, value: c.value, occur: c.occur };
      case "match": return { type: "match", field: c.field, value: c.value, occur: c.occur };
      case "match_phrase": return { type: "match_phrase", field: c.field, value: c.value, occur: c.occur };
      case "prefix": return { type: "prefix", field: c.field, value: c.value, occur: c.occur };
      case "wildcard": return { type: "wildcard", field: c.field, value: c.value, occur: c.occur };
      case "exists": return { type: "exists", field: c.field, occur: c.occur };
      case "range": return { type: "range", field: c.field, gte: c.gte || undefined, lte: c.lte || undefined, occur: c.occur };
    }
  }

  function dslOf(tab: Tab): Record<string, unknown> {
    return buildDsl(tab.clauses.map(toQueryClause), {
      size: tab.size,
      ...(tab.sortField.trim() ? { sortField: tab.sortField.trim(), sortOrder: tab.sortOrder } : {}),
      minimumShouldMatch: tab.minimumShouldMatch,
    });
  }

  async function runTab(tab: Tab): Promise<void> {
    if (!tab.index) {
      tab.error = t("qb.err.noIndex");
      return;
    }
    loading = true;
    tab.error = "";
    tab.ran = true;
    try {
      const res = await searchIndex(bridge, connectionId, tab.index, dslOf(tab));
      tab.hits = res.hits;
      tab.raw = res.raw;
      if (res.error) tab.error = res.error;
    } catch (cause) {
      tab.error = cause instanceof Error ? cause.message : String(cause);
      tab.hits = [];
    } finally {
      loading = false;
    }
  }

  function preview(source: unknown): string {
    try {
      const s = JSON.stringify(source);
      return s.length > 90 ? s.slice(0, 90) + "…" : s;
    } catch {
      return String(source);
    }
  }

  // `hits.total` is an object on ES 7+/8 and a plain number on 6.x.
  function totalOf(tab: Tab): number {
    const raw = tab.raw as { hits?: { total?: number | { value?: number } } } | null;
    const total = raw?.hits?.total;
    if (typeof total === "number") return total;
    return total?.value ?? 0;
  }
  function tookOf(tab: Tab): number {
    return (tab.raw as { took?: number } | null)?.took ?? 0;
  }

  const OCCUR_OPTIONS: { value: ClauseOccur; label: string; hint: string }[] = [
    { value: "must", label: "must", hint: "qb.occur.must" },
    { value: "filter", label: "filter", hint: "qb.occur.filter" },
    { value: "should", label: "should", hint: "qb.occur.should" },
    { value: "must_not", label: "must_not", hint: "qb.occur.mustNot" },
  ];
</script>

<div class="page">
  <header>
    <div>
      <h1>{t("qb.title")}</h1>
      <p>
        {t("qb.hint1")}<strong>must / filter / should / must_not</strong>{t("qb.hint2")}
      </p>
    </div>
  </header>

  <div class="tabs">
    {#each tabs as tab (tab.id)}
      <span class="tab" class:active={tab.id === activeId}>
        <button type="button" class="tab-name" onclick={() => activate(tab.id)} title={tabLabel(tab)}>
          {tabLabel(tab)}{#if tab.clauses.length} <em>{t("qb.tab.clauses", { count: tab.clauses.length })}</em>{/if}
        </button>
        {#if tabs.length > 1}
          <button type="button" class="tab-x" onclick={() => closeTab(tab.id)} title={t("qb.tab.close")}>×</button>
        {/if}
      </span>
    {/each}
    <button type="button" class="add-tab" onclick={addTab} disabled={loadingIndices || indices.length === 0}>{t("qb.tab.add")}</button>
  </div>

  {#if !active.index}
    <StateBlock kind="empty" message={t("qb.empty.title")} detail={t("qb.empty.detail")} />
  {:else}
    <section class="bar">
      <label>{t("index.col.index")}
        <select value={active.index} onchange={(e) => changeIndex(active, (e.currentTarget as HTMLSelectElement).value)}>
          {#each indices as ix (ix.index)}<option value={ix.index}>{ix.index}</option>{/each}
        </select>
      </label>
      <label>{t("qb.form.size")}<input type="number" min="1" max="1000" bind:value={active.size} /></label>
      <label>{t("qb.form.sortField")}
        <input placeholder={t("qb.form.sortOptional")} list="qb-fields-{active.id}" bind:value={active.sortField} />
      </label>
      <label>{t("qb.form.order")}
        <select bind:value={active.sortOrder}><option value="asc">asc</option><option value="desc">desc</option></select>
      </label>
      {#if hasShould(active)}
        <label>minimum_should_match
          <input placeholder={t("qb.form.minShouldPlaceholder")} bind:value={active.minimumShouldMatch} />
        </label>
      {/if}
      <span class="fcount">{loadingFields === active.index ? "loading fields…" : `${fieldsOf(active).length} fields`}</span>
    </section>

    <section class="clauses">
      <h2>{t("qb.conditions.title")} <span class="count">{active.clauses.length === 0 ? t("qb.conditions.none") : t("qb.tab.clauses", { count: active.clauses.length })}</span></h2>

      {#if active.clauses.length === 0}
        <p class="none">{t("qb.conditions.empty")}</p>
      {/if}

      {#each active.clauses as c (c.id)}
        <div class="clause">
          <select class="occur {c.occur}" bind:value={c.occur} title={t(OCCUR_OPTIONS.find((o) => o.value === c.occur)?.hint ?? "")}>
            {#each OCCUR_OPTIONS as o (o.value)}<option value={o.value}>{o.label}</option>{/each}
          </select>

          <select class="op" value={c.type} onchange={(e) => changeType(c, (e.currentTarget as HTMLSelectElement).value as LeafClauseType)}>
            {#each operatorsOf(active, c) as op (op.value)}<option value={op.value}>{op.label}</option>{/each}
          </select>

          <select class="field" value={c.field} onchange={(e) => changeField(active, c, (e.currentTarget as HTMLSelectElement).value)}>
            <option value="">{t("qb.form.selectField")}</option>
            <option value="_id">_id · keyword</option>
            <option value="_index">_index · keyword</option>
            {#each fieldsOf(active) as f (f.name)}<option value={f.name}>{f.name} · {f.type}</option>{/each}
          </select>

          {#if clauseHasNoValue(c.type)}
            <span class="novalue">{t("qb.form.noValue")}</span>
          {:else if c.type === "range"}
            <input class="value" placeholder="gte (≥)" bind:value={c.gte} />
            <input class="value" placeholder="lte (≤)" bind:value={c.lte} />
          {:else if isBooleanField(active, c.field)}
            <select class="value" bind:value={c.value}>
              <option value="true">true</option>
              <option value="false">false</option>
            </select>
          {:else}
            <input class="value" placeholder="value" bind:value={c.value} />
          {/if}

          {#if c.field}
            <span class="ftype" title={t("qb.fieldTypeTitle")}>{fieldTypeOf(active, c.field) || t("common.unknown")}</span>
          {/if}

          <button type="button" class="mini" onclick={() => removeClause(active, c.id)} title={t("qb.removeClause")}>✕</button>
        </div>
      {/each}

      <div class="actions">
        <button type="button" class="add" onclick={() => addClause(active)} disabled={fieldsOf(active).length === 0}>{t("qb.form.addClause")}</button>
        <button type="button" class="primary" onclick={() => runTab(active)} disabled={!active.index || loading}>
          {loading ? t("qb.form.running") : t("qb.form.run")}
        </button>
      </div>

      <datalist id="qb-fields-{active.id}">
        {#each fieldsOf(active) as f (f.name)}<option value={f.name}>{f.name} · {f.type}</option>{/each}
      </datalist>
    </section>

    <details class="dsl" bind:open={showDsl}>
      <summary>{t("qb.dsl.summary")}</summary>
      <pre>{JSON.stringify(dslOf(active), null, 2)}</pre>
    </details>

    {#if active.error}
      <StateBlock kind="error" message={t("qb.queryFailed")} detail={active.error} />
    {:else if loading}
      <StateBlock kind="loading" message={t("qb.runningQuery")} detail={`POST /${active.index}/_search`} />
    {:else if active.hits.length > 0}
      <p class="meta">total {totalOf(active)} hits · took {tookOf(active)} ms</p>
      <table class="docs">
        <thead><tr><th>_id</th><th>{t("qb.col.score")}</th><th>_source</th></tr></thead>
        <tbody>
          {#each active.hits as hit (hit.id)}
            <tr><td class="mono">{hit.id}</td><td class="mono">{hit.score}</td><td class="mono src">{preview(hit.source)}</td></tr>
          {/each}
        </tbody>
      </table>
      <button type="button" class="link" onclick={() => (showRaw = !showRaw)}>{showRaw ? t("qb.hideRaw") : t("qb.showRaw")}</button>
      {#if showRaw}<JsonView value={active.raw} />{/if}
    {:else if active.ran}
      <StateBlock kind="empty" message={t("qb.noMatch")} />
    {/if}
  {/if}
</div>

<style>
  .page { max-width: 1060px; }
  header { margin-bottom: 14px; }
  h1 { margin: 0 0 6px; font-size: var(--es-fs-3xl); }
  header p { margin: 0; color: var(--es-muted); max-width: 820px; line-height: 1.7; }
  header strong { color: var(--es-text); font-family: var(--es-mono); font-size: var(--es-fs-sm); }
  button { padding: 6px 14px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); cursor: pointer; white-space: nowrap; }
  button:hover:not(:disabled):not(.primary) { background: var(--es-surface-2); }
  .primary:hover:not(:disabled) { filter: brightness(0.92); }
  button:disabled { opacity: 0.55; cursor: default; }

  .tabs { display: flex; gap: 4px; align-items: stretch; margin-bottom: 12px; flex-wrap: wrap; border-bottom: 1px solid var(--es-border); padding-bottom: 0; }
  .tab { display: inline-flex; align-items: center; border: 1px solid var(--es-border); border-bottom: none; border-radius: 6px 6px 0 0; background: var(--es-surface-2); }
  .tab.active { background: var(--es-surface); border-color: var(--es-accent); }
  .tab-name { border: none; background: none; padding: 6px 10px; font-family: var(--es-mono); font-size: var(--es-fs-xs); color: var(--es-muted); max-width: 220px; overflow: hidden; text-overflow: ellipsis; }
  .tab.active .tab-name { color: var(--es-text); font-weight: 600; }
  .tab-name em { margin-left: 6px; font-style: normal; font-size: var(--es-fs-2xs); color: var(--es-accent); }
  .tab-x { border: none; background: none; padding: 2px 6px 2px 0; color: var(--es-muted); font-size: var(--es-fs-sm); }
  .tab-x:hover { color: var(--es-red); }
  .add-tab { margin-left: 4px; padding: 4px 10px; font-size: var(--es-fs-xs); align-self: center; }

  .bar { display: flex; gap: 12px; align-items: end; flex-wrap: wrap; margin-bottom: 14px; }
  .bar label { display: flex; flex-direction: column; gap: 4px; font-size: var(--es-fs-xs); color: var(--es-muted); }
  .bar select, .bar input { padding: 6px 10px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); }
  .fcount { font-family: var(--es-mono); font-size: var(--es-fs-2xs); color: var(--es-muted); padding-bottom: 8px; }

  .clauses { border: 1px solid var(--es-border); border-radius: var(--es-radius); padding: 14px; margin-bottom: 12px; }
  .clauses h2 { margin: 0 0 10px; font-size: var(--es-fs-sm); text-transform: uppercase; letter-spacing: 0.08em; color: var(--es-muted); }
  .clauses h2 .count { margin-left: 8px; font-family: var(--es-mono); font-size: var(--es-fs-2xs); letter-spacing: 0; text-transform: none; }
  .none { margin: 0 0 10px; font-size: var(--es-fs-sm); color: var(--es-muted); }
  .clause { display: flex; gap: 8px; margin-bottom: 8px; align-items: center; flex-wrap: wrap; }
  .clause select, .clause input { padding: 6px 8px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); }
  .clause .occur { font-family: var(--es-mono); font-size: var(--es-fs-xs); width: 92px; }
  .clause .occur.must { color: var(--es-green); border-color: var(--es-green); }
  .clause .occur.filter { color: var(--es-accent); border-color: var(--es-accent); }
  .clause .occur.should { color: var(--es-yellow); border-color: var(--es-yellow); }
  .clause .occur.must_not { color: var(--es-red); border-color: var(--es-red); }
  .clause .op { font-size: var(--es-fs-xs); width: 168px; }
  .clause .field { flex: 1 1 200px; min-width: 160px; font-family: var(--es-mono); font-size: var(--es-fs-xs); }
  .clause .value { width: 150px; }
  .novalue { font-size: var(--es-fs-xs); color: var(--es-muted); }
  .ftype { font-family: var(--es-mono); font-size: var(--es-fs-2xs); color: var(--es-muted); border: 1px solid var(--es-border); border-radius: 4px; padding: 2px 6px; white-space: nowrap; }
  .actions { display: flex; gap: 8px; margin-top: 4px; }
  .primary { background: var(--es-accent); color: #fff; border-color: var(--es-accent); min-width: 104px; }
  .mini { padding: 3px 8px; font-size: var(--es-fs-xs); }

  .dsl { margin-bottom: 14px; }
  .dsl summary { cursor: pointer; font-size: var(--es-fs-xs); color: var(--es-muted); }
  .dsl pre { margin: 8px 0 0; padding: 10px 12px; background: var(--es-surface-2); border: 1px solid var(--es-border); border-radius: var(--es-radius); font-family: var(--es-mono); font-size: var(--es-fs-xs); overflow: auto; max-height: 260px; }

  .meta { font-size: var(--es-fs-xs); color: var(--es-muted); font-family: var(--es-mono); }
  table.docs { width: 100%; border-collapse: collapse; font-size: var(--es-fs-sm); border: 1px solid var(--es-border); border-radius: var(--es-radius); overflow: hidden; }
  .docs th, .docs td { padding: 6px 8px; border-bottom: 1px solid var(--es-border); }
  .docs th { background: var(--es-surface-2); color: var(--es-muted); text-align: left; font-weight: 600; }
  .docs td.mono { font-family: var(--es-mono); }
  .docs td.src { max-width: 460px; overflow: hidden; text-overflow: ellipsis; }
  .link { background: none; border: none; color: var(--es-accent); cursor: pointer; padding: 8px 0; }
</style>
