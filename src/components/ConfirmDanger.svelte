<script lang="ts">
  import { t } from "../lib/i18n.svelte.js";

  interface Props {
    open: boolean;
    title: string;
    message: string;
    scope: string;
    /** Empty means "use the translated default"; resolving inside the template
     *  keeps the label reactive when the host language changes. */
    confirmLabel?: string;
    onConfirm: () => void;
    onCancel: () => void;
  }
  let { open, title, message, scope, confirmLabel = "", onConfirm, onCancel }: Props = $props();

  let acknowledged = $state(false);

  function cancel(): void {
    acknowledged = false;
    onCancel();
  }
  function confirm(): void {
    if (!acknowledged) return;
    acknowledged = false;
    onConfirm();
  }
</script>

{#if open}
  <div class="overlay" role="alertdialog" aria-modal="true" aria-label={title}>
    <div class="modal">
      <h3 class="danger-title">{title}</h3>
      <p class="msg">{message}</p>
      <div class="scope">
        <span class="scope-label">{t("common.scope")}</span>
        <code>{scope}</code>
      </div>
      <label class="ack">
        <input type="checkbox" bind:checked={acknowledged} />
        {t("common.dangerAck")}
      </label>
      <div class="actions">
        <button type="button" class="ghost" onclick={cancel}>{t("common.cancel")}</button>
        <button type="button" class="danger" disabled={!acknowledged} onclick={confirm}>
          {confirmLabel || t("common.confirm")}
        </button>
      </div>
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
    z-index: 50;
  }
  .modal {
    width: min(440px, 92vw);
    background: var(--es-surface);
    border: 1px solid var(--es-border);
    border-radius: var(--es-radius);
    padding: 20px;
    box-shadow: 0 12px 40px rgba(0, 0, 0, 0.25);
  }
  .danger-title { margin: 0 0 8px; font-size: var(--es-fs-xl); color: var(--es-red); }
  .msg { margin: 0 0 12px; line-height: 1.6; }
  .scope { margin-bottom: 12px; padding: 10px 12px; border: 1px dashed var(--es-red); border-radius: 6px; background: var(--es-surface-2); }
  .scope-label { display: block; font-size: var(--es-fs-2xs); color: var(--es-muted); text-transform: uppercase; letter-spacing: 0.08em; margin-bottom: 4px; }
  code { font-family: var(--es-mono); font-size: var(--es-fs-sm); word-break: break-all; }
  .ack { display: flex; align-items: center; gap: 8px; margin-bottom: 16px; font-size: var(--es-fs-sm); color: var(--es-muted); }
  .actions { display: flex; justify-content: flex-end; gap: 10px; }
  button { padding: 7px 16px; border-radius: 6px; cursor: pointer; border: 1px solid var(--es-border); }
  .ghost { background: var(--es-surface); color: var(--es-text); }
  .ghost:hover { background: var(--es-surface-2); }
  .danger { background: var(--es-red); color: #fff; border-color: var(--es-red); }
  .danger:disabled { opacity: 0.5; cursor: not-allowed; }
</style>
