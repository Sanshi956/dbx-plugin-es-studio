<script lang="ts">
  import StateBlock from "../components/StateBlock.svelte";
  import JsonView from "../components/JsonView.svelte";
  import { handleEditorKey } from "../lib/editorKeys";
  import { fetchIndices, runProfile, type IndexRow, type ProfileShard, type ProfileNode } from "../lib/services";
  import type { DbxBridge } from "../lib/bridge";
  import { t } from "../lib/i18n.svelte.js";

  interface Props {
    bridge: DbxBridge;
    connectionId: string;
  }
  let { bridge, connectionId }: Props = $props();

  let indices = $state<IndexRow[]>([]);
  let selectedIndex = $state("");
  let loadingIndices = $state(true);
  let body = $state(`{
  "size": 10,
  "query": { "match_all": {} }
}`);
  let running = $state(false);
  let error = $state("");
  let formError = $state("");

  let took = $state(0);
  let hits = $state(0);
  let shards = $state<ProfileShard[]>([]);
  let raw = $state<unknown>(null);
  let showRaw = $state(false);

  async function loadIndices(id: string): Promise<void> {
    if (!id) return;
    loadingIndices = true;
    try {
      indices = await fetchIndices(bridge, id);
      if (!selectedIndex && indices.length) selectedIndex = indices[0].index;
    } catch {
      indices = [];
    } finally {
      loadingIndices = false;
    }
  }
  $effect(() => {
    void loadIndices(connectionId);
  });

  async function run(): Promise<void> {
    formError = "";
    let parsed: Record<string, unknown>;
    try {
      const value = JSON.parse(body);
      if (value === null || typeof value !== "object" || Array.isArray(value)) {
        formError = t("prof.err.notObject");
        return;
      }
      parsed = value as Record<string, unknown>;
    } catch (cause) {
      formError = t("prof.err.parse", { detail: cause instanceof Error ? cause.message : String(cause) });
      return;
    }
    running = true;
    error = "";
    try {
      const res = await runProfile(bridge, connectionId, selectedIndex, parsed);
      if (!res.ok) {
        error = res.error ?? "profile failed";
        shards = [];
        raw = res.raw;
        return;
      }
      took = res.took;
      hits = res.hits;
      shards = res.shards;
      raw = res.raw;
    } catch (cause) {
      error = cause instanceof Error ? cause.message : String(cause);
    } finally {
      running = false;
    }
  }

  function ms(nanos: number): string {
    return (nanos / 1_000_000).toFixed(3);
  }

  function treeNodes(rows: ProfileShard[]): { shard: string; node: ProfileNode }[] {
    const out: { shard: string; node: ProfileNode }[] = [];
    for (const shard of rows) for (const node of shard.nodes) out.push({ shard: shard.shard, node });
    return out;
  }
</script>

