<script lang="ts">
  import StateBlock from "../components/StateBlock.svelte";
  import ConfirmDanger from "../components/ConfirmDanger.svelte";
  import {
    fetchAliases, fetchIndices, single, isError, aliasActionsBody, parseAliasFilter,
    type AliasAdd, type AliasRow, type IndexRow,
  } from "../lib/services";
  import type { DbxBridge } from "../lib/bridge";
  import { t } from "../lib/i18n.svelte.js";

  interface Props {
    bridge: DbxBridge;
    connectionId: string;
  }
  let { bridge, connectionId }: Props = $props();

  let aliases = $state<AliasRow[]>([]);
  let indices = $state<IndexRow[]>([]);
  let loading = $state(true);
  let error = $state("");
  let toast = $state("");
  let inflight = false;

  // ---- query side ---------------------------------------------------------
  let filterText = $state("");
  let onlyFiltered = $state(false);
  let onlyHidden = $state(false);

  // ---- write side ---------------------------------------------------------
  let newIndex = $state("");
  let newAlias = $state("");
  let newFilter = $state("");
  let newRouting = $state("");
  let newIndexRouting = $state("");
  let newSearchRouting = $state("");
  let newWriteIndex = $state("");
  let newHidden = $state("");
  let addError = $state("");

  let pending = $state<{ body: Record<string, unknown>; label: string; scope: string } | null>(null);
  let busy = $state(false);

  async function load(id: string): Promise<void> {
    if (inflight) return;
    if (!id) {
      aliases = [];
      error = "NO_CONNECTION";
      loading = false;
      return;
    }
    inflight = true;
    loading = true;
    error = "";
    try {
      const [aliasRows, indexRows] = await Promise.all([
        fetchAliases(bridge, id),
        fetchIndices(bridge, id),
      ]);
      aliases = aliasRows;
      indices = indexRows;
      if (!newIndex && indexRows.length) newIndex = indexRows[0].index;
    } catch (cause) {
      aliases = [];
      error = cause instanceof Error ? cause.message : String(cause);
    } finally {
      inflight = false;
      loading = false;
    }
  }

  $effect(() => {
    void load(connectionId);
  });

  // Filter by alias name, target index, or the alias filter expression — all
  // three are what people actually search for on this page.
  function visible(): AliasRow[] {
    const q = filterText.trim().toLowerCase();
    return aliases.filter((row) => {
      if (onlyFiltered && !row.filter) return false;
      if (onlyHidden && !row.hidden) return false;
      if (!q) return true;
      return (
        row.alias.toLowerCase().includes(q) ||
        row.index.toLowerCase().includes(q) ||
        row.filter.toLowerCase().includes(q)
      );
    });
  }

  function aliasCount(index: string): number {
    return aliases.filter((a) => a.index === index).length;
  }

  function routingText(row: AliasRow): string {
    const index = row.indexRouting;
    const search = row.searchRouting;
    if (!index && !search) return "";
    if (index === search) return index;
    if (index && search) return `idx ${index} / search ${search}`;
    return index ? `idx ${index}` : `search ${search}`;
  }

  function askRemove(row: AliasRow): void {
    pending = {
      body: aliasActionsBody([{ remove: { index: row.index, alias: row.alias } }]),
      label: t("aliases.remove"),
      scope: t("aliases.scope.aliasIndex", { alias: row.alias, index: row.index }),
    };
  }

  function describeAdd(add: AliasAdd): string {
    const parts = [t("aliases.scope.aliasIndex", { alias: add.alias, index: add.index })];
    if (add.filter) parts.push(`filter ${JSON.stringify(add.filter)}`);
    if (add.routing) parts.push(`routing ${add.routing}`);
    if (add.index_routing) parts.push(`index_routing ${add.index_routing}`);
    if (add.search_routing) parts.push(`search_routing ${add.search_routing}`);
    if (add.is_write_index !== undefined) parts.push(`is_write_index ${add.is_write_index}`);
    if (add.is_hidden !== undefined) parts.push(`is_hidden ${add.is_hidden}`);
    return parts.join(" · ");
  }

  function askAdd(): void {
    addError = "";
    const index = newIndex.trim();
    const alias = newAlias.trim();
    if (!index) {
      addError = t("aliases.err.noIndex");
      return;
    }
    if (!alias) {
      addError = t("aliases.err.noAlias");
      return;
    }
    if (!/^[a-z0-9._-]+$/i.test(alias)) {
      addError = t("aliases.err.aliasCharset");
      return;
    }
    const parsed = parseAliasFilter(newFilter);
    if (!parsed.ok) {
      addError = parsed.error;
      return;
    }
    const add: AliasAdd = { index, alias };
    if (parsed.value) add.filter = parsed.value;
    if (newRouting.trim()) add.routing = newRouting.trim();
    if (newIndexRouting.trim()) add.index_routing = newIndexRouting.trim();
    if (newSearchRouting.trim()) add.search_routing = newSearchRouting.trim();
    if (newWriteIndex !== "") add.is_write_index = newWriteIndex === "true";
    if (newHidden !== "") add.is_hidden = newHidden === "true";
    pending = { body: aliasActionsBody([{ add }]), label: t("aliases.add"), scope: describeAdd(add) };
  }

  function cancel(): void {
    pending = null;
  }

  async function confirm(): Promise<void> {
    if (!pending) return;
    busy = true;
    toast = "";
    try {
      const res = await single(bridge, connectionId, "POST", "/_aliases", pending.body);
      if (isError(res)) {
        toast = `${pending.label} failed: ${res.error?.message ?? "HTTP " + res.status}`;
      } else {
        toast = `${pending.label} succeeded`;
        newAlias = "";
        newFilter = "";
        newRouting = "";
        newIndexRouting = "";
        newSearchRouting = "";
        newWriteIndex = "";
        newHidden = "";
        await load(connectionId);
      }
    } finally {
      busy = false;
      pending = null;
    }
  }
