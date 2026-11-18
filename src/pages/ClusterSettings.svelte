<script lang="ts">
  import StateBlock from "../components/StateBlock.svelte";
  import ConfirmDanger from "../components/ConfirmDanger.svelte";
  import Combobox from "../components/Combobox.svelte";
  import { fetchClusterSettings, updateClusterSettings, isError, type ClusterSettingsSnapshot } from "../lib/services";
  import { settingMeta, settingKeyCandidates, type SettingMeta } from "../lib/clusterSettings";
  import type { DbxBridge } from "../lib/bridge";
  import { t } from "../lib/i18n.svelte.js";

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
  let meta = $state<SettingMeta>({ kind: "text", options: [], hintKey: "cs.guess.text" });
  let keyCandidates = $state<string[]>([]);
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

  // Picker candidates: curated keys plus whatever this cluster already sets.
  $effect(() => {
    const extra = [...Object.keys(snapshot.persistent), ...Object.keys(snapshot.transient)];
    keyCandidates = settingKeyCandidates(extra);
  });

  function entries(source: Record<string, string>): { key: string; value: string }[] {
    const q = filter.trim().toLowerCase();
    return Object.entries(source)
      .filter(([k]) => (q ? k.toLowerCase().includes(q) : true))
      .map(([key, value]) => ({ key, value }))
      .sort((a, b) => a.key.localeCompare(b.key));
  }

  function onKeyChange(next: string): void {
    editKey = next;
    meta = settingMeta(next);
    formError = "";
  }

  function valuePlaceholder(): string {
    switch (meta.kind) {
      case "number": return t("cs.hint.number");
      case "duration": return t("cs.hint.duration");
      case "size": return t("cs.hint.size");
      case "boolean":
      case "enum": return t("cs.hint.enum");
      default: return t("cs.hint.default");
    }
  }

  /** persistent > transient > default — what is actually in effect right now. */
  function currentOf(key: string): string {
    const k = key.trim();
    if (!k) return "";
    const persistent = snapshot.persistent[k];
    if (persistent !== undefined) return t("cs.current.persistent", { value: persistent });
    const transient = snapshot.transient[k];
    if (transient !== undefined) return t("cs.current.transient", { value: transient });
    const fallback = snapshot.defaults[k];
    if (fallback !== undefined) return t("cs.current.default", { value: fallback });
    return t("cs.current.unset");
  }

  function askSet(): void {
    formError = "";
    const key = editKey.trim();
    if (!key) {
      formError = t("cs.err.noKey");
      return;
    }
    if (!key.startsWith("cluster.")) {
      formError = t("cs.err.prefix");
      return;
    }
    pending = { mode: "set", scope, key, value: editValue.trim() };
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
      toast = t("common.toast.failed", { detail: res.error?.message ?? "HTTP " + res.status });
    } else {
      toast = job.mode === "remove"
        ? t("cs.toast.removed", { key: job.key })
        : t("cs.toast.written", { scope: job.scope, key: job.key, value: job.value });
      editValue = "";
      await load();
    }
  }
</script>

