// ES Studio Sidecar: the single backend executable declared in manifest.json.
// stdout is reserved for the stdio-jsonl protocol; all diagnostics go to stderr
// and contain no connection credentials.
package main

import (
	"fmt"
	"os"

	"github.com/t8y2/dbx/plugins/sdk/go/dbx-plugin-sdk"
	"github.com/xihonglei/es-studio/backend/internal/transport"
)

const (
	pluginID      = "local.es.studio"
	pluginVersion = "0.2.1"
)

func main() {
	handler := transport.NewHandler(transport.Deps{})
	server := dbxpluginsdk.NewServer(dbxpluginsdk.Metadata{
		ID:      pluginID,
		Version: pluginVersion,
		Capabilities: []string{
			"es/ping", "es/request", "es/batch", "es/test",
			"connection/test", "connection/connect", "connection/disconnect",
		},
	}, handler)
	if err := server.Serve(); err != nil {
		fmt.Fprintln(os.Stderr, "[es-studio] sidecar stopped:", err)
		os.Exit(1)
	}
}
