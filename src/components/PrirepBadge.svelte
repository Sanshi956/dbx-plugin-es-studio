<script lang="ts">
  interface Props {
    value: "p" | "r" | string;
  }
  let { value }: Props = $props();
  let isPrimary = $derived.by(() => value === "p");
</script>

<span class="badge {isPrimary ? 'p' : 'r'}">{isPrimary ? "primary" : "replica"}</span>

<style>
  .badge {
    display: inline-block;
    padding: 1px 8px;
    border-radius: 4px;
    font-size: 10px;
    font-weight: 700;
    text-transform: uppercase;
  }
  .p { color: var(--es-accent); background: var(--es-accent-soft); }
  .r { color: var(--es-muted); background: var(--es-surface-2); }
</style>
