import { svelte } from "@sveltejs/vite-plugin-svelte";
import { defineConfig } from "vite";
import { existsSync, readFileSync, writeFileSync, rmSync } from "node:fs";
import { fileURLToPath } from "node:url";

// DBX loads the workbench from an opaque-origin iframe served through
// read_plugin_ui_entry. A Vite-style `<script type="module" crossorigin
// src="./assets/...">` is NOT resolvable there (no base URL, CORS-mode module
// fetch), which yields a completely blank workbench even though the package is
// installed correctly. Every plugin that actually renders in DBX ships a
// self-contained HTML document, so we inline CSS + JS after each build.
//
// Critical detail: Vite hoists the module script into <head>. Once inlined as a
// plain <script>, it becomes synchronous and executes BEFORE <body> exists, so
// `document.getElementById("app")` returns null and the app never mounts (the
// static boot marker stays on screen forever). Working DBX plugins all place
// their inline <script> at the very end of <body>; we do the same here.
function singleFileUi() {
  const ui = (relative) => fileURLToPath(new URL(`./ui/${relative}`, import.meta.url));

  return {
    name: "dbx-ui-single-file",
    apply: "build",
    closeBundle() {
      const htmlPath = ui("index.html");
      if (!existsSync(htmlPath)) return;

      let html = readFileSync(htmlPath, "utf8");
      html = html.replace(
        /<link\b[^>]*rel="stylesheet"[^>]*href="([^"]+)"[^>]*>/g,
        (_tag, href) => {
          const css = readFileSync(ui(href.replace(/^\.?\//, "")), "utf8")
            .replace(/<\/style/gi, "<\\/style");
          return `<style>\n${css}\n</style>`;
        },
      );

      // Pull the Vite script out of <head> and re-insert it just before
      // </body>, with a boot-phase marker so a script that starts but fails
      // later is distinguishable from a script that never runs at all.
      const scriptTag = /<script\b[^>]*\ssrc="([^"]+)"[^>]*>\s*<\/script>/;
      const match = html.match(scriptTag);
      if (!match) throw new Error("ES Studio UI: Vite script tag missing");
      const code = readFileSync(ui(match[1].replace(/^\.?\//, "")), "utf8")
        .replace(/<\/script/gi, "<\\/script");
      const bootMarker =
        `(function(){var e=document.getElementById("es-boot");` +
        `if(e){var s=e.querySelector("small");` +
        `if(s)s.textContent="Application script running…";}})();`;
      html = html.replace(scriptTag, "");
      // Function replacer, not a string: a string replacement expands `$&`,
      // `$'` and `$1` found in the minified bundle. That is not theoretical —
      // `caps && !caps.x` minifies to `$&&!$`, and `$&` pasted the matched
      // `</body>` into the middle of the app, which then failed to parse with
      // "Unexpected token '<'" and left a blank workbench.
      const inline = `<script>\n${bootMarker}\n${code}\n</script>\n  </body>`;
      html = html.replace(/<\/body>/i, () => inline);
      if (!html.includes(`<script>\n${bootMarker}\n${code}\n</script>`)) {
        throw new Error("ES Studio UI: inlined script was corrupted during insertion");
      }

      // Only inspect tags, not the inlined JS/CSS bodies (which may legitimately
      // contain strings such as src="...").
      if (/<script\b[^>]*\ssrc=|<link\b[^>]*\shref=/i.test(html)) {
        throw new Error("ES Studio UI must be self-contained; external refs remain");
      }
      if (!html.includes("<script>\n") || !html.includes("<style>\n")) {
        throw new Error("ES Studio UI inline script/style missing");
      }
      const bodyAt = html.search(/<body\b/i);
      const scriptAt = html.indexOf("<script>\n");
      const bodyEndAt = html.search(/<\/body>/i);
      if (!(bodyAt >= 0 && scriptAt > bodyAt && scriptAt < bodyEndAt)) {
        throw new Error("ES Studio UI inline script must be inside <body> after the app root");
      }

      writeFileSync(htmlPath, html);
      rmSync(ui("assets"), { recursive: true, force: true });
      console.log("DBX_UI_SINGLEFILE_OK");
    },
  };
}

export default defineConfig({
  base: "./",
  // The topbar shows the release version. Read it from manifest.json at build
  // time instead of hardcoding it in the UI: manifest.json / backend/main.go /
  // package name have already drifted apart once, so this keeps one source.
  define: {
    __PLUGIN_VERSION__: JSON.stringify(
      JSON.parse(
        readFileSync(fileURLToPath(new URL("./manifest.json", import.meta.url)), "utf8"),
      ).version,
    ),
  },
  plugins: [
    svelte(),
    singleFileUi(),
    { name: "dbx-build-signal", closeBundle() { console.log("DBX_UI_BUILD_SUCCESS"); } },
  ],
  build: { outDir: "ui", emptyOutDir: true },
});
