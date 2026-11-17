// Standalone browser bridge for ES Studio P2 UI testing WITHOUT the DBX host.
//
//   node scripts/browser-bridge.mjs
//     [ES_URL=http://127.0.0.1:19200] [READ_ONLY=0|1] [BRIDGE_PORT=5199]
//
// It spawns the real Go sidecar (bin/es-studio-sidecar.exe) as a subprocess and
// serves the built ui/index.html with a `window.dbxPlugin` shim injected. The
// shim forwards every es/request + es/batch to the sidecar over stdio-jsonl and
// injects `direct: {url, read_only}` so the request hits the REAL Elasticsearch
// cluster. This exercises the exact product path (UI -> sidecar -> ES) that DBX
// would otherwise provide, just without the DBX shell.
import { spawn } from "node:child_process";
import { readFileSync } from "node:fs";
import { createServer } from "node:http";
import { resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = dirname(fileURLToPath(import.meta.url));
const root = resolve(__dirname, "..");
const ES_URL = process.env.ES_URL || "http://127.0.0.1:19200";
// The `local-es-mirror` fixture connection points here. scripts/mock-cluster.py
// serves a deliberately different cluster so Multi-Cluster Compare has a real
// second side to diff against (one local ES is all this machine can hold).
const ES_URL_MIRROR = process.env.ES_URL_MIRROR || ES_URL;
const MIRROR_CONNECTION = "local-es-mirror";
const READ_ONLY = process.env.READ_ONLY === "1";
const PORT = parseInt(process.env.BRIDGE_PORT || "5199", 10);
const SIDECAR = resolve(root, "bin", "es-studio-sidecar.exe");

// ---- spawn the real sidecar (stdio-jsonl) ---------------------------------
const sidecar = spawn(SIDECAR, [], { stdio: ["pipe", "pipe", "inherit"] });
let buf = "";
const pending = new Map();
sidecar.stdout.on("data", (chunk) => {
  buf += chunk.toString();
  let idx;
  while ((idx = buf.indexOf("\n")) >= 0) {
    const line = buf.slice(0, idx);
    buf = buf.slice(idx + 1);
    if (!line.trim()) continue;
    try {
      const msg = JSON.parse(line);
      if (msg.id !== undefined && pending.has(msg.id)) {
        pending.get(msg.id)(msg);
        pending.delete(msg.id);
      }
    } catch {
      /* ignore non-JSON stdout noise */
    }
  }
});
sidecar.on("exit", (code) => console.error(`[browser-bridge] sidecar exited code=${code}`));

let rpcId = 1;
function sidecarCall(method, params) {
  return new Promise((resolve, reject) => {
    const id = rpcId++;
    const timer = setTimeout(() => {
      pending.delete(id);
      reject(new Error("sidecar timeout for " + method));
    }, 120000);
    pending.set(id, (msg) => {
      clearTimeout(timer);
      resolve(msg);
    });
    sidecar.stdin.write(JSON.stringify({ jsonrpc: "2.0", id, method, params }) + "\n");
  });
}

// ---- inject the bridge shim into the built UI -----------------------------
// Read ui/index.html on every request: `npm run build` during a debugging
// session then needs no bridge restart to be picked up.
function renderPage() {
  const uiHtml = readFileSync(resolve(root, "ui", "index.html"), "utf8");
  return uiHtml.replace("</head>", bridgeScript + "\n</head>");
}
const bridgeScript = `<script>
(function(){
  var ES_URL = ${JSON.stringify(ES_URL)};
  var READ_ONLY = ${READ_ONLY ? "true" : "false"};
  function rpc(method, params){
    return fetch('/rpc',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({method:method,params:params||{}})}).then(function(r){return r.json();});
  }
  // host.storage persists across app restarts in DBX; sessionStorage is the
  // closest faithful stand-in here (survives reload, dies with the tab) so that
  // "settings stick" can actually be asserted.
  var storageMem = {};
  function storageKey(k){ return 'dbxshim:' + k; }
  window.dbxPlugin = {
    ready: Promise.resolve({ connectionId: 'local-es', providerId:'es.studio', connectionType:'elasticsearch' }),
    context: { connectionId: 'local-es', providerId:'es.studio', connectionType:'elasticsearch' },
    locale: ${JSON.stringify(process.env.BRIDGE_LOCALE || 'zh-CN')},
    contributionId: 'local.es.studio',
    theme: ${JSON.stringify({ appearance: process.env.BRIDGE_THEME || 'light', tokens: JSON.parse(process.env.BRIDGE_TOKENS || '{}') })},
    invoke: function(method, params, options){
      return rpc(method, params || {}).then(function(res){
        if (res && res.error) { var e = new Error(res.error.message || 'RPC error'); e.code = res.error.code; throw e; }
        return res ? res.result : undefined;
      });
    },
    getConnections: function(){
      return Promise.resolve([
        { id:'local-es', name:'Local ES (test)', label:'Local ES (test)', dbType:'elasticsearch', dialect:'elasticsearch', connected:true, readOnly: READ_ONLY }
      ]);
    },
    listConnections: function(){ return this.getConnections(); },
    storage: {
      get: function(k){
        try {
          var raw = sessionStorage.getItem(storageKey(k));
          if (raw !== null) return Promise.resolve(JSON.parse(raw));
        } catch (e) { /* fall through to memory */ }
        return Promise.resolve(k in storageMem ? storageMem[k] : null);
      },
      set: function(k,v){
        storageMem[k] = v;
        try { sessionStorage.setItem(storageKey(k), JSON.stringify(v)); } catch (e) { /* memory only */ }
        return Promise.resolve();
      },
      delete: function(k){
        delete storageMem[k];
        try { sessionStorage.removeItem(storageKey(k)); } catch (e) { /* ignore */ }
        return Promise.resolve();
      }
    },
    onContext: function(fn){ return function(){}; },
    workbench: { setTitle: function(t){ document.title = String(t); return Promise.resolve(); } }
  };
  console.log('[browser-bridge] window.dbxPlugin ready; ES_URL=' + ES_URL + ' readOnly=' + READ_ONLY);
})();
</script>`;

// ---- http server ----------------------------------------------------------
const server = createServer((req, res) => {
  if (req.method === "POST" && req.url === "/rpc") {
    let body = "";
    req.on("data", (c) => (body += c));
    req.on("end", async () => {
      try {
        const { method, params } = JSON.parse(body || "{}");
        const p = Object.assign({}, params);
        // Methods that need to reach Elasticsearch get the direct override;
        // mon/* does too, because the collector talks to ES itself.
        if (["es/request", "es/batch", "mon/start", "mon/sample", "mcp/call", "mcp/grant"].includes(method)) {
          const url = p.connection_id === MIRROR_CONNECTION ? ES_URL_MIRROR : ES_URL;
          p.direct = { url, read_only: READ_ONLY };
        }
        const out = await sidecarCall(method, p);
        res.writeHead(200, { "Content-Type": "application/json" });
        res.end(JSON.stringify(out));
      } catch (e) {
        res.writeHead(500, { "Content-Type": "application/json" });
        res.end(JSON.stringify({ error: { code: -32603, message: String((e && e.message) || e) } }));
      }
    });
    return;
  }
  if (req.url === "/" || req.url === "/index.html") {
    res.writeHead(200, { "Content-Type": "text/html; charset=utf-8" });
    res.end(renderPage());
    return;
  }
  if (req.url === "/favicon.ico") {
    res.writeHead(204);
    res.end();
    return;
  }
  res.writeHead(404);
  res.end("not found");
});

server.listen(PORT, () => {
  console.log(`[browser-bridge] http://localhost:${PORT}  ES_URL=${ES_URL}  readOnly=${READ_ONLY}`);
});

function shutdown() {
  try {
    sidecar.kill();
  } catch {
    /* ignore */
  }
  process.exit(0);
}
process.on("SIGINT", shutdown);
process.on("SIGTERM", shutdown);
