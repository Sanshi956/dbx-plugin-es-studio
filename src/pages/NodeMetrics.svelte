<script lang="ts">
  import StateBlock from "../components/StateBlock.svelte";
  import MetricChart from "../components/MetricChart.svelte";
  import {
    startMonitor, monitorStatus, sampleNow, fetchMonitorHistory,
    type MonitorSample, type MonitorStatus,
  } from "../lib/services";
  import type { DbxBridge } from "../lib/bridge";
  import { t } from "../lib/i18n.svelte.js";
  import type { ChartSeries } from "../lib/types";

  interface Props {
    bridge: DbxBridge;
    connectionId: string;
  }
  let { bridge, connectionId }: Props = $props();

  // Node lines get one color each; beyond eight nodes the hue repeats and the
  // chips below stay the reliable way to tell series apart.
  const PALETTE = [
    "var(--es-accent)", "var(--es-green)", "var(--es-yellow)", "var(--es-red)",
    "#8250df", "#1b7c83", "#bc4c00", "#0969da",
  ];

  let samples = $state<MonitorSample[]>([]);
  let status = $state<MonitorStatus | null>(null);
  let selected = $state<string[]>([]);
  let loading = $state(true);
  let error = $state("");

  let timer: ReturnType<typeof setInterval> | null = null;

  function stopTimer(): void {
    if (timer) {
      clearInterval(timer);
      timer = null;
    }
  }

  function nodeNames(): string[] {
    const latest = samples.length > 0 ? samples[samples.length - 1] : null;
    if (latest) return Object.keys(latest.nodes).sort();
    const seen = new Set<string>();
    for (const sample of samples) for (const name of Object.keys(sample.nodes)) seen.add(name);
    return [...seen].sort();
  }

  function isOn(name: string): boolean {
    return selected.length === 0 || selected.includes(name);
  }

  function toggle(name: string): void {
    const all = nodeNames();
    const current = selected.length === 0 ? all : selected;
    selected = current.includes(name) ? current.filter((item) => item !== name) : [...current, name];
    if (selected.length === all.length) selected = [];
  }

  async function refresh(): Promise<void> {
    if (!connectionId) return;
    const history = await fetchMonitorHistory(bridge, connectionId, { limit: 720 });
    samples = history.samples;
    status = await monitorStatus(bridge, connectionId);
  }

  async function bootstrap(id: string): Promise<void> {
    stopTimer();
    if (!id) {
      error = "NO_CONNECTION";
      loading = false;
      return;
    }
    loading = true;
    error = "";
    try {
      const current = await monitorStatus(bridge, id);
      status = current.running ? current : await startMonitor(bridge, id, 60);
      await refresh();
      if (samples.length === 0) {
        await sampleNow(bridge, id);
        await refresh();
      }
      timer = setInterval(() => {
        void refresh().catch(() => {
          /* transient: next tick retries */
        });
      }, 3000);
    } catch (cause) {
      error = cause instanceof Error ? cause.message : String(cause);
    } finally {
      loading = false;
    }
  }

  $effect(() => {
    const id = connectionId;
    void bootstrap(id);
    return () => stopTimer();
  });

  function colorOf(name: string): string {
    const names = nodeNames();
    return PALETTE[Math.max(0, names.indexOf(name)) % PALETTE.length];
  }

  function seriesFor(pick: (node: { heapPercent: number; cpuPercent: number; load1: number; diskPercent: number }) => number): ChartSeries[] {
    const names = nodeNames().filter(isOn);
    return names.map((name) => ({
      label: name,
      color: colorOf(name),
      points: samples
        .filter((sample) => sample.nodes[name] !== undefined)
        .map((sample) => ({ x: sample.at, y: pick(sample.nodes[name]) })),
    }));
  }

  let latest = $derived(samples.length > 0 ? samples[samples.length - 1] : null);
  let rows = $derived.by(() => {
    if (!latest) return [];
    return Object.keys(latest.nodes)
      .sort()
      .map((name) => ({ ...latest.nodes[name], name }));
  });
</script>

