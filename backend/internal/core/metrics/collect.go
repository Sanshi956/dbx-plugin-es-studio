// Package metrics collects one monitoring sample from a live cluster.
//
// Three read-only endpoints are enough for the whole P4 metric surface, and all
// of them exist on 7.x / 8.x / OpenSearch:
//
//	GET /_cluster/health   shard/node counts + status
//	GET /_cluster/stats    docs, store size, heap and CPU at cluster level
//	GET /_cat/nodes        per-node heap / CPU / load / disk
//
// `_cluster/stats` is deliberately used instead of `_nodes/stats`: the latter
// returns per-node payloads an order of magnitude larger, and the per-node
// numbers we chart come from `_cat/nodes` anyway.
package metrics

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/xihonglei/es-studio/backend/internal/core/es"
)

// Cluster is the cluster-wide part of a sample.
type Cluster struct {
	Status       string  `json:"status"`
	Nodes        int     `json:"nodes"`
	Primary      int     `json:"primary"`
	Active       int     `json:"active"`
	Unassigned   int     `json:"unassigned"`
	Relocating   int     `json:"relocating"`
	Initializing int     `json:"initializing"`
	Indices      int     `json:"indices"`
	Docs         int64   `json:"docs"`
	StoreBytes   int64   `json:"store_bytes"`
	HeapPercent  float64 `json:"heap_percent"`
	CpuPercent   float64 `json:"cpu_percent"`
	DiskFree     int64   `json:"disk_free_bytes"`
	DiskTotal    int64   `json:"disk_total_bytes"`
}

// Node is one node's contribution to a sample.
type Node struct {
	Name        string  `json:"name"`
	HeapPercent float64 `json:"heap_percent"`
	CpuPercent  float64 `json:"cpu_percent"`
	Load1       float64 `json:"load_1m"`
	DiskPercent float64 `json:"disk_percent"`
}

// Sample is what the collector stores, one per collection tick.
type Sample struct {
	At      int64           `json:"t"`
	Cluster Cluster         `json:"cluster"`
	Nodes   map[string]Node `json:"nodes,omitempty"`
}

// Collect performs the three requests and assembles a sample. A health failure
// is fatal (without it the sample means nothing); stats/nodes failures only
// zero those fields so a restricted cluster still records health trends.
func Collect(ctx context.Context, client *es.Client) (Sample, error) {
	health, err := get(ctx, client, "/_cluster/health", nil)
	if err != nil {
		return Sample{}, fmt.Errorf("cluster health: %w", err)
	}
	sample := Sample{At: nowMillis(), Cluster: parseHealth(health)}

	if stats, err := get(ctx, client, "/_cluster/stats", nil); err == nil {
		applyStats(&sample.Cluster, stats)
	}
	query := url.Values{}
	query.Set("format", "json")
	query.Set("h", "name,heap.percent,cpu,load_1m,disk.used_percent")
	if rows, err := getList(ctx, client, "/_cat/nodes", query); err == nil {
		sample.Nodes = parseNodes(rows)
	}
	return sample, nil
}

func get(ctx context.Context, client *es.Client, path string, query url.Values) (map[string]any, error) {
	resp, err := client.Do(ctx, es.Request{Method: http.MethodGet, Path: path, Query: query})
	if err != nil {
		return nil, err
	}
	if resp.Status < 200 || resp.Status >= 300 {
		return nil, fmt.Errorf("%s -> HTTP %d", path, resp.Status)
	}
	var body map[string]any
	if err := json.Unmarshal(resp.Body, &body); err != nil {
		return nil, fmt.Errorf("%s: unexpected body: %w", path, err)
	}
	return body, nil
}

func getList(ctx context.Context, client *es.Client, path string, query url.Values) ([]any, error) {
	resp, err := client.Do(ctx, es.Request{Method: http.MethodGet, Path: path, Query: query})
	if err != nil {
		return nil, err
	}
	if resp.Status < 200 || resp.Status >= 300 {
		return nil, fmt.Errorf("%s -> HTTP %d", path, resp.Status)
	}
	var body []any
	if err := json.Unmarshal(resp.Body, &body); err != nil {
		return nil, fmt.Errorf("%s: unexpected body: %w", path, err)
	}
	return body, nil
}

func parseHealth(body map[string]any) Cluster {
	return Cluster{
		Status:       text(body["status"]),
		Nodes:        int(number(body["number_of_nodes"])),
		Primary:      int(number(body["active_primary_shards"])),
		Active:       int(number(body["active_shards"])),
		Unassigned:   int(number(body["unassigned_shards"])),
		Relocating:   int(number(body["relocating_shards"])),
		Initializing: int(number(body["initializing_shards"])),
	}
}

// applyStats reads the cluster-stats shape defensively: every field path has a
// zero fallback, because restricted clusters may omit parts of the tree.
func applyStats(cluster *Cluster, body map[string]any) {
	indices := object(body["indices"])
	cluster.Indices = int(number(indices["count"]))
	cluster.Docs = int64(number(object(indices["docs"])["count"]))
	cluster.StoreBytes = int64(number(object(indices["store"])["size_in_bytes"]))

	nodes := object(body["nodes"])
	if total := int(number(object(nodes["count"])["total"])); total > 0 && cluster.Nodes == 0 {
		cluster.Nodes = total
	}
	// Real 8.x shape: nodes.jvm.mem.heap_* (not jvm.memory) and nodes.fs as a
	// flat object (not nodes.fs.total). Verified against a live cluster.
	heap := object(object(nodes["jvm"])["mem"])
	used, max := number(heap["heap_used_in_bytes"]), number(heap["heap_max_in_bytes"])
	if max > 0 {
		// Two decimals is enough for a percentage and keeps the JSON small.
		cluster.HeapPercent = round2(used / max * 100)
	}
	cluster.CpuPercent = number(object(object(nodes["process"])["cpu"])["percent"])

	fs := object(nodes["fs"])
	cluster.DiskFree = int64(number(fs["available_in_bytes"]))
	cluster.DiskTotal = int64(number(fs["total_in_bytes"]))
}

func parseNodes(rows []any) map[string]Node {
	nodes := make(map[string]Node, len(rows))
	for _, row := range rows {
		entry := object(row)
		name := text(entry["name"])
		if name == "" {
			continue
		}
		nodes[name] = Node{
			Name:        name,
			HeapPercent: parsePercent(entry["heap.percent"]),
			CpuPercent:  parsePercent(entry["cpu"]),
			Load1:       parsePercent(entry["load_1m"]),
			DiskPercent: parsePercent(entry["disk.used_percent"]),
		}
	}
	return nodes
}

// `_cat` renders every value as a string and uses "-" for "not available"
// (e.g. disk.used_percent on a node without a data path). Values are kept as
// reported — rounding is a presentation concern.
func parsePercent(value any) float64 {
	text := strings.TrimSpace(textOf(value))
	if text == "" || text == "-" {
		return 0
	}
	parsed, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return 0
	}
	return parsed
}

func object(value any) map[string]any {
	if m, ok := value.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func number(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case int:
		return float64(typed)
	case json.Number:
		parsed, _ := typed.Float64()
		return parsed
	}
	return 0
}

func text(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}

func textOf(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	}
	return ""
}

func round2(value float64) float64 {
	return math.Round(value*100) / 100
}

func nowMillis() int64 {
	return time.Now().UnixMilli()
}
