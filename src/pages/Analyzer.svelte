<script lang="ts">
  import StateBlock from "../components/StateBlock.svelte";
  import { analyzeText, fetchIndices, fetchFields, type AnalyzeToken, type IndexRow, type FieldInfo } from "../lib/services";
  import type { DbxBridge } from "../lib/bridge";
  import { t } from "../lib/i18n.svelte.js";

  interface Props {
    bridge: DbxBridge;
    connectionId: string;
  }
  let { bridge, connectionId }: Props = $props();

  let index = $state("");
  let text = $state("The quick brown fox jumps over the lazy dog");
  let analyzer = $state("");
  let field = $state("");
  let tokenizer = $state("");
  let filters = $state("");

  let loading = $state(false);
  let error = $state("");
  let tokens = $state<AnalyzeToken[]>([]);

  let indices = $state<IndexRow[]>([]);
  let fields = $state<FieldInfo[]>([]);
  let loadingFields = $state(false);

  // ES 内置分析器/分词器/过滤器候选，省得靠记忆拼名字（仍可自由输入）。
  const ANALYZERS = ["standard", "simple", "whitespace", "stop", "keyword", "pattern", "english", "fingerprint"];
  const TOKENIZERS = ["standard", "letter", "lowercase", "whitespace", "uax_url_email", "pattern", "keyword", "path_hierarchy"];
  const FILTERS = ["lowercase", "uppercase", "asciifolding", "stop", "stemmer", "snowball", "trim", "unique", "word_delimiter", "ngram", "edge_ngram"];

  $effect(() => {
    const id = connectionId;
    if (!id) return;
    void fetchIndices(bridge, id)
      .then((rows) => {
        indices = rows;
      })
      .catch(() => {
        indices = [];
      });
  });

  // Field suggestions must come from the chosen index mapping: a field name from
  // another index silently analyses nothing.
  $effect(() => {
    const ix = index;
    field = "";
    if (!ix) {
      fields = [];
      return;
    }
    loadingFields = true;
    void fetchFields(bridge, connectionId, ix)
      .then((rows) => {
        if (index === ix) fields = rows;
      })
      .catch(() => {
        if (index === ix) fields = [];
      })
      .finally(() => {
        if (index === ix) loadingFields = false;
      });
  });

  async function run(): Promise<void> {
    if (!text.trim()) {
      error = t("analyzer.err.emptyText");
      return;
    }
    loading = true;
    error = "";
    const path = index.trim() ? `/${encodeURIComponent(index.trim())}/_analyze` : "/_analyze";
    try {
      const res = await analyzeText(bridge, connectionId, index, {
        text: text.trim(),
        ...(analyzer.trim() ? { analyzer: analyzer.trim() } : {}),
        ...(field.trim() ? { field: field.trim() } : {}),
        ...(tokenizer.trim() ? { tokenizer: tokenizer.trim() } : {}),
        ...(filters.trim() ? { filter: filters.split(",").map((f) => f.trim()).filter(Boolean) } : {}),
      });
      if (res.detail && typeof res.detail === "object" && "error" in (res.detail as object)) {
        error = String((res.detail as any).error?.message ?? "analyze failed");
        tokens = [];
      } else {
        tokens = res.tokens;
      }
    } catch (cause) {
      error = cause instanceof Error ? cause.message : String(cause);
    } finally {
      loading = false;
    }
  }
</script>

