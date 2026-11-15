<script lang="ts">
  import { t } from "../lib/i18n.svelte.js";

  interface Props {
    health: string;
  }
  let { health }: Props = $props();
  let cls = $derived.by(() => (["green", "yellow", "red"].includes(health) ? health : "unknown"));
</script>

<span class="badge {cls}">{health || t("common.unknown")}</span>

<style>
  .badge {
    display: inline-block;
    padding: 2px 9px;
    border-radius: 999px;
    font-size: var(--es-fs-xs);
    font-weight: 700;
    text-transform: uppercase;
    border: 1px solid var(--es-border);
    color: var(--es-muted);
  }
  .green { color: var(--es-green); border-color: var(--es-green); }
  .yellow { color: var(--es-yellow); border-color: var(--es-yellow); }
  .red { color: var(--es-red); border-color: var(--es-red); }
</style>
