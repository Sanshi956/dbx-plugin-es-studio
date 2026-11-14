module github.com/xihonglei/es-studio/backend

go 1.22

require github.com/t8y2/dbx/plugins/sdk/go/dbx-plugin-sdk v0.1.0

// Vendor the official SDK so `go build` works fully offline / on a clean machine.
replace github.com/t8y2/dbx/plugins/sdk/go/dbx-plugin-sdk => ./internal/dbxpluginsdk
