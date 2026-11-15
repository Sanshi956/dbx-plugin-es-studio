<script lang="ts">
  import type { NavGroup } from "../lib/stores";
  import { t } from "../lib/i18n.svelte.js";

  interface Props {
    groups: NavGroup[];
    active: string;
    onselect: (id: string) => void;
  }
  let { groups, active, onselect }: Props = $props();
</script>

<nav aria-label={t("nav.aria")}>
  {#each groups as group (group.id)}
    <section>
      <h2>{t(group.label)}</h2>
      {#each group.items as item (item.id)}
        <button
          type="button"
          class:active={active === item.id}
          aria-current={active === item.id ? "page" : undefined}
          onclick={() => onselect(item.id)}
        >
          <span>{t(item.label)}</span>
        </button>
      {/each}
    </section>
  {/each}
</nav>

<style>
  nav { padding: 10px 8px 24px; }
  section { margin-bottom: 10px; }
  h2 {
    margin: 12px 10px 4px;
    color: var(--es-muted);
    font-size: var(--es-fs-2xs);
    font-weight: 700;
    letter-spacing: 0.09em;
    text-transform: uppercase;
  }
  button {
    display: flex;
    justify-content: space-between;
    align-items: center;
    width: 100%;
    padding: 6px 10px;
    border: 0;
    border-radius: 6px;
    background: transparent;
    color: var(--es-text);
    text-align: left;
    line-height: 1.5;
  }
  button { cursor: pointer; }
  button:hover { background: var(--es-surface-2); }
  button.active {
    background: var(--es-accent-soft);
    color: var(--es-accent);
    font-weight: 600;
  }
</style>
