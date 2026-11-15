<script lang="ts">
  import { untrack } from "svelte";
  import StateBlock from "../components/StateBlock.svelte";
  import {
    fetchShards, explainAllocation,
    type ShardRow, type AllocationExplain,
  } from "../lib/services";
  import { decisionLabel, deciderLabel, explanationText } from "../lib/explainText";
  import type { DbxBridge } from "../lib/bridge";
  import { t } from "../lib/i18n.svelte.js";

  interface Props {
    bridge: DbxBridge;
    connectionId: string;
  }
  let { bridge, connectionId }: Props = $props();

  /** 一次最多解释多少个未分配分片：explain 是逐分片请求，超过就截断并提示。 */
  const EXPLAIN_CAP = 12;

  let shards = $state<ShardRow[]>([]);
  let loading = $state(true);
  let error = $state("");
  let lastUpdated = $state("");
  let inflight = $state(false);

  interface ExplainOutcome {
    key: string;
    row: ShardRow;
    result: AllocationExplain | null;
    errorText: string;
  }
  let outcomes = $state<ExplainOutcome[]>([]);
  let explaining = $state(false);
  let explainDone = $state(0);
  let explainTotal = $state(0);
  let explainTruncated = $state(false);

  const unassigned = $derived(shards.filter((s) => s.unassigned));

  async function load(id: string): Promise<void> {
    if (inflight) return;
    if (!id) {
      shards = [];
      error = "NO_CONNECTION";
      loading = false;
      return;
    }
    inflight = true;
    loading = true;
    error = "";
    try {
      shards = await fetchShards(bridge, id);
      lastUpdated = new Date().toLocaleTimeString();
    } catch (cause) {
      shards = [];
      error = cause instanceof Error ? cause.message : String(cause);
    } finally {
      inflight = false;
      loading = false;
    }
  }

  // untrack: load() reads/writes inflight & loading synchronously; tracking them
  // would re-run this effect after every fetch (infinite load loop).
  $effect(() => {
    untrack(() => void load(connectionId));
  });

  /** 解释「未分配分片」面板里的全部行（超过 EXPLAIN_CAP 截断），结果按分片分组展示。 */
  async function explainAll(): Promise<void> {
    const rows = unassigned.slice(0, EXPLAIN_CAP);
    explainTruncated = unassigned.length > EXPLAIN_CAP;
    explaining = true;
    explainDone = 0;
    explainTotal = rows.length;
    outcomes = [];
    for (const row of rows) {
      const key = `${row.index}/${row.shard}/${row.prirep}`;
      try {
        const result = await explainAllocation(bridge, connectionId, {
          index: row.index,
          shard: row.shard,
          primary: row.prirep === "p",
        });
        outcomes = [...outcomes, { key, row, result, errorText: "" }];
      } catch (cause) {
        const errorText = cause instanceof Error ? cause.message : String(cause);
        outcomes = [...outcomes, { key, row, result: null, errorText }];
      }
      explainDone += 1;
    }
    explaining = false;
  }
</script>

<header>
  <h1>{t("sdiag.title")}</h1>
  <p>{t("sdiag.hint")}</p>
</header>

