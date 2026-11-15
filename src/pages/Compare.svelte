<script lang="ts">
  import StateBlock from "../components/StateBlock.svelte";
  import { collectFacts, diffFacts, summarize, toCsv, type ClusterFacts, type DiffRow } from "../lib/compare";
  import type { DbxBridge, ConnectionSummary } from "../lib/bridge";
  import { t } from "../lib/i18n.svelte.js";

  interface Props {
    bridge: DbxBridge;
    connectionId: string;
    connections: ConnectionSummary[];
  }
  let { bridge, connectionId, connections }: Props = $props();

  let other = $state("");
  let left = $state<ClusterFacts | null>(null);
  let right = $state<ClusterFacts | null>(null);
  let loading = $state(false);
  let error = $state("");
  let otherError = $state("");
  let toast = $state("");
  let showCsv = $state(false);

  let candidates = $derived(connections.filter((row) => row.id !== connectionId));
  let rows = $derived(left && right ? diffFacts(left, right) : []);
  let stats = $derived(summarize(rows));
  let groups = $derived.by(() => {
    const out: { name: string; rows: DiffRow[] }[] = [];
    for (const row of rows) {
      const last = out[out.length - 1];
      if (last && last.name === row.group) last.rows.push(row);
      else out.push({ name: row.group, rows: [row] });
    }
    return out;
  });

  function labelOf(id: string): string {
    const found = connections.find((row) => row.id === id);
    return found?.name || found?.label || id || "—";
  }

  async function compare(): Promise<void> {
    if (!connectionId || !other) return;
    loading = true;
    error = "";
    otherError = "";
    left = null;
    right = null;
    const [leftResult, rightResult] = await Promise.allSettled([
      collectFacts(bridge, connectionId),
      collectFacts(bridge, other),
    ]);
    if (leftResult.status === "fulfilled") left = leftResult.value;
    else error = leftResult.reason instanceof Error ? leftResult.reason.message : String(leftResult.reason);
    if (rightResult.status === "fulfilled") right = rightResult.value;
    else otherError = rightResult.reason instanceof Error ? rightResult.reason.message : String(rightResult.reason);
    loading = false;
  }

  $effect(() => {
    // Default to the first other connection so the page is useful on arrival.
    if (!other && candidates.length > 0) {
      other = candidates[0].id;
      void compare();
    }
  });

  async function copyCsv(): Promise<void> {
    const csv = toCsv(rows, labelOf(connectionId), labelOf(other));
    try {
      await navigator.clipboard.writeText(csv);
      showCsv = false;
      toast = t("cmp.toast.copied", { rows: rows.length });
    } catch {
      showCsv = true;
      toast = t("cmp.toast.denied");
    }
  }

  function notConnectedHint(message: string): string {
    return /not connected/i.test(message)
      ? `${message} — ${t("cmp.notConnected")}`
      : message;
  }
</script>

