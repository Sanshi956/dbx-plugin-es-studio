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
        /<script\b[^>]*\ssrc="([^"]+)"[^>]*>\s*<\/script>/g,
        (_tag, src) => {
          const code = readFileSync(ui(src.replace(/^\.?\//, "")), "utf8")
            .replace(/<\/script/gi, "<\\/script");
          return `<script>\n${code}\n</script>`;
        },
      );
      html = html.replace(
        /<link\b[^>]*rel="stylesheet"[^>]*href="([^"]+)"[^>]*>/g,
        (_tag, href) => {
          const css = readFileSync(ui(href.replace(/^\.?\//, "")), "utf8")
            .replace(/<\/style/gi, "<\\/style");
          return `<style>\n${css}\n</style>`;
        },
      );

      // Only inspect tags, not the inlined JS/CSS bodies (which may legitimately
      // contain strings such as src="...").
      if (/<script\b[^>]*\ssrc=|<link\b[^>]*\shref=/i.test(html)) {
        throw new Error("ES Studio UI must be self-contained; external refs remain");
      }
      if (!html.includes("<script>\n") || !html.includes("<style>\n")) {
        throw new Error("ES Studio UI inline script/style missing");
      }

      writeFileSync(htmlPath, html);
      rmSync(ui("assets"), { recursive: true, force: true });
      console.log("DBX_UI_SINGLEFILE_OK");
    },
  };
}

export default defineConfig({
  base: "./",
  plugins: [
    svelte(),
    singleFileUi(),
    { name: "dbx-build-signal", closeBundle() { console.log("DBX_UI_BUILD_SUCCESS"); } },
  ],
  build: { outDir: "ui", emptyOutDir: true },
});
