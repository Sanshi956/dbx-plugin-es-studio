<script lang="ts">
  import StateBlock from "../components/StateBlock.svelte";
  import {
    RULES, SEVERITY_ORDER, severityLabel,
    collectDiagnostics, evaluate, countBySeverity, normalizeConfigs, serializeConfigs,
    type RuleConfigs, type Snapshot, type Severity,
  } from "../lib/diagnostics";
  import type { DbxBridge } from "../lib/bridge";
  import { t } from "../lib/i18n.svelte.js";

  interface Props {
    bridge: DbxBridge;
    connectionId: string;
  }
  let { bridge, connectionId }: Props = $props();

  const STORAGE_KEY = "es.studio.diagnostics.rules";

  let snapshot = $state<Snapshot | null>(null);
  let configs = $state<RuleConfigs>(normalizeConfigs(null));
  let loading = $state(true);
  let error = $state("");
  let showRules = $state(false);
  let toast = $state("");
  // Rules are read from host.storage once per mount. Re-reading them on every
  // re-check would race a still-pending save and silently revert a fresh edit.
  let configsLoaded = $state(false);

  let findings = $derived(snapshot ? evaluate(snapshot, configs) : []);
  let counts = $derived(countBySeverity(findings));
  let enabledCount = $derived(RULES.filter((rule) => configs[rule.id]?.enabled !== false).length);

  async function load(): Promise<void> {
    if (!connectionId) {
      error = "NO_CONNECTION";
      loading = false;
      return;
    }
    loading = true;
    error = "";
    try {
      if (!configsLoaded) {
        configs = normalizeConfigs(await bridge.storage.get<string>(STORAGE_KEY));
        configsLoaded = true;
      }
      snapshot = await collectDiagnostics(bridge, connectionId);
    } catch (cause) {
      error = cause instanceof Error ? cause.message : String(cause);
      snapshot = null;
    } finally {
      loading = false;
    }
  }

  $effect(() => {
    void connectionId;
    void load();
  });

  function persist(): void {
    void bridge.storage.set(STORAGE_KEY, serializeConfigs(configs)).catch(() => undefined);
  }

  function toggleRule(id: string): void {
    const current = configs[id];
    configs = { ...configs, [id]: { ...current, enabled: current?.enabled === false } };
    persist();
  }

  function setThreshold(id: string, value: number): void {
    if (!Number.isFinite(value)) return;
    const current = configs[id];
    configs = { ...configs, [id]: { ...current, threshold: value } };
    persist();
  }

  function resetRules(): void {
    configs = normalizeConfigs(null);
    persist();
    toast = t("diag.rulesRestored");
  }

  function stamp(ms: number): string {
    const date = new Date(ms);
    const pad = (value: number) => String(value).padStart(2, "0");
    return `${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`;
  }

  function severityClass(severity: Severity): string {
    return severity;
  }
</script>

