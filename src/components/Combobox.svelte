<script lang="ts">
  /**
   * Lightweight dropdown used instead of a native <select>.
   *
   * Native select popups are painted by the OS/WebView and ignore the page's
   * theme tokens, so a light page on a dark host showed a black popup. This
   * component renders its own popup from --es-* tokens, so it follows the
   * host theme, and keeps the visual language deliberately plain.
   *
   * `searchable` decides how the control behaves:
   *  - true  (indices, fields): the input filters the list as you type.
   *  - false (fixed enums):     the input is read-only; click to open.
   */
  interface ComboboxOption {
    value: string;
    label?: string;
  }
  interface Props {
    value: string;
    options: ComboboxOption[];
    placeholder?: string;
    /** Hook for tests / automation, rendered as data-list on the input. */
    listId?: string;
    /** Extra class on the input (e.g. key-input for existing selectors). */
    class?: string;
    disabled?: boolean;
    searchable?: boolean;
    /** Tooltip forwarded to the input (used by compact inline pickers). */
    title?: string;
    /** Fires on every value change (typing or picking) for controlled parents. */
    onValueChange?: (value: string) => void;
  }
  let {
    value = $bindable(""),
    options = [],
    placeholder = "",
    listId,
    class: className = "",
    disabled = false,
    searchable = true,
    title,
    onValueChange,
  }: Props = $props();

  // Stable per-instance id so the combobox can point at its popup for a11y.
  const popId = `combo-pop-${Math.random().toString(36).slice(2, 9)}`;

  let open = $state(false);
  let activeIdx = $state(-1);
  // null means "not typing": show the committed value and the full list.
  // Keeping this separate from `value` is what stops a filled control from
  // reopening with only its current value listed.
  let query = $state<string | null>(null);
  let wrapEl: HTMLDivElement;
  let inputEl: HTMLInputElement;
  let popEl = $state<HTMLDivElement>();
  // The popup is position:fixed (see the style block): parent cards with
  // overflow clipping (e.g. the MCP panels) would otherwise cut it off.
  let popStyle = $state("");

  const shown = $derived(query ?? value);
  const visible = $derived.by(() => {
    if (query === null) return options;
    const q = query.trim().toLowerCase();
    if (!q) return options;
    return options.filter(
      (o) =>
        o.value.toLowerCase().includes(q) ||
        (o.label ?? "").toLowerCase().includes(q),
    );
  });

  function setValue(next: string) {
    value = next;
    onValueChange?.(next);
  }
  function place() {
    if (!inputEl) return;
    const r = inputEl.getBoundingClientRect();
    const below = r.bottom + 2;
    // Flip above when the list would fall out of the iframe viewport.
    if (below + 244 > window.innerHeight && r.top > 244) {
      popStyle = `left:${r.left}px; bottom:${window.innerHeight - r.top + 2}px; width:${r.width}px;`;
    } else {
      popStyle = `left:${r.left}px; top:${below}px; width:${r.width}px;`;
    }
  }
  function showAll() {
    if (disabled) return;
    if (!open) query = null;
    place();
    open = true;
    activeIdx = -1;
  }
  function hide() {
    open = false;
    activeIdx = -1;
    query = null;
  }
  function pick(o: ComboboxOption) {
    setValue(o.value);
    hide();
    inputEl?.focus();
  }
  function scrollActive() {
    requestAnimationFrame(() => {
      popEl?.querySelector<HTMLElement>("button.active")?.scrollIntoView({ block: "nearest" });
    });
  }
  function onKeydown(e: KeyboardEvent) {
    if (e.key === "ArrowDown") {
      e.preventDefault();
      if (!open) {
        showAll();
        return;
      }
      activeIdx = visible.length ? Math.min(activeIdx + 1, visible.length - 1) : -1;
      scrollActive();
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      if (!open) {
        showAll();
        return;
      }
      activeIdx = visible.length ? Math.max(activeIdx - 1, 0) : -1;
      scrollActive();
    } else if (e.key === "Enter") {
      if (open && activeIdx >= 0 && visible[activeIdx]) {
        e.preventDefault();
        pick(visible[activeIdx]);
      } else if (open) {
        hide();
      }
    } else if (e.key === "Escape") {
      if (open) {
        e.preventDefault();
        hide();
      }
    } else if (e.key === "Tab") {
      // Don't interrupt focus movement, just drop the overlay.
      if (open) hide();
    }
  }

  // Close on outside pointer-down. Kept separate from blur so dragging the
  // popup scrollbar does not dismiss the list.
  $effect(() => {
    if (!open) return;
    const onDoc = (e: PointerEvent) => {
      if (wrapEl && !wrapEl.contains(e.target as Node)) hide();
    };
    document.addEventListener("pointerdown", onDoc);
    // Fixed positioning is viewport-relative: follow the control when any
    // ancestor scrolls (capture catches the workbench's inner scrollers).
    window.addEventListener("scroll", place, true);
    window.addEventListener("resize", place);
    return () => {
      document.removeEventListener("pointerdown", onDoc);
      window.removeEventListener("scroll", place, true);
      window.removeEventListener("resize", place);
    };
  });
