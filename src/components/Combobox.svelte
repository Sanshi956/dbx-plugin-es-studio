<script lang="ts">
  /**
   * Theme-aware replacement for the native <input list="..."> + <datalist>.
   * Native datalist popups are rendered by the browser UI layer and ignore the
   * page's `color-scheme`, so they stay dark on a light-themed page. This
   * component renders its own popup from CSS tokens, styled to match the native
   * <select> popup on this platform (square corners, no shadow, blue-on-white
   * selected row) so every dropdown on a page looks identical.
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
    /** Extra class on the input (e.g. key-input for existing selectors). */
    class?: string;
    disabled?: boolean;
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
    onValueChange,
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

  function setValue(next: string) {
    value = next;
    onValueChange?.(next);
  }
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
    setValue(o.value);
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
    class={className}
    {value}
    {placeholder}
    {disabled}
    data-list={listId}
    data-options-count={options.length}
    role="combobox"
    aria-controls={popId}
    aria-expanded={open}
    aria-autocomplete="list"
    autocomplete="off"
    oninput={(e) => {
      setValue(e.currentTarget.value);
      // Only open the popup for genuine user typing. Programmatic fills
      // (Playwright/driver, isTrusted=false) update the value but must not
      // leave an overlay covering sibling controls — native datalist behaves
      // the same way.
      if (e.isTrusted) show();
    }}
    onfocus={(e) => {
      if (e.isTrusted) show();
    }}
    onkeydown={onKeydown}
  />
  {#if open}
    <div class="combo-pop" id={popId} role="listbox">
      {#each visible as o, i (o.value)}
        <button
          type="button"
          role="option"
          aria-selected={i === activeIdx || o.value === value}
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
  .combo :global(input) {
    box-sizing: border-box;
  }
  /* Match the native <select> popup: square corners, no shadow, tight to the
     control, blue selected row. All colors from tokens so dark theme works. */
  .combo-pop {
    position: absolute;
    z-index: 60;
    top: calc(100% + 1px);
    left: 0;
    min-width: 100%;
    max-width: 340px;
    max-height: 280px;
    overflow-y: auto;
    margin: 0;
    padding: 2px 0;
    background: var(--es-surface);
    border: 1px solid var(--es-border);
    border-radius: 0;
    box-shadow: none;
    color-scheme: light;
  }
  .combo-pop button {
    display: block;
    width: 100%;
    text-align: left;
    background: transparent;
    border: none;
    cursor: pointer;
    padding: 3px 10px;
    border-radius: 0;
    color: var(--es-text);
    font-family: inherit;
    font-size: var(--es-fs-sm);
    line-height: 1.5;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .combo-pop button:hover,
  .combo-pop button.active,
  .combo-pop button.selected {
    background: var(--es-accent);
    color: #fff;
  }
  .combo-none {
    padding: 4px 10px;
    color: var(--es-muted);
    font-size: var(--es-fs-sm);
  }
</style>
