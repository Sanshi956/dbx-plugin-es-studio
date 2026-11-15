<script lang="ts">
  import StateBlock from "../components/StateBlock.svelte";
  import { analyzeText, fetchIndices, fetchFields, type AnalyzeToken, type IndexRow, type FieldInfo } from "../lib/services";
  import type { DbxBridge } from "../lib/bridge";

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
      error = "请输入待分析文本";
      return;
    }
    loading = true;
    error = "";
    const path = index.trim() ? `/${encodeURIComponent(index.trim())}/_analyze` : "/_analyze";
    try {
      const res = await analyzeText(bridge, connectionId, {
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
      <h1>Analyzer</h1>
      <p>Test how text is tokenized via <code>{index.trim() ? "/{index}" : ""}/_analyze</code>.</p>
    </div>
  </header>

  <section class="form">
    <label class="full">Text
      <textarea bind:value={text} spellcheck="false"></textarea>
    </label>
    <label>Context index（用该索引的 mapping / 默认分析器）
      <select bind:value={index}>
        <option value="">（不指定索引，用全局 _analyze）</option>
        {#each indices as ix (ix.index)}<option value={ix.index}>{ix.index}</option>{/each}
      </select>
    </label>
    <label>Field（按字段的 analyzer 分析）
      <select bind:value={field} disabled={!index || loadingFields}>
        <option value="">{loadingFields ? "loading fields…" : `（不指定字段，共 ${fields.length} 个可选）`}</option>
        {#each fields as f (f.name)}<option value={f.name}>{f.name} · {f.type}</option>{/each}
      </select>
    </label>
    <label>Analyzer<input placeholder="e.g. standard" list="an-analyzers" bind:value={analyzer} /></label>
    <label>Tokenizer<input placeholder="e.g. whitespace" list="an-tokenizers" bind:value={tokenizer} /></label>
    <label>Filters (comma)<input placeholder="e.g. lowercase" list="an-filters" bind:value={filters} /></label>
    <datalist id="an-analyzers">{#each ANALYZERS as a (a)}<option value={a}></option>{/each}</datalist>
    <datalist id="an-tokenizers">{#each TOKENIZERS as t (t)}<option value={t}></option>{/each}</datalist>
    <datalist id="an-filters">{#each FILTERS as f (f)}<option value={f}></option>{/each}</datalist>
    <div class="run"><button type="button" class="primary" onclick={run} disabled={loading || !connectionId}>{loading ? "Analyzing…" : "Analyze"}</button></div>
  </section>

  {#if error}
    <StateBlock kind="error" message="Analyze failed" detail={error} />
  {:else if loading}
    <StateBlock kind="loading" message="Analyzing text…" detail="POST /_analyze" />
  {:else if tokens.length > 0}
    <p class="meta">{tokens.length} tokens</p>
    <table class="tok">
      <thead><tr><th>#</th><th>Token</th><th>Position</th><th>Type</th><th>Offsets</th></tr></thead>
      <tbody>
        {#each tokens as t, i (i)}
          <tr><td class="mono">{t.position}</td><td class="mono">{t.token}</td><td class="mono">{t.position}</td><td class="mono">{t.type}</td><td class="mono">{t.start}-{t.end}</td></tr>
        {/each}
      </tbody>
    </table>
  {:else}
    <StateBlock kind="empty" message="Run an analysis to see tokens" />
  {/if}
</div>

<style>
  .page { max-width: 1000px; }
  header { margin-bottom: 18px; }
  h1 { margin: 0 0 4px; font-size: 18px; }
  header p { margin: 0; color: var(--es-muted); }
  header code { font-family: var(--es-mono); font-size: 11px; }
  button { padding: 6px 14px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); cursor: pointer; }
  button:disabled { opacity: 0.55; cursor: default; }
  .primary { background: var(--es-accent); color: #fff; border-color: var(--es-accent); }
  .form { display: grid; grid-template-columns: repeat(2, 1fr); gap: 12px; margin-bottom: 16px; }
  .form .full { grid-column: 1 / -1; }
  .form label { display: flex; flex-direction: column; gap: 4px; font-size: 11px; color: var(--es-muted); }
  .form select { padding: 6px 10px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); font-family: var(--es-mono); font-size: 12px; }
  .form input, .form textarea { padding: 6px 10px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); font-family: var(--es-mono); font-size: 12px; }
  .form textarea { min-height: 70px; resize: vertical; }
  .run { grid-column: 1 / -1; display: flex; justify-content: flex-end; }
  .meta { font-size: 11px; color: var(--es-muted); font-family: var(--es-mono); }
  table.tok { width: 100%; border-collapse: collapse; font-size: 12px; border: 1px solid var(--es-border); border-radius: var(--es-radius); overflow: hidden; }
  .tok th, .tok td { padding: 6px 8px; border-bottom: 1px solid var(--es-border); white-space: nowrap; }
  .tok th { background: var(--es-surface-2); color: var(--es-muted); text-align: left; font-weight: 600; }
  .tok td.mono { font-family: var(--es-mono); }
</style>
