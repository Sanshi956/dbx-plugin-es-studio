<script lang="ts">
  import type { Snippet } from "svelte";
  import { t } from "../lib/i18n.svelte.js";

  interface Props {
    open: boolean;
    title: string;
    subtitle?: string;
    onClose: () => void;
    children: Snippet;
  }
  let { open, title, subtitle = "", onClose, children }: Props = $props();
</script>

<svelte:window
  onkeydown={(event) => {
    if (open && event.key === "Escape") onClose();
  }}
/>

{#if open}
  <div class="overlay">
    <div class="panel" role="dialog" aria-modal="true" aria-label={title}>
      <header>
        <div class="titles">
          <h3>{title}</h3>
          {#if subtitle}<span class="sub">{subtitle}</span>{/if}
        </div>
        <button type="button" class="close" aria-label={t("common.close")} onclick={onClose}>✕ Esc</button>
      </header>
      <div class="body">{@render children()}</div>
    </div>
  </div>
{/if}

<style>
  .overlay {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.45);
    display: grid;
    place-items: center;
    z-index: 60;
    padding: 24px;
  }
  .panel {
    width: min(900px, 94vw);
    max-height: 82vh;
    display: flex;
    flex-direction: column;
    background: var(--es-surface);
    border: 1px solid var(--es-border);
    border-radius: var(--es-radius);
    box-shadow: 0 12px 40px rgba(0, 0, 0, 0.25);
    overflow: hidden;
  }
  header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 12px 14px;
    border-bottom: 1px solid var(--es-border);
    background: var(--es-surface-2);
  }
  .titles { display: flex; align-items: baseline; gap: 8px; min-width: 0; }
  h3 { margin: 0; font-size: var(--es-fs-md); font-family: var(--es-mono); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .sub { font-size: var(--es-fs-xs); color: var(--es-muted); font-family: var(--es-mono); }
  .close {
    flex: 0 0 auto;
    padding: 3px 8px;
    font-size: var(--es-fs-xs);
    font-family: var(--es-mono);
    border: 1px solid var(--es-border);
    border-radius: 6px;
    background: var(--es-surface);
    color: var(--es-muted);
    cursor: pointer;
  }
  .close:hover { background: var(--es-surface-2); color: var(--es-text); }
  .body { padding: 14px; overflow: auto; }
</style>
