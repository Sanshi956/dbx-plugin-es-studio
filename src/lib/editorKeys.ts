import { tick } from "svelte";

/**
 * Tab inside a code editor must indent, never move focus into the widget below
 * (that used to happen mid-typing in every JSON editor of this plugin). Esc arms
 * one real Tab so the field can still be left by keyboard; Shift+Tab always
 * leaves. Ctrl/⌘+Enter runs when the caller supplies a shortcut.
 *
 * Kept here instead of per-page so a new editor cannot silently reintroduce the
 * bug — every textarea that holds JSON uses this.
 */
const escapeArmed = new WeakSet<HTMLElement>();

export function handleEditorKey(
  event: KeyboardEvent,
  getValue: () => string,
  setValue: (next: string) => void,
  onRun?: () => void,
): void {
  const el = event.currentTarget as HTMLTextAreaElement | null;
  if (!el) return;

  if (event.key === "Escape") {
    escapeArmed.add(el);
    return;
  }
  if (event.key === "Enter" && (event.ctrlKey || event.metaKey) && onRun) {
    event.preventDefault();
    onRun();
    return;
  }
  if (event.key !== "Tab") return;
  if (event.shiftKey || escapeArmed.has(el)) {
    escapeArmed.delete(el);
    return;
  }

  event.preventDefault();
  const start = el.selectionStart;
  const end = el.selectionEnd;
  const indent = "  ";
  const value = getValue();
  setValue(value.slice(0, start) + indent + value.slice(end));
  void tick().then(() => {
    el.selectionStart = el.selectionEnd = start + indent.length;
  });
}