</script>

<div class="combo" class:is-disabled={disabled} bind:this={wrapEl}>
  <input
    bind:this={inputEl}
    class={className}
    value={shown}
    {placeholder}
    {disabled}
    readonly={!searchable}
    {title}
    data-list={listId}
    data-options-count={options.length}
    role="combobox"
    aria-controls={popId}
    aria-expanded={open}
    aria-autocomplete="list"
    autocomplete="off"
    onclick={() => showAll()}
    oninput={(e) => {
      if (!searchable) return;
      const next = e.currentTarget.value;
      query = next;
      setValue(next);
      if (e.isTrusted) {
        open = true;
        activeIdx = -1;
      }
    }}
    onkeydown={onKeydown}
  />
  <button
    type="button"
    class="caret"
    tabindex="-1"
    aria-hidden="true"
    {disabled}
    onmousedown={(e) => {
      e.preventDefault();
      if (open) hide();
      else showAll();
    }}
  >
    <svg viewBox="0 0 10 6" aria-hidden="true">
      <path d="M1 1l4 4 4-4" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round" />
    </svg>
  </button>
  {#if open}
    <div class="combo-pop" id={popId} role="listbox" bind:this={popEl} style={popStyle}>
      {#each visible as o, i (o.value)}
        <button
          type="button"
          role="option"
          aria-selected={o.value === value}
          class:active={i === activeIdx}
          class:selected={o.value === value}
          onmousedown={(e) => {
            e.preventDefault();
            pick(o);
          }}
        >{o.label ?? o.value}</button>
      {/each}
      {#if visible.length === 0}
        <div class="combo-none">—</div>
      {/if}
    </div>
  {/if}
</div>

<style>
  .combo {
    position: relative;
    display: inline-block;
  }
  /* The input must fill the wrapper: with only min-width set by a page, the
     intrinsic input width can be narrower, leaving the caret floating outside
     the field (QueryProfiler). */
  .combo > input {
    width: 100%;
    padding-right: 26px;
  }
  .combo > input:read-only {
    cursor: pointer;
  }
  .combo.is-disabled > input {
    opacity: 0.6;
    cursor: not-allowed;
  }

  /* Small chevron, muted until hovered. No icon buttons, no accent fill. */
  .caret {
    position: absolute;
    top: 50%;
    right: 8px;
    display: grid;
    place-items: center;
    width: 16px;
    height: 16px;
    padding: 0;
    transform: translateY(-50%);
    border: 0;
    background: transparent;
    color: var(--es-muted);
    cursor: pointer;
    line-height: 0;
  }
  .caret:hover {
    color: var(--es-text);
  }
  .caret:disabled {
    opacity: 0.5;
    cursor: default;
  }
  .caret svg {
    width: 9px;
    height: 9px;
  }

  /* Quiet popup: same width as the control, 1px border, very light shadow.
     Fixed + JS-placed so cards with overflow clipping cannot cut it off. */
  .combo-pop {
    position: fixed;
    z-index: 60;
    max-height: 240px;
    overflow-y: auto;
    margin: 0;
    padding: 2px 0;
    background: var(--es-surface);
    border: 1px solid var(--es-border);
    border-radius: 6px;
    box-shadow: 0 2px 6px rgba(0, 0, 0, 0.08);
  }
  .combo-pop button {
    display: block;
    width: 100%;
    padding: 3px 8px;
    border: none;
    border-radius: 0;
    background: transparent;
    color: var(--es-text);
    cursor: pointer;
    font-family: inherit;
    font-size: var(--es-fs-sm);
    line-height: 1.5;
    text-align: left;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .combo-pop button:hover,
  .combo-pop button.active {
    background: var(--es-surface-2);
    color: var(--es-text);
  }
  .combo-pop button.selected {
    background: var(--es-accent-soft);
    color: var(--es-text);
  }
  .combo-none {
    padding: 3px 8px;
    color: var(--es-muted);
    font-size: var(--es-fs-sm);
  }
</style>
