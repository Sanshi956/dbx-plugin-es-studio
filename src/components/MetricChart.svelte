<script lang="ts">
  // Self-drawn SVG time series. No chart library: the UI ships as one inlined
  // HTML file, so every KB of dependency is a KB in the plugin bundle.
  //
  // x is a real timestamp (not an index), so a collector that was stopped for an
  // hour shows an hour-wide gap instead of a lie.
  import type { ChartPoint, ChartSeries } from "../lib/types";

  interface Props {
    series: ChartSeries[];
    height?: number;
    /** Fixed top of the Y axis (e.g. 100 for percentages). */
    yMax?: number;
    format?: (value: number) => string;
    emptyText?: string;
  }

  let {
    series,
    height = 150,
    yMax,
    format = (value: number) => String(Math.round(value * 10) / 10),
    emptyText = "暂无采样点",
  }: Props = $props();

  const PAD_L = 46;
  const PAD_R = 10;
  const PAD_T = 8;
  const PAD_B = 20;
  const GRID = 4;
  const TICKS = Array.from({ length: GRID + 1 }, (_, index) => index);
  // Axis tops read as 20 / 40 / 250KB instead of 21.3 / 43 / 237KB.
  const NICE_STEPS = [1, 2, 2.5, 5, 10];

  function niceMax(value: number): number {
    if (!Number.isFinite(value) || value <= 0) return 1;
    const magnitude = Math.pow(10, Math.floor(Math.log10(value)));
    const normalized = value / magnitude;
    const step = NICE_STEPS.find((candidate) => normalized <= candidate) ?? 10;
    return step * magnitude;
  }

  let width = $state(0);
  let hover = $state(-1);

  let plotW = $derived(Math.max(10, width - PAD_L - PAD_R));
  let plotH = $derived(Math.max(10, height - PAD_T - PAD_B));
  let total = $derived(series.reduce((sum, item) => sum + item.points.length, 0));

  let domain = $derived.by(() => {
    let x0 = Number.POSITIVE_INFINITY;
    let x1 = Number.NEGATIVE_INFINITY;
    let peak = 0;
    for (const item of series) {
      for (const point of item.points) {
        if (point.x < x0) x0 = point.x;
        if (point.x > x1) x1 = point.x;
        if (point.y > peak) peak = point.y;
      }
    }
    if (!Number.isFinite(x0) || !Number.isFinite(x1)) return { x0: 0, x1: 1, y1: 1 };
    return {
      x0,
      x1: x1 === x0 ? x0 + 1 : x1,
      y1: yMax ?? niceMax(peak),
    };
  });

  function sx(x: number): number {
    return PAD_L + ((x - domain.x0) / (domain.x1 - domain.x0)) * plotW;
  }

  function sy(y: number): number {
    return PAD_T + (1 - y / domain.y1) * plotH;
  }

  function linePath(points: ChartPoint[]): string {
    return points
      .map((point, index) => `${index === 0 ? "M" : "L"}${sx(point.x).toFixed(1)},${sy(point.y).toFixed(1)}`)
      .join(" ");
  }

  function areaPath(points: ChartPoint[]): string {
    if (points.length === 0) return "";
    const base = PAD_T + plotH;
    const last = points[points.length - 1];
    const first = points[0];
    return `${linePath(points)} L${sx(last.x).toFixed(1)},${base} L${sx(first.x).toFixed(1)},${base} Z`;
  }

  /** The longest series defines the time axis (all of them share timestamps). */
  function axis(): ChartPoint[] {
    let longest: ChartPoint[] = [];
    for (const item of series) {
      if (item.points.length > longest.length) longest = item.points;
    }
    return longest;
  }

  function onMove(event: MouseEvent): void {
    const ticks = axis();
    if (ticks.length === 0) return;
    const rect = (event.currentTarget as SVGRectElement).getBoundingClientRect();
    const ratio = Math.min(1, Math.max(0, (event.clientX - rect.left) / Math.max(1, rect.width)));
    const target = domain.x0 + ratio * (domain.x1 - domain.x0);
    let best = 0;
    let bestDelta = Number.POSITIVE_INFINITY;
    ticks.forEach((point, index) => {
      const delta = Math.abs(point.x - target);
      if (delta < bestDelta) {
        bestDelta = delta;
        best = index;
      }
    });
    hover = best;
  }

  function clock(ms: number): string {
    const date = new Date(ms);
    const pad = (value: number) => String(value).padStart(2, "0");
    return `${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`;
  }

  let reading = $derived.by(() => {
    const ticks = axis();
    if (ticks.length === 0) return null;
    const index = hover >= 0 && hover < ticks.length ? hover : ticks.length - 1;
    return {
      at: ticks[index].x,
      values: series.map((item) => ({
        label: item.label,
        color: item.color,
        value: index < item.points.length ? item.points[index].y : null,
      })),
    };
  });
