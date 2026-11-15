/**
 * Locale plumbing.
 *
 * DBX hands the workbench a locale in the `init` message and re-sends it (with
 * the theme) in an `env` message whenever the user switches language. The host
 * runtime mirrors that into `window.dbxPlugin.locale` and dispatches a
 * `dbx-plugin-env` DOM event, so switching is live — no remount, no reload.
 *
 * Every visible string in this plugin goes through `t()`. Nothing else is
 * allowed to hold copy, otherwise the two languages drift apart.
 */
import { en } from "./locales/en";
import { toolsEn } from "./locales/tools.en";
import { zhCN } from "./locales/zh-CN";

export type Locale = "zh-CN" | "en";

/**
 * Reading `current` inside `t()` is what makes Svelte 5 re-render the caller
 * when the host flips the language: the read happens while the template effect
 * is running, so the proxy records it as a dependency.
 */
let current = $state<Locale>("zh-CN");

// MCP tool documentation is English-only by design (see locales/tools.en.ts);
// the Chinese side reads the same text out of the Go registry instead.
const DICTS: Record<Locale, Record<string, string>> = { "zh-CN": zhCN, en: { ...en, ...toolsEn } };

export function normalizeLocale(raw: string | null | undefined): Locale {
  if (!raw) return "zh-CN";
  return raw.toLowerCase().startsWith("zh") ? "zh-CN" : "en";
}

export function getLocale(): Locale {
  return current;
}

export function setLocale(next: Locale): void {
  if (next === current) return;
  current = next;
  if (typeof document !== "undefined") document.documentElement.lang = next;
}

/** Translate a key, substituting `{name}` placeholders. */
export function t(key: string, vars?: Record<string, string | number>): string {
  const dict = DICTS[current];
  let text = dict[key];
  if (text === undefined) {
    // Never blank a label because one language lags behind; show the Chinese
    // string and keep the key reachable for whoever fixes it.
    text = zhCN[key];
    if (text === undefined) return key;
  }
  if (!vars) return text;
  return text.replace(/\{(\w+)\}/g, (match, name: string) =>
    Object.prototype.hasOwnProperty.call(vars, name) ? String(vars[name]) : match,
  );
}

/**
 * Translate, but fall back to caller-supplied text when the key is unknown.
 * Used where the source of truth lives outside the dictionaries — the MCP
 * registry ships its own titles, and English is an overlay on top of it.
 */
export function tOr(key: string, fallback: string): string {
  const text = DICTS[current][key];
  return text === undefined ? fallback : text;
}

/** Count-aware shorthand: picks `<key>.one` / `<key>.other` when present. */
export function tn(key: string, count: number, vars?: Record<string, string | number>): string {
  const base = count === 1 ? `${key}.one` : `${key}.other`;
  const dict = DICTS[current];
  if (dict[base] !== undefined || zhCN[base] !== undefined) {
    return t(base, { count, ...vars });
  }
  return t(key, { count, ...vars });
}

/** True when the key exists in either language; used by tests that police coverage. */
export function hasKey(key: string): boolean {
  return zhCN[key] !== undefined || en[key] !== undefined;
}

/** Every key currently defined, exposed for the locale-parity check. */
export function allKeys(): string[] {
  return [...new Set([...Object.keys(zhCN), ...Object.keys(en)])].sort();
}

export { zhCN, en };
