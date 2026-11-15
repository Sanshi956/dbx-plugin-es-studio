<script lang="ts">
  import StateBlock from "../components/StateBlock.svelte";
  import { fetchMonitorHistory, type MonitorSample } from "../lib/services";
  import { formatBytes, formatCount } from "../lib/api";
  import type { DbxBridge } from "../lib/bridge";

  interface Props {
    bridge: DbxBridge;
    connectionId: string;
  }
  let { bridge, connectionId }: Props = $props();

  const RANGES = [
    { label: "最近 1 小时", hours: 1 },
    { label: "最近 6 小时", hours: 6 },
    { label: "最近 24 小时", hours: 24 },
    { label: "全部（最多 5000 点）", hours: 0 },
  ];
  const MAX_ROWS = 500;

  let samples = $state<MonitorSample[]>([]);
  let stored = $state(0);
  let bytes = $state(0);
  let file = $state("");
  let loading = $state(true);
  let error = $state("");
  let toast = $state("");
  let rangeHours = $state(1);
  let showCsv = $state(false);

  async function load(): Promise<void> {
    if (!connectionId) {
      error = "NO_CONNECTION";
      loading = false;
      return;
    }
    loading = true;
    error = "";
    try {
      const since = rangeHours > 0 ? Date.now() - rangeHours * 3600_000 : 0;
      const history = await fetchMonitorHistory(bridge, connectionId, { sinceMs: since, limit: 5000 });
      samples = history.samples;
      stored = history.stored;
      bytes = history.bytes;
      file = history.file;
    } catch (cause) {
      error = cause instanceof Error ? cause.message : String(cause);
    } finally {
      loading = false;
    }
  }

  $effect(() => {
    void connectionId;
    void rangeHours;
    void load();
  });

  function stamp(ms: number): string {
    const date = new Date(ms);
    const pad = (value: number) => String(value).padStart(2, "0");
    return `${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`;
  }

  let rows = $derived(samples.slice(-MAX_ROWS).reverse());

  function toCsv(): string {
    const header = [
      "time", "status", "nodes", "primary_shards", "active_shards", "unassigned_shards",
      "relocating_shards", "initializing_shards", "indices", "docs", "store_bytes",
      "heap_percent", "cpu_percent",
    ].join(",");
    const lines = rows.map((sample) =>
      [
        new Date(sample.at).toISOString(),
        sample.cluster.status,
        sample.cluster.nodes,
        sample.cluster.primary,
        sample.cluster.active,
        sample.cluster.unassigned,
        sample.cluster.relocating,
        sample.cluster.initializing,
        sample.cluster.indices,
        sample.cluster.docs,
        sample.cluster.storeBytes,
        sample.cluster.heapPercent,
        sample.cluster.cpuPercent,
      ].join(","),
    );
    return [header, ...lines].join("\n");
  }

  async function copyCsv(): Promise<void> {
    const csv = toCsv();
    try {
      await navigator.clipboard.writeText(csv);
      showCsv = false;
      toast = `已复制 ${rows.length} 行 CSV 到剪贴板`;
    } catch {
      // Opaque-origin iframe may refuse clipboard writes without a user gesture
      // grant; showing the text keeps the data reachable.
      showCsv = true;
      toast = "剪贴板被拒绝，已展开文本，可手动全选复制";
    }
  }
</script>

