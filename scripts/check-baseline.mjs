#!/usr/bin/env node
/**
 * Single P1 baseline command: go vet + go test + UI build + sandbox asset check.
 * Mirrors docs/01-PLAN.md T3. Run from anywhere; all caches stay on D:.
 */
import { spawnSync } from "node:child_process";
import { readFileSync, existsSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const backend = path.join(root, "backend");
const go = process.env.GO || "go";
const env = {
  ...process.env,
  GOCACHE: process.env.GOCACHE || "D:/tmp/go-build",
  GOPATH: process.env.GOPATH || "D:/tmp/go-path",
  GOMODCACHE: process.env.GOMODCACHE || "D:/tmp/go-mod",
  GOPROXY: "off",
};

function run(label, cmd, args, cwd) {
  const r = spawnSync(cmd, args, { cwd, env, encoding: "utf8", shell: process.platform === "win32" });
  const out = `${r.stdout || ""}${r.stderr || ""}`.trim();
  console.log(`\n=== ${label} (exit ${r.status}) ===`);
  if (out) console.log(out.split("\n").slice(-25).join("\n"));
  if (r.status !== 0) {
    console.error(`FAIL: ${label}`);
    process.exit(r.status || 1);
  }
  return out;
}

run("go vet ./...", go, ["vet", "./..."], backend);
run("go test ./...", go, ["test", "./..."], backend);
const buildOut = run("npm run build", "npm", ["run", "build"], root);
if (!buildOut.includes("DBX_UI_BUILD_SUCCESS")) {
  console.error("FAIL: DBX_UI_BUILD_SUCCESS signal missing");
  process.exit(1);
}

const htmlPath = path.join(root, "ui", "index.html");
if (!existsSync(htmlPath)) {
  console.error("FAIL: ui/index.html missing");
  process.exit(1);
}
const html = readFileSync(htmlPath, "utf8");
const refs = [...html.matchAll(/(?:src|href)="([^"]+)"/g)].map((m) => m[1]);
const absolute = refs.filter((r) => r.startsWith("/"));
if (absolute.length) {
  console.error("FAIL: absolute asset refs break the DBX sandboxed iframe:", absolute);
  process.exit(1);
}
console.log("\n=== sandbox asset refs ===");
console.log(refs.join("  |  "));
console.log("\nBASELINE OK");