<div class="page">
  <header>
    <div>
      <h1>{t("nm.title")}</h1>
      <p>{t("nm.hint")}</p>
    </div>
    <span class="live" class:on={status?.running}>{status?.running ? t("mc.running") : t("mc.stopped")}</span>
  </header>

  {#if error && error !== "NO_CONNECTION"}
    <StateBlock kind="error" message={t("mc.readFailed")} detail={error} />
  {:else if error === "NO_CONNECTION"}
    <StateBlock kind="error" message={t("mc.noConnection")} detail={t("mc.noConnectionDetail")} />
  {:else if loading && samples.length === 0}
    <StateBlock kind="loading" message={t("mc.reading")} detail="mon/history" />
  {:else if samples.length === 0}
    <StateBlock kind="empty" message={t("nm.empty")} detail={t("nm.emptyDetail")} />
  {:else}
    <section class="bar">
      <span class="lbl">{t("nm.lbl.node")}</span>
      {#each nodeNames() as name (name)}
        <button type="button" class="chip" class:on={isOn(name)} onclick={() => toggle(name)}>
          <i style={`background:${colorOf(name)}`}></i>{name}
        </button>
      {/each}
      <span class="foot">{selected.length === 0 ? t("nm.allNodes") : `${selected.length} / ${nodeNames().length}`}</span>
    </section>

    <section class="grid">
      <div class="panel">
        <h2>{t("nm.sec.heap")}</h2>
        <MetricChart series={seriesFor((node) => node.heapPercent)} yMax={100} format={(value) => `${value.toFixed(0)}%`} />
      </div>
      <div class="panel">
        <h2>{t("nm.sec.cpu")}</h2>
        <MetricChart series={seriesFor((node) => node.cpuPercent)} yMax={100} format={(value) => `${value.toFixed(0)}%`} />
      </div>
      <div class="panel">
        <h2>{t("nm.sec.load")}</h2>
        <MetricChart series={seriesFor((node) => node.load1)} format={(value) => value.toFixed(2)} />
      </div>
      <div class="panel">
        <h2>{t("nm.latest")}</h2>
        <table>
          <thead>
            <tr>
              <th>{t("nm.col.node")}</th>
              <th class="right" title={t("nm.tip.heap")}>{t("nm.col.heap")}</th>
              <th class="right" title={t("nm.tip.cpu")}>{t("nm.col.cpu")}</th>
              <th class="right" title={t("nm.tip.load")}>{t("nm.col.load")}</th>
              <th class="right" title={t("nm.tip.disk")}>{t("nm.col.disk")}</th>
            </tr>
          </thead>
          <tbody>
            {#each rows as row (row.name)}
              <tr>
                <td class="mono"><i class="swatch" style={`background:${colorOf(row.name)}`}></i>{row.name}</td>
                <td class="right mono">{row.heapPercent.toFixed(0)}%</td>
                <td class="right mono">{row.cpuPercent.toFixed(0)}%</td>
                <td class="right mono">{row.load1.toFixed(2)}</td>
                <td class="right mono">{row.diskPercent > 0 ? `${row.diskPercent.toFixed(0)}%` : "—"}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    </section>
  {/if}
</div>

<style>
  .page { max-width: 1180px; }
  header { display: flex; justify-content: space-between; align-items: start; gap: 16px; margin-bottom: 14px; }
  h1 { margin: 0 0 4px; font-size: var(--es-fs-3xl); }
  header p { margin: 0; color: var(--es-muted); }
  .live { font-size: var(--es-fs-xs); color: var(--es-muted); border: 1px solid var(--es-border); border-radius: 999px; padding: 2px 10px; }
  .live.on { color: var(--es-green); border-color: var(--es-green); }
  .bar { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; padding: 10px 14px; border: 1px solid var(--es-border); border-radius: var(--es-radius); background: var(--es-surface); margin-bottom: 14px; }
  .bar .lbl { font-size: var(--es-fs-xs); color: var(--es-muted); text-transform: uppercase; letter-spacing: 0.06em; }
  .chip { display: inline-flex; align-items: center; gap: 6px; padding: 3px 10px; border: 1px solid var(--es-border); border-radius: 999px; background: var(--es-surface-2); color: var(--es-muted); font-size: var(--es-fs-xs); font-family: var(--es-mono); cursor: pointer; }
  .chip i { width: 8px; height: 8px; border-radius: 2px; }
  .chip.on { background: var(--es-surface); color: var(--es-text); border-color: var(--es-accent); }
  .foot { margin-left: auto; font-size: var(--es-fs-xs); color: var(--es-muted); font-family: var(--es-mono); }
  .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(420px, 1fr)); gap: 12px; }
  .panel { border: 1px solid var(--es-border); border-radius: var(--es-radius); background: var(--es-surface); padding: 12px 14px; }
  .panel h2 { margin: 0 0 8px; font-size: var(--es-fs-sm); text-transform: uppercase; letter-spacing: 0.08em; color: var(--es-muted); }
  table { width: 100%; border-collapse: collapse; font-size: var(--es-fs-sm); }
  th, td { padding: 5px 8px; border-bottom: 1px solid var(--es-border); text-align: left; }
  th { color: var(--es-muted); font-size: var(--es-fs-xs); font-weight: 600; }
  .right { text-align: right; }
  .mono { font-family: var(--es-mono); }
  .swatch { display: inline-block; width: 8px; height: 8px; border-radius: 2px; margin-right: 6px; }
</style>
