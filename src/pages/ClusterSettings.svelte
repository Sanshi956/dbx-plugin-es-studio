<script lang="ts">
  import StateBlock from "../components/StateBlock.svelte";
  import ConfirmDanger from "../components/ConfirmDanger.svelte";
  import { fetchClusterSettings, updateClusterSettings, isError, type ClusterSettingsSnapshot } from "../lib/services";
  import type { DbxBridge } from "../lib/bridge";

  interface Props {
    bridge: DbxBridge;
    connectionId: string;
  }
  let { bridge, connectionId }: Props = $props();

  let snapshot = $state<ClusterSettingsSnapshot>({ persistent: {}, transient: {}, defaults: {} });
  let loading = $state(true);
  let error = $state("");
  let toast = $state("");
  let filter = $state("");
  let includeDefaults = $state(false);

  let scope = $state<"persistent" | "transient">("persistent");
  let editKey = $state("");
  let editValue = $state("");
  let formError = $state("");
  let pending = $state<{ mode: "set" | "remove"; scope: "persistent" | "transient"; key: string; value: string } | null>(null);

  async function load(): Promise<void> {
    loading = true;
    error = "";
    try {
      snapshot = await fetchClusterSettings(bridge, connectionId, includeDefaults);
    } catch (cause) {
      error = cause instanceof Error ? cause.message : String(cause);
    } finally {
      loading = false;
    }
  }
  $effect(() => {
    void includeDefaults;
    void load();
  });

  function entries(source: Record<string, string>): { key: string; value: string }[] {
    const q = filter.trim().toLowerCase();
    return Object.entries(source)
      .filter(([k]) => (q ? k.toLowerCase().includes(q) : true))
      .map(([key, value]) => ({ key, value }))
      .sort((a, b) => a.key.localeCompare(b.key));
  }

  function askSet(): void {
    formError = "";
    const key = editKey.trim();
    if (!key) {
      formError = "请填写设置项名称";
      return;
    }
    if (!key.startsWith("cluster.")) {
      formError = "集群设置项通常以 cluster. 开头（如 cluster.routing.allocation.enable）";
      return;
    }
    pending = { mode: "set", scope, key, value: editValue };
  }

  function askRemove(target: "persistent" | "transient", key: string): void {
    pending = { mode: "remove", scope: target, key, value: "" };
  }

  async function confirm(): Promise<void> {
    if (!pending) return;
    const job = pending;
    pending = null;
    const res = await updateClusterSettings(bridge, connectionId, job.scope, job.key, job.mode === "remove" ? "" : job.value);
    if (isError(res)) {
      toast = `失败：${res.error?.message ?? "HTTP " + res.status}`;
    } else {
      toast = job.mode === "remove" ? `已移除 ${job.key}` : `已写入 ${job.scope}.${job.key} = ${job.value}`;
      if (job.mode === "set") editKey = "";
      editValue = "";
      await load();
    }
  }
</script>

