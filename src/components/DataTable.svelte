<script lang="ts">
  import type { TableColumn } from "../lib/types";

  interface Props {
    columns: TableColumn[];
    rows: Record<string, unknown>[];
    sortable?: boolean;
    emptyText?: string;
    rowKey?: (row: Record<string, unknown>) => string;
  }
  let { columns, rows, sortable = true, emptyText = "No data", rowKey }: Props = $props();

  let sortKey = $state("");
  let sortDir = $state<"asc" | "desc">("asc");

  function toggleSort(key: string): void {
    if (!sortable) return;
    if (sortKey === key) {
      sortDir = sortDir === "asc" ? "desc" : "asc";
    } else {
      sortKey = key;
      sortDir = "asc";
    }
  }

  function compare(a: unknown, b: unknown): number {
    if (typeof a === "number" && typeof b === "number") return a - b;
    return String(a).localeCompare(String(b), "en", { numeric: true });
  }

  // $derived.by keeps reactivity without the $state(null) narrowing pitfall.
  let sorted = $derived.by(() => {
    if (!sortKey) return rows;
    const copy = [...rows];
    copy.sort((a, b) => {
      const av = a[sortKey];
      const bv = b[sortKey];
      const r = compare(av, bv);
      return sortDir === "asc" ? r : -r;
    });
    return copy;
  });

  function cellText(col: TableColumn, row: Record<string, unknown>): string {
    const value = row[col.key];
    if (col.render) return col.render(value, row);
    return value === undefined || value === null ? "" : String(value);
  }
</script>

<div class="table-wrap">
  {#if sorted.length === 0}
    <div class="empty">{emptyText}</div>
  {:else}
    <table>
      <thead>
        <tr>
          {#each columns as col (col.key)}
            <th
              class:sortable={sortable && col.sortable !== false}
              class:right={col.align === "right"}
              class:center={col.align === "center"}
              onclick={() => col.sortable !== false && toggleSort(col.key)}
            >
              {col.label}
              {#if sortable && sortKey === col.key}
                <span class="sort">{sortDir === "asc" ? "▲" : "▼"}</span>
              {/if}
            </th>
          {/each}
        </tr>
      </thead>
      <tbody>
        {#each sorted as row, i (rowKey ? rowKey(row) : i)}
          <tr>
            {#each columns as col (col.key)}
              <td
                class:right={col.align === "right"}
                class:center={col.align === "center"}
                class:mono={col.mono}
                class={col.cellClass ? col.cellClass(row[col.key], row) : ""}
              >{cellText(col, row)}</td>
            {/each}
          </tr>
        {/each}
      </tbody>
    </table>
  {/if}
</div>

<style>
  .table-wrap { overflow-x: auto; border: 1px solid var(--es-border); border-radius: var(--es-radius); }
  table { width: 100%; border-collapse: collapse; font-size: 12px; }
  th, td { padding: 8px 12px; text-align: left; border-bottom: 1px solid var(--es-border); white-space: nowrap; }
  th { background: var(--es-surface-2); color: var(--es-muted); font-weight: 600; position: sticky; top: 0; }
  th.sortable { cursor: pointer; user-select: none; }
  th.right, td.right { text-align: right; }
  th.center, td.center { text-align: center; }
  td.mono { font-family: var(--es-mono); }
  tbody tr:hover { background: var(--es-surface-2); }
  .sort { font-size: 9px; margin-left: 4px; }
  .empty { padding: 18px; text-align: center; color: var(--es-muted); }
</style>
