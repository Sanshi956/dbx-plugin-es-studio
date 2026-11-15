// ES Studio Sidecar: the single backend executable declared in manifest.json.
// stdout is reserved for the stdio-jsonl protocol; all diagnostics go to stderr
// and contain no connection credentials.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/t8y2/dbx/plugins/sdk/go/dbx-plugin-sdk"
	"github.com/xihonglei/es-studio/backend/internal/collector"
	"github.com/xihonglei/es-studio/backend/internal/storage"
	"github.com/xihonglei/es-studio/backend/internal/transport"
)

const (
	pluginID = "local.es.studio"
	// Keep in sync with manifest.json — check-manifest.py asserts the two match.
	pluginVersion = "0.3.0"
)

func main() {
	dataDir, err := dbxpluginsdk.EnsureDataDir()
	if err != nil || dataDir == "" {
		// The official dev host does not set DBX_PLUGIN_DATA_DIR; monitoring
		// history still needs somewhere to live, and it must never land in the
		// plugin install directory (which is replaced on upgrade).
		dataDir = filepath.Join(os.TempDir(), "es-studio")
		fmt.Fprintln(os.Stderr, "[es-studio] no plugin data dir, using", dataDir)
	}
	store, err := storage.New(dataDir, storage.Options{})
	if err != nil {
		fmt.Fprintln(os.Stderr, "[es-studio] monitoring storage unavailable:", err)
		store = nil
	}
	var coll *collector.Collector
	if store != nil {
		coll = collector.New(store)
	}

	handler := transport.NewHandler(transport.Deps{Collector: coll, Store: store})
	server := dbxpluginsdk.NewServer(dbxpluginsdk.Metadata{
		ID:      pluginID,
		Version: pluginVersion,
		Capabilities: []string{
			"es/ping", "es/request", "es/batch", "es/test",
			"connection/test", "connection/connect", "connection/disconnect",
			"mon/start", "mon/stop", "mon/status", "mon/sample", "mon/history",
		},
	}, handler)
	if err := server.Serve(); err != nil {
		fmt.Fprintln(os.Stderr, "[es-studio] sidecar stopped:", err)
		os.Exit(1)
	}
}
