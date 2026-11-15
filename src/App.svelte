<script lang="ts">
  import { onMount, onDestroy } from "svelte";
  import Nav from "./components/Nav.svelte";
  import StateBlock from "./components/StateBlock.svelte";
  import Overview from "./pages/Overview.svelte";
  import Nodes from "./pages/Nodes.svelte";
  import Shards from "./pages/Shards.svelte";
  import IndexList from "./pages/IndexList.svelte";
  import Aliases from "./pages/Aliases.svelte";
  import Browser from "./pages/Browser.svelte";
  import QueryBuilder from "./pages/QueryBuilder.svelte";
  import DslEditor from "./pages/DslEditor.svelte";
  import Analyzer from "./pages/Analyzer.svelte";
  import RestConsole from "./pages/RestConsole.svelte";
  import ClusterSettings from "./pages/ClusterSettings.svelte";
  import Templates from "./pages/Templates.svelte";
  import ComponentTemplates from "./pages/ComponentTemplates.svelte";
  import DataStreams from "./pages/DataStreams.svelte";
  import Lifecycle from "./pages/Lifecycle.svelte";
  import QueryProfiler from "./pages/QueryProfiler.svelte";
  import Snapshots from "./pages/Snapshots.svelte";
  import Tasks from "./pages/Tasks.svelte";
  import ClusterMetrics from "./pages/ClusterMetrics.svelte";
  import NodeMetrics from "./pages/NodeMetrics.svelte";
  import History from "./pages/History.svelte";
  import McpService from "./pages/McpService.svelte";
  import Diagnostics from "./pages/Diagnostics.svelte";
  import { ready } from "./lib/bridge";
  import type { DbxBridge, ConnectionSummary } from "./lib/bridge";
  import { listConnections } from "./lib/api";
  import { NAV_GROUPS, DEFAULT_PAGE } from "./lib/stores";
  import { t, normalizeLocale, setLocale } from "./lib/i18n.svelte.js";
  import { syncTypeScale } from "./lib/hostTheme";

  let bridge: DbxBridge | null = $state(null);
  let connections: ConnectionSummary[] = $state([]);
  let activeId: string = $state("");
  let page: string = $state(DEFAULT_PAGE);
  let booting: boolean = $state(true);
  let bootError: string = $state("");
  let unsubscribe: (() => void) | null = null;
  let unsubscribeEnv: (() => void) | null = null;

  function ensureConnection(id: string): void {
    // The host context is authoritative for the frame's active connection. In
    // the official dev host (and whenever host.connections:read returns a
    // partial view) that id may be absent from getConnections(), so keep the
    // picker in sync instead of showing an empty selection.
    if (!id || connections.some((row) => row.id === id)) return;
    connections = [{ id, name: "Current cluster", connected: true }, ...connections];
  }

  onMount(() => {
    void (async () => {
      try {
        const host = await ready(); // never touch the bridge before host init
        bridge = host;
        setLocale(normalizeLocale(host.locale));
        syncTypeScale(host.theme);
        // The host re-sends locale + theme in an `env` message when the user
        // switches language; the runtime mirrors it into bridge.locale and
        // dispatches this event, so the UI flips without a remount.
        const onEnv = (): void => {
          setLocale(normalizeLocale(host.locale));
          syncTypeScale(host.theme);
        };
        window.addEventListener("dbx-plugin-env", onEnv);
        unsubscribeEnv = () => window.removeEventListener("dbx-plugin-env", onEnv);
        // The workbench tab label is the only place the user can tell two
        // plugin frames apart, so it carries the connection name, not the
        // generic product name (DBX would otherwise number tabs "ES Studio 1/2").
        connections = await listConnections(host);

        const contextId = host.context?.connectionId;
        activeId = typeof contextId === "string" && contextId ? contextId : (connections[0]?.id ?? "");
        if (!activeId) {
          const saved = await host.storage.get<string>("es.studio.connectionId");
          if (typeof saved === "string") activeId = saved;
        }
        ensureConnection(activeId);
        const active = connections.find((row) => row.id === activeId);
        await host.workbench.setTitle(active && active.name !== "Current cluster" ? active.name : "ES Studio");
        unsubscribe = host.onContext((context) => {
          if (typeof context.connectionId === "string" && context.connectionId) {
            activeId = context.connectionId;
            ensureConnection(activeId);
          }
        });
      } catch (cause) {
        bootError = cause instanceof Error ? cause.message : String(cause);
      } finally {
        booting = false;
      }
    })();
  });

  onDestroy(() => {
    unsubscribe?.();
    unsubscribeEnv?.();
  });
</script>

