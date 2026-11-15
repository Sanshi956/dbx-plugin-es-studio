<script lang="ts">
  import StateBlock from "../components/StateBlock.svelte";
  import ConfirmDanger from "../components/ConfirmDanger.svelte";
  import {
    fetchAliases, fetchIndices, single, isError, aliasActionsBody,
    type AliasRow, type IndexRow,
  } from "../lib/services";
  import type { DbxBridge } from "../lib/bridge";

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

  let filterText = $state("");
  let onlyFiltered = $state(false);

  let newIndex = $state("");
  let newAlias = $state("");
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

  function askRemove(row: AliasRow): void {
    pending = {
      body: aliasActionsBody([{ remove: { index: row.index, alias: row.alias } }]),
      label: "移除别名",
      scope: `别名 ${row.alias} → 索引 ${row.index}`,
    };
  }

  function askAdd(): void {
    if (!newIndex.trim() || !newAlias.trim()) {
      toast = "请选择索引并填写别名";
      return;
    }
    if (!/^[a-z0-9._-]+$/i.test(newAlias.trim())) {
      toast = "别名只能包含字母、数字、点、下划线与连字符（ES 命名规则）";
      return;
    }
    pending = {
      body: aliasActionsBody([{ add: { index: newIndex.trim(), alias: newAlias.trim() } }]),
      label: "新增别名",
      scope: `别名 ${newAlias.trim()} → 索引 ${newIndex.trim()}`,
    };
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
      <h1>Aliases</h1>
      <p>Aliases from <code>/_cat/aliases</code>; add / remove via <code>POST /_aliases</code>.</p>
    </div>
    <button type="button" onclick={() => load(connectionId)} disabled={loading || !connectionId}>
      {loading ? "Loading…" : "Refresh"}
    </button>
  </header>

  {#if toast}<div class="toast">{toast}</div>{/if}

  <section class="add">
    <label>索引
      <select bind:value={newIndex} disabled={indices.length === 0}>
        {#if indices.length === 0}<option value="">（没有可用索引）</option>{/if}
        {#each indices as ix (ix.index)}
          <option value={ix.index}>{ix.index} · {aliasCount(ix.index)} 个别名</option>
        {/each}
      </select>
    </label>
    <label class="grow">别名
      <input placeholder="例如 prod-view" bind:value={newAlias} />
    </label>
    <button type="button" class="primary" onclick={askAdd} disabled={!newIndex || !newAlias.trim()}>Add alias</button>
  </section>

  {#if loading && aliases.length === 0}
    <StateBlock kind="loading" message="Loading aliases…" detail="GET /_cat/aliases" />
  {:else if error}
    <StateBlock kind="error" message="Could not load aliases" detail={error === "NO_CONNECTION" ? "No DBX connection is selected." : error} />
  {:else if aliases.length === 0}
    <StateBlock kind="empty" message="No aliases returned" detail="用上面的表单给某个索引建个别名" />
  {:else}
    <section class="filter-row">
      <input placeholder="筛选：别名 / 索引 / 过滤条件" bind:value={filterText} />
      <label class="check"><input type="checkbox" bind:checked={onlyFiltered} />只看带过滤条件的</label>
      <span class="count">{visible().length} / {aliases.length}</span>
    </section>

    {#if visible().length === 0}
      <StateBlock kind="empty" message="没有匹配的别名" detail="清空筛选条件试试" />
    {:else}
      <table class="ax">
        <thead>
          <tr>
            <th>Alias</th>
            <th>Index</th>
            <th title="同一别名指向多个索引时，写操作落到被标记的那个">Write</th>
            <th title="挂在别名上的过滤查询（alias filter）：通过该别名只能命中匹配的文档">Filter</th>
            <th title="读写使用的 routing 值">Routing</th>
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
              <td class="mono">{row.routing || "—"}</td>
              <td class="actions">
                <button type="button" class="mini danger" onclick={() => askRemove(row)}>Remove</button>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  {/if}
</div>

<ConfirmDanger
  open={pending !== null}
  title={pending ? pending.label : ""}
  message="别名变更通过 ES REST 执行，Go Sidecar 会先校验只读模式与路径安全。"
  scope={pending ? pending.scope : ""}
  confirmLabel={pending ? pending.label : "确认"}
  onConfirm={confirm}
  onCancel={cancel}
/>

<style>
  .page { max-width: 1100px; }
  header { display: flex; justify-content: space-between; align-items: flex-start; gap: 16px; margin-bottom: 16px; }
  h1 { margin: 0 0 4px; font-size: 18px; }
  header p { margin: 0; color: var(--es-muted); }
  header code { font-family: var(--es-mono); font-size: 11px; }
  button { padding: 6px 14px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); cursor: pointer; white-space: nowrap; }
  button:hover:not(:disabled):not(.primary) { background: var(--es-surface-2); }
  .primary:hover:not(:disabled) { filter: brightness(0.92); }
  button:disabled { opacity: 0.55; cursor: default; }
  .toast { padding: 8px 12px; margin-bottom: 12px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface-2); font-size: 12px; }
  .add { display: flex; gap: 8px; margin-bottom: 14px; align-items: end; }
  .add label { display: flex; flex-direction: column; gap: 4px; font-size: 11px; color: var(--es-muted); }
  .add label.grow { flex: 1; }
  .add select, .add input { padding: 6px 10px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); width: 100%; }
  .add .primary { background: var(--es-accent); color: #fff; border-color: var(--es-accent); }
  .filter-row { display: flex; gap: 12px; align-items: center; margin-bottom: 12px; flex-wrap: wrap; }
  .filter-row input:not([type]) { padding: 6px 10px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); min-width: 260px; }
  .filter-row .check { display: flex; align-items: center; gap: 6px; font-size: 11px; color: var(--es-muted); }
  .filter-row .count { font-family: var(--es-mono); font-size: 11px; color: var(--es-muted); }
  table.ax { width: 100%; border-collapse: collapse; font-size: 12px; border: 1px solid var(--es-border); border-radius: var(--es-radius); overflow: hidden; }
  .ax th, .ax td { padding: 8px 10px; border-bottom: 1px solid var(--es-border); white-space: nowrap; vertical-align: middle; }
  .ax th { background: var(--es-surface-2); color: var(--es-muted); font-weight: 600; text-align: left; }
  .ax td.mono { font-family: var(--es-mono); }
  .ax td.center { text-align: center; }
  .ax tbody tr:hover { background: var(--es-surface-2); }
  .actions { text-align: right; }
  .mini { padding: 3px 8px; font-size: 11px; }
  .mini.danger { color: var(--es-red); border-color: var(--es-red); }
  .mini.danger:hover { background: var(--es-red); color: #fff; }
  .muted { color: var(--es-muted); }
  .tag { display: inline-block; padding: 1px 6px; border-radius: 4px; font-size: 10px; font-family: var(--es-mono); border: 1px solid var(--es-border); }
  .tag.write { color: var(--es-accent); border-color: var(--es-accent); }
  .expr { display: inline-block; max-width: 420px; overflow: hidden; text-overflow: ellipsis; vertical-align: bottom; font-size: 11px; color: var(--es-muted); }
</style>
