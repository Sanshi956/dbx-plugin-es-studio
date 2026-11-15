// es-studio-mcp is the standalone MCP stdio server (docs/00-SPEC.md §2.1).
//
// It is the same server the packaged sidecar runs with `--mcp`; this binary
// exists for users who prefer a dedicated process per MCP client. It reads only
// its own sealed grant file and never touches DBX internals.
package main

import (
	"os"

	"github.com/xihonglei/es-studio/backend/internal/mcpcmd"
)

// Keep in sync with manifest.json / backend/main.go.
const version = "0.4.0"

func main() {
	os.Exit(mcpcmd.Run(os.Args[1:], version))
}
