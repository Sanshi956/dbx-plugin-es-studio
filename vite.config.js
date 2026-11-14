import { svelte } from "@sveltejs/vite-plugin-svelte";
import { defineConfig } from "vite";

// DBX loads the workbench from an opaque-origin sandboxed iframe.
// base:"./" is REQUIRED - the official svelte template omits it and code-split
// chunks then resolve to /assets/... which 404s inside the sandbox.
export default defineConfig({
  base: "./",
  plugins: [
    svelte(),
    { name: "dbx-build-signal", closeBundle() { console.log("DBX_UI_BUILD_SUCCESS"); } },
  ],
  build: { outDir: "ui", emptyOutDir: true },
});
