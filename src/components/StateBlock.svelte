<script lang="ts">
  interface Props {
    kind: "loading" | "error" | "empty";
    message: string;
    detail?: string;
  }
  let { kind, message, detail = "" }: Props = $props();
</script>

<div class="state {kind}" role="status" aria-live="polite">
  <span class="marker" aria-hidden="true"></span>
  <div class="text">
    <strong>{message}</strong>
    {#if detail}<p>{detail}</p>{/if}
  </div>
</div>

<style>
  .state {
    display: flex;
    gap: 12px;
    align-items: flex-start;
    padding: 18px;
    border: 1px solid var(--es-border);
    border-radius: var(--es-radius);
    background: var(--es-surface);
  }
  .marker {
    width: 10px;
    height: 10px;
    margin-top: 4px;
    border-radius: 50%;
    background: var(--es-muted);
    flex: none;
  }
  .loading .marker { background: var(--es-accent); }
  .error .marker { background: var(--es-red); }
  .empty .marker { background: var(--es-yellow); }
  .text strong { font-size: 14px; }
  .text p {
    margin: 6px 0 0;
    color: var(--es-muted);
    font-family: var(--es-mono);
    font-size: 12px;
    line-height: 1.6;
    word-break: break-word;
  }
</style>
