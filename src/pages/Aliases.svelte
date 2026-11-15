<script lang="ts">
  import StateBlock from "../components/StateBlock.svelte";
  import ConfirmDanger from "../components/ConfirmDanger.svelte";
  import { fetchAliases, single, isError, aliasActionsBody, type AliasRow } from "../lib/services";
  import type { DbxBridge } from "../lib/bridge";

  interface Props {
    bridge: DbxBridge;
    connectionId: string;
  }
  let { bridge, connectionId }: Props = $props();

  let aliases = $state<AliasRow[]>([]);
  let loading = $state(true);
  let error = $state("");
  let toast = $state("");
  let inflight = false;

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
      aliases = await fetchAliases(bridge, id);
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

  function askRemove(row: AliasRow): void {
    pending = {
      body: aliasActionsBody([{ remove: { index: row.index, alias: row.alias } }]),
      label: "移除别名",
      scope: `别名 ${row.alias} → 索引 ${row.index}`,
    };
  }

  function askAdd(): void {
    if (!newIndex.trim() || !newAlias.trim()) {
      toast = "请填写索引名与别名";
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
        newIndex = "";
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
      <p>Index aliases from <code>/_cat/aliases</code>. Add or remove through <code>POST /_aliases</code>.</p>
    </div>
    <button type="button" onclick={() => load(connectionId)} disabled={loading || !connectionId}>
      {loading ? "Loading…" : "Refresh"}
    </button>
  </header>

  {#if toast}<div class="toast">{toast}</div>{/if}

  <section class="add">
    <input placeholder="Index name" bind:value={newIndex} />
    <input placeholder="Alias name" bind:value={newAlias} />
    <button type="button" class="primary" onclick={askAdd}>Add alias</button>
  </section>

  {#if loading && aliases.length === 0}
    <StateBlock kind="loading" message="Loading aliases…" detail="GET /_cat/aliases" />
  {:else if error}
    <StateBlock kind="error" message="Could not load aliases" detail={error === "NO_CONNECTION" ? "No DBX connection is selected." : error} />
  {:else if aliases.length === 0}
    <StateBlock kind="empty" message="No aliases returned" />
  {:else}
    <table class="ax">
      <thead>
        <tr><th>Alias</th><th>Index</th><th>Filter</th><th></th></tr>
      </thead>
      <tbody>
        {#each aliases as row (row.alias + "::" + row.index)}
          <tr>
            <td class="mono">{row.alias}</td>
            <td class="mono">{row.index}</td>
            <td class="mono">{row.filter || "—"}</td>
            <td class="actions">
              <button type="button" class="mini danger" onclick={() => askRemove(row)}>Remove</button>
            </td>
          </tr>
        {/each}
      </tbody>
    </table>
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
  .page { max-width: 1000px; }
  header { display: flex; justify-content: space-between; align-items: flex-start; gap: 16px; margin-bottom: 18px; }
  h1 { margin: 0 0 4px; font-size: 18px; }
  header p { margin: 0; color: var(--es-muted); }
  header code { font-family: var(--es-mono); font-size: 11px; }
  button { padding: 6px 14px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); cursor: pointer; }
  button:hover:not(:disabled) { background: var(--es-surface-2); }
  button:disabled { opacity: 0.55; cursor: default; }
  .toast { padding: 8px 12px; margin-bottom: 12px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface-2); font-size: 12px; }
  .add { display: flex; gap: 8px; margin-bottom: 16px; }
  .add input { padding: 6px 10px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); flex: 1; }
  .add .primary { background: var(--es-accent); color: #fff; border-color: var(--es-accent); }
  table.ax { width: 100%; border-collapse: collapse; font-size: 12px; border: 1px solid var(--es-border); border-radius: var(--es-radius); overflow: hidden; }
  .ax th, .ax td { padding: 8px 10px; border-bottom: 1px solid var(--es-border); white-space: nowrap; }
  .ax th { background: var(--es-surface-2); color: var(--es-muted); font-weight: 600; text-align: left; }
  .ax td.mono { font-family: var(--es-mono); }
  .ax tbody tr:hover { background: var(--es-surface-2); }
  .actions { text-align: right; }
  .mini { padding: 3px 8px; font-size: 11px; }
  .mini.danger { color: var(--es-red); border-color: var(--es-red); }
  .mini.danger:hover { background: var(--es-red); color: #fff; }
</style>
