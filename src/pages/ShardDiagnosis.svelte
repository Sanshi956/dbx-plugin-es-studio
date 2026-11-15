<script lang="ts">
  import { untrack } from "svelte";
  import StateBlock from "../components/StateBlock.svelte";
  import {
    fetchShards, explainAllocation,
    type ShardRow, type AllocationExplain,
  } from "../lib/services";
  import type { DbxBridge } from "../lib/bridge";
  import { t } from "../lib/i18n.svelte.js";

  interface Props {
    bridge: DbxBridge;
    connectionId: string;
  }
  let { bridge, connectionId }: Props = $props();

  let shards = $state<ShardRow[]>([]);
  let loading = $state(true);
  let error = $state("");
  let lastUpdated = $state("");
  let inflight = $state(false);

  let explain = $state<AllocationExplain | null>(null);
  let explaining = $state("");
  let explainError = $state("");

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

  async function runExplain(row: ShardRow): Promise<void> {
    const key = `${row.index}/${row.shard}/${row.prirep}`;
    explaining = key;
    explainError = "";
    try {
      // Targeted explain: address the exact copy instead of letting ES pick one.
      explain = await explainAllocation(bridge, connectionId, {
        index: row.index,
        shard: row.shard,
        primary: row.prirep === "p",
      });
    } catch (cause) {
      explain = null;
      explainError = cause instanceof Error ? cause.message : String(cause);
    } finally {
      explaining = "";
    }
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
      <button type="button" onclick={() => void load(connectionId)} disabled={inflight}>
        {inflight ? t("common.refreshing") : t("common.refresh")}
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
            <th class="right">{t("sdiag.col.actions")}</th>
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
              <td class="right">
                <button type="button" class="link" disabled={explaining !== ""}
                        onclick={() => void runExplain(row)}>
                  {explaining === row.index + "/" + row.shard + "/" + row.prirep ? t("sdiag.explaining") : t("sdiag.explain")}
                </button>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </section>

  {#if explainError}
    <section class="panel"><p class="hint">{t("sdiag.failed", { detail: explainError })}</p></section>
  {/if}

  {#if explain}
    <section class="panel">
      <div class="panel-head">
        <h2>{t("sdiag.result.title")}</h2>
        {#if explain.ok}
          <span class="mono">{explain.index} · shard {explain.shard}{explain.primary ? t("sdiag.copy.primarySuffix") : t("sdiag.copy.replicaSuffix")}</span>
          <span class="verdict"><span class={explain.canAllocate === "yes" ? "ok" : "bad"}>{explain.canAllocate}</span> {explain.reason}</span>
        {:else}
          <strong class="note">{t("sdiag.noExplain", { detail: explain.error ?? "" })}</strong>
        {/if}
      </div>
      {#if explain.ok && explain.nodes.length}
        <table>
          <thead>
            <tr>
              <th>{t("sdiag.col.node")}</th>
              <th>{t("sdiag.col.decision")}</th>
              <th>{t("sdiag.col.reason")}</th>
            </tr>
          </thead>
          <tbody>
            {#each explain.nodes as d (d.node)}
              <tr>
                <td class="mono">{d.node}</td>
                <td><span class={d.decision === "yes" ? "ok" : "bad"}>{d.decision}</span></td>
                <td class="decider">{d.reason}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
      {#if explain.ok && explain.note}<p class="note">{explain.note}</p>{/if}
    </section>
  {/if}
{/if}

<style>
  header h1 { margin: 0 0 4px; font-size: var(--es-fs-xl); }
  header p { margin: 0; color: var(--es-muted); }
  .panel { border: 1px solid var(--es-border); border-radius: var(--es-radius); background: var(--es-surface); padding: 10px 14px; margin-bottom: 14px; overflow-x: auto; }
  .panel-head { display: flex; align-items: baseline; gap: 14px; flex-wrap: wrap; margin-bottom: 6px; }
  .panel-head h2 { margin: 0 0 0; font-size: var(--es-fs-sm); text-transform: uppercase; letter-spacing: 0.08em; color: var(--es-muted); }
  .panel-head button { padding: 4px 10px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); cursor: pointer; white-space: nowrap; }
  button.link { border: none; background: none; color: var(--es-accent); padding: 0; cursor: pointer; }
  table { width: 100%; border-collapse: collapse; font-size: var(--es-fs-sm); margin-top: 6px; }
  th, td { padding: 6px 10px; border-bottom: 1px solid var(--es-border); text-align: left; vertical-align: top; }
  th { background: var(--es-surface-2); color: var(--es-muted); font-weight: 600; font-size: var(--es-fs-xs); white-space: nowrap; }
  .right { text-align: right; }
  .mono { font-family: var(--es-mono); }
  .reason { font-size: var(--es-fs-xs); }
  .decider { font-size: var(--es-fs-xs); color: var(--es-muted); }
  .verdict { font-size: var(--es-fs-sm); }
  .ok { color: var(--es-ok, #2e7d32); }
  .bad { color: var(--es-danger, #c62828); }
  .note { font-size: var(--es-fs-xs); color: var(--es-muted); margin: 6px 0 0; }
  .hint { font-size: var(--es-fs-xs); color: var(--es-muted); margin: 6px 0; }
</style>
