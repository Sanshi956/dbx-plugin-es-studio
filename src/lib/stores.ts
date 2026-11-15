/** Full navigation from requirement chapter 4. Items not delivered in P1 are
 *  DISABLED and phase-labelled - requirement 16.4 forbids decorative buttons. */
export type Phase = "P1" | "P2" | "P3" | "P4" | "P5";

export interface NavItem {
  id: string;
  label: string;
  phase: Phase;
  enabled: boolean;
}

export interface NavGroup {
  id: string;
  label: string;
  items: NavItem[];
}

export const NAV_GROUPS: NavGroup[] = [
  { id: "overview", label: "Overview", items: [{ id: "overview", label: "Cluster Overview", phase: "P1", enabled: true }] },
  {
    id: "cluster", label: "Cluster", items: [
      { id: "nodes", label: "Nodes", phase: "P2", enabled: true },
      { id: "shards", label: "Shards", phase: "P2", enabled: true },
      { id: "allocation", label: "Allocation", phase: "P3", enabled: false },
      { id: "cluster-settings", label: "Cluster Settings", phase: "P3", enabled: false },
    ],
  },
  {
    id: "indices", label: "Indices", items: [
      { id: "index-list", label: "Index List", phase: "P2", enabled: true },
      { id: "aliases", label: "Aliases", phase: "P2", enabled: true },
      { id: "templates", label: "Templates", phase: "P3", enabled: false },
      { id: "component-templates", label: "Component Templates", phase: "P3", enabled: false },
      { id: "data-streams", label: "Data Streams", phase: "P3", enabled: false },
      { id: "lifecycle", label: "Lifecycle", phase: "P3", enabled: false },
    ],
  },
  { id: "documents", label: "Documents", items: [{ id: "browser", label: "Browser", phase: "P2", enabled: true }] },
  {
    id: "query", label: "Query Studio", items: [
      { id: "query-builder", label: "Query Builder", phase: "P2", enabled: true },
      { id: "dsl-editor", label: "DSL Editor", phase: "P2", enabled: true },
      { id: "query-profiler", label: "Query Profiler", phase: "P3", enabled: false },
      { id: "analyzer", label: "Analyzer", phase: "P2", enabled: true },
    ],
  },
  { id: "rest", label: "REST Console", items: [{ id: "rest-console", label: "REST Console", phase: "P2", enabled: true }] },
  {
    id: "monitoring", label: "Monitoring", items: [
      { id: "cluster-metrics", label: "Cluster Metrics", phase: "P4", enabled: false },
      { id: "node-metrics", label: "Node Metrics", phase: "P4", enabled: false },
      { id: "history", label: "History", phase: "P4", enabled: false },
    ],
  },
  { id: "diagnostics", label: "Diagnostics", items: [{ id: "diagnostics", label: "Cluster Diagnostics", phase: "P4", enabled: false }] },
  { id: "snapshots", label: "Snapshots", items: [{ id: "snapshots", label: "Snapshots", phase: "P3", enabled: false }] },
  { id: "tasks", label: "Tasks", items: [{ id: "tasks", label: "Tasks", phase: "P3", enabled: false }] },
  { id: "compare", label: "Compare", items: [{ id: "compare", label: "Multi-Cluster Compare", phase: "P4", enabled: false }] },
  { id: "mcp", label: "MCP", items: [{ id: "mcp", label: "MCP Tool Service", phase: "P5", enabled: false }] },
];

export const DEFAULT_PAGE = "overview";