<div class="page">
  <header>
    <div>
      <h1>History</h1>
      <p>Raw samples from <code>mon/history</code>, newest first.</p>
    </div>
    <div class="tools">
      <select bind:value={rangeHours}>
        {#each RANGES as range (range.hours)}
          <option value={range.hours}>{range.label}</option>
        {/each}
      </select>
      <button type="button" onclick={() => load()} disabled={loading}>{loading ? "Loading…" : "刷新"}</button>
      <button type="button" onclick={copyCsv} disabled={rows.length === 0}>复制 CSV</button>
    </div>
  </header>

  {#if toast}<div class="toast">{toast}</div>{/if}

  <div class="meta">
    当前范围 {rows.length} 行 · 库内共 {formatCount(stored)} 点 · {formatBytes(bytes)}{#if file} · <code>{file}</code>{/if}
  </div>

  {#if error === "NO_CONNECTION"}
    <StateBlock kind="error" message="No connection selected" detail="Select a connected cluster first." />
  {:else if error}
    <StateBlock kind="error" message="Could not read the monitoring series" detail={error} />
  {:else if loading && samples.length === 0}
    <StateBlock kind="loading" message="Reading samples…" detail="mon/history" />
  {:else if rows.length === 0}
    <StateBlock kind="empty" message="该范围内没有采样点" detail="换个时间范围，或先去 Cluster Metrics 开始采集" />
  {:else}
    {#if showCsv}
      <textarea class="csv" readonly rows="10" value={toCsv()}></textarea>
    {/if}
    <table class="hist">
      <thead>
        <tr>
          <th>Time</th>
          <th>Health</th>
          <th class="right">Nodes</th>
          <th class="right" title="主分片数">Primary</th>
          <th class="right" title="活跃分片总数">Active</th>
          <th class="right" title="未分配分片数">Unassigned</th>
          <th class="right">Docs</th>
          <th class="right">Store</th>
          <th class="right">Heap</th>
          <th class="right">CPU</th>
        </tr>
      </thead>
      <tbody>
        {#each rows as row (row.at)}
          <tr>
            <td class="mono">{stamp(row.at)}</td>
            <td>
              <span class="tag" class:green={row.cluster.status === "green"} class:yellow={row.cluster.status === "yellow"} class:red={row.cluster.status === "red"}>
                {row.cluster.status || "—"}
              </span>
            </td>
            <td class="right mono">{row.cluster.nodes}</td>
            <td class="right mono">{row.cluster.primary}</td>
            <td class="right mono">{row.cluster.active}</td>
            <td class="right mono" class:bad={row.cluster.unassigned > 0}>{row.cluster.unassigned}</td>
            <td class="right mono">{formatCount(row.cluster.docs)}</td>
            <td class="right mono">{formatBytes(row.cluster.storeBytes)}</td>
            <td class="right mono">{row.cluster.heapPercent.toFixed(0)}%</td>
            <td class="right mono">{row.cluster.cpuPercent.toFixed(1)}%</td>
          </tr>
        {/each}
      </tbody>
    </table>
    {#if samples.length > MAX_ROWS}
      <p class="note">仅显示最新 {MAX_ROWS} 行；完整范围可用「复制 CSV」导出（{samples.length} 行）。</p>
    {/if}
  {/if}
</div>

<style>
  .page { max-width: 1180px; }
  header { display: flex; justify-content: space-between; align-items: start; gap: 16px; margin-bottom: 12px; flex-wrap: wrap; }
  h1 { margin: 0 0 4px; font-size: 18px; }
  header p { margin: 0; color: var(--es-muted); }
  header code { font-family: var(--es-mono); font-size: 11px; }
  .tools { display: flex; gap: 8px; align-items: center; }
  select { padding: 5px 8px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); }
  button { padding: 5px 12px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); cursor: pointer; white-space: nowrap; }
  button:hover:not(:disabled) { background: var(--es-surface-2); }
  button:disabled { opacity: 0.55; cursor: default; }
  .toast { padding: 8px 12px; margin-bottom: 12px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface-2); font-size: 12px; }
  .meta { font-size: 11px; color: var(--es-muted); font-family: var(--es-mono); margin-bottom: 10px; }
  .meta code { font-size: 10px; }
  .csv { width: 100%; font-family: var(--es-mono); font-size: 11px; padding: 8px 10px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); margin-bottom: 12px; }
  table.hist { width: 100%; border-collapse: collapse; font-size: 12px; border: 1px solid var(--es-border); border-radius: var(--es-radius); overflow: hidden; }
  .hist th, .hist td { padding: 6px 10px; border-bottom: 1px solid var(--es-border); text-align: left; white-space: nowrap; }
  .hist th { background: var(--es-surface-2); color: var(--es-muted); font-weight: 600; font-size: 11px; }
  .hist tbody tr:hover { background: var(--es-surface-2); }
  .right { text-align: right; }
  .mono { font-family: var(--es-mono); }
  .bad { color: var(--es-red); }
  .tag { display: inline-block; padding: 1px 6px; border-radius: 4px; font-size: 10px; font-family: var(--es-mono); border: 1px solid var(--es-border); }
  .tag.green { color: var(--es-green); border-color: var(--es-green); }
  .tag.yellow { color: var(--es-yellow); border-color: var(--es-yellow); }
  .tag.red { color: var(--es-red); border-color: var(--es-red); }
  .note { margin: 8px 0 0; font-size: 11px; color: var(--es-muted); }
</style>
