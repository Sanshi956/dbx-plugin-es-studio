// Package mcpcmd is the shared entry point for the MCP stdio server, used by the
// packaged sidecar (`es-studio-sidecar --mcp`) and by the standalone
// `cmd/es-studio-mcp` binary (docs/00-SPEC.md §2.1).
//
// One implementation, two launch paths: the packaged plugin can only declare a
// single backend executable, so the shipped path is `--mcp`; the standalone
// binary exists for people who want a dedicated process.
package mcpcmd

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/xihonglei/es-studio/backend/internal/mcp"
	"github.com/xihonglei/es-studio/backend/internal/mcpserver"
	"github.com/xihonglei/es-studio/backend/internal/storage"
)

// Run executes the MCP server and returns a process exit code.
//
// stdout is the protocol channel: every diagnostic (including fatal errors) goes
// to stderr so a client never receives a non-JSON line.
func Run(args []string, version string) int {
	flags := flag.NewFlagSet("es-studio-mcp", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	dataDir := flags.String("data-dir", "", "plugin data dir that holds the sealed grants (required if DBX_PLUGIN_DATA_DIR is unset)")
	token := flags.String("token", "", "authorisation token (defaults to ES_STUDIO_MCP_TOKEN)")
	list := flags.Bool("list", false, "list authorised connections and exit")
	check := flags.Bool("check", false, "validate the token and exit")
	if err := flags.Parse(args); err != nil {
		return 2
	}

	key := *token
	if key == "" {
		key = os.Getenv("ES_STUDIO_MCP_TOKEN")
	}
	dir := *dataDir
	if dir == "" {
		dir = os.Getenv("DBX_PLUGIN_DATA_DIR")
	}
	if dir == "" {
		dir = filepath.Join(os.TempDir(), "es-studio")
	}
	store := mcp.NewGrantStore(dir)

	if *list {
		grants, err := store.List()
		if err != nil {
			fmt.Fprintln(os.Stderr, "[es-studio-mcp] cannot read grants:", err)
			return 1
		}
		if len(grants) == 0 {
			fmt.Fprintln(os.Stderr, "[es-studio-mcp] no authorised connections in", dir)
			return 0
		}
		for _, grant := range grants {
			public := grant.Public(time.Now())
			state := "valid"
			if public.Expired {
				state = "expired"
			}
			fmt.Fprintf(os.Stderr, "[es-studio-mcp] %s  %s:%d  fp=%s  %s\n",
				public.ConnectionID, public.Host, public.Port, public.Fingerprint, state)
		}
		return 0
	}

	grant, err := store.Find(key)
	if err != nil {
		fmt.Fprintln(os.Stderr, "[es-studio-mcp]", err)
		fmt.Fprintln(os.Stderr, "[es-studio-mcp] open ES Studio → MCP Tool Service, authorise the connection,")
		fmt.Fprintln(os.Stderr, "[es-studio-mcp] then put the token in ES_STUDIO_MCP_TOKEN (or --token).")
		if grants, _ := store.List(); len(grants) == 0 {
			fmt.Fprintln(os.Stderr, "[es-studio-mcp] no grant file found under", dir)
		}
		return 2
	}
	if *check {
		fmt.Fprintf(os.Stderr, "[es-studio-mcp] token ok for %s (%s:%d)\n",
			grant.ConnectionID, grant.Host, grant.Port)
		return 0
	}

	var logger *mcp.Logger
	if store, err := storage.New(dir, storage.Options{}); err == nil {
		logger = mcp.NewLogger(store)
	} else {
		fmt.Fprintln(os.Stderr, "[es-studio-mcp] audit log unavailable:", err)
	}

	server := &mcpserver.Server{
		Caller:  mcp.NewCaller(mcp.Default(), logger),
		Grant:   grant,
		Version: version,
		In:      os.Stdin,
		Out:     os.Stdout,
		Logf: func(format string, a ...any) {
			fmt.Fprintf(os.Stderr, "[es-studio-mcp] "+format+"\n", a...)
		},
	}
	fmt.Fprintf(os.Stderr, "[es-studio-mcp] serving %s (%s:%d), read-only, %d tools\n",
		grant.ConnectionID, grant.Host, grant.Port, server.Caller.Registry().Len())
	if err := server.Serve(); err != nil && err != io.EOF {
		fmt.Fprintln(os.Stderr, "[es-studio-mcp] stopped:", err)
		return 1
	}
	return 0
}
