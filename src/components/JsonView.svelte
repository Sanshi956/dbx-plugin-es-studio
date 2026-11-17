<script lang="ts">
  import { t } from "../lib/i18n.svelte.js";

  interface Props {
    value: unknown;
    compact?: boolean;
    /** Show a "copy JSON" button. Defaults on; pass false to suppress. */
    copyable?: boolean;
  }
  let { value, compact = false, copyable = true }: Props = $props();

  function escapeHtml(s: string): string {
    return s
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;");
  }

  // Light, safe syntax highlighting: escape first, then wrap tokens with spans.
  function highlight(input: string): string {
    const escaped = escapeHtml(input);
    return escaped
      .replace(/(&quot;[^&]*?&quot;)(\s*:)/g, '<span class="k">$1</span>$2')
      .replace(/:\s*(&quot;[^&]*?&quot;)/g, ': <span class="s">$1</span>')
      .replace(/:\s*(-?\d+\.?\d*([eE][+-]?\d+)?)/g, ': <span class="n">$1</span>')
      .replace(/\b(true|false|null)\b/g, '<span class="b">$1</span>');
  }

  let raw = $derived.by(() => {
    try {
      return JSON.stringify(value, null, compact ? 0 : 2);
    } catch {
      return String(value);
    }
  });
  let html = $derived(highlight(raw));
  let copied = $state(false);

  async function copy(): Promise<void> {
    try {
      await navigator.clipboard.writeText(raw);
      copied = true;
      window.setTimeout(() => (copied = false), 1500);
    } catch {
      copied = false;
    }
  }
</script>

<div class="json-wrap">
  {#if copyable}
    <button type="button" class="copy" onclick={copy} title={t("common.copyJson")}>
      {copied ? t("common.copied") : t("common.copyJson")}
    </button>
  {/if}
  <pre class="json">{@html html}</pre>
</div>

<style>
  .json-wrap { position: relative; }
  .copy {
    position: absolute;
    top: 6px;
    right: 6px;
    z-index: 1;
    padding: 2px 8px;
    font-size: var(--es-fs-2xs);
    border: 1px solid var(--es-border);
    border-radius: 4px;
    background: var(--es-surface);
    color: var(--es-muted);
    cursor: pointer;
  }
  .copy:hover { color: var(--es-text); background: var(--es-surface-2); }
  .json {
    margin: 0;
    padding: 12px 14px;
    background: var(--es-surface-2);
    border: 1px solid var(--es-border);
    border-radius: var(--es-radius);
    font-family: var(--es-mono);
    font-size: var(--es-fs-sm);
    line-height: 1.6;
    white-space: pre-wrap;
    word-break: break-word;
    overflow-x: auto;
  }
  :global(.json .k) { color: var(--es-accent); }
  :global(.json .s) { color: var(--es-green); }
  :global(.json .n) { color: var(--es-yellow); }
  :global(.json .b) { color: var(--es-red); }
</style>
