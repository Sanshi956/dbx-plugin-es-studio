/** Full navigation from requirement chapter 4.
 *
 *  Labels are i18n keys, not copy: Nav renders them through t() so switching
 *  the host language relabels the sidebar without a remount. The phase-era
 *  badges that used to sit here were build scaffolding and are gone. */
export interface NavItem {
  id: string;
  label: string;
}

export interface NavGroup {
  id: string;
  label: string;
  items: NavItem[];
}

export const NAV_GROUPS: NavGroup[] = [
  { id: "overview", label: "nav.group.overview", items: [{ id: "overview", label: "nav.overview" }] },
  {
    id: "cluster", label: "nav.group.cluster", items: [
      { id: "nodes", label: "nav.nodes" },
      { id: "shards", label: "nav.shards" },
      { id: "shard-diagnosis", label: "nav.shardDiag" },
      { id: "cluster-settings", label: "nav.clusterSettings" },
    ],
  },
  {
    id: "indices", label: "nav.group.indices", items: [
      { id: "index-list", label: "nav.indexList" },
      { id: "aliases", label: "nav.aliases" },
      { id: "templates", label: "nav.templates" },
      { id: "component-templates", label: "nav.componentTemplates" },
      { id: "data-streams", label: "nav.dataStreams" },
      { id: "lifecycle", label: "nav.lifecycle" },
    ],
  },
  { id: "documents", label: "nav.group.documents", items: [{ id: "browser", label: "nav.browser" }] },
  {
    id: "query", label: "nav.group.query", items: [
      { id: "query-builder", label: "nav.queryBuilder" },
      { id: "dsl-editor", label: "nav.dslEditor" },
      { id: "query-profiler", label: "nav.queryProfiler" },
      { id: "analyzer", label: "nav.analyzer" },
      { id: "rest-console", label: "nav.restConsole" },
    ],
  },
  {
    id: "monitoring", label: "nav.group.monitoring", items: [
      { id: "cluster-metrics", label: "nav.clusterMetrics" },
      { id: "node-metrics", label: "nav.nodeMetrics" },
      { id: "history", label: "nav.history" },
    ],
  },
  { id: "diagnostics", label: "nav.group.diagnostics", items: [{ id: "diagnostics", label: "nav.diagnostics" }] },
  { id: "snapshots", label: "nav.group.snapshots", items: [{ id: "snapshots", label: "nav.snapshots" }] },
  { id: "tasks", label: "nav.group.tasks", items: [{ id: "tasks", label: "nav.tasks" }] },
  { id: "mcp", label: "nav.group.mcp", items: [{ id: "mcp", label: "nav.mcp" }] },
];

export const DEFAULT_PAGE = "overview";
