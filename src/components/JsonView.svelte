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
  // A single pass is required — chaining regexes let the number rule fire on
  // digits inside string values, turning "2026-04-01T08:00:00Z" into
  // "2026-04-01T08: 00: 00Z".
  function highlight(input: string): string {
    let out = "";
    for (let i = 0; i < input.length; ) {
      const ch = input[i];
      if (ch === '"') {
        let j = i + 1;
        while (j < input.length) {
          if (input[j] === "\\") { j += 2; continue; }
          if (input[j] === '"') { j++; break; }
          j++;
        }
        let k = j;
        while (k < input.length && (input[k] === " " || input[k] === "\t")) k++;
        const kind = input[k] === ":" ? "k" : "s";
        out += `<span class="${kind}">${escapeHtml(input.slice(i, j))}</span>`;
        i = j;
        continue;
      }
      if (ch === "-" || (ch >= "0" && ch <= "9")) {
        let j = i + 1;
        while (j < input.length && /[0-9.eE+\-]/.test(input[j])) j++;
        const num = input.slice(i, j);
        if (/^-?\d/.test(num)) {
          out += `<span class="n">${num}</span>`;
          i = j;
          continue;
        }
      }
      const lit = ["true", "false", "null"].find((w) => input.startsWith(w, i));
      if (lit) {
        out += `<span class="b">${lit}</span>`;
        i += lit.length;
        continue;
      }
      out += escapeHtml(ch);
      i++;
    }
    return out;
  }

  // Some endpoints return a bare string body (a plain-text search response,
  // an error page, a compatibility notice). Try to parse it as JSON first so a
  // JSON-looking body still gets pretty-printed, otherwise render the text
  // verbatim — wrapping a whitespace-sensitive body would mangle it.
  let rendered = $derived.by(() => {
    if (typeof value === "string") {
      try {
        return { text: JSON.stringify(JSON.parse(value), null, compact ? 0 : 2), plain: false };
      } catch {
        return { text: value, plain: true };
      }
    }
    try {
      return { text: JSON.stringify(value, null, compact ? 0 : 2), plain: false };
    } catch {
      return { text: String(value), plain: true };
    }
  });
  let html = $derived(highlight(rendered.text));
  let copied = $state(false);

  async function copy(): Promise<void> {
    try {
      await navigator.clipboard.writeText(rendered.text);
      copied = true;
      window.setTimeout(() => (copied = false), 1500);
    } catch {
      copied = false;
    }
  }
</script>

<div class="json-wrap">
  {#if copyable}
    <!-- Kept in normal flow: an absolutely positioned button sits on top of
         the pre's scrollbar and long plain-text bodies. -->
    <div class="json-bar">
      <button type="button" class="copy" onclick={copy} title={t("common.copyJson")}>
        {copied ? t("common.copied") : t("common.copyJson")}
      </button>
    </div>
  {/if}
  <pre class="json" class:plain={rendered.plain}>{#if rendered.plain}{rendered.text}{:else}{@html html}{/if}</pre>
</div>

<style>
  .json-wrap { position: relative; }
  .json-bar { display: flex; justify-content: flex-end; margin-bottom: 4px; }
  .copy {
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
  /* Plain text keeps its own line breaks and scrolls sideways instead of
     wrapping every long line into an unreadable block. */
  .json.plain {
    white-space: pre;
    word-break: normal;
    max-height: 480px;
    overflow: auto;
  }
  :global(.json .k) { color: var(--es-accent); }
  :global(.json .s) { color: var(--es-green); }
  :global(.json .n) { color: var(--es-yellow); }
  :global(.json .b) { color: var(--es-red); }
</style>
