<script lang="ts">
  import StateBlock from "../components/StateBlock.svelte";
  import ConfirmDanger from "../components/ConfirmDanger.svelte";
  import { handleEditorKey } from "../lib/editorKeys";
  import {
    fetchIlmStatus, fetchIlmPolicies, fetchIlmExplain, setIlmStatus, deleteIlmPolicy, putIlmPolicy, isError,
    type IlmStatus, type IlmPolicyRow, type IlmExplainRow,
  } from "../lib/services";
  import type { DbxBridge } from "../lib/bridge";
  import { t } from "../lib/i18n.svelte.js";

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
      formError = t("ilm.err.noName");
      return;
    }
    try {
      JSON.parse(draft);
    } catch (cause) {
      formError = t("common.jsonParseFailed", { detail: cause instanceof Error ? cause.message : String(cause) });
      return;
    }
    pendingCreate = true;
  }

  async function confirmCreate(): Promise<void> {
    pendingCreate = false;
    const res = await putIlmPolicy(bridge, connectionId, newName.trim(), JSON.parse(draft));
    if (isError(res)) {
      toast = t("common.toast.writeFailed", { detail: res.error?.message ?? "HTTP " + res.status });
    } else {
      toast = t("ilm.toast.written", { name: newName.trim() });
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
      ? t("ilm.toast.toggleFailed", { detail: res.error?.message ?? "HTTP " + res.status })
      : mode === "stop" ? t("ilm.toast.stopped") : t("ilm.toast.started");
    await load();
  }

  async function confirmDelete(): Promise<void> {
    if (!pendingDelete) return;
    const target = pendingDelete;
    pendingDelete = null;
    const res = await deleteIlmPolicy(bridge, connectionId, target.name);
    toast = isError(res)
      ? t("common.toast.deleteFailed", { detail: res.error?.message ?? "HTTP " + res.status })
      : t("ilm.toast.deleted", { name: target.name });
    await load();
  }
</script>

<div class="page">
  <header>
    <div>
      <h1>{t("ilm.title")}</h1>
      <p>{t("ilm.hint")}<code>_ilm/status</code> · <code>_ilm/policy</code> · <code>_ilm/explain</code></p>
    </div>
    <div class="status">
      <span class="mode {status.mode.toLowerCase()}">{status.mode}</span>
      <button type="button" disabled={status.mode === "RUNNING"} onclick={() => (pendingStatus = "start")}>{t("ilm.start")}</button>
      <button type="button" class="danger" disabled={status.mode === "STOPPED"} onclick={() => (pendingStatus = "stop")}>{t("ilm.stop")}</button>
    </div>
  </header>

  {#if toast}<div class="toast">{toast}</div>{/if}
  {#if error}<StateBlock kind="error" message={t("ilm.apiError")} detail={error} />{/if}

  <section class="creator">
    <div class="bar">
      <h2>{t("ilm.policies")}</h2>
      <button type="button" onclick={() => (creating = !creating)}>{creating ? t("tpl.closeEditor") : t("ilm.newPolicy")}</button>
    </div>
    {#if creating}
      <div class="fields">
        <label>{t("ilm.policyName")}<input placeholder="demo-policy" bind:value={newName} /></label>
        <button type="button" class="primary" onclick={askCreate}>{t("common.write")}</button>
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
    <StateBlock kind="loading" message={t("ilm.reading")} />
  {:else}
    <section class="block">
      <h2>{t("ilm.policies")} <span class="count">{policies.length}</span></h2>
      {#if policies.length === 0}
        <p class="empty">{t("ilm.noPolicies")}</p>
      {:else}
        <table>
          <thead><tr><th>{t("ilm.col.name")}</th><th>{t("ilm.col.phases")}</th><th>{t("ilm.col.minAge")}</th><th>{t("ilm.col.ver")}</th><th>{t("ilm.col.modified")}</th><th></th></tr></thead>
          <tbody>
            {#each policies as row (row.name)}
              <tr>
                <td class="mono">{row.name}</td>
                <td>{row.phases.join(" → ") || "-"}</td>
                <td class="mono small">{row.minAges.join(" / ") || "-"}</td>
                <td class="mono">{row.version || "-"}</td>
                <td class="mono small">{formatDate(row.modified)}</td>
                <td class="act"><button type="button" class="mini danger" onclick={() => (pendingDelete = row)}>{t("common.delete")}</button></td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
    </section>

    <section class="block">
      <h2>{t("ilm.explain")} <span class="count">{explain.length}</span></h2>
      {#if explain.length === 0}
        <p class="empty">{t("ilm.noManaged")}</p>
      {:else}
        <table>
          <thead><tr><th>{t("ilm.col.index")}</th><th>{t("ilm.col.managed")}</th><th>{t("ilm.col.policy")}</th><th>{t("ilm.col.phase")}</th><th>{t("ilm.col.action")}</th><th>{t("ilm.col.step")}</th><th>{t("ilm.col.error")}</th></tr></thead>
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
  title={pendingStatus === "stop" ? t("ilm.confirm.stopTitle") : t("ilm.confirm.startTitle")}
  message={pendingStatus === "stop" ? t("ilm.confirm.stopMessage") : t("ilm.confirm.startMessage")}
  scope={t("ilm.confirm.statusScope")}
  confirmLabel={pendingStatus === "stop" ? t("ilm.confirm.stopTitle") : t("ilm.confirm.startTitle")}
  onConfirm={confirmStatus}
  onCancel={() => (pendingStatus = null)}
/>

<ConfirmDanger
  open={pendingCreate}
  title={t("ilm.confirm.writeTitle")}
  message={t("ilm.confirm.writeMessage")}
  scope={newName ? t("ilm.confirm.scope", { name: newName }) : ""}
  confirmLabel={t("ilm.confirm.writeLabel")}
  onConfirm={confirmCreate}
  onCancel={() => (pendingCreate = false)}
/>

<ConfirmDanger
  open={pendingDelete !== null}
  title={t("ilm.confirm.deleteTitle")}
  message={t("ilm.confirm.deleteMessage")}
  scope={pendingDelete ? t("ilm.confirm.scope", { name: pendingDelete.name }) : ""}
  confirmLabel={t("ilm.confirm.deleteLabel")}
  onConfirm={confirmDelete}
  onCancel={() => (pendingDelete = null)}
/>

<style>
  .page { max-width: 1120px; }
  header { display: flex; justify-content: space-between; align-items: start; gap: 16px; margin-bottom: 16px; }
  h1 { margin: 0 0 4px; font-size: var(--es-fs-3xl); }
  header p { margin: 0; color: var(--es-muted); }
  header code { font-family: var(--es-mono); font-size: var(--es-fs-xs); }
  .status { display: flex; align-items: center; gap: 8px; }
  .mode { font-family: var(--es-mono); font-size: var(--es-fs-xs); padding: 3px 8px; border-radius: 4px; border: 1px solid var(--es-border); }
  .mode.running { color: var(--es-green); border-color: var(--es-green); }
  .mode.stopped { color: var(--es-red); border-color: var(--es-red); }
  button { padding: 6px 14px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); cursor: pointer; white-space: nowrap; }
  button:hover:not(:disabled):not(.primary) { background: var(--es-surface-2); }
  .primary { background: var(--es-accent); color: #fff; border-color: var(--es-accent); }
  .primary:hover:not(:disabled) { filter: brightness(0.92); }
  button:disabled { opacity: 0.5; cursor: default; }
  button.danger { color: var(--es-red); border-color: var(--es-red); }
  .mini { padding: 3px 8px; font-size: var(--es-fs-xs); }
  .mini.danger { color: var(--es-red); border-color: var(--es-red); }
  .mini.danger:hover { background: var(--es-red); color: #fff; }
  .toast { padding: 8px 12px; margin-bottom: 12px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface-2); font-size: var(--es-fs-sm); }
  .creator, .block { border: 1px solid var(--es-border); border-radius: var(--es-radius); padding: 14px; margin-bottom: 14px; background: var(--es-surface); }
  .bar { display: flex; justify-content: space-between; align-items: center; margin-bottom: 10px; }
  .creator h2, .block h2 { margin: 0; font-size: var(--es-fs-sm); text-transform: uppercase; letter-spacing: 0.08em; color: var(--es-muted); }
  .count { margin-left: 6px; font-family: var(--es-mono); letter-spacing: 0; text-transform: none; }
  .fields { display: flex; gap: 12px; align-items: end; flex-wrap: wrap; margin-bottom: 10px; }
  .fields label { display: flex; flex-direction: column; gap: 4px; font-size: var(--es-fs-xs); color: var(--es-muted); }
  .fields input { padding: 6px 10px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); min-width: 220px; }
  textarea { width: 100%; box-sizing: border-box; font-family: var(--es-mono); font-size: var(--es-fs-sm); padding: 10px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface-2); color: var(--es-text); }
  .form-error { margin: 8px 0 0; font-size: var(--es-fs-xs); color: var(--es-red); }
  table { width: 100%; border-collapse: collapse; font-size: var(--es-fs-sm); }
  th, td { padding: 5px 8px; border-bottom: 1px solid var(--es-border); text-align: left; }
  th { color: var(--es-muted); font-size: var(--es-fs-xs); font-weight: 600; }
  .mono { font-family: var(--es-mono); }
  .small { font-size: var(--es-fs-xs); }
  .err { color: var(--es-red); }
  .act { text-align: right; white-space: nowrap; }
  .empty { margin: 0; font-size: var(--es-fs-xs); color: var(--es-muted); }
</style>