<div class="page">
  <header>
    <div>
      <h1>{t("analyzer.title")}</h1>
      <p>{t("analyzer.hintPrefix")}<code>{index.trim() ? "/{index}" : ""}/_analyze</code>{t("analyzer.hintSuffix")}</p>
    </div>
  </header>

  <section class="form">
    <label class="full">{t("analyzer.text")}
      <textarea bind:value={text} spellcheck="false"></textarea>
    </label>
    <label>{t("analyzer.contextIndex")}
      <select bind:value={index}>
        <option value="">{t("analyzer.noIndex")}</option>
        {#each indices as ix (ix.index)}<option value={ix.index}>{ix.index}</option>{/each}
      </select>
    </label>
    <label>{t("analyzer.field")}
      <select bind:value={field} disabled={!index || loadingFields}>
        <option value="">{loadingFields ? t("analyzer.loadingFields") : t("analyzer.noField", { count: fields.length })}</option>
        {#each fields as f (f.name)}<option value={f.name}>{f.name} · {f.type}</option>{/each}
      </select>
    </label>
    <label>{t("analyzer.analyzerLabel")}<input placeholder="e.g. standard" list="an-analyzers" bind:value={analyzer} /></label>
    <label>{t("analyzer.tokenizerLabel")}<input placeholder="e.g. whitespace" list="an-tokenizers" bind:value={tokenizer} /></label>
    <label>{t("analyzer.filtersLabel")}<input placeholder="e.g. lowercase" list="an-filters" bind:value={filters} /></label>
    <datalist id="an-analyzers">{#each ANALYZERS as a (a)}<option value={a}></option>{/each}</datalist>
    <datalist id="an-tokenizers">{#each TOKENIZERS as t (t)}<option value={t}></option>{/each}</datalist>
    <datalist id="an-filters">{#each FILTERS as f (f)}<option value={f}></option>{/each}</datalist>
    <div class="run"><button type="button" class="primary" onclick={run} disabled={loading || !connectionId}>{loading ? t("analyzer.analyzing") : t("analyzer.run")}</button></div>
  </section>

  {#if error}
    <StateBlock kind="error" message={t("analyzer.failed")} detail={error} />
  {:else if loading}
    <StateBlock kind="loading" message={t("analyzer.running")} detail="POST /_analyze" />
  {:else if tokens.length > 0}
    <p class="meta">{t("analyzer.metaCount", { n: tokens.length })}</p>
    <table class="tok">
      <thead><tr><th>#</th><th>{t("analyzer.col.token")}</th><th>{t("analyzer.col.position")}</th><th>{t("analyzer.col.type")}</th><th>{t("analyzer.col.offsets")}</th></tr></thead>
      <tbody>
        {#each tokens as t, i (i)}
          <tr><td class="mono">{t.position}</td><td class="mono">{t.token}</td><td class="mono">{t.position}</td><td class="mono">{t.type}</td><td class="mono">{t.start}-{t.end}</td></tr>
        {/each}
      </tbody>
    </table>
  {:else}
    <StateBlock kind="empty" message={t("analyzer.empty")} />
  {/if}
</div>

<style>
  .page { max-width: 1000px; }
  header { margin-bottom: 18px; }
  h1 { margin: 0 0 4px; font-size: var(--es-fs-3xl); }
  header p { margin: 0; color: var(--es-muted); }
  header code { font-family: var(--es-mono); font-size: var(--es-fs-xs); }
  button { padding: 6px 14px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); cursor: pointer; }
  button:disabled { opacity: 0.55; cursor: default; }
  .primary { background: var(--es-accent); color: #fff; border-color: var(--es-accent); }
  .form { display: grid; grid-template-columns: repeat(2, 1fr); gap: 12px; margin-bottom: 16px; }
  .form .full { grid-column: 1 / -1; }
  .form label { display: flex; flex-direction: column; gap: 4px; font-size: var(--es-fs-xs); color: var(--es-muted); }
  .form input, .form textarea { font-family: var(--es-mono); font-size: var(--es-fs-sm); }
  .form textarea { min-height: 70px; resize: vertical; }
  .run { grid-column: 1 / -1; display: flex; justify-content: flex-end; }
  .meta { font-size: var(--es-fs-xs); color: var(--es-muted); font-family: var(--es-mono); }
  table.tok { width: 100%; border-collapse: collapse; font-size: var(--es-fs-sm); border: 1px solid var(--es-border); border-radius: var(--es-radius); overflow: hidden; }
  .tok th, .tok td { padding: 6px 8px; border-bottom: 1px solid var(--es-border); white-space: nowrap; }
  .tok th { background: var(--es-surface-2); color: var(--es-muted); text-align: left; font-weight: 600; }
  .tok td.mono { font-family: var(--es-mono); }
</style>
