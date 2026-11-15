<script lang="ts">
  import StateBlock from "../components/StateBlock.svelte";
  import {
    CATEGORY_LABEL, callMcpTool, fetchMcpLog, grantConnection, listGrants, listMcpTools,
    mcpErrorText, revokeGrant,
    type McpCallResult, type McpCallRow, type McpGrantResult, type McpGrants, type McpLog,
    type McpToolSummary,
  } from "../lib/mcp";
  import type { DbxBridge } from "../lib/bridge";
  import { t, tOr } from "../lib/i18n.svelte.js";

  interface Props {
    bridge: DbxBridge;
    connectionId: string;
  }
  let { bridge, connectionId }: Props = $props();

  let catalogue = $state<McpToolSummary[]>([]);
  let categories = $state<Record<string, number>>({});
  let grants = $state<McpGrants>({ grants: [], count: 0 });
  let log = $state<McpLog>({ calls: [], count: 0 });
  let loading = $state(true);
  let error = $state("");
  let toast = $state("");

  let filter = $state("");
  let ttlDays = $state(30);
  let selected = $state<McpToolSummary | null>(null);
  let argsText = $state("{}");
  let callResult = $state<McpCallResult | null>(null);
  let callError = $state("");
  let calling = $state(false);
  let issued = $state<McpGrantResult | null>(null);

  let visible = $derived.by(() => {
    const needle = filter.trim().toLowerCase();
    if (!needle) return catalogue;
    return catalogue.filter((tool) =>
      (tool.name + " " + tool.title + " " + tool.description + " " + tool.category).toLowerCase().includes(needle));
  });

  let grouped = $derived.by(() => {
    const order = ["cluster", "index", "doc", "template", "stream", "ilm", "snapshot", "task", "advanced"];
    const map = new Map<string, McpToolSummary[]>();
    for (const tool of visible) {
      const bucket = map.get(tool.category) ?? [];
      bucket.push(tool);
      map.set(tool.category, bucket);
    }
    return order.filter((key) => map.has(key)).map((key) => ({ key, label: CATEGORY_LABEL[key] ? t(CATEGORY_LABEL[key]) : key, tools: map.get(key)! }));
  });

  let grantForThis = $derived(grants.grants.find((grant) => grant.connection_id === connectionId));

  function say(message: string): void {
    toast = message;
    window.setTimeout(() => { if (toast === message) toast = ""; }, 4000);
  }

  async function load(): Promise<void> {
    loading = true;
    error = "";
    try {
      const [tools, grantList, calls] = await Promise.all([
        listMcpTools(bridge),
        listGrants(bridge),
        fetchMcpLog(bridge, 50),
      ]);
      catalogue = tools.catalogue ?? [];
      categories = tools.categories ?? {};
      grants = grantList;
      log = calls;
    } catch (cause) {
      error = mcpErrorText(cause);
    } finally {
      loading = false;
    }
  }

  $effect(() => { void load(); });

  /** Prefill the argument object from the tool's own declaration. */
  function templateFor(tool: McpToolSummary): string {
    const draft: Record<string, unknown> = {};
    for (const arg of tool.args) {
      if (arg.type === "object") draft[arg.name] = arg.name === "query" ? { match_all: {} } : {};
      else if (arg.type === "array") draft[arg.name] = [];
      else if (arg.type === "integer") draft[arg.name] = arg.default ?? 10;
      else if (arg.type === "boolean") draft[arg.name] = arg.default ?? false;
      else draft[arg.name] = arg.enum?.[0] ?? arg.default ?? "";
    }
    return JSON.stringify(draft, null, 2);
  }

  function choose(tool: McpToolSummary): void {
    selected = tool;
    argsText = templateFor(tool);
    callResult = null;
    callError = "";
  }

  async function runTool(): Promise<void> {
    if (!selected) return;
    calling = true;
    callError = "";
    callResult = null;
    try {
      let args: Record<string, unknown> = {};
      if (argsText.trim() !== "") args = JSON.parse(argsText) as Record<string, unknown>;
      callResult = await callMcpTool(bridge, connectionId, selected.name, args);
      log = await fetchMcpLog(bridge, 50);
    } catch (cause) {
      callError = cause instanceof SyntaxError ? t("mcp.badJson") : mcpErrorText(cause);
    } finally {
      calling = false;
    }
  }

  async function authorize(): Promise<void> {
    issued = null;
    try {
      issued = await grantConnection(bridge, connectionId, ttlDays);
      grants = await listGrants(bridge);
      say(t("mcp.authorized"));
      await copy(issued.snippet);
    } catch (cause) {
      say(mcpErrorText(cause));
    }
  }

  async function revoke(connectionID: string): Promise<void> {
    try {
      await revokeGrant(bridge, connectionID);
      grants = await listGrants(bridge);
      if (issued?.public.connection_id === connectionID) issued = null;
      say(t("mcp.revoked"));
    } catch (cause) {
      say(mcpErrorText(cause));
    }
  }

  async function copy(text: string): Promise<void> {
    try {
      await navigator.clipboard.writeText(text);
      say(t("mcp.copied"));
    } catch {
      say(t("mcp.copyFailed"));
    }
  }

  function stamp(value: number): string {
    if (!value) return "—";
    const date = new Date(value);
    return `${date.getMonth() + 1}/${date.getDate()} ${String(date.getHours()).padStart(2, "0")}:${String(date.getMinutes()).padStart(2, "0")}`;
  }