<div class="page">
  <header>
    <div>
      <h1>{t("prof.title")}</h1>
      <p>{t("prof.hintPrefix")}<code>"profile": true</code>{t("prof.hintMid")}</p>
    </div>
  </header>

  <section class="bar">
    <label>{t("index.col.index")}
      <select bind:value={selectedIndex} disabled={loadingIndices}>
        {#if loadingIndices}<option>{t("common.loading")}</option>{/if}
        {#each indices as ix (ix.index)}<option value={ix.index}>{ix.index}</option>{/each}
      </select>
    </label>
    <button type="button" class="primary" onclick={run} disabled={!selectedIndex || running}>
      {running ? t("prof.running") : t("prof.run")}
    </button>
  </section>

  <textarea
    spellcheck="false"
    rows="8"
    bind:value={body}
    onkeydown={(event) => handleEditorKey(event, () => body, (next) => (body = next), () => void run())}
  ></textarea>
  {#if formError}<p class="form-error">{formError}</p>{/if}

  {#if error}
    <StateBlock kind="error" message={t("prof.failed")} detail={error} />
  {:else if running}
    <StateBlock kind="loading" message={t("prof.runningQuery")} detail={`POST /${selectedIndex}/_search with profile=true`} />
  {:else if shards.length > 0}
    <p class="meta">{shards.length} shards · took {took} ms · {hits} hits</p>

    <table class="shards">
      <thead><tr><th>{t("prof.col.shard")}</th><th>{t("prof.col.index")}</th><th>{t("prof.col.node")}</th><th>{t("prof.col.query")}</th><th>{t("prof.col.rewrite")}</th><th>{t("prof.col.collector")}</th><th>{t("prof.col.nodes")}</th></tr></thead>
      <tbody>
        {#each shards as shard, i (`${shard.index}-${shard.shard}-${i}`)}
          <tr>
            <td class="mono">{shard.shard}</td>
            <td class="mono">{shard.index || "-"}</td>
            <td class="mono small">{shard.node.slice(0, 10)}…</td>
            <td class="mono">{ms(shard.queryNanos)}</td>
            <td class="mono">{ms(shard.rewriteNanos)}</td>
            <td class="mono">{ms(shard.collectorNanos)}</td>
            <td class="mono">{shard.nodes.length}</td>
          </tr>
        {/each}
      </tbody>
    </table>

    {#if treeNodes(shards).length > 0}
      <h2 class="sub">{t("prof.tree")}</h2>
      <table class="tree">
        <thead><tr><th>{t("prof.col.type")}</th><th>{t("prof.col.time")}</th></tr></thead>
        <tbody>
          {#each treeNodes(shards) as row, i (`${row.shard}-${i}`)}
            <tr>
              <td>
                <span class="indent" style={`padding-left:${row.node.depth * 16}px`}>
                  <span class="type">{row.node.type}</span>
                  {#if row.node.description}<span class="desc">{row.node.description}</span>{/if}
                </span>
              </td>
              <td class="mono">{ms(row.node.timeNanos)}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}

    <button type="button" class="link" onclick={() => (showRaw = !showRaw)}>{showRaw ? t("prof.hideRaw") : t("prof.showRaw")}</button>
    {#if showRaw}<JsonView value={raw} />{/if}
  {:else}
    <StateBlock kind="empty" message={t("prof.empty")} detail={t("prof.emptyDetail")} />
  {/if}
</div>

<style>
  .page { max-width: 1080px; }
  header { margin-bottom: 16px; }
  h1 { margin: 0 0 4px; font-size: var(--es-fs-3xl); }
  header p { margin: 0; color: var(--es-muted); }
  header code { font-family: var(--es-mono); font-size: var(--es-fs-xs); }
  button { padding: 6px 14px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); cursor: pointer; white-space: nowrap; }
  button:hover:not(:disabled):not(.primary) { background: var(--es-surface-2); }
  .primary { background: var(--es-accent); color: #fff; border-color: var(--es-accent); }
  .primary:hover:not(:disabled) { filter: brightness(0.92); }
  button:disabled { opacity: 0.55; cursor: default; }
  .bar { display: flex; gap: 12px; align-items: end; margin-bottom: 10px; flex-wrap: wrap; }
  .bar label { display: flex; flex-direction: column; gap: 4px; font-size: var(--es-fs-xs); color: var(--es-muted); }
  .bar select { padding: 6px 10px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); }
  textarea { width: 100%; box-sizing: border-box; font-family: var(--es-mono); font-size: var(--es-fs-sm); padding: 10px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface-2); color: var(--es-text); margin-bottom: 10px; }
  .form-error { margin: 0 0 10px; font-size: var(--es-fs-xs); color: var(--es-red); }
  .meta { font-size: var(--es-fs-xs); color: var(--es-muted); font-family: var(--es-mono); margin: 0 0 10px; }
  table { width: 100%; border-collapse: collapse; font-size: var(--es-fs-sm); border: 1px solid var(--es-border); border-radius: var(--es-radius); overflow: hidden; margin-bottom: 14px; }
  th, td { padding: 6px 8px; border-bottom: 1px solid var(--es-border); text-align: left; }
  th { background: var(--es-surface-2); color: var(--es-muted); font-size: var(--es-fs-xs); font-weight: 600; }
  .mono { font-family: var(--es-mono); }
  .small { font-size: var(--es-fs-xs); }
  .sub { font-size: var(--es-fs-sm); text-transform: uppercase; letter-spacing: 0.08em; color: var(--es-muted); margin: 4px 0 8px; }
  .indent { display: inline-block; }
  .type { font-family: var(--es-mono); font-size: var(--es-fs-xs); }
  .desc { margin-left: 8px; color: var(--es-muted); font-size: var(--es-fs-xs); }
  .link { background: none; border: none; color: var(--es-accent); cursor: pointer; padding: 6px 0; }
</style>
