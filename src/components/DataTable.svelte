<script lang="ts">
  import type { TableColumn } from "../lib/types";
  import { t } from "../lib/i18n.svelte.js";

  interface Props {
    columns: TableColumn[];
    rows: Record<string, unknown>[];
    sortable?: boolean;
    /** Empty means "use the translated default"; resolved in the template so a
     *  language switch re-renders it. */
    emptyText?: string;
    rowKey?: (row: Record<string, unknown>) => string;
    /** Rows per page. 0 (default) keeps the old single-page behaviour. */
    pageSize?: number;
  }
  let { columns, rows, sortable = true, emptyText = "", rowKey, pageSize = 0 }: Props = $props();

  let sortKey = $state("");
  let sortDir = $state<"asc" | "desc">("asc");
  let page = $state(1);

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

  const totalPages = $derived(pageSize > 0 ? Math.max(1, Math.ceil(sorted.length / pageSize)) : 1);
  // Clamp page when the data set shrinks (filter / refresh) so we never render an empty page.
  $effect(() => {
    if (page > totalPages) page = totalPages;
  });
  const currentRows = $derived(
    pageSize > 0 ? sorted.slice((page - 1) * pageSize, page * pageSize) : sorted,
  );

  function cellText(col: TableColumn, row: Record<string, unknown>): string {
    const value = row[col.key];
    if (col.render) return col.render(value, row);
    return value === undefined || value === null ? "" : String(value);
  }
</script>

<div class="table-wrap">
  {#if sorted.length === 0}
    <div class="empty">{emptyText || t("common.noData")}</div>
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
              {t(col.label)}
              {#if sortable && sortKey === col.key}
                <span class="sort">{sortDir === "asc" ? "▲" : "▼"}</span>
              {/if}
            </th>
          {/each}
        </tr>
      </thead>
      <tbody>
        {#each currentRows as row, i (rowKey ? rowKey(row) : (page - 1) * pageSize + i)}
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
    {#if pageSize > 0 && totalPages > 1}
      <div class="pager">
        <button type="button" onclick={() => (page = Math.max(1, page - 1))} disabled={page <= 1}>{t("common.prev")}</button>
        <span>{t("common.pageInfo", { page, pages: totalPages, total: sorted.length })}</span>
        <button type="button" onclick={() => (page = Math.min(totalPages, page + 1))} disabled={page >= totalPages}>{t("common.next")}</button>
      </div>
    {/if}
  {/if}
</div>

<style>
  .table-wrap { overflow-x: auto; border: 1px solid var(--es-border); border-radius: var(--es-radius); }
  table { width: 100%; border-collapse: collapse; font-size: var(--es-fs-sm); }
  th, td { padding: 8px 12px; text-align: left; border-bottom: 1px solid var(--es-border); white-space: nowrap; }
  th { background: var(--es-surface-2); color: var(--es-muted); font-weight: 600; position: sticky; top: 0; }
  th.sortable { cursor: pointer; user-select: none; }
  th.right, td.right { text-align: right; }
  th.center, td.center { text-align: center; }
  td.mono { font-family: var(--es-mono); }
  tbody tr:hover { background: var(--es-surface-2); }
  .sort { font-size: var(--es-fs-2xs); margin-left: 4px; }
  .empty { padding: 18px; text-align: center; color: var(--es-muted); }
  .pager { display: flex; align-items: center; justify-content: flex-end; gap: 12px; padding: 8px 12px; border-top: 1px solid var(--es-border); font-size: var(--es-fs-xs); color: var(--es-muted); }
  .pager button { padding: 3px 10px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); cursor: pointer; }
  .pager button:hover:not(:disabled) { background: var(--es-surface-2); }
  .pager button:disabled { opacity: 0.45; cursor: default; }
</style>