</script>

<div class="page">
  <header>
    <div>
      <h1>{t("aliases.title")}</h1>
      <p>{t("aliases.hintPrefix")}<code>GET /_alias</code>{t("aliases.hintMid")}<code>POST /_aliases</code>{t("aliases.hintSuffix")}</p>
    </div>
    <button type="button" onclick={() => load(connectionId)} disabled={loading || !connectionId}>
      {loading ? t("common.loading") : t("common.refresh")}
    </button>
  </header>

  {#if toast}<div class="toast">{toast}</div>{/if}

  <section class="panel op">
    <h2>{t("aliases.section.add")}</h2>
    <div class="grid">
      <label>{t("aliases.form.index")}
        <select bind:value={newIndex} disabled={indices.length === 0}>
          {#if indices.length === 0}<option value="">{t("aliases.form.noIndex")}</option>{/if}
          {#each indices as ix (ix.index)}
            <option value={ix.index}>{t("aliases.form.indexOption", { index: ix.index, count: aliasCount(ix.index) })}</option>
          {/each}
        </select>
      </label>
      <label class="span2">{t("aliases.form.alias")}
        <input placeholder={t("aliases.form.aliasPlaceholder")} bind:value={newAlias} />
      </label>
      <label class="span3">{t("aliases.form.filter")}
        <textarea
          rows="2"
          placeholder={t("aliases.form.filterPlaceholder")}
          bind:value={newFilter}
        ></textarea>
      </label>
      <label>{t("aliases.form.routing")}
        <input placeholder={t("aliases.form.routingPlaceholder")} bind:value={newRouting} />
      </label>
      <label>index_routing
        <input placeholder={t("aliases.form.followRouting")} bind:value={newIndexRouting} />
      </label>
      <label>search_routing
        <input placeholder={t("aliases.form.followRouting")} bind:value={newSearchRouting} />
      </label>
      <label>is_write_index
        <select bind:value={newWriteIndex}>
          <option value="">{t("aliases.form.notSet")}</option>
          <option value="true">true</option>
          <option value="false">false</option>
        </select>
      </label>
      <label>is_hidden
        <select bind:value={newHidden}>
          <option value="">{t("aliases.form.notSet")}</option>
          <option value="true">true</option>
          <option value="false">false</option>
        </select>
      </label>
      <div class="submit">
        <button type="button" class="primary" onclick={askAdd} disabled={!newIndex || !newAlias.trim()}>{t("aliases.form.addButton")}</button>
      </div>
    </div>
    {#if addError}<p class="form-error">{addError}</p>{/if}
  </section>

  <section class="panel">
    <h2>{t("aliases.section.list")} <span class="count">{visible().length} / {aliases.length}</span></h2>
    <div class="filters">
      <input placeholder={t("aliases.filterPlaceholder")} bind:value={filterText} />
      <label class="check"><input type="checkbox" bind:checked={onlyFiltered} />{t("aliases.onlyFiltered")}</label>
      <label class="check"><input type="checkbox" bind:checked={onlyHidden} />{t("aliases.onlyHidden")}</label>
    </div>

    {#if loading && aliases.length === 0}
      <StateBlock kind="loading" message={t("aliases.loadingMessage")} detail="GET /_alias" />
    {:else if error}
      <StateBlock kind="error" message={t("aliases.loadFailed")} detail={error === "NO_CONNECTION" ? t("common.noConnection") : error} />
    {:else if aliases.length === 0}
      <StateBlock kind="empty" message={t("aliases.empty")} detail={t("aliases.emptyDetail")} />
    {:else if visible().length === 0}
      <StateBlock kind="empty" message={t("aliases.noMatch")} detail={t("aliases.noMatchDetail")} />
    {:else}
      <table class="ax">
        <thead>
          <tr>
            <th>{t("aliases.col.alias")}</th>
            <th>{t("aliases.col.index")}</th>
            <th title={t("aliases.tip.write")}>{t("aliases.col.write")}</th>
            <th title={t("aliases.tip.filter")}>{t("aliases.col.filter")}</th>
            <th title={t("aliases.tip.routing")}>{t("aliases.col.routing")}</th>
            <th title={t("aliases.tip.hidden")}>{t("aliases.col.hidden")}</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          {#each visible() as row (row.alias + "::" + row.index)}
            <tr>
              <td class="mono">{row.alias}</td>
              <td class="mono">{row.index}</td>
              <td class="center">{#if row.writeIndex}<span class="tag write">write</span>{:else}<span class="muted">—</span>{/if}</td>
              <td class="mono">
                {#if row.filter}
                  <code class="expr" title={row.filter}>{row.filter}</code>
                {:else}
                  <span class="muted">—</span>
                {/if}
              </td>
              <td class="mono">{routingText(row) || "—"}</td>
              <td class="center">{#if row.hidden}<span class="tag">hidden</span>{:else}<span class="muted">—</span>{/if}</td>
              <td class="actions">
                <button type="button" class="mini danger" onclick={() => askRemove(row)}>{t("aliases.removeButton")}</button>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </section>
</div>

<ConfirmDanger
  open={pending !== null}
  title={pending ? pending.label : ""}
  message={t("aliases.confirm.message")}
  scope={pending ? pending.scope : ""}
  confirmLabel={pending ? pending.label : t("common.confirm")}
  onConfirm={confirm}
  onCancel={cancel}
/>

<style>
  .page { max-width: 1180px; }
  header { display: flex; justify-content: space-between; align-items: flex-start; gap: 16px; margin-bottom: 16px; }
  h1 { margin: 0 0 4px; font-size: var(--es-fs-3xl); }
  header p { margin: 0; color: var(--es-muted); }
  header code { font-family: var(--es-mono); font-size: var(--es-fs-xs); }
  button { padding: 6px 14px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); cursor: pointer; white-space: nowrap; }
  button:hover:not(:disabled):not(.primary) { background: var(--es-surface-2); }
  .primary { background: var(--es-accent); color: #fff; border-color: var(--es-accent); }
  .primary:hover:not(:disabled) { filter: brightness(0.92); }
  button:disabled { opacity: 0.55; cursor: default; }
  .toast { padding: 8px 12px; margin-bottom: 12px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface-2); font-size: var(--es-fs-sm); }
  .panel { border: 1px solid var(--es-border); border-radius: var(--es-radius); background: var(--es-surface); padding: 14px; margin-bottom: 14px; }
  .panel.op { border-left: 3px solid var(--es-accent); }
  .panel h2 { margin: 0 0 12px; font-size: var(--es-fs-sm); text-transform: uppercase; letter-spacing: 0.08em; color: var(--es-muted); }
  .count { margin-left: 6px; font-family: var(--es-mono); letter-spacing: 0; text-transform: none; }
  .grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 10px 12px; align-items: end; }
  .grid label { display: flex; flex-direction: column; gap: 4px; font-size: var(--es-fs-xs); color: var(--es-muted); }
  .grid .span2 { grid-column: span 2; }
  .grid .span3 { grid-column: 1 / -1; }
  .grid input, .grid select, .grid textarea { padding: 6px 10px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); width: 100%; }
  .grid textarea { font-family: var(--es-mono); font-size: var(--es-fs-xs); resize: vertical; min-height: 46px; }
  .submit { display: flex; justify-content: flex-end; }
  .form-error { margin: 10px 0 0; font-size: var(--es-fs-xs); color: var(--es-red); }
  .filters { display: flex; gap: 12px; align-items: center; margin-bottom: 12px; flex-wrap: wrap; }
  .filters input:not([type]) { padding: 6px 10px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); min-width: 300px; flex: 1; }
  .filters .check { display: flex; align-items: center; gap: 6px; font-size: var(--es-fs-xs); color: var(--es-muted); white-space: nowrap; }
  table.ax { width: 100%; border-collapse: collapse; font-size: var(--es-fs-sm); border: 1px solid var(--es-border); border-radius: var(--es-radius); overflow: hidden; }
  .ax th, .ax td { padding: 8px 10px; border-bottom: 1px solid var(--es-border); white-space: nowrap; vertical-align: middle; }
  .ax th { background: var(--es-surface-2); color: var(--es-muted); font-weight: 600; text-align: left; }
  .ax td.mono { font-family: var(--es-mono); }
  .ax td.center { text-align: center; }
  .ax tbody tr:hover { background: var(--es-surface-2); }
  .actions { text-align: right; }
  .mini { padding: 3px 8px; font-size: var(--es-fs-xs); }
  .mini.danger { color: var(--es-red); border-color: var(--es-red); }
  .mini.danger:hover { background: var(--es-red); color: #fff; }
  .muted { color: var(--es-muted); }
  .tag { display: inline-block; padding: 1px 6px; border-radius: 4px; font-size: var(--es-fs-2xs); font-family: var(--es-mono); border: 1px solid var(--es-border); }
  .tag.write { color: var(--es-accent); border-color: var(--es-accent); }
  .expr { display: inline-block; max-width: 320px; overflow: hidden; text-overflow: ellipsis; vertical-align: bottom; font-size: var(--es-fs-xs); color: var(--es-muted); }
</style>
