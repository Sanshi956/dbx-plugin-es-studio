<script lang="ts">
  /**
   * Theme-aware replacement for the native <input list="..."> + <datalist>.
   * Native datalist popups are rendered by the browser UI layer and ignore the
   * page's `color-scheme`, so they stay dark on a light-themed page. This
   *component renders its own popup from CSS tokens so every dropdown matches.
   *
   * The input still accepts free text (same as datalist), and offers keyboard
   * navigation (Up/Down/Enter/Esc) plus click-to-pick.
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
    disabled?: boolean;
  }
  let {
    value = $bindable(""),
    options = [],
    placeholder = "",
    listId,
    disabled = false,
  }: Props = $props();

  // Stable per-instance id so the combobox can point at its popup for a11y.
  const popId = `combo-pop-${Math.random().toString(36).slice(2, 9)}`;

  let open = $state(false);
  let activeIdx = $state(-1);
  let wrapEl: HTMLDivElement;
  let inputEl: HTMLInputElement;

  const visible = $derived.by(() => {
    const q = value.trim().toLowerCase();
    if (!q) return options;
    return options.filter(
      (o) =>
        o.value.toLowerCase().includes(q) ||
        (o.label ?? "").toLowerCase().includes(q),
    );
  });

  function show() {
    if (!disabled) {
      open = true;
      activeIdx = -1;
    }
  }
  function hide() {
    open = false;
    activeIdx = -1;
  }
  function pick(o: ComboboxOption) {
    value = o.value;
    hide();
    inputEl?.focus();
  }
  function onKeydown(e: KeyboardEvent) {
    if (e.key === "ArrowDown") {
      e.preventDefault();
      if (!open) {
        show();
        return;
      }
      activeIdx = visible.length ? Math.min(activeIdx + 1, visible.length - 1) : -1;
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      if (!open) {
        show();
        return;
      }
      activeIdx = visible.length ? Math.max(activeIdx - 1, 0) : -1;
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
    }
  }

  // Close on outside pointer-down.
  $effect(() => {
    if (!open) return;
    const onDoc = (e: PointerEvent) => {
      if (wrapEl && !wrapEl.contains(e.target as Node)) hide();
    };
    document.addEventListener("pointerdown", onDoc);
    return () => document.removeEventListener("pointerdown", onDoc);
  });
</script>

<div class="combo" bind:this={wrapEl}>
  <input
    bind:this={inputEl}
    {value}
    {placeholder}
    {disabled}
    data-list={listId}
    role="combobox"
    aria-controls={popId}
    aria-expanded={open}
    aria-autocomplete="list"
    autocomplete="off"
    oninput={(e) => {
      value = e.currentTarget.value;
      show();
    }}
    onfocus={show}
    onkeydown={onKeydown}
  />
  {#if open}
    <div class="combo-pop" id={popId} role="listbox">
      {#each visible as o, i (o.value)}
        <button
          type="button"
          role="option"
          aria-selected={i === activeIdx}
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
  .combo input {
    /* Keep the native input's default sizing so the bar layout is unchanged. */
    box-sizing: border-box;
  }
  .combo-pop {
    position: absolute;
    z-index: 60;
    top: 100%;
    left: 0;
    min-width: 100%;
    max-width: 340px;
    max-height: 280px;
    overflow-y: auto;
    margin-top: 3px;
    padding: 3px;
    background: var(--es-surface);
    border: 1px solid var(--es-border);
    border-radius: var(--es-radius);
    box-shadow: 0 6px 18px rgba(0, 0, 0, 0.16);
    color-scheme: light;
  }
  :global([data-dbx-theme="dark"]) .combo-pop {
    box-shadow: 0 6px 18px rgba(0, 0, 0, 0.5);
  }
  .combo-pop button {
    display: block;
    width: 100%;
    text-align: left;
    background: none;
    border: none;
    cursor: pointer;
    padding: 5px 8px;
    border-radius: 4px;
    color: var(--es-text);
    font-family: inherit;
    font-size: var(--es-fs-sm);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .combo-pop button:hover,
  .combo-pop button.active {
    background: var(--es-surface-2);
  }
  .combo-pop button.selected {
    color: var(--es-accent);
    font-weight: 600;
  }
  .combo-none {
    padding: 6px 8px;
    color: var(--es-muted);
    font-size: var(--es-fs-xs);
  }
</style>
