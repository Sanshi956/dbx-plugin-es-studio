<script lang="ts">
  import { onMount, onDestroy } from "svelte";
  import Nav from "./components/Nav.svelte";
  import ClusterPicker from "./components/ClusterPicker.svelte";
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
  import Allocation from "./pages/Allocation.svelte";
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
  import { ready } from "./lib/bridge";
  import type { DbxBridge, ConnectionSummary } from "./lib/bridge";
  import { listConnections } from "./lib/api";
  import { NAV_GROUPS, DEFAULT_PAGE } from "./lib/stores";

  let bridge: DbxBridge | null = $state(null);
  let connections: ConnectionSummary[] = $state([]);
  let activeId: string = $state("");
  let page: string = $state(DEFAULT_PAGE);
  let booting: boolean = $state(true);
  let bootError: string = $state("");
  let unsubscribe: (() => void) | null = null;

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
        await host.workbench.setTitle("ES Studio");
        connections = await listConnections(host);

        const contextId = host.context?.connectionId;
        activeId = typeof contextId === "string" && contextId ? contextId : (connections[0]?.id ?? "");
        if (!activeId) {
          const saved = await host.storage.get<string>("es.studio.connectionId");
          if (typeof saved === "string") activeId = saved;
        }
        ensureConnection(activeId);
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
  });

  function changeConnection(id: string): void {
    activeId = id;
    if (bridge) void bridge.storage.set("es.studio.connectionId", id).catch(() => undefined);
  }
</script>

<div class="app">
  <header class="topbar">
    <div class="brand">
      <span class="logo">ES</span>
      <div><strong>ES Studio</strong><small>DBX Elasticsearch workbench</small></div>
    </div>
    <ClusterPicker
      connections={connections}
      activeId={activeId}
      busy={booting}
      onchange={changeConnection}
    />
    <div class="spacer"></div>
    <span class="phase-chip">P3 · Governance & Ops</span>
  </header>

  <div class="shell">
    <aside class="sidebar">
      <Nav groups={NAV_GROUPS} active={page} onselect={(id: string) => (page = id)} />
    </aside>
    <main>
      {#if booting}
        <StateBlock kind="loading" message="Waiting for the DBX host…" detail="window.dbxPlugin.ready" />
      {:else if bootError}
        <StateBlock
          kind="error"
          message="ES Studio must run inside DBX"
          detail={bootError === "NOT_IN_DBX_HOST"
            ? "The DBX host bridge is not available. Open this workbench from DBX or the official dbx-plugin dev host."
            : bootError}
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
        {:else if page === "allocation"}
          <Allocation bridge={bridge} connectionId={activeId} />
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
    font-size: 12px;
  }
  .brand strong { display: block; font-size: 14px; }
  .brand small { display: block; color: var(--es-muted); font-size: 11px; }
  .spacer { flex: 1; }
  .phase-chip {
    padding: 4px 10px;
    border: 1px solid var(--es-border);
    border-radius: 999px;
    color: var(--es-muted);
    font-size: 11px;
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
