/**
 * Windows compatibility shim for `dbx-plugin dev`.
 *
 * The official Rust CLI (0.1.10) canonicalizes `--path` on Windows into an
 * extended-length path (`\\?\D:\...`) and passes it to the JS dev runtime as
 * `project`, `dataDir`, `cwd` and build output paths. Node's child_process
 * spawn and CMD.EXE both reject that prefix as a working directory, so every
 * build command fails before it starts ("Cannot start build command" or
 * "'vite' is not recognized as an internal or external command").
 *
 * This shim is installed through DBX_PLUGIN_DEV_RUNTIME. It strips the
 * namespace prefix from the config, then delegates to the untouched official
 * runtime. Nothing in the global CLI installation is modified.
 */
import { existsSync } from "node:fs";
import { execFileSync } from "node:child_process";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const NS = "\\\\?\\";
const NS_UNC = "\\\\?\\UNC\\";

/** Strip the Win32 extended-length namespace, recursively. */
export function normalize(value) {
  if (typeof value === "string") {
    if (value.startsWith(NS_UNC)) return "\\\\" + value.slice(NS_UNC.length);
    if (value.startsWith(NS)) return value.slice(NS.length);
    return value;
  }
  if (Array.isArray(value)) return value.map(normalize);
  if (value && typeof value === "object") {
    return Object.fromEntries(
      Object.entries(value).map(([key, item]) => [key, normalize(item)]),
    );
  }
  return value;
}

function globalRoot() {
  const npm = process.platform === "win32" ? "npm.cmd" : "npm";
  return execFileSync(npm, ["root", "-g"], {
    encoding: "utf8",
    shell: process.platform === "win32",
  }).trim();
}

function officialRuntime() {
  if (process.env.DBX_PLUGIN_OFFICIAL_RUNTIME) {
    return process.env.DBX_PLUGIN_OFFICIAL_RUNTIME;
  }
  const candidate = resolve(
    globalRoot(),
    "@dbx-app",
    "plugin-cli",
    "dev-runtime",
    "runtime.mjs",
  );
  if (!existsSync(candidate)) {
    throw new Error(
      `Official dev runtime not found at ${candidate}. ` +
        "Set DBX_PLUGIN_OFFICIAL_RUNTIME to the runtime.mjs of @dbx-app/plugin-cli.",
    );
  }
  return candidate;
}

async function main() {
  if (process.argv[2] !== "--config" || !process.argv[3]) {
    throw new Error("Start this runtime using `node scripts/dev.mjs`");
  }
  const config = normalize(JSON.parse(process.argv[3]));
  console.error(`[es-studio] dev host project: ${config.project}`);

  const runtimePath = officialRuntime();
  const runtime = await import(pathToFileURL(runtimePath).href);
  await runtime.startDevelopment(config);
}

const isEntrypoint =
  process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href;

if (isEntrypoint) {
  main().catch((error) => {
    console.error("[es-studio] dev host failed:", error?.message || error);
    process.exitCode = 1;
  });
}