</script>

<div class="page">
  <header>
    <div>
      <h1>{t("mcp.title")}</h1>
      <p>{t("mcp.hint", { tools: catalogue.length, cats: Object.keys(categories).length })}</p>
    </div>
    <div class="tools">
      <button onclick={() => void load()} disabled={loading}>{t("common.refresh")}</button>
    </div>
  </header>

  {#if toast}<div class="toast">{toast}</div>{/if}
  {#if error}
    <StateBlock kind="error" message={t("mcp.serviceDown")} detail={error} />
  {:else}
    <section class="summary">
      <div class="card">
        <span class="lbl">{t("mcp.card.tools")}</span>
        <span class="num-value">{catalogue.length}</span>
      </div>
      <div class="card">
        <span class="lbl">{t("mcp.card.write")}</span>
        <span class="num-value">0</span>
      </div>
      <div class="card">
        <span class="lbl">{t("mcp.card.grants")}</span>
        <span class="num-value">{grants.count}</span>
      </div>
      <div class="card">
        <span class="lbl">{t("mcp.card.calls")}</span>
        <span class="num-value">{log.count}</span>
      </div>
    </section>

    <section class="panel">
      <div class="panel-head">
        <h2>{t("mcp.grants.title")}</h2>
        <div class="inline">
          <select bind:value={ttlDays} title={t("mcp.ttl")}>
            <option value={7}>{t("mcp.days", { n: 7 })}</option>
            <option value={30}>{t("mcp.days", { n: 30 })}</option>
            <option value={90}>{t("mcp.days", { n: 90 })}</option>
          </select>
          <button class="primary" onclick={() => void authorize()} disabled={!connectionId}>
            {grantForThis ? t("mcp.reauthorize") : t("mcp.authorize")}
          </button>
        </div>
      </div>
      {#if grants.count === 0}
        <p class="hint">{t("mcp.grants.empty")}</p>
      {:else}
        <table class="grants">
          <thead>
            <tr>
              <th>{t("mcp.col.connection")}</th>
              <th>{t("mcp.col.endpoint")}</th>
              <th title={t("mcp.tip.fingerprint")}>{t("mcp.col.fingerprint")}</th>
              <th>{t("mcp.col.granted")}</th>
              <th>{t("mcp.col.expires")}</th>
              <th class="right">{t("mcp.col.actions")}</th>
            </tr>
          </thead>
          <tbody>
            {#each grants.grants as grant (grant.connection_id)}
              <tr class:expired={grant.expired}>
                <td>
                  {grant.label || grant.connection_id}
                  <div class="sub">{grant.connection_id}</div>
                </td>
                <td class="mono">{grant.host}:{grant.port}</td>
                <td class="mono">{grant.fingerprint}</td>
                <td class="mono">{stamp(grant.created_at)}</td>
                <td class="mono">{grant.expired ? t("mcp.expired") : stamp(grant.expires_at)}</td>
                <td class="right">
                  <button class="link" onclick={() => void revoke(grant.connection_id)}>{t("mcp.revoke")}</button>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}

      {#if issued}
        <div class="issued">
          <div class="issued-head">
            <strong>{t("mcp.issued.title")}</strong>
            <button class="link" onclick={() => void copy(issued!.snippet)}>{t("mcp.copy")}</button>
          </div>
          <p class="hint">
            {t("mcp.issued.note", { token: `${issued.token.slice(0, 12)}…`, fingerprint: issued.public.fingerprint })}
          </p>
          <textarea class="snippet" readonly rows="9">{issued.snippet}</textarea>
          <p class="hint">{t("mcp.issued.check")}</p>
        </div>
      {/if}
    </section>

    {#if selected}
      <section class="panel call">
        <div class="panel-head">
          <h2>{t("mcp.try")} · <span class="mono">{selected.name}</span></h2>
          <button class="primary" onclick={() => void runTool()} disabled={calling || !connectionId}>
            {calling ? t("mcp.call.running") : t("mcp.call.run")}
          </button>
        </div>
        <p class="hint">{tOr(`mcp.tool.${selected.name}.desc`, selected.description)}</p>
        {#if selected.args.length > 0}
          <p class="hint">{t("mcp.call.args", { list: selected.args.map((arg) => `${arg.name}${arg.required ? t("mcp.call.required") : ""} ${arg.type}`).join(t("common.listSep")) })}</p>
        {/if}
        <textarea class="args-input" rows="6" bind:value={argsText} spellcheck="false"></textarea>
        {#if callError}
          <p class="call-error">{callError}</p>
        {/if}
        {#if callResult}
          <p class="call-meta">
            <span class="mono">{callResult.request.method} {callResult.request.path}</span>
            · HTTP {callResult.status} · {callResult.duration_ms} ms · {callResult.bytes} B
            {#if callResult.truncated}<span class="warn">{t("mcp.call.truncated")}</span>{/if}
          </p>
          <pre class="result">{callResult.text}</pre>
        {/if}
      </section>
    {/if}

    <section class="panel">
      <div class="panel-head">
        <h2>{t("mcp.catalog.title")}</h2>
        <input class="search" placeholder={t("mcp.catalog.filter")} bind:value={filter} />
      </div>
      {#if visible.length === 0}
        <p class="hint">{t("mcp.catalog.empty")}</p>
      {:else}
        <table class="tools">
          <thead>
            <tr>
              <th>{t("mcp.col.tool")}</th>
              <th class="col-args">{t("mcp.col.args")}</th>
              <th class="col-act"></th>
            </tr>
          </thead>
          <tbody>
            {#each grouped as group (group.key)}
              <tr class="group-row">
                <td colspan="3">{group.label} <span class="count">{group.tools.length}</span></td>
              </tr>
              {#each group.tools as tool (tool.name)}
                <tr class:active={selected?.name === tool.name}>
                  <td>
                    <span class="mono name">{tool.name}</span>
                    <div class="sub">{tOr(`mcp.tool.${tool.name}`, tool.title)} · {tOr(`mcp.tool.${tool.name}.desc`, tool.description)}</div>
                  </td>
                  <td class="args">
                    {#if tool.args.length === 0}
                      <span class="muted">—</span>
                    {:else}
                      {tool.args.map((arg) => arg.name + (arg.required ? "*" : "")).join(" · ")}
                    {/if}
                  </td>
                  <td class="right">
                    <button class="link" onclick={() => choose(tool)}>{t("mcp.try")}</button>
                  </td>
                </tr>
              {/each}
            {/each}
          </tbody>
        </table>
      {/if}
    </section>

    <section class="panel">
      <div class="panel-head">
        <h2>{t("mcp.log.title")}</h2>
        <span class="hint mono">{log.file ?? "—"}{log.bytes ? ` · ${log.bytes} B` : ""}</span>
      </div>
      {#if log.calls.length === 0}
        <p class="hint">{t("mcp.log.empty")}</p>
      {:else}
        <table class="calls">
          <thead>
            <tr>
              <th>{t("mcp.col.time")}</th>
              <th>{t("mcp.col.tool")}</th>
              <th>{t("mcp.col.connection")}</th>
              <th class="right">{t("mcp.col.status")}</th>
              <th class="right">{t("mcp.col.duration")}</th>
              <th class="right">{t("mcp.col.size")}</th>
            </tr>
          </thead>
          <tbody>
            {#each [...log.calls].reverse() as row (row.at + row.tool)}
              <tr class:failed={!row.ok}>
                <td class="mono">{stamp(row.at)}</td>
                <td class="mono">{row.tool}</td>
                <td class="mono sub">{row.connection ?? "—"}</td>
                <td class="right mono">{row.status ?? "ERR"}</td>
                <td class="right mono">{row.ms ?? 0} ms</td>
                <td class="right mono">{row.bytes ?? 0} B</td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
    </section>
  {/if}
</div>

<style>
  .page { max-width: 1180px; }
  header { display: flex; justify-content: space-between; align-items: start; gap: 16px; margin-bottom: 12px; flex-wrap: wrap; }
  h1 { margin: 0 0 4px; font-size: var(--es-fs-3xl); }
  header p { margin: 0; color: var(--es-muted); }
  .tools { display: flex; gap: 8px; align-items: center; }
  button { padding: 5px 12px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface); color: var(--es-text); cursor: pointer; white-space: nowrap; }
  button:hover:not(:disabled):not(.primary) { background: var(--es-surface-2); }
  button:disabled { opacity: 0.55; cursor: default; }
  button.primary { background: var(--es-accent); border-color: var(--es-accent); color: #fff; }
  button.primary:hover:not(:disabled) { filter: brightness(0.92); }
  button.link { border: none; background: none; color: var(--es-accent); padding: 0; }
  .toast { padding: 8px 12px; margin-bottom: 12px; border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-surface-2); font-size: var(--es-fs-sm); }
  .summary { display: grid; grid-template-columns: repeat(auto-fit, minmax(150px, 1fr)); gap: 10px; margin-bottom: 14px; }
  .card { border: 1px solid var(--es-border); border-radius: var(--es-radius); background: var(--es-surface); padding: 10px 12px; display: flex; flex-direction: column; gap: 4px; }
  .card .lbl { font-size: var(--es-fs-xs); color: var(--es-muted); text-transform: uppercase; letter-spacing: 0.06em; }
  .card .num-value { font-family: var(--es-mono); font-size: var(--es-fs-4xl); font-weight: 700; }
  .panel { border: 1px solid var(--es-border); border-radius: var(--es-radius); background: var(--es-surface); padding: 10px 14px; margin-bottom: 14px; overflow-x: auto; }
  .panel-head { display: flex; justify-content: space-between; align-items: center; gap: 12px; }
  .panel-head h2 { margin: 0 0 6px; font-size: var(--es-fs-sm); text-transform: uppercase; letter-spacing: 0.08em; color: var(--es-muted); }
  .inline { display: flex; gap: 8px; align-items: center; }
  select, input.search, textarea { border: 1px solid var(--es-border); border-radius: 6px; background: var(--es-bg); color: var(--es-text); padding: 5px 8px; font-size: var(--es-fs-sm); font-family: inherit; }
  input.search { width: 240px; }
  table { width: 100%; border-collapse: collapse; font-size: var(--es-fs-sm); margin-top: 6px; }
  th, td { padding: 6px 10px; border-bottom: 1px solid var(--es-border); text-align: left; vertical-align: top; }
  th { background: var(--es-surface-2); color: var(--es-muted); font-weight: 600; font-size: var(--es-fs-xs); }
  /* 窄容器（DBX 侧栏 + iframe 挤压）下，auto 表格可能把某列压到几个像素，
     表头就会逐字换行成竖排 —— 表头永远整词显示，列用 min-width 托底。 */
  th { white-space: nowrap; }
  .tools td:first-child, .calls td:nth-child(2) { min-width: 220px; }
  tbody tr:hover { background: var(--es-surface-2); }
  .right { text-align: right; }
  .mono { font-family: var(--es-mono); }
  .sub { font-size: var(--es-fs-xs); color: var(--es-muted); margin-top: 2px; }
  .name { font-weight: 600; }
  .muted { color: var(--es-muted); }
  .hint { font-size: var(--es-fs-xs); color: var(--es-muted); margin: 6px 0; }
  .col-args { width: 30%; }
  .col-act { width: 78px; }
  tr.group-row td { background: var(--es-surface-2); color: var(--es-muted); font-size: var(--es-fs-xs); font-weight: 600; letter-spacing: 0.04em; text-transform: uppercase; padding: 5px 10px; }
  .args { font-family: var(--es-mono); font-size: var(--es-fs-xs); color: var(--es-muted); }
  tr.group-row .count { color: var(--es-muted); font-weight: 400; margin-left: 4px; }
  tr.expired td { opacity: 0.6; }
  tr.active td { background: var(--es-surface-2); }
  tr.failed td { color: var(--es-red); }
  .issued { margin-top: 10px; border-top: 1px dashed var(--es-border); padding-top: 8px; }
  .issued-head { display: flex; justify-content: space-between; align-items: center; font-size: var(--es-fs-sm); }
  textarea.snippet { width: 100%; font-family: var(--es-mono); font-size: var(--es-fs-xs); }
  textarea.args-input { width: 100%; font-family: var(--es-mono); font-size: var(--es-fs-xs); }
  .call-error { font-size: var(--es-fs-sm); color: var(--es-red); margin: 6px 0; }
  .call-meta { font-size: var(--es-fs-xs); color: var(--es-muted); margin: 8px 0 4px; }
  .call-meta .warn { color: var(--es-yellow); }
  pre.result { margin: 0; max-height: 320px; overflow: auto; border: 1px solid var(--es-border); border-radius: 6px; padding: 10px; background: var(--es-bg); font-family: var(--es-mono); font-size: var(--es-fs-xs); white-space: pre-wrap; }
</style>