<div class="page">
  <header>
    <div>
      <h1>{t("cmp.title")}</h1>
      <p>{t("cmp.hint")}</p>
    </div>
    <div class="tools">
      <select bind:value={other} aria-label={t("cmp.aria.other")}>
        {#if candidates.length === 0}
          <option value="">{t("cmp.noSecond")}</option>
        {/if}
        {#each candidates as row (row.id)}
          <option value={row.id}>{row.name || row.label || row.id}</option>
        {/each}
      </select>
      <button type="button" onclick={() => compare()} disabled={loading || !other}>
        {loading ? t("cmp.comparing") : t("cmp.compare")}
      </button>
      <button type="button" onclick={copyCsv} disabled={rows.length === 0}>{t("cmp.copyCsv")}</button>
    </div>
  </header>

  {#if toast}<div class="toast">{toast}</div>{/if}

  {#if !connectionId}
    <StateBlock kind="error" message={t("cmp.noConnection")} detail={t("cmp.noConnectionDetail")} />
  {:else if candidates.length === 0}
    <StateBlock
      kind="empty"
      message={t("cmp.needTwo.title")}
      detail={t("cmp.needTwo.detail")}
    />
  {:else if error}
    <StateBlock kind="error" message={t("cmp.leftFailed")} detail={error} />
  {:else if otherError}
    <StateBlock kind="error" message={t("cmp.rightFailed")} detail={notConnectedHint(otherError)} />
  {:else if loading && rows.length === 0}
    <StateBlock kind="loading" message={t("cmp.reading")} detail="es/batch · health / stats / nodes / settings" />
  {:else if rows.length > 0 && left && right}
    <div class="meta">
      <span class="side">{labelOf(connectionId)}</span>
      <span class="vs">vs</span>
      <span class="side">{labelOf(other)}</span>
      <span class="counts">{t("cmp.summary", { different: stats.different, same: stats.same })}</span>
    </div>

    {#if showCsv}
      <textarea class="csv" readonly rows="10" value={toCsv(rows, labelOf(connectionId), labelOf(other))}></textarea>
    {/if}

    <table class="diff">
      <thead>
        <tr>
          <th>{t("cmp.col.item")}</th>
          <th>{labelOf(connectionId)}</th>
          <th>{labelOf(other)}</th>
        </tr>
      </thead>
      <tbody>
        {#each groups as group (group.name)}
          <tr class="group"><td colspan="3">{t(group.name)}</td></tr>
          {#each group.rows as row (row.item)}
            <tr class:diff={!row.same}>
              <td title={row.hint ? t(row.hint) : ""}>{t(row.item)}</td>
              <td class="mono">{row.left}</td>
              <td class="mono">{row.right}</td>
            </tr>
          {/each}
        {/each}
      </tbody>
    </table>
  {/if}
</div>

<style>
  .page { max-width: 1180px; }
  header { display: flex; justify-content: space-between; align-items: start; gap: 16px; margin-bottom: 12px; flex-wrap: wrap; }
  h1 { margin: 0 0 4px; font-size: var(--es-fs-3xl); }
  header p { margin: 0; color: var(--es-muted); }
  .tools { display: flex; gap: 8px; align-items: center; }
  select { padding: 5px 8px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); max-width: 220px; }
  button { padding: 5px 12px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); cursor: pointer; white-space: nowrap; }
  button:hover:not(:disabled) { background: var(--es-surface-2); }
  button:disabled { opacity: 0.55; cursor: default; }
  .toast { padding: 8px 12px; margin-bottom: 12px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface-2); font-size: var(--es-fs-sm); }
  .meta { display: flex; align-items: center; gap: 10px; font-size: var(--es-fs-xs); color: var(--es-muted); margin-bottom: 10px; font-family: var(--es-mono); }
  .meta .side { color: var(--es-text); }
  .meta .vs { opacity: 0.6; }
  .meta .counts { margin-left: auto; }
  .csv { width: 100%; font-family: var(--es-mono); font-size: var(--es-fs-xs); padding: 8px 10px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); margin-bottom: 12px; }
  table.diff { width: 100%; border-collapse: collapse; font-size: var(--es-fs-sm); border: 1px solid var(--es-border); border-radius: var(--es-radius); overflow: hidden; }
  .diff th, .diff td { padding: 6px 10px; border-bottom: 1px solid var(--es-border); text-align: left; }
  .diff th { background: var(--es-surface-2); color: var(--es-muted); font-weight: 600; font-size: var(--es-fs-xs); }
  .diff tr.group td { background: var(--es-surface-2); color: var(--es-muted); font-size: var(--es-fs-2xs); text-transform: uppercase; letter-spacing: 0.08em; padding: 4px 10px; }
  .diff tbody tr:hover { background: var(--es-surface-2); }
  .diff tr.diff td { font-weight: 600; }
  .diff tr.diff td:first-child { box-shadow: inset 3px 0 0 var(--es-yellow); }
  .mono { font-family: var(--es-mono); }
</style>