<div class="page">
  <header>
    <div>
      <h1>{t("diag.title")}</h1>
      <p>{t("diag.hint")}</p>
    </div>
    <div class="tools">
      <button type="button" onclick={() => (showRules = !showRules)}>
        {t("diag.rulesButton", { on: enabledCount, total: RULES.length })}
      </button>
      <button type="button" onclick={() => load()} disabled={loading}>{loading ? t("diag.checking") : t("diag.recheck")}</button>
    </div>
  </header>

  {#if toast}<div class="toast">{toast}</div>{/if}

  {#if error === "NO_CONNECTION"}
    <StateBlock kind="error" message={t("diag.noConnection")} detail={t("diag.noConnectionDetail")} />
  {:else if error}
    <StateBlock kind="error" message={t("diag.failed")} detail={error} />
  {:else if loading && !snapshot}
    <StateBlock kind="loading" message={t("diag.collecting")} detail="es/batch · health / nodes / settings / shards / ilm" />
  {:else if snapshot}
    <div class="meta">
      {t("diag.meta", { at: stamp(snapshot.collectedAt), nodes: snapshot.nodes, active: snapshot.active, unassigned: snapshot.unassigned, indices: snapshot.indices.length })}
    </div>

    {#if showRules}
      <section class="panel rules">
        <div class="panel-head">
          <h2>{t("diag.rules.title")}</h2>
          <button type="button" class="link" onclick={resetRules}>{t("diag.rules.reset")}</button>
        </div>
        <table>
          <thead>
            <tr>
              <th>{t("diag.col.rule")}</th>
              <th>{t("diag.col.domain")}</th>
              <th class="center" title={t("diag.tip.enabled")}>{t("diag.col.enabled")}</th>
              <th class="right">{t("diag.col.threshold")}</th>
            </tr>
          </thead>
          <tbody>
            {#each RULES as rule (rule.id)}
              <tr class:off={configs[rule.id]?.enabled === false}>
                <td title={t(rule.basisKey)}>{t(rule.titleKey)}</td>
                <td class="muted">{t(rule.domainKey)}</td>
                <td class="center">
                  <input
                    type="checkbox"
                    checked={configs[rule.id]?.enabled !== false}
                    onchange={() => toggleRule(rule.id)}
                    aria-label={t("diag.aria.enableRule", { title: t(rule.titleKey) })}
                  />
                </td>
                <td class="right">
                  {#if rule.threshold}
                    <input
                      class="num"
                      type="number"
                      min={rule.threshold.min}
                      max={rule.threshold.max}
                      step={rule.threshold.step}
                      value={configs[rule.id]?.threshold ?? rule.threshold.fallback}
                      oninput={(event) => setThreshold(rule.id, Number(event.currentTarget.value))}
                      title={t("diag.thresholdRange", { label: t(rule.threshold.labelKey), min: rule.threshold.min, max: rule.threshold.max, unit: t(rule.threshold.unitKey) })}
                    />
                    <span class="muted">{t(rule.threshold.unitKey)}</span>
                  {:else}
                    <span class="muted">—</span>
                  {/if}
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </section>
    {/if}

    <section class="summary">
      {#each SEVERITY_ORDER as level (level)}
        <div class="card {level}">
          <span class="lbl">{severityLabel(level)}</span>
          <span class="num-value">{counts[level]}</span>
        </div>
      {/each}
      <div class="card">
        <span class="lbl">{t("diag.card.enabledRules")}</span>
        <span class="num-value">{enabledCount}/{RULES.length}</span>
      </div>
    </section>

    {#if findings.length === 0}
      <StateBlock
        kind="empty"
        message={t("diag.pass.title")}
        detail={t("diag.pass.detail", { count: enabledCount })}
      />
    {:else}
      <table class="findings">
        <thead>
          <tr>
            <th>{t("diag.col.severity")}</th>
            <th>{t("diag.col.finding")}</th>
            <th>{t("diag.col.evidence")}</th>
            <th title={t("diag.tip.suggestion")}>{t("diag.col.suggestion")}</th>
          </tr>
        </thead>
        <tbody>
          {#each findings as finding (finding.ruleId)}
            <tr class={severityClass(finding.severity)}>
              <td><span class="tag {finding.severity}">{severityLabel(finding.severity)}</span></td>
              <td title={finding.ruleId}>
                {finding.title}
                {#if finding.targets.length > 0}
                  <div class="targets">{finding.targets.slice(0, 3).join(t("common.listSep"))}{finding.targets.length > 3 ? t("diag.more", { count: finding.targets.length }) : ""}</div>
                {/if}
              </td>
              <td class="evidence">{finding.evidence}</td>
              <td class="suggest">{finding.suggestion}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  {/if}
</div>

<style>
  .page { max-width: 1180px; }
  header { display: flex; justify-content: space-between; align-items: start; gap: 16px; margin-bottom: 12px; flex-wrap: wrap; }
  h1 { margin: 0 0 4px; font-size: var(--es-fs-3xl); }
  header p { margin: 0; color: var(--es-muted); }
  .tools { display: flex; gap: 8px; align-items: center; }
  button { padding: 5px 12px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); cursor: pointer; white-space: nowrap; }
  button:hover:not(:disabled) { background: var(--es-surface-2); }
  button:disabled { opacity: 0.55; cursor: default; }
  button.link { border: none; background: none; color: var(--es-accent); padding: 0; }
  .toast { padding: 8px 12px; margin-bottom: 12px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface-2); font-size: var(--es-fs-sm); }
  .meta { font-size: var(--es-fs-xs); color: var(--es-muted); font-family: var(--es-mono); margin-bottom: 12px; }
  .panel { border: 1px solid var(--es-border); border-radius: var(--es-radius); background: var(--es-surface); padding: 10px 14px; margin-bottom: 14px; }
  .panel-head { display: flex; justify-content: space-between; align-items: center; }
  .panel-head h2 { margin: 0 0 6px; font-size: var(--es-fs-sm); text-transform: uppercase; letter-spacing: 0.08em; color: var(--es-muted); }
  .summary { display: grid; grid-template-columns: repeat(auto-fit, minmax(150px, 1fr)); gap: 10px; margin-bottom: 14px; }
  .card { border: 1px solid var(--es-border); border-radius: var(--es-radius); background: var(--es-surface); padding: 10px 12px; display: flex; flex-direction: column; gap: 4px; }
  .card .lbl { font-size: var(--es-fs-xs); color: var(--es-muted); text-transform: uppercase; letter-spacing: 0.06em; }
  .card .num-value { font-family: var(--es-mono); font-size: var(--es-fs-4xl); font-weight: 700; }
  .card.critical .num-value { color: var(--es-red); }
  .card.warning .num-value { color: var(--es-yellow); }
  .card.info .num-value { color: var(--es-accent); }
  table { width: 100%; border-collapse: collapse; font-size: var(--es-fs-sm); }
  th, td { padding: 6px 10px; border-bottom: 1px solid var(--es-border); text-align: left; vertical-align: top; }
  th { background: var(--es-surface-2); color: var(--es-muted); font-weight: 600; font-size: var(--es-fs-xs); }
  .rules table { margin-top: 4px; table-layout: fixed; }
  .rules th:nth-child(1), .rules td:nth-child(1) { width: 46%; }
  .rules th:nth-child(2), .rules td:nth-child(2) { width: 14%; }
  .rules th:nth-child(3), .rules td:nth-child(3) { width: 10%; }
  .rules th:nth-child(4), .rules td:nth-child(4) { width: 30%; text-align: right; }
  .rules tr.off td { opacity: 0.5; }
  table.findings { border: 1px solid var(--es-border); border-radius: var(--es-radius); overflow: hidden; }
  .findings tbody tr:hover { background: var(--es-surface-2); }
  .center { text-align: center; }
  .right { text-align: right; }
  .muted { color: var(--es-muted); }
  .evidence { font-family: var(--es-mono); font-size: var(--es-fs-xs); color: var(--es-text); }
  .suggest { color: var(--es-muted); }
  .targets { margin-top: 3px; font-size: var(--es-fs-xs); color: var(--es-muted); font-family: var(--es-mono); }
  .num { width: 74px; padding: 3px 6px; border: 1px solid var(--es-border); border-radius: 5px; background: var(--es-surface); color: var(--es-text); font-family: var(--es-mono); font-size: var(--es-fs-xs); text-align: right; }
  .tag { display: inline-block; padding: 1px 7px; border-radius: 4px; font-size: var(--es-fs-2xs); font-family: var(--es-mono); border: 1px solid var(--es-border); white-space: nowrap; }
  .tag.critical { color: var(--es-red); border-color: var(--es-red); }
  .tag.warning { color: var(--es-yellow); border-color: var(--es-yellow); }
  .tag.info { color: var(--es-accent); border-color: var(--es-accent); }
</style>
