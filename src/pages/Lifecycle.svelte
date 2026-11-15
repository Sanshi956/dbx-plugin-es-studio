<script lang="ts">
  import StateBlock from "../components/StateBlock.svelte";
  import ConfirmDanger from "../components/ConfirmDanger.svelte";
  import { handleEditorKey } from "../lib/editorKeys";
  import {
    fetchIlmStatus, fetchIlmPolicies, fetchIlmExplain, setIlmStatus, deleteIlmPolicy, putIlmPolicy, isError,
    type IlmStatus, type IlmPolicyRow, type IlmExplainRow,
  } from "../lib/services";
  import type { DbxBridge } from "../lib/bridge";

  interface Props {
    bridge: DbxBridge;
    connectionId: string;
  }
  let { bridge, connectionId }: Props = $props();

  let status = $state<IlmStatus>({ mode: "unknown" });
  let policies = $state<IlmPolicyRow[]>([]);
  let explain = $state<IlmExplainRow[]>([]);
  let loading = $state(true);
  let error = $state("");
  let toast = $state("");

  let creating = $state(false);
  let newName = $state("");
  let draft = $state(`{
  "policy": {
    "phases": {
      "hot":    { "actions": { "rollover": { "max_primary_shard_size": "1gb" } } },
      "delete": { "min_age": "7d", "actions": { "delete": {} } }
    }
  }
}`);
  let formError = $state("");

  let pendingStatus = $state<"start" | "stop" | null>(null);
  let pendingDelete = $state<IlmPolicyRow | null>(null);
  let pendingCreate = $state(false);

  async function load(): Promise<void> {
    loading = true;
    error = "";
    try {
      const [s, p, e] = await Promise.all([
        fetchIlmStatus(bridge, connectionId),
        fetchIlmPolicies(bridge, connectionId),
        fetchIlmExplain(bridge, connectionId),
      ]);
      status = s;
      policies = p;
      explain = e;
      if (s.error) error = s.error;
    } catch (cause) {
      error = cause instanceof Error ? cause.message : String(cause);
    } finally {
      loading = false;
    }
  }
  $effect(() => {
    void load();
  });

  function formatDate(raw: string): string {
    if (!raw) return "-";
    const date = new Date(raw);
    return Number.isNaN(date.getTime()) ? raw : date.toLocaleString();
  }

  function askCreate(): void {
    formError = "";
    if (!newName.trim()) {
      formError = "请填写策略名";
      return;
    }
    try {
      JSON.parse(draft);
    } catch (cause) {
      formError = `JSON 解析失败：${cause instanceof Error ? cause.message : String(cause)}`;
      return;
    }
    pendingCreate = true;
  }

  async function confirmCreate(): Promise<void> {
    pendingCreate = false;
    const res = await putIlmPolicy(bridge, connectionId, newName.trim(), JSON.parse(draft));
    if (isError(res)) {
      toast = `写入失败：${res.error?.message ?? "HTTP " + res.status}`;
    } else {
      toast = `已写入 ILM 策略 ${newName.trim()}`;
      creating = false;
      newName = "";
      await load();
    }
  }

  async function confirmStatus(): Promise<void> {
    if (!pendingStatus) return;
    const mode = pendingStatus;
    pendingStatus = null;
    const res = await setIlmStatus(bridge, connectionId, mode);
    toast = isError(res)
      ? `切换失败：${res.error?.message ?? "HTTP " + res.status}`
      : `ILM 已${mode === "stop" ? "停止（stopped）" : "启动（running）"}`;
    await load();
  }

  async function confirmDelete(): Promise<void> {
    if (!pendingDelete) return;
    const target = pendingDelete;
    pendingDelete = null;
    const res = await deleteIlmPolicy(bridge, connectionId, target.name);
    toast = isError(res)
      ? `删除失败：${res.error?.message ?? "HTTP " + res.status}`
      : `已删除 ILM 策略 ${target.name}`;
    await load();
  }
</script>

