/**
 * Cross-platform launcher for the official DBX dev host.
 *
 *   node scripts/dev.mjs [--port 5190]
 *
 * It injects scripts/dev-host.mjs as DBX_PLUGIN_DEV_RUNTIME so the Windows
 * extended-length-path bug in @dbx-app/plugin-cli 0.1.10 is corrected without
 * touching the globally installed CLI. On Linux/macOS the shim is a no-op
 * (no path prefix to strip), so behaviour stays identical to upstream.
 */
import { existsSync } from "node:fs";
import { execFileSync, spawn } from "node:child_process";
import { delimiter, dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const scriptsDir = dirname(fileURLToPath(import.meta.url));
const project = resolve(scriptsDir, "..");
const shim = resolve(scriptsDir, "dev-host.mjs");

function argValue(flag, fallback) {
  const index = process.argv.indexOf(flag);
  return index >= 0 && process.argv[index + 1] ? process.argv[index + 1] : fallback;
}

function globalRoot() {
  const npm = process.platform === "win32" ? "npm.cmd" : "npm";
  return execFileSync(npm, ["root", "-g"], {
    encoding: "utf8",
    shell: process.platform === "win32",
  }).trim();
}

function cliEntrypoint() {
  if (process.env.DBX_PLUGIN_CLI_BIN) return process.env.DBX_PLUGIN_CLI_BIN;
  const candidate = resolve(
    globalRoot(),
    "@dbx-app",
    "plugin-cli",
    "bin",
    "dbx-plugin.js",
  );
  if (!existsSync(candidate)) {
    throw new Error(
      `dbx-plugin CLI not found at ${candidate}. ` +
        "Set DBX_PLUGIN_CLI_BIN to its bin/dbx-plugin.js.",
    );
  }
  return candidate;
}

function officialRuntime() {
  if (process.env.DBX_PLUGIN_OFFICIAL_RUNTIME) {
    return process.env.DBX_PLUGIN_OFFICIAL_RUNTIME;
  }
  return resolve(
    globalRoot(),
    "@dbx-app",
    "plugin-cli",
    "dev-runtime",
    "runtime.mjs",
  );
}

/**
 * The dev runtime spawns `go build` directly (no shell), so the toolchain must
 * be on PATH. Git Bash sessions here do not export it by default. Accept an
 * explicit DBX_GO_BIN_DIR, otherwise probe the usual install locations.
 */
function withGoOnPath(env) {
  const probe = (candidate) => {
    if (!candidate) return false;
    const exe = process.platform === "win32" ? "go.exe" : "go";
    if (!existsSync(resolve(candidate, exe))) return false;
    env.PATH = candidate + delimiter + (env.PATH || "");
    console.log(`[es-studio] Go toolchain: ${candidate}`);
    return true;
  };

  try {
    execFileSync("go", ["version"], { env, stdio: "ignore" });
    return env;
  } catch {
    // not on PATH, fall through to known install locations
  }

  const candidates = [
    process.env.DBX_GO_BIN_DIR,
    "D:/software/dev/Go/go/bin",
    "C:/Program Files/Go/bin",
    "C:/Go/bin",
    "/usr/local/go/bin",
    "/usr/lib/go/bin",
  ];
  if (!candidates.some(probe)) {
    throw new Error(
      "Go toolchain not found. Add go/bin to PATH or set DBX_GO_BIN_DIR.",
    );
  }
  return env;
}

const port = argValue("--port", process.env.DBX_DEV_PORT || "5190");
const args = ["dev", "--path", project, "--port", port];
const env = withGoOnPath({
  ...process.env,
  DBX_PLUGIN_DEV_RUNTIME: shim,
  DBX_PLUGIN_OFFICIAL_RUNTIME: officialRuntime(),
});

console.log(`[es-studio] starting dev host on port ${port}`);
const child = spawn(process.execPath, [cliEntrypoint(), ...args], {
  cwd: project,
  env,
  stdio: "inherit",
});

for (const signal of ["SIGINT", "SIGTERM"]) {
  process.on(signal, () => child.kill(signal));
}
child.on("exit", (code, signal) => {
  if (signal) process.kill(process.pid, signal);
  else process.exit(code ?? 1);
});
