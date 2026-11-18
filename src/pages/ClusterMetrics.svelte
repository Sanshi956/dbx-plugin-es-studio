<script lang="ts">
  import StateBlock from "../components/StateBlock.svelte";
  import MetricChart from "../components/MetricChart.svelte";
  import {
    startMonitor, stopMonitor, monitorStatus, sampleNow, fetchMonitorHistory,
    type MonitorSample, type MonitorStatus,
  } from "../lib/services";
  import { formatBytes, formatCount } from "../lib/api";
  import type { DbxBridge } from "../lib/bridge";
  import { t } from "../lib/i18n.svelte.js";
  import type { ChartPoint, ChartSeries } from "../lib/types";

  interface Props {
    bridge: DbxBridge;
    connectionId: string;
  }
  let { bridge, connectionId }: Props = $props();

  const INTERVALS = [10, 30, 60, 120, 300];
  const INTERVAL_KEY = "es.studio.monitor.interval";

  let samples = $state<MonitorSample[]>([]);
  let status = $state<MonitorStatus | null>(null);
  let loading = $state(true);
  let error = $state("");
  let toast = $state("");
  let busy = $state(false);
  let intervalSec = $state(60);
  let stored = $state(0);
  let bytes = $state(0);
  let file = $state("");

  let timer: ReturnType<typeof setInterval> | null = null;

  function stopTimer(): void {
    if (timer) {
      clearInterval(timer);
      timer = null;
    }
  }

  async function refresh(): Promise<void> {
    if (!connectionId) return;
    const history = await fetchMonitorHistory(bridge, connectionId, { limit: 720 });
    const current = await monitorStatus(bridge, connectionId);
    samples = history.samples;
    stored = history.stored;
    bytes = history.bytes;
    file = history.file;
    status = current;
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
      const saved = await bridge.storage.get<number>(INTERVAL_KEY);
      if (typeof saved === "number" && INTERVALS.includes(saved)) intervalSec = saved;
      const current = await monitorStatus(bridge, id);
      // Opening the page starts collection: the whole point of the screen is a
      // series, and an idle chart with "press start" is a worse default.
      status = current.running ? current : await startMonitor(bridge, id, intervalSec);
      await refresh();
      if (samples.length === 0) {
        // The collector writes its first point from a goroutine, so a fresh
        // series can still be empty here. Collect synchronously once instead of
        // showing "no samples" for a whole tick.
        await sampleNow(bridge, id);
        await refresh();
      }
      timer = setInterval(() => {
        void refresh().catch(() => {
          /* transient sidecar hiccup: the next tick retries */
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

  async function toggleRun(): Promise<void> {
    if (!connectionId || busy) return;
    busy = true;
    try {
      if (status?.running) {
        status = await stopMonitor(bridge, connectionId);
        toast = t("mc.toast.stopped");
      } else {
        status = await startMonitor(bridge, connectionId, intervalSec);
        toast = t("mc.toast.started", { interval: intervalSec });
      }
    } catch (cause) {
      toast = t("mc.toast.failed", { detail: cause instanceof Error ? cause.message : String(cause) });
    } finally {
      busy = false;
    }
  }

  async function applyInterval(): Promise<void> {
    await bridge.storage.set(INTERVAL_KEY, intervalSec);
    if (!connectionId || !status?.running) return;
    busy = true;
    try {
      status = await startMonitor(bridge, connectionId, intervalSec);
      toast = t("mc.toast.intervalChanged", { interval: intervalSec });
    } catch (cause) {
      toast = t("mc.toast.intervalFailed", { detail: cause instanceof Error ? cause.message : String(cause) });
    } finally {
      busy = false;
    }
  }

  async function collectNow(): Promise<void> {
    if (!connectionId || busy) return;
    busy = true;
    try {
      status = await sampleNow(bridge, connectionId);
      await refresh();
      toast = t("mc.toast.sampled");
    } catch (cause) {
      toast = t("mc.toast.sampleFailed", { detail: cause instanceof Error ? cause.message : String(cause) });
    } finally {
      busy = false;
    }
  }

  function seriesOf(pick: (sample: MonitorSample) => number): ChartPoint[] {
    return samples.map((sample) => ({ x: sample.at, y: pick(sample) }));
  }

  function series(label: string, color: string, pick: (sample: MonitorSample) => number): ChartSeries {
    return { label, color, points: seriesOf(pick) };
  }

  let latest = $derived(samples.length > 0 ? samples[samples.length - 1] : null);
  let span = $derived.by(() => {
    if (samples.length < 2) return "";
    const first = samples[0].at;
    const last = samples[samples.length - 1].at;
    const minutes = Math.max(1, Math.round((last - first) / 60000));
    return t("mc.span", { minutes });
  });
</script>

<div class="page">
  <header>
    <div>
      <h1>{t("mc.title")}</h1>
      <p>{t("mc.hint", { interval: intervalSec })}</p>
    </div>
  </header>

  {#if toast}<div class="toast">{toast}</div>{/if}
  {#if error && error !== "NO_CONNECTION"}
    <StateBlock kind="error" message={t("mc.readFailed")} detail={error} />
  {:else if error === "NO_CONNECTION"}
    <StateBlock kind="error" message={t("mc.noConnection")} detail={t("mc.noConnectionDetail")} />
  {:else}
    <section class="bar">
      <span class="dot" class:on={status?.running}></span>
      <span class="state">{status?.running ? t("mc.running") : t("mc.stopped")}</span>
      <label>{t("mc.interval")}
        <select bind:value={intervalSec} onchange={applyInterval} disabled={busy}>
          {#each INTERVALS as seconds (seconds)}
            <option value={seconds}>{seconds}s</option>
          {/each}
        </select>
      </label>
      <button type="button" class:primary={!status?.running} onclick={toggleRun} disabled={busy}>
        {status?.running ? t("mc.stop") : t("mc.start")}
      </button>
      <button type="button" onclick={collectNow} disabled={busy}>{t("mc.collectNow")}</button>
      <span class="foot">
        {stored} {t("mc.points")} · {formatBytes(bytes)}{#if span} · {span}{/if}{#if file} · <code>{file}</code>{/if}
      </span>
      {#if status?.lastError}<span class="err" title={status.lastError}>{t("mc.lastError")}</span>{/if}
    </section>

    {#if loading && samples.length === 0}
      <StateBlock kind="loading" message={t("mc.reading")} detail="mon/history" />
    {:else if samples.length === 0}
      <StateBlock
        kind="empty"
        message={t("mc.empty")}
        detail={status?.lastError || t("mc.emptyDetail")}
      />
    {:else}
      <section class="cards">
        <div class="card">
          <span class="lbl">{t("mc.lbl.health")}</span>
          <span class="num" class:green={latest?.cluster.status === "green"} class:yellow={latest?.cluster.status === "yellow"} class:red={latest?.cluster.status === "red"}>
            {latest?.cluster.status ?? "—"}
          </span>
        </div>
        <div class="card">
          <span class="lbl">{t("mc.lbl.nodes")}</span>
          <span class="num">{latest?.cluster.nodes ?? 0}</span>
        </div>
        <div class="card">
          <span class="lbl">{t("mc.lbl.shards")}</span>
          <span class="num">{latest?.cluster.active ?? 0} <small>/ {latest?.cluster.unassigned ?? 0}</small></span>
        </div>
        <div class="card">
          <span class="lbl">{t("mc.lbl.documents")}</span>
          <span class="num">{formatCount(latest?.cluster.docs ?? 0)}</span>
        </div>
        <div class="card">
          <span class="lbl">{t("mc.lbl.store")}</span>
          <span class="num">{formatBytes(latest?.cluster.storeBytes ?? 0)}</span>
        </div>
        <div class="card">
          <span class="lbl">{t("mc.lbl.heapCpu")}</span>
          <span class="num">{(latest?.cluster.heapPercent ?? 0).toFixed(1)}% <small>/ {(latest?.cluster.cpuPercent ?? 0).toFixed(1)}%</small></span>
        </div>
      </section>

      <section class="grid">
        <div class="panel">
          <h2>{t("mc.section.shards")}</h2>
          <MetricChart
            series={[
              series("active", "var(--es-accent)", (s) => s.cluster.active),
              series("unassigned", "var(--es-red)", (s) => s.cluster.unassigned),
            ]}
            format={(value) => formatCount(Math.round(value))}
          />
        </div>
        <div class="panel">
          <h2>{t("mc.section.documents")}</h2>
          <MetricChart series={[series("docs", "var(--es-green)", (s) => s.cluster.docs)]} format={(value) => formatCount(Math.round(value))} />
        </div>
        <div class="panel">
          <h2>{t("mc.section.store")}</h2>
          <MetricChart series={[series("store", "var(--es-accent)", (s) => s.cluster.storeBytes)]} format={(value) => formatBytes(Math.round(value))} />
        </div>
        <div class="panel">
          <h2>{t("mc.section.heapCpu")}</h2>
          <MetricChart
            yMax={100}
            series={[
              series("heap", "var(--es-accent)", (s) => s.cluster.heapPercent),
              series("cpu", "var(--es-yellow)", (s) => s.cluster.cpuPercent),
            ]}
            format={(value) => `${value.toFixed(0)}%`}
          />
        </div>
      </section>
    {/if}
  {/if}
</div>

<style>
  .page { max-width: 1180px; }
  header { display: flex; justify-content: space-between; align-items: start; gap: 16px; margin-bottom: 14px; }
  h1 { margin: 0 0 4px; font-size: var(--es-fs-3xl); }
  header p { margin: 0; color: var(--es-muted); }
  .toast { padding: 8px 12px; margin-bottom: 12px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface-2); font-size: var(--es-fs-sm); }
  .bar { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; padding: 10px 14px; border: 1px solid var(--es-border); border-radius: var(--es-radius); background: var(--es-surface); margin-bottom: 14px; }
  .dot { width: 8px; height: 8px; border-radius: 50%; background: var(--es-muted); }
  .dot.on { background: var(--es-green); box-shadow: 0 0 0 3px var(--es-accent-soft); }
  .state { font-size: var(--es-fs-sm); font-weight: 600; }
  .bar label { display: flex; align-items: center; gap: 6px; font-size: var(--es-fs-xs); color: var(--es-muted); }
  .foot { margin-left: auto; font-size: var(--es-fs-xs); color: var(--es-muted); font-family: var(--es-mono); }
  .foot code { font-size: var(--es-fs-2xs); }
  .err { font-size: var(--es-fs-xs); color: var(--es-red); }
  button { padding: 5px 12px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); cursor: pointer; white-space: nowrap; }
  button:hover:not(:disabled):not(.primary) { background: var(--es-surface-2); }
  .primary { background: var(--es-accent); color: #fff; border-color: var(--es-accent); }
  .primary:hover:not(:disabled) { filter: brightness(0.92); }
  button:disabled { opacity: 0.55; cursor: default; }
  .cards { display: grid; grid-template-columns: repeat(auto-fit, minmax(150px, 1fr)); gap: 10px; margin-bottom: 14px; }
  .card { display: flex; flex-direction: column; gap: 4px; padding: 10px 12px; border: 1px solid var(--es-border); border-radius: var(--es-radius); background: var(--es-surface); }
  .card .lbl { font-size: var(--es-fs-2xs); text-transform: uppercase; letter-spacing: 0.06em; color: var(--es-muted); }
  .card .num { font-size: var(--es-fs-2xl); font-weight: 600; font-family: var(--es-mono); }
  .card .num small { font-size: var(--es-fs-xs); color: var(--es-muted); font-weight: 400; }
  .num.green { color: var(--es-green); }
  .num.yellow { color: var(--es-yellow); }
  .num.red { color: var(--es-red); }
  .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(420px, 1fr)); gap: 12px; }
  .panel { border: 1px solid var(--es-border); border-radius: var(--es-radius); background: var(--es-surface); padding: 12px 14px; }
  .panel h2 { margin: 0 0 8px; font-size: var(--es-fs-sm); text-transform: uppercase; letter-spacing: 0.08em; color: var(--es-muted); }
</style>