{#if loading}
  <StateBlock kind="loading" message={t("sdiag.loading")} detail="GET /_cat/shards" />
{:else if error}
  <StateBlock kind="error" message={t("sdiag.loadFailed")} detail={error === "NO_CONNECTION" ? t("common.noConnection") : error} />
{:else}
  <section class="panel">
    <div class="panel-head">
      <h2>{t("sdiag.list.title", { count: unassigned.length })}</h2>
      <span class="hint mono">{lastUpdated}</span>
      <span class="grow"></span>
      <button type="button" onclick={() => void load(connectionId)} disabled={inflight}>
        {inflight ? t("common.refreshing") : t("common.refresh")}
      </button>
      <button type="button" class="primary" disabled={explaining || unassigned.length === 0}
              onclick={() => void explainAll()}>
        {explaining ? t("sdiag.explainRunning", { done: explainDone, total: explainTotal }) : t("sdiag.explainAll")}
      </button>
    </div>
    {#if unassigned.length === 0}
      <p class="hint">{t("sdiag.empty")}</p>
    {:else}
      <table>
        <thead>
          <tr>
            <th>{t("sdiag.col.index")}</th>
            <th>{t("sdiag.col.shard")}</th>
            <th>{t("sdiag.col.copy")}</th>
            <th>{t("sdiag.col.reason")}</th>
            <th title={t("sdiag.tip.waiting")}>{t("sdiag.col.waiting")}</th>
          </tr>
        </thead>
        <tbody>
          {#each unassigned as row, i (i)}
            <tr>
              <td class="mono">{row.index}</td>
              <td>{row.shard}</td>
              <td>{row.prirep === "p" ? t("sdiag.copy.primary") : t("sdiag.copy.replica")}</td>
              <td class="mono reason">{row.reason || "—"}</td>
              <td>{row.waiting || "—"}</td>
            </tr>
          {/each}
        </tbody>
      </table>
      {#if explainTruncated}
        <p class="hint">{t("sdiag.explainCap", { cap: EXPLAIN_CAP })}</p>
      {/if}
    {/if}
  </section>

  <!-- 结果列表单次运行内追加、只清空不重排，用位置做键；真实集群的 _cat/shards
       会出现重复行（实测 metrics-daily 0 r 两次），分片三元组会撞键白屏。 -->
  {#each outcomes as outcome, oi (oi)}
    <section class="panel">
      <div class="panel-head">
        <h2>{t("sdiag.result.title")} · <span class="mono">{outcome.row.index}</span> · shard {outcome.row.shard}{outcome.row.prirep === "p" ? t("sdiag.copy.primarySuffix") : t("sdiag.copy.replicaSuffix")}</h2>
        {#if outcome.errorText}
          <strong class="bad">{t("sdiag.failed", { detail: outcome.errorText })}</strong>
        {:else if outcome.result && !outcome.result.ok}
          <strong class="bad">{t("sdiag.noExplain", { detail: outcome.result.error ?? "" })}</strong>
        {:else if outcome.result}
          <span class="verdict"><span class={outcome.result.canAllocate === "yes" ? "ok" : "bad"}>{decisionLabel(outcome.result.canAllocate)}</span> {explanationText(outcome.result.reason)}</span>
        {/if}
      </div>
      {#if outcome.result?.ok && outcome.result.nodes.length}
        <table>
          <thead>
            <tr>
              <th>{t("sdiag.col.node")}</th>
              <th>{t("sdiag.col.decider")}</th>
              <th>{t("sdiag.col.decision")}</th>
              <th>{t("sdiag.col.explanation")}</th>
            </tr>
          </thead>
          <tbody>
            {#each outcome.result.nodes as d (d.node)}
              <tr>
                <td class="mono">{d.node}</td>
                <td>{deciderLabel(d.decider)}</td>
                <td><span class={d.decision === "yes" ? "ok" : "bad"}>{decisionLabel(d.decision)}</span></td>
                <td class="decider" title={d.reason}>{explanationText(d.reason)}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
      {#if outcome.result?.ok && outcome.result.note}<p class="note">{outcome.result.note}</p>{/if}
    </section>
  {/each}
{/if}

<style>
  header h1 { margin: 0 0 4px; font-size: var(--es-fs-xl); }
  header p { margin: 0; color: var(--es-muted); }
  .panel { border: 1px solid var(--es-border); border-radius: var(--es-radius); background: var(--es-surface); padding: 10px 14px; margin-bottom: 14px; overflow-x: auto; }
  .panel-head { display: flex; align-items: baseline; gap: 14px; flex-wrap: wrap; margin-bottom: 6px; }
  .panel-head h2 { margin: 0 0 0; font-size: var(--es-fs-sm); text-transform: uppercase; letter-spacing: 0.08em; color: var(--es-muted); }
  .panel-head button { padding: 4px 10px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); cursor: pointer; white-space: nowrap; }
  .panel-head button.primary { background: var(--es-accent); border-color: var(--es-accent); color: #fff; }
  .panel-head button.primary:hover:not(:disabled) { filter: brightness(0.92); }
  .panel-head button:disabled { opacity: 0.55; cursor: default; }
  .grow { flex: 1; }
  table { width: 100%; border-collapse: collapse; font-size: var(--es-fs-sm); margin-top: 6px; }
  th, td { padding: 6px 10px; border-bottom: 1px solid var(--es-border); text-align: left; vertical-align: top; }
  th { background: var(--es-surface-2); color: var(--es-muted); font-weight: 600; font-size: var(--es-fs-xs); white-space: nowrap; }
  .mono { font-family: var(--es-mono); }
  .reason { font-size: var(--es-fs-xs); }
  .decider { font-size: var(--es-fs-xs); color: var(--es-muted); white-space: normal; }
  .verdict { font-size: var(--es-fs-sm); }
  .ok { color: var(--es-ok, #2e7d32); }
  .bad { color: var(--es-danger, #c62828); }
  .note { font-size: var(--es-fs-xs); color: var(--es-muted); margin: 6px 0 0; }
  .hint { font-size: var(--es-fs-xs); color: var(--es-muted); margin: 6px 0; }
</style>