</script>

<div class="chart" bind:clientWidth={width}>
  <div class="readout">
    {#if reading}
      <span class="at">{clock(reading.at)}</span>
      {#each reading.values as item (item.label)}
        <span class="val">
          <i style={`background:${item.color}`}></i>{item.label}
          <b>{item.value === null ? "—" : format(item.value)}</b>
        </span>
      {/each}
      {#if hover >= 0}<span class="muted">hover 读数（移开显示最新）</span>{/if}
    {:else}
      <span class="muted">{emptyText}</span>
    {/if}
  </div>

  {#if width > 0 && total > 0}
    <svg width={width} height={height} role="img" aria-label="time series">
      {#each TICKS as tick (tick)}
        {@const y = PAD_T + (plotH / GRID) * tick}
        <line x1={PAD_L} x2={width - PAD_R} y1={y} y2={y} class="grid" />
        <text x={PAD_L - 6} y={y + 3} class="tick" text-anchor="end">{format(domain.y1 * (1 - tick / GRID))}</text>
      {/each}

      {#each series as item, index (item.label)}
        {#if index === 0 && item.points.length > 1}
          <path d={areaPath(item.points)} class="area" style={`fill:${item.color}`} />
        {/if}
        <path d={linePath(item.points)} class="line" style={`stroke:${item.color}`} />
      {/each}

      {#if reading}
        <text x={PAD_L} y={height - 4} class="tick">{clock(domain.x0)}</text>
        <text x={width - PAD_R} y={height - 4} class="tick" text-anchor="end">{clock(domain.x1)}</text>
      {/if}

      {#if hover >= 0}
        {@const tickPoint = axis()[hover]}
        {#if tickPoint}
          <line x1={sx(tickPoint.x)} x2={sx(tickPoint.x)} y1={PAD_T} y2={PAD_T + plotH} class="cursor" />
          {#each series as item (item.label)}
            {#if hover < item.points.length}
              <circle cx={sx(item.points[hover].x)} cy={sy(item.points[hover].y)} r="3" style={`fill:${item.color}`} />
            {/if}
          {/each}
        {/if}
      {/if}

      <!-- Hover is an enhancement: every value is also reachable in the tables
           below, so the hit area stays out of the a11y tree on purpose. -->
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <rect
        x={PAD_L}
        y={PAD_T}
        width={plotW}
        height={plotH}
        class="hit"
        onmousemove={onMove}
        onmouseleave={() => (hover = -1)}
      />
    </svg>
  {:else}
    <div class="empty" style={`height:${height}px`}>{emptyText}</div>
  {/if}
</div>

<style>
  .chart { width: 100%; }
  .readout { display: flex; align-items: center; gap: 12px; min-height: 20px; font-size: 11px; color: var(--es-muted); flex-wrap: wrap; }
  .at { font-family: var(--es-mono); color: var(--es-text); }
  .val { display: inline-flex; align-items: center; gap: 5px; }
  .val i { width: 8px; height: 8px; border-radius: 2px; display: inline-block; }
  .val b { font-family: var(--es-mono); color: var(--es-text); font-weight: 600; }
  .muted { color: var(--es-muted); }
  svg { display: block; }
  .grid { stroke: var(--es-border); stroke-width: 1; stroke-dasharray: 3 3; }
  .tick { fill: var(--es-muted); font-size: 10px; font-family: var(--es-mono); }
  .line { fill: none; stroke-width: 1.6; stroke-linejoin: round; stroke-linecap: round; }
  .area { opacity: 0.1; stroke: none; }
  .cursor { stroke: var(--es-muted); stroke-width: 1; stroke-dasharray: 2 2; }
  .hit { fill: transparent; cursor: crosshair; }
  .empty { display: grid; place-items: center; font-size: 11px; color: var(--es-muted); border: 1px dashed var(--es-border); border-radius: 6px; }
</style>
