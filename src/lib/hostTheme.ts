/**
 * Text size follows the host, not a constant.
 *
 * DBX pushes a theme object whose `tokens` map is applied to the plugin's
 * document root. The token that carries the user's preferred text size is not
 * part of the documented set and has changed name between host builds, so this
 * module probes the map for a font/text size token and pins `--es-fs-base` to
 * it. When nothing matches, the CSS chain in tokens.css keeps its 13px
 * fallback — never a hardcoded per-component size.
 */

const TOKEN = /^--(?:dbx-)?(?:font|text|ui)[-a-z]*(?:size|scale)/i;
const LENGTH = /^\d+(?:\.\d+)?(?:px|rem|em|pt)$/;

export interface HostTheme {
  appearance?: string;
  tokens?: Record<string, string>;
}

/** Apply the host's text-size token, if it exposes one. Safe to call repeatedly. */
export function syncTypeScale(theme: HostTheme | null | undefined): void {
  if (typeof document === "undefined") return;
  const tokens = theme?.tokens ?? {};
  const value = Object.entries(tokens).find(([name, token]) => TOKEN.test(name) && LENGTH.test(String(token).trim()));
  const root = document.documentElement;
  if (value) root.style.setProperty("--es-fs-base", String(value[1]).trim());
  else root.style.removeProperty("--es-fs-base");
}