<div class="page">
  <header>
    <div>
      <h1>Lifecycle</h1>
      <p>Index Lifecycle Management: <code>_ilm/status</code>, <code>_ilm/policy</code> and <code>_ilm/explain</code>.</p>
    </div>
    <div class="status">
      <span class="mode {status.mode.toLowerCase()}">{status.mode}</span>
      <button type="button" disabled={status.mode === "RUNNING"} onclick={() => (pendingStatus = "start")}>Start</button>
      <button type="button" class="danger" disabled={status.mode === "STOPPED"} onclick={() => (pendingStatus = "stop")}>Stop</button>
    </div>
  </header>

  {#if toast}<div class="toast">{toast}</div>{/if}
  {#if error}<StateBlock kind="error" message="ILM API error" detail={error} />{/if}

  <section class="creator">
    <div class="bar">
      <h2>策略</h2>
      <button type="button" onclick={() => (creating = !creating)}>{creating ? "关闭编辑器" : "新建 / 写入策略"}</button>
    </div>
    {#if creating}
      <div class="fields">
        <label>策略名<input placeholder="p3-demo-policy" bind:value={newName} /></label>
        <button type="button" class="primary" onclick={askCreate}>写入</button>
      </div>
      <textarea
        spellcheck="false"
        rows="10"
        bind:value={draft}
        onkeydown={(event) => handleEditorKey(event, () => draft, (next) => (draft = next))}
      ></textarea>
      {#if formError}<p class="form-error">{formError}</p>{/if}
    {/if}
  </section>

  {#if loading}
    <StateBlock kind="loading" message="Reading ILM state…" />
  {:else}
    <section class="block">
      <h2>Policies <span class="count">{policies.length}</span></h2>
      {#if policies.length === 0}
        <p class="empty">（无策略）</p>
      {:else}
        <table>
          <thead><tr><th>Name</th><th>Phases</th><th>min_age</th><th>Ver</th><th>Modified</th><th></th></tr></thead>
          <tbody>
            {#each policies as row (row.name)}
              <tr>
                <td class="mono">{row.name}</td>
                <td>{row.phases.join(" → ") || "-"}</td>
                <td class="mono small">{row.minAges.join(" / ") || "-"}</td>
                <td class="mono">{row.version || "-"}</td>
                <td class="mono small">{formatDate(row.modified)}</td>
                <td class="act"><button type="button" class="mini danger" onclick={() => (pendingDelete = row)}>Delete</button></td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
    </section>

    <section class="block">
      <h2>Index explain <span class="count">{explain.length}</span></h2>
      {#if explain.length === 0}
        <p class="empty">（没有由 ILM 管理的索引）</p>
      {:else}
        <table>
          <thead><tr><th>Index</th><th>Managed</th><th>Policy</th><th>Phase</th><th>Action</th><th>Step</th><th>Error</th></tr></thead>
          <tbody>
            {#each explain as row (row.index)}
              <tr>
                <td class="mono">{row.index}</td>
                <td>{row.managed ? "yes" : "no"}</td>
                <td class="mono small">{row.policy || "-"}</td>
                <td>{row.phase || "-"}</td>
                <td class="small">{row.action || "-"}</td>
                <td class="small">{row.step || "-"}</td>
                <td class="small err">{row.error || ""}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
    </section>
  {/if}
</div>

<ConfirmDanger
  open={pendingStatus !== null}
  title={pendingStatus === "stop" ? "停止 ILM" : "启动 ILM"}
  message={pendingStatus === "stop"
    ? "停止后所有生命周期动作（rollover / shrink / forcemerge / delete）都会暂停，旧索引不会被按策略删除，磁盘可能持续增长。"
    : "启动后 ILM 会立即恢复处理积压的生命周期步骤，可能触发 rollover 与删除。"}
  scope="全局生命周期管理（所有被 ILM 管理的索引）"
  confirmLabel={pendingStatus === "stop" ? "停止 ILM" : "启动 ILM"}
  onConfirm={confirmStatus}
  onCancel={() => (pendingStatus = null)}
/>

<ConfirmDanger
  open={pendingCreate}
  title="写入 ILM 策略"
  message="策略写入后不会自动生效，需要索引通过 index.lifecycle.name 引用；错误的 delete 阶段会真实删除数据。"
  scope={newName ? `ILM 策略 ${newName}` : ""}
  confirmLabel="写入策略"
  onConfirm={confirmCreate}
  onCancel={() => (pendingCreate = false)}
/>

<ConfirmDanger
  open={pendingDelete !== null}
  title="删除 ILM 策略"
  message="仍引用该策略的索引会进入 ILM 错误状态（policy not found），生命周期动作停止。"
  scope={pendingDelete ? `ILM 策略 ${pendingDelete.name}` : ""}
  confirmLabel="删除策略"
  onConfirm={confirmDelete}
  onCancel={() => (pendingDelete = null)}
/>

<style>
  .page { max-width: 1120px; }
  header { display: flex; justify-content: space-between; align-items: start; gap: 16px; margin-bottom: 16px; }
  h1 { margin: 0 0 4px; font-size: 18px; }
  header p { margin: 0; color: var(--es-muted); }
  header code { font-family: var(--es-mono); font-size: 11px; }
  .status { display: flex; align-items: center; gap: 8px; }
  .mode { font-family: var(--es-mono); font-size: 11px; padding: 3px 8px; border-radius: 4px; border: 1px solid var(--es-border); }
  .mode.running { color: var(--es-green); border-color: var(--es-green); }
  .mode.stopped { color: var(--es-red); border-color: var(--es-red); }
  button { padding: 6px 14px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); cursor: pointer; white-space: nowrap; }
  button:hover:not(:disabled):not(.primary) { background: var(--es-surface-2); }
  .primary { background: var(--es-accent); color: #fff; border-color: var(--es-accent); }
  .primary:hover:not(:disabled) { filter: brightness(0.92); }
  button:disabled { opacity: 0.5; cursor: default; }
  button.danger { color: var(--es-red); border-color: var(--es-red); }
  .mini { padding: 3px 8px; font-size: 11px; }
  .mini.danger { color: var(--es-red); border-color: var(--es-red); }
  .mini.danger:hover { background: var(--es-red); color: #fff; }
  .toast { padding: 8px 12px; margin-bottom: 12px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface-2); font-size: 12px; }
  .creator, .block { border: 1px solid var(--es-border); border-radius: var(--es-radius); padding: 14px; margin-bottom: 14px; background: var(--es-surface); }
  .bar { display: flex; justify-content: space-between; align-items: center; margin-bottom: 10px; }
  .creator h2, .block h2 { margin: 0; font-size: 12px; text-transform: uppercase; letter-spacing: 0.08em; color: var(--es-muted); }
  .count { margin-left: 6px; font-family: var(--es-mono); letter-spacing: 0; text-transform: none; }
  .fields { display: flex; gap: 12px; align-items: end; flex-wrap: wrap; margin-bottom: 10px; }
  .fields label { display: flex; flex-direction: column; gap: 4px; font-size: 11px; color: var(--es-muted); }
  .fields input { padding: 6px 10px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); min-width: 220px; }
  textarea { width: 100%; box-sizing: border-box; font-family: var(--es-mono); font-size: 12px; padding: 10px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface-2); color: var(--es-text); }
  .form-error { margin: 8px 0 0; font-size: 11px; color: var(--es-red); }
  table { width: 100%; border-collapse: collapse; font-size: 12px; }
  th, td { padding: 5px 8px; border-bottom: 1px solid var(--es-border); text-align: left; }
  th { color: var(--es-muted); font-size: 11px; font-weight: 600; }
  .mono { font-family: var(--es-mono); }
  .small { font-size: 11px; }
  .err { color: var(--es-red); }
  .act { text-align: right; white-space: nowrap; }
  .empty { margin: 0; font-size: 11px; color: var(--es-muted); }
</style>
