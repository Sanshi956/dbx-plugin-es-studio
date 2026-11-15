// Package mcp is the single tool layer shared by both MCP transports
// (docs/00-SPEC.md §2.1, requirement 6.2):
//
//	DBX built-in AI  → Sidecar  mcp/tools + mcp/call
//	external client  → cmd/es-studio-mcp  initialize / tools/list / tools/call
//
// Both adapters call Call(), which is the only place a tool becomes an
// Elasticsearch request. There is deliberately no second implementation: a bug
// fixed here is fixed for every client.
//
// Read-only by construction (requirement 14.4): every tool declares ReadOnly and
// the executor independently re-checks the HTTP method against the same
// safe-POST allow-list used by the permission layer. A tool that tries to write
// fails even if a future contributor forgets the annotation.
package mcp
