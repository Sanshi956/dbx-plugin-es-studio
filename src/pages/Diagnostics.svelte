<script lang="ts">
  import StateBlock from "../components/StateBlock.svelte";
  import {
    RULES, SEVERITY_LABEL, SEVERITY_ORDER,
    collectDiagnostics, evaluate, countBySeverity, normalizeConfigs, serializeConfigs,
    type RuleConfigs, type Snapshot, type Severity,
  } from "../lib/diagnostics";
  import type { DbxBridge } from "../lib/bridge";

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
    toast = "规则已恢复默认";
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
      <h1>Cluster Diagnostics</h1>
      <p>Deterministic rules over one read-only snapshot — findings and suggestions, never auto-fix.</p>
    </div>
    <div class="tools">
      <button type="button" onclick={() => (showRules = !showRules)}>
        规则 {enabledCount}/{RULES.length}
      </button>
      <button type="button" onclick={() => load()} disabled={loading}>{loading ? "Checking…" : "重新检查"}</button>
    </div>
  </header>

  {#if toast}<div class="toast">{toast}</div>{/if}

  {#if error === "NO_CONNECTION"}
    <StateBlock kind="error" message="No connection selected" detail="Select a connected cluster first." />
  {:else if error}
    <StateBlock kind="error" message="Could not collect diagnostics" detail={error} />
  {:else if loading && !snapshot}
    <StateBlock kind="loading" message="Collecting cluster facts…" detail="es/batch · health / nodes / settings / shards / ilm" />
  {:else if snapshot}
    <div class="meta">
      上次检查 {stamp(snapshot.collectedAt)} · 节点 {snapshot.nodes} · 活跃分片 {snapshot.active} ·
      未分配 {snapshot.unassigned} · 可检查索引 {snapshot.indices.length}
    </div>

    {#if showRules}
      <section class="panel rules">
        <div class="panel-head">
          <h2>规则与阈值</h2>
          <button type="button" class="link" onclick={resetRules}>恢复默认</button>
        </div>
        <table>
          <thead>
            <tr>
              <th>规则</th>
              <th>范围</th>
              <th class="center" title="关闭后该条不再产生结论">启用</th>
              <th class="right">阈值</th>
            </tr>
          </thead>
          <tbody>
            {#each RULES as rule (rule.id)}
              <tr class:off={configs[rule.id]?.enabled === false}>
                <td title={rule.basis}>{rule.title}</td>
                <td class="muted">{rule.domain}</td>
                <td class="center">
                  <input
                    type="checkbox"
                    checked={configs[rule.id]?.enabled !== false}
                    onchange={() => toggleRule(rule.id)}
                    aria-label={`启用 ${rule.title}`}
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
                      title={`${rule.threshold.label}（${rule.threshold.min}-${rule.threshold.max}${rule.threshold.unit}）`}
                    />
                    <span class="muted">{rule.threshold.unit}</span>
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
          <span class="lbl">{SEVERITY_LABEL[level]}</span>
          <span class="num-value">{counts[level]}</span>
        </div>
      {/each}
      <div class="card">
        <span class="lbl">已启用规则</span>
        <span class="num-value">{enabledCount}/{RULES.length}</span>
      </div>
    </section>

    {#if findings.length === 0}
      <StateBlock
        kind="empty"
        message="所有已启用规则都通过"
        detail={`按当前阈值检查了 ${enabledCount} 条规则，没有发现异常`}
      />
    {:else}
      <table class="findings">
        <thead>
          <tr>
            <th>级别</th>
            <th>结论</th>
            <th>依据</th>
            <th title="只给建议，不会自动修改集群">建议</th>
          </tr>
        </thead>
        <tbody>
          {#each findings as finding (finding.ruleId)}
            <tr class={severityClass(finding.severity)}>
              <td><span class="tag {finding.severity}">{SEVERITY_LABEL[finding.severity]}</span></td>
              <td title={finding.ruleId}>
                {finding.title}
                {#if finding.targets.length > 0}
                  <div class="targets">{finding.targets.slice(0, 3).join("、")}{finding.targets.length > 3 ? ` 等 ${finding.targets.length} 项` : ""}</div>
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
  h1 { margin: 0 0 4px; font-size: 18px; }
  header p { margin: 0; color: var(--es-muted); }
  .tools { display: flex; gap: 8px; align-items: center; }
  button { padding: 5px 12px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); cursor: pointer; white-space: nowrap; }
  button:hover:not(:disabled) { background: var(--es-surface-2); }
  button:disabled { opacity: 0.55; cursor: default; }
  button.link { border: none; background: none; color: var(--es-accent); padding: 0; }
  .toast { padding: 8px 12px; margin-bottom: 12px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface-2); font-size: 12px; }
  .meta { font-size: 11px; color: var(--es-muted); font-family: var(--es-mono); margin-bottom: 12px; }
  .panel { border: 1px solid var(--es-border); border-radius: var(--es-radius); background: var(--es-surface); padding: 10px 14px; margin-bottom: 14px; }
  .panel-head { display: flex; justify-content: space-between; align-items: center; }
  .panel-head h2 { margin: 0 0 6px; font-size: 12px; text-transform: uppercase; letter-spacing: 0.08em; color: var(--es-muted); }
  .summary { display: grid; grid-template-columns: repeat(auto-fit, minmax(150px, 1fr)); gap: 10px; margin-bottom: 14px; }
  .card { border: 1px solid var(--es-border); border-radius: var(--es-radius); background: var(--es-surface); padding: 10px 12px; display: flex; flex-direction: column; gap: 4px; }
  .card .lbl { font-size: 11px; color: var(--es-muted); text-transform: uppercase; letter-spacing: 0.06em; }
  .card .num-value { font-family: var(--es-mono); font-size: 20px; font-weight: 700; }
  .card.critical .num-value { color: var(--es-red); }
  .card.warning .num-value { color: var(--es-yellow); }
  .card.info .num-value { color: var(--es-accent); }
  table { width: 100%; border-collapse: collapse; font-size: 12px; }
  th, td { padding: 6px 10px; border-bottom: 1px solid var(--es-border); text-align: left; vertical-align: top; }
  th { background: var(--es-surface-2); color: var(--es-muted); font-weight: 600; font-size: 11px; }
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
  .evidence { font-family: var(--es-mono); font-size: 11px; color: var(--es-text); }
  .suggest { color: var(--es-muted); }
  .targets { margin-top: 3px; font-size: 11px; color: var(--es-muted); font-family: var(--es-mono); }
  .num { width: 74px; padding: 3px 6px; border: 1px solid var(--es-border); border-radius: 5px; background: var(--es-surface); color: var(--es-text); font-family: var(--es-mono); font-size: 11px; text-align: right; }
  .tag { display: inline-block; padding: 1px 7px; border-radius: 4px; font-size: 10px; font-family: var(--es-mono); border: 1px solid var(--es-border); white-space: nowrap; }
  .tag.critical { color: var(--es-red); border-color: var(--es-red); }
  .tag.warning { color: var(--es-yellow); border-color: var(--es-yellow); }
  .tag.info { color: var(--es-accent); border-color: var(--es-accent); }
</style>