<div class="page">
  <header>
    <div>
      <h1>{t("cs.title")}</h1>
      <p>{t("cs.hintPrefix")}<code>GET /_cluster/settings?flat_settings=true</code>{t("cs.hintSuffix")}</p>
    </div>
    <label class="toggle"><input type="checkbox" bind:checked={includeDefaults} /> include defaults</label>
  </header>

  {#if toast}<div class="toast">{toast}</div>{/if}
  {#if error}<StateBlock kind="error" message={t("cs.failed")} detail={error} />{/if}

  <section class="writer">
    <h2>{t("cs.section.write")}</h2>
    <div class="fields">
      <label>{t("cs.scope")}
        <select bind:value={scope}>
          <option value="persistent">persistent</option>
          <option value="transient">transient</option>
        </select>
      </label>
      <label class="wide">{t("cs.key")}
        <Combobox
          listId="cluster-setting-keys"
          class="key-input"
          placeholder={t("cs.keyPlaceholder")}
          value={editKey}
          options={keyCandidates.map((k) => ({ value: k }))}
          onValueChange={onKeyChange}
        />
      </label>
      <label class="wide">{t("cs.value")}
        <input class="value-input" title={t(meta.hintKey)} placeholder={valuePlaceholder()} bind:value={editValue} />
      </label>
      <button type="button" class="primary" onclick={askSet}>{t("common.apply")}</button>
    </div>

    {#if editKey.trim()}
      <div class="value-row">
        <span class="kind" title={t(meta.hintKey)}>{meta.kind}</span>
        {#if meta.options.length}
          <div class="chips">
            {#each meta.options as option (option)}
              <button
                type="button"
                class="chip"
                class:on={editValue.trim() === option}
                onclick={() => (editValue = option)}
              >{option}</button>
            {/each}
          </div>
        {/if}
        <span class="current">{currentOf(editKey)}</span>
      </div>
    {/if}

    {#if formError}<p class="form-error">{formError}</p>{/if}
  </section>

  <div class="filter-row">
    <label>{t("index.filterLabel")}<input placeholder={t("cs.filterPlaceholder")} bind:value={filter} /></label>
  </div>

  {#if loading}
    <StateBlock kind="loading" message={t("cs.reading")} />
  {:else}
    {#each [{ id: "persistent", label: "Persistent" }, { id: "transient", label: "Transient" }] as group (group.id)}
      <section class="group">
        <h2>{group.label} <span class="count">{entries(snapshot[group.id as "persistent" | "transient"]).length}</span></h2>
        {#if entries(snapshot[group.id as "persistent" | "transient"]).length === 0}
          <p class="empty">{t("cs.none")}</p>
        {:else}
          <table>
            <thead><tr><th>{t("cs.col.setting")}</th><th>{t("cs.col.value")}</th><th></th></tr></thead>
            <tbody>
              {#each entries(snapshot[group.id as "persistent" | "transient"]) as row (`${group.id}:${row.key}`)}
                <tr>
                  <td class="mono">
                    <button type="button" class="link" title={t("cs.fillForm")} onclick={() => onKeyChange(row.key)}>{row.key}</button>
                  </td>
                  <td class="mono value">{row.value}</td>
                  <td class="act"><button type="button" class="mini danger" onclick={() => askRemove(group.id as "persistent" | "transient", row.key)}>{t("cs.remove")}</button></td>
                </tr>
              {/each}
            </tbody>
          </table>
        {/if}
      </section>
    {/each}

    {#if includeDefaults}
      <section class="group">
        <h2>{t("cs.defaults")} <span class="count">{entries(snapshot.defaults).length}</span></h2>
        <p class="empty">{t("cs.defaultsNote", { total: Object.keys(snapshot.defaults).length })}</p>
        <table>
          <thead><tr><th>{t("cs.col.setting")}</th><th>{t("cs.col.value")}</th></tr></thead>
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
  title={pending?.mode === "remove" ? t("cs.confirm.removeTitle") : t("cs.confirm.setTitle")}
  message={t("cs.confirm.message")}
  scope={pending
    ? `${pending.scope}.${pending.key}${pending.mode === "remove" ? t("cs.confirm.scopeRemove") : ` = ${pending.value || "null"}`}`
    : ""}
  confirmLabel={pending?.mode === "remove" ? t("cs.confirm.removeLabel") : t("cs.confirm.writeLabel")}
  onConfirm={confirm}
  onCancel={() => (pending = null)}
/>

<style>
  .page { max-width: 1080px; }
  header { display: flex; justify-content: space-between; align-items: start; gap: 16px; margin-bottom: 16px; }
  h1 { margin: 0 0 4px; font-size: var(--es-fs-3xl); }
  header p { margin: 0; color: var(--es-muted); }
  header code { font-family: var(--es-mono); font-size: var(--es-fs-xs); }
  .toggle { display: flex; align-items: center; gap: 6px; font-size: var(--es-fs-xs); color: var(--es-muted); white-space: nowrap; }
  button { padding: 6px 14px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); cursor: pointer; white-space: nowrap; }
  button:hover:not(:disabled):not(.primary) { background: var(--es-surface-2); }
  .primary { background: var(--es-accent); color: #fff; border-color: var(--es-accent); }
  .primary:hover:not(:disabled) { filter: brightness(0.92); }
  .mini { padding: 3px 8px; font-size: var(--es-fs-xs); }
  .mini.danger { color: var(--es-red); border-color: var(--es-red); }
  .mini.danger:hover { background: var(--es-red); color: #fff; }
  .toast { padding: 8px 12px; margin-bottom: 12px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface-2); font-size: var(--es-fs-sm); }
  .writer, .group { border: 1px solid var(--es-border); border-radius: var(--es-radius); padding: 14px; margin-bottom: 14px; background: var(--es-surface); }
  .writer h2, .group h2 { margin: 0 0 10px; font-size: var(--es-fs-sm); text-transform: uppercase; letter-spacing: 0.08em; color: var(--es-muted); }
  .count { margin-left: 6px; font-family: var(--es-mono); letter-spacing: 0; text-transform: none; }
  .fields { display: flex; gap: 12px; align-items: end; flex-wrap: wrap; }
  .fields label { display: flex; flex-direction: column; gap: 4px; font-size: var(--es-fs-xs); color: var(--es-muted); }
  .fields .wide { flex: 1 1 260px; }
  .fields input, .fields select, .filter-row input { width: 100%; }
  .value-row { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; margin-top: 10px; }
  .kind { font-size: var(--es-fs-2xs); font-family: var(--es-mono); color: var(--es-muted); border: 1px solid var(--es-border); border-radius: 4px; padding: 2px 6px; }
  .chips { display: flex; gap: 6px; flex-wrap: wrap; }
  .chip { padding: 2px 8px; font-size: var(--es-fs-xs); font-family: var(--es-mono); border-radius: 999px; background: var(--es-surface-2); }
  .chip.on { border-color: var(--es-accent); color: var(--es-accent); background: var(--es-surface); }
  .current { font-size: var(--es-fs-xs); color: var(--es-muted); font-family: var(--es-mono); }
  .filter-row { margin-bottom: 14px; }
  .filter-row label { display: flex; flex-direction: column; gap: 4px; font-size: var(--es-fs-xs); color: var(--es-muted); }
  .form-error { margin: 8px 0 0; font-size: var(--es-fs-xs); color: var(--es-red); }
  table { width: 100%; border-collapse: collapse; font-size: var(--es-fs-sm); }
  th, td { padding: 5px 8px; border-bottom: 1px solid var(--es-border); text-align: left; }
  th { color: var(--es-muted); font-size: var(--es-fs-xs); font-weight: 600; }
  .mono { font-family: var(--es-mono); }
  .link { padding: 0; border: 0; background: none; font-family: var(--es-mono); font-size: var(--es-fs-sm); color: var(--es-text); text-align: left; white-space: normal; }
  .link:hover:not(:disabled):not(.primary) { background: none; color: var(--es-accent); text-decoration: underline; }
  .value { word-break: break-all; }
  .act { text-align: right; white-space: nowrap; }
  .empty { margin: 0; font-size: var(--es-fs-xs); color: var(--es-muted); }
</style>
