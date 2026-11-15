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
