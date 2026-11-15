<script lang="ts">
  import StateBlock from "../components/StateBlock.svelte";
  import { analyzeText, type AnalyzeToken } from "../lib/services";
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
    <label>Context index<input placeholder="optional" bind:value={index} /></label>
    <label>Analyzer<input placeholder="e.g. standard" bind:value={analyzer} /></label>
    <label>Field<input placeholder="uses index mapping" bind:value={field} /></label>
    <label>Tokenizer<input placeholder="e.g. whitespace" bind:value={tokenizer} /></label>
    <label>Filters (comma)<input placeholder="e.g. lowercase" bind:value={filters} /></label>
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
  .form input, .form textarea { padding: 6px 10px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); font-family: var(--es-mono); font-size: 12px; }
  .form textarea { min-height: 70px; resize: vertical; }
  .run { grid-column: 1 / -1; display: flex; justify-content: flex-end; }
  .meta { font-size: 11px; color: var(--es-muted); font-family: var(--es-mono); }
  table.tok { width: 100%; border-collapse: collapse; font-size: 12px; border: 1px solid var(--es-border); border-radius: var(--es-radius); overflow: hidden; }
  .tok th, .tok td { padding: 6px 8px; border-bottom: 1px solid var(--es-border); white-space: nowrap; }
  .tok th { background: var(--es-surface-2); color: var(--es-muted); text-align: left; font-weight: 600; }
  .tok td.mono { font-family: var(--es-mono); }
</style>
