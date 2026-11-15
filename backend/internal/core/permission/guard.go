package permission

import (
	"errors"
	"fmt"
	"strings"

	"github.com/xihonglei/es-studio/backend/internal/core/connection"
	"github.com/xihonglei/es-studio/backend/internal/core/es"
)

// ErrReadOnlyViolation is returned at the service layer, never merely hidden
// behind a disabled UI button (requirement 10.4).
var ErrReadOnlyViolation = errors.New("connection is read-only: write operation denied")

// ErrUnsafePath is shared with the ES adapter so callers see one contract.
var ErrUnsafePath = es.ErrUnsafePath

// POST endpoints that are read-only semantically and therefore allowed even on
// a read-only connection (search, analysis, validation, explain).
var safePOSTSuffixes = []string{
	"/_search", "/_msearch", "/_count", "/_analyze", "/_validate/query",
	"/_explain", "/_field_caps", "/_rank_eval", "/_scripts/painless/_execute",
	"/_search/template", "/_msearch/template", "/_pit", // PIT close is POST but harmless to data
}

// ValidatePath delegates to the single path guard in the ES adapter.
func ValidatePath(path string) error {
	return es.ValidatePath(path)
}

// Check enforces read-only mode and path safety BEFORE any request is sent.
func Check(conn connection.Config, req es.Request) error {
	if err := ValidatePath(req.Path); err != nil {
		return err
	}
	if !conn.ReadOnly {
		return nil
	}
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = "GET"
	}
	if method == "GET" || method == "HEAD" || method == "OPTIONS" {
		return nil
	}
	if method == "POST" && isSafePOST(req.Path) {
		return nil
	}
	return fmt.Errorf("%w: %s %s", ErrReadOnlyViolation, method, req.Path)
}

func isSafePOST(path string) bool {
	clean := path
	if i := strings.IndexByte(clean, '?'); i >= 0 {
		clean = clean[:i]
	}
	for _, suffix := range safePOSTSuffixes {
		if strings.HasSuffix(clean, suffix) {
			return true
		}
	}
	return false
}

// Risk is the structured description the UI must display before a dangerous
// operation. It never performs or suggests an automatic fix.
type Risk struct {
	Name   string `json:"name"`
	Target string `json:"target"`
	Scope  string `json:"scope"`
	Reason string `json:"reason"`
}

// DescribeRisk flags writes and high-impact cluster operations.
func DescribeRisk(req es.Request) (Risk, bool) {
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" || method == "GET" || method == "HEAD" || method == "OPTIONS" {
		return Risk{}, false
	}
	path := req.Path
	clean := path
	if i := strings.IndexByte(clean, '?'); i >= 0 {
		clean = clean[:i]
	}
	target := clean
	name, scope, reason := "Write Operation", "target resource",
		"This request changes data or cluster state on the selected connection."
	switch {
	case strings.Contains(clean, "/_reindex"):
		name, scope, reason = "Reindex", "all documents matched by the source",
			"Reindex copies documents and can saturate CPU, disk and indexing throughput."
	case strings.Contains(clean, "/_cluster/reroute") || strings.Contains(clean, "/_reroute"):
		name, scope, reason = "Cluster Reroute", "shard allocation across the whole cluster",
			"Manual reroute can move shards and destabilise cluster balance."
	case strings.Contains(clean, "/_cluster/settings"):
		name, scope, reason = "Update Cluster Settings", "entire cluster",
			"Cluster-level settings affect every node and index."
	case strings.Contains(clean, "/_snapshot/") && method == "POST":
		name, scope, reason = "Restore Snapshot", "indices selected for restore",
			"Restoring can overwrite existing indices and aliases."
	case strings.Contains(clean, "/_forcemerge") || strings.Contains(clean, "/_optimize"):
		name, scope, reason = "Force Merge", "segments of the target index",
			"Force merge is I/O intensive and can block indexing."
	case method == "DELETE":
		name, scope, reason = "Delete Resource", "the target index / document / snapshot",
			"Deletion is irreversible and removes data or configuration."
	case strings.Contains(clean, "/_close"):
		name, scope, reason = "Close Index", "the target index",
			"Closing an index makes its data unavailable until reopened."
	case strings.Contains(clean, "/_open"):
		name, scope, reason = "Open Index", "the target index",
			"Opening a previously closed index restores its availability."
	case strings.Contains(clean, "/_aliases"):
		name, scope, reason = "Modify Aliases", "the index alias mapping",
			"Changing aliases reroutes queries and can hide or expose indices."
	}
	return Risk{Name: name, Target: target, Scope: scope, Reason: reason}, true
}
