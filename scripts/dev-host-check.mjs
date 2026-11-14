/**
 * End-to-end verification against the official `dbx-plugin dev` mock host.
 *
 * Prerequisites:
 *   node scripts/dev.mjs --port 5190        (dev host, in another terminal)
 *   python scripts/es-cluster.py start       (local ES 8.19.23 on 127.0.0.1:19200)
 *
 * Run: node scripts/dev-host-check.mjs [--port 5190]
 *
 * It speaks the same HTTP protocol the dev-host shell UI uses, so it exercises
 * bootstrap -> connection lifecycle -> iframe document -> host bridge ->
 * Go sidecar -> real Elasticsearch, not a standalone JSON-RPC smoke test.
 */
const portArg = process.argv.indexOf("--port");
const port = portArg >= 0 && process.argv[portArg + 1] ? process.argv[portArg + 1] : (process.env.DBX_DEV_PORT || "5190");
const origin = `http://127.0.0.1:${port}`;
const CONNECTION_ID = process.env.DBX_DEV_CONNECTION || "es-studio-local";
const WORKBENCH = "local.es.studio.workbench";

let cookie = "";
let csrf = "";
let passed = 0;
let failed = 0;

function check(name, condition, detail = "") {
  if (condition) {
    passed++;
    console.log(`  PASS  ${name}`);
  } else {
    failed++;
    console.log(`  FAIL  ${name}${detail ? ` :: ${detail}` : ""}`);
  }
}

async function call(route, body, method = "POST") {
  const headers = { Accept: "application/json" };
  if (method === "POST") {
    headers["content-type"] = "application/json";
    headers.origin = origin;
    headers["x-mock-csrf"] = csrf;
    headers["x-dbx-page"] = "default";
  }
  if (cookie) headers.cookie = cookie;
  const response = await fetch(`${origin}${route}`, {
    method,
    headers,
    body: method === "POST" ? JSON.stringify(body ?? {}) : undefined,
  });
  const setCookie = response.headers.get("set-cookie");
  if (setCookie) cookie = setCookie.split(";")[0];
  const text = await response.text();
  let payload;
  try {
    payload = JSON.parse(text);
  } catch {
    payload = text;
  }
  if (!response.ok) throw new Error(`${route} -> ${response.status}: ${text.slice(0, 400)}`);
  // POST /api/* endpoints wrap their result in { value }; bootstrap and
  // diagnostics (GET) answer with the bare object.
  if (method === "POST" && payload && typeof payload === "object" && "value" in payload) {
    return payload.value;
  }
  return payload;
}

async function bridge(frameId, channel, method, params = {}) {
  return call("/api/bridge", { frameId, channel, method, params });
}

async function main() {
  console.log(`ES Studio dev-host check against ${origin}`);

  const bootstrap = await call("/api/bootstrap", undefined, "GET");
  csrf = bootstrap.csrf;
  check("bootstrap returns CSRF token", typeof csrf === "string" && csrf.length > 10);
  check("manifest id is local.es.studio", bootstrap.manifest?.id === "local.es.studio", JSON.stringify(bootstrap.manifest?.id));
  check("backend state is ready", bootstrap.state === "ready", String(bootstrap.state));
  check("preset connection is listed", bootstrap.connections?.some((c) => c.id === CONNECTION_ID), JSON.stringify(bootstrap.connections));

  const diagnostics = await call("/api/diagnostics", undefined, "GET");
  check("diagnostics backendState ready", diagnostics.backendState === "ready", String(diagnostics.backendState));
  check("diagnostics port matches", diagnostics.port === Number(port), String(diagnostics.port));

  const opened = await call("/api/connections/connect", { id: CONNECTION_ID });
  const frame = opened.frame;
  check("connection frame opened", Boolean(frame?.id), JSON.stringify(opened).slice(0, 300));
  check("frame context carries connectionId", frame?.context?.connectionId === CONNECTION_ID, JSON.stringify(frame?.context));

  const documentResponse = await call("/api/frame-document", { frameId: frame.id });
  const html = documentResponse.html || "";
  const channel = documentResponse.channel;
  check("frame document has channel", typeof channel === "string" && channel.length > 0);
  check("frame html contains #app mount", html.includes('id="app"'), html.slice(0, 200));
  check("frame assets use relative paths (sandbox safe)", !/(?:src|href)="\/assets\//.test(html), html.slice(0, 300));
  check("frame html injects host bridge", html.includes("dbxPlugin"), html.slice(0, 200));

  const context = await bridge(frame.id, channel, "host.getContext", {});
  check("host.getContext matches frame", context?.connectionId === CONNECTION_ID, JSON.stringify(context));

  const ping = await bridge(frame.id, channel, "backend.invoke", { method: "es/ping", params: {} });
  check("es/ping through host bridge", ping?.pong === true && ping?.plugin === "local.es.studio", JSON.stringify(ping));

  const root = await bridge(frame.id, channel, "backend.invoke", {
    method: "es/request",
    params: { connection_id: CONNECTION_ID, method: "GET", path: "/" },
  });
  check("es/request GET / reaches real ES", root?.status === 200, JSON.stringify(root).slice(0, 300));
  check("real ES version is 8.x", String(root?.body?.version?.number || "").startsWith("8."), JSON.stringify(root?.body?.version?.number));
  check("real cluster name is es-studio-test", root?.body?.cluster_name === "es-studio-test", String(root?.body?.cluster_name));

  const batch = await bridge(frame.id, channel, "backend.invoke", {
    method: "es/batch",
    params: {
      connection_id: CONNECTION_ID,
      requests: [
        { method: "GET", path: "/" },
        { method: "GET", path: "/_cluster/health" },
        { method: "GET", path: "/_cluster/stats" },
        { method: "GET", path: "/_cat/nodes?format=json" },
        { method: "GET", path: "/_cat/indices?format=json&bytes=b" },
      ],
    },
  });
  const results = batch?.results || [];
  check("es/batch returned 5 results", results.length === 5, String(results.length));
  check("all Overview APIs succeeded", results.every((r) => r?.status === 200), JSON.stringify(results.map((r) => r?.status)));
  const health = results[1]?.body;
  check("cluster health is green/yellow", ["green", "yellow"].includes(health?.status), String(health?.status));
  check("query string preserved on _cat/indices", Array.isArray(results[4]?.body), JSON.stringify(results[4]?.body).slice(0, 200));

  const serialized = JSON.stringify({ root, batch });
  check("no credentials in bridge responses", !/password|api_key|bearer_token|Authorization/i.test(serialized));

  const closeResult = await call("/api/frames/close", { id: frame.id });
  check("frame close returns connection listing", Array.isArray(closeResult?.connections));
  let secondCloseRejected = false;
  try {
    await call("/api/frames/close", { id: frame.id });
  } catch {
    secondCloseRejected = true;
  }
  check("closing a removed frame is rejected", secondCloseRejected);

  console.log(`\n${passed} passed, ${failed} failed`);
  if (failed) process.exitCode = 1;
}

main().catch((error) => {
  console.error("DEV HOST CHECK ERROR:", error?.message || error);
  process.exitCode = 1;
});