<div class="app">
  <header class="topbar">
    <div class="brand">
      <span class="logo">ES</span>
      <div><strong>ES Studio</strong><small>{t("app.tagline")}</small></div>
    </div>
    <div class="spacer"></div>
    <span class="version-chip">v{__PLUGIN_VERSION__}</span>
  </header>

  <div class="shell">
    <aside class="sidebar">
      <Nav groups={NAV_GROUPS} active={page} onselect={(id: string) => (page = id)} />
    </aside>
    <main>
      {#if booting}
        <StateBlock kind="loading" message={t("app.boot.waiting")} detail={t("app.boot.detail")} />
      {:else if bootError}
        <StateBlock
          kind="error"
          message={t("app.error.title")}
          detail={bootError === "NOT_IN_DBX_HOST" ? t("app.error.notInHost") : bootError}
        />
      {:else if bridge}
        {#if page === "overview"}
          <Overview bridge={bridge} connectionId={activeId} />
        {:else if page === "nodes"}
          <Nodes bridge={bridge} connectionId={activeId} />
        {:else if page === "shards"}
          <Shards bridge={bridge} connectionId={activeId} />
        {:else if page === "index-list"}
          <IndexList bridge={bridge} connectionId={activeId} />
        {:else if page === "aliases"}
          <Aliases bridge={bridge} connectionId={activeId} />
        {:else if page === "browser"}
          <Browser bridge={bridge} connectionId={activeId} />
        {:else if page === "query-builder"}
          <QueryBuilder bridge={bridge} connectionId={activeId} />
        {:else if page === "dsl-editor"}
          <DslEditor bridge={bridge} connectionId={activeId} />
        {:else if page === "analyzer"}
          <Analyzer bridge={bridge} connectionId={activeId} />
        {:else if page === "rest-console"}
          <RestConsole bridge={bridge} connectionId={activeId} />
        {:else if page === "cluster-settings"}
          <ClusterSettings bridge={bridge} connectionId={activeId} />
        {:else if page === "templates"}
          <Templates bridge={bridge} connectionId={activeId} />
        {:else if page === "component-templates"}
          <ComponentTemplates bridge={bridge} connectionId={activeId} />
        {:else if page === "data-streams"}
          <DataStreams bridge={bridge} connectionId={activeId} />
        {:else if page === "lifecycle"}
          <Lifecycle bridge={bridge} connectionId={activeId} />
        {:else if page === "query-profiler"}
          <QueryProfiler bridge={bridge} connectionId={activeId} />
        {:else if page === "snapshots"}
          <Snapshots bridge={bridge} connectionId={activeId} />
        {:else if page === "tasks"}
          <Tasks bridge={bridge} connectionId={activeId} />
        {:else if page === "cluster-metrics"}
          <ClusterMetrics bridge={bridge} connectionId={activeId} />
        {:else if page === "node-metrics"}
          <NodeMetrics bridge={bridge} connectionId={activeId} />
        {:else if page === "history"}
          <History bridge={bridge} connectionId={activeId} />
        {:else if page === "diagnostics"}
          <Diagnostics bridge={bridge} connectionId={activeId} />
        {:else if page === "mcp"}
          <McpService bridge={bridge} connectionId={activeId} />
        {/if}
      {/if}
    </main>
  </div>
</div>

<style>
  .app { display: flex; flex-direction: column; height: 100vh; }
  .topbar {
    display: flex;
    align-items: center;
    gap: 20px;
    padding: 10px 18px;
    border-bottom: 1px solid var(--es-border);
    background: var(--es-surface);
    flex: none;
  }
  .brand { display: flex; align-items: center; gap: 10px; }
  .logo {
    display: grid;
    place-items: center;
    width: 30px;
    height: 30px;
    border-radius: 8px;
    background: var(--es-accent);
    color: var(--es-surface);
    font-weight: 800;
    font-size: var(--es-fs-sm);
  }
  .brand strong { display: block; font-size: var(--es-fs-lg); }
  .brand small { display: block; color: var(--es-muted); font-size: var(--es-fs-xs); }
  .spacer { flex: 1; }
  .version-chip {
    padding: 4px 10px;
    border: 1px solid var(--es-border);
    border-radius: 999px;
    color: var(--es-muted);
    font-size: var(--es-fs-xs);
    font-family: var(--es-mono);
  }
  .shell { display: flex; flex: 1; min-height: 0; }
  .sidebar {
    width: 210px;
    flex: none;
    overflow-y: auto;
    border-right: 1px solid var(--es-border);
    background: var(--es-surface);
  }
  main { flex: 1; overflow-y: auto; padding: 22px 26px; }
</style>