<div class="page">
  <header>
    <div>
      <h1>Cluster Settings</h1>
      <p>Persistent and transient cluster settings from <code>GET /_cluster/settings?flat_settings=true</code>.</p>
    </div>
    <label class="toggle"><input type="checkbox" bind:checked={includeDefaults} /> include defaults</label>
  </header>

  {#if toast}<div class="toast">{toast}</div>{/if}
  {#if error}<StateBlock kind="error" message="Failed to read cluster settings" detail={error} />{/if}

  <section class="writer">
    <h2>写入 / 移除设置</h2>
    <div class="fields">
      <label>作用域
        <select bind:value={scope}>
          <option value="persistent">persistent</option>
          <option value="transient">transient</option>
        </select>
      </label>
      <label class="wide">设置项<input placeholder="cluster.routing.allocation.enable" bind:value={editKey} /></label>
      <label class="wide">值<input placeholder='all / none / "new_primaries" / 30s（留空或 null = 移除）' bind:value={editValue} /></label>
      <button type="button" class="primary" onclick={askSet}>应用</button>
    </div>
    {#if formError}<p class="form-error">{formError}</p>{/if}
  </section>

  <div class="filter-row">
    <label>Filter<input placeholder="按设置项名过滤（defaults 有数百项，建议先输关键字）" bind:value={filter} /></label>
  </div>

  {#if loading}
    <StateBlock kind="loading" message="Reading cluster settings…" />
  {:else}
    {#each [{ id: "persistent", label: "Persistent" }, { id: "transient", label: "Transient" }] as group (group.id)}
      <section class="group">
        <h2>{group.label} <span class="count">{entries(snapshot[group.id as "persistent" | "transient"]).length}</span></h2>
        {#if entries(snapshot[group.id as "persistent" | "transient"]).length === 0}
          <p class="empty">（无）</p>
        {:else}
          <table>
            <thead><tr><th>Setting</th><th>Value</th><th></th></tr></thead>
            <tbody>
              {#each entries(snapshot[group.id as "persistent" | "transient"]) as row (`${group.id}:${row.key}`)}
                <tr>
                  <td class="mono">{row.key}</td>
                  <td class="mono value">{row.value}</td>
                  <td class="act"><button type="button" class="mini danger" onclick={() => askRemove(group.id as "persistent" | "transient", row.key)}>移除</button></td>
                </tr>
              {/each}
            </tbody>
          </table>
        {/if}
      </section>
    {/each}

    {#if includeDefaults}
      <section class="group">
        <h2>Defaults <span class="count">{entries(snapshot.defaults).length}</span></h2>
        <p class="empty">默认值只读；共 {Object.keys(snapshot.defaults).length} 项，上面最多显示 200 行，用 Filter 缩小范围。</p>
        <table>
          <thead><tr><th>Setting</th><th>Value</th></tr></thead>
          <tbody>
            {#each entries(snapshot.defaults).slice(0, 200) as row (row.key)}
              <tr><td class="mono">{row.key}</td><td class="mono value">{row.value}</td></tr>
            {/each}
          </tbody>
        </table>
      </section>
    {/if}
  {/if}
</div>

<ConfirmDanger
  open={pending !== null}
  title={pending?.mode === "remove" ? "移除集群设置" : "修改集群设置"}
  message="集群级设置影响所有节点与索引；错误的值可能阻断分片分配、触发全集群重分配，或让集群进入不可恢复状态。"
  scope={pending ? `${pending.scope}.${pending.key}${pending.mode === "remove" ? " → 移除（null）" : ` = ${pending.value || "null"}`}` : ""}
  confirmLabel={pending?.mode === "remove" ? "移除设置" : "写入设置"}
  onConfirm={confirm}
  onCancel={() => (pending = null)}
/>

<style>
  .page { max-width: 1080px; }
  header { display: flex; justify-content: space-between; align-items: start; gap: 16px; margin-bottom: 16px; }
  h1 { margin: 0 0 4px; font-size: 18px; }
  header p { margin: 0; color: var(--es-muted); }
  header code { font-family: var(--es-mono); font-size: 11px; }
  .toggle { display: flex; align-items: center; gap: 6px; font-size: 11px; color: var(--es-muted); white-space: nowrap; }
  button { padding: 6px 14px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); cursor: pointer; white-space: nowrap; }
  button:hover:not(:disabled):not(.primary) { background: var(--es-surface-2); }
  .primary { background: var(--es-accent); color: #fff; border-color: var(--es-accent); }
  .primary:hover:not(:disabled) { filter: brightness(0.92); }
  .mini { padding: 3px 8px; font-size: 11px; }
  .mini.danger { color: var(--es-red); border-color: var(--es-red); }
  .mini.danger:hover { background: var(--es-red); color: #fff; }
  .toast { padding: 8px 12px; margin-bottom: 12px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface-2); font-size: 12px; }
  .writer, .group { border: 1px solid var(--es-border); border-radius: var(--es-radius); padding: 14px; margin-bottom: 14px; background: var(--es-surface); }
  .writer h2, .group h2 { margin: 0 0 10px; font-size: 12px; text-transform: uppercase; letter-spacing: 0.08em; color: var(--es-muted); }
  .count { margin-left: 6px; font-family: var(--es-mono); letter-spacing: 0; text-transform: none; }
  .fields { display: flex; gap: 12px; align-items: end; flex-wrap: wrap; }
  .fields label { display: flex; flex-direction: column; gap: 4px; font-size: 11px; color: var(--es-muted); }
  .fields .wide { flex: 1 1 260px; }
  .fields input, .fields select, .filter-row input { padding: 6px 10px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); width: 100%; }
  .filter-row { margin-bottom: 14px; }
  .filter-row label { display: flex; flex-direction: column; gap: 4px; font-size: 11px; color: var(--es-muted); }
  .form-error { margin: 8px 0 0; font-size: 11px; color: var(--es-red); }
  table { width: 100%; border-collapse: collapse; font-size: 12px; }
  th, td { padding: 5px 8px; border-bottom: 1px solid var(--es-border); text-align: left; }
  th { color: var(--es-muted); font-size: 11px; font-weight: 600; }
  .mono { font-family: var(--es-mono); }
  .value { word-break: break-all; }
  .act { text-align: right; white-space: nowrap; }
  .empty { margin: 0; font-size: 11px; color: var(--es-muted); }
</style>
