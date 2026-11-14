<script lang="ts">
  import type { ConnectionSummary } from "../lib/bridge";

  interface Props {
    connections: ConnectionSummary[];
    activeId: string;
    busy: boolean;
    onchange: (id: string) => void;
  }
  let { connections, activeId, busy, onchange }: Props = $props();
</script>

<label class="picker">
  <span class="caption">Cluster</span>
  {#if connections.length > 0}
    <select
      value={activeId}
      disabled={busy}
      onchange={(event) => onchange(event.currentTarget.value)}
      aria-label="Current Elasticsearch cluster"
    >
      {#each connections as connection (connection.id)}
        <option value={connection.id}>
          {connection.name}{connection.readOnly ? " · read only" : ""}
        </option>
      {/each}
    </select>
  {:else}
    <span class="none">No connection available</span>
  {/if}
</label>

<style>
  .picker { display: flex; align-items: center; gap: 8px; }
  .caption { color: var(--es-muted); font-size: 11px; text-transform: uppercase; letter-spacing: 0.06em; }
  select {
    min-width: 180px;
    padding: 5px 8px;
    border: 1px solid var(--es-border);
    border-radius: 6px;
    background: var(--es-surface);
    color: var(--es-text);
  }
  select:disabled { opacity: 0.6; }
  .none {
    padding: 5px 8px;
    border: 1px dashed var(--es-border);
    border-radius: 6px;
    color: var(--es-muted);
  }
</style>
