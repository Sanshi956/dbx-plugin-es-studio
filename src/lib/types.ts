// Shared UI types. Kept outside .svelte components so they can be imported by
// both the component and the pages that build column definitions.

export interface TableColumn {
  key: string;
  label: string;
  align?: "left" | "right" | "center";
  mono?: boolean;
  sortable?: boolean;
  render?: (value: unknown, row: Record<string, unknown>) => string;
  cellClass?: (value: unknown, row: Record<string, unknown>) => string;
}

// --- P4 charts -------------------------------------------------------------

export interface ChartPoint {
  /** x is a timestamp in ms, so gaps (collector stopped) show as real gaps. */
  x: number;
  y: number;
}

export interface ChartSeries {
  label: string;
  /** Any CSS color, including `var(--es-accent)`. */
  color: string;
  points: ChartPoint[];
}
