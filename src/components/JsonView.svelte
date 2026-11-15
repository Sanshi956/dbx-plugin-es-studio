<script lang="ts">
  interface Props {
    value: unknown;
    compact?: boolean;
  }
  let { value, compact = false }: Props = $props();

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

  let html = $derived.by(() => {
    try {
      return highlight(JSON.stringify(value, null, compact ? 0 : 2));
    } catch {
      return escapeHtml(String(value));
    }
  });
</script>

<pre class="json">{@html html}</pre>

<style>
  .json {
    margin: 0;
    padding: 12px 14px;
    background: var(--es-surface-2);
    border: 1px solid var(--es-border);
    border-radius: var(--es-radius);
    font-family: var(--es-mono);
    font-size: 12px;
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
