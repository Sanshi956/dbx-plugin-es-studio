package mcp

import (
	"fmt"
	"strings"
)

// clusterTools cover "what is this cluster and how healthy is it" — the first
// question every AI client asks. All of them are plain GETs.
func clusterTools() []Tool {
	return []Tool{
		{
			Name:     "es_get_cluster_health",
			Title:    "集群健康",
			Category: "cluster",
			Description: "读取集群健康状态：status(green/yellow/red)、节点数、活跃/未分配分片数、重定位情况。" +
				"对应 GET /_cluster/health。排查任何问题都应先看它。",
			ReadOnly: true,
			Build: func(a Args) (Request, error) {
				return Request{Method: "GET", Path: "/_cluster/health"}, nil
			},
		},
		{
			Name:     "es_get_cluster_stats",
			Title:    "集群统计",
			Category: "cluster",
			Description: "读取集群维度统计：节点数/角色、JVM 堆、磁盘容量、索引与文档总量、分片分布。" +
				"对应 GET /_cluster/stats。返回体较大，客户端应按需读取。",
			ReadOnly: true,
			Build: func(a Args) (Request, error) {
				return Request{Method: "GET", Path: "/_cluster/stats"}, nil
			},
		},
		{
			Name:     "es_get_cluster_settings",
			Title:    "集群设置",
			Category: "cluster",
			Description: "读取 persistent / transient / defaults 三层集群设置（扁平键名）。" +
				"对应 GET /_cluster/settings?flat_settings=true。",
			ReadOnly: true,
			Build: func(a Args) (Request, error) {
				return Request{Method: "GET", Path: "/_cluster/settings",
					Query: map[string]string{"flat_settings": "true"}}, nil
			},
		},
		{
			Name:        "es_get_cluster_pending_tasks",
			Title:       "待执行任务",
			Category:    "cluster",
			Description: "读取主节点排队中尚未应用的集群状态任务。对应 GET /_cluster/pending_tasks。",
			ReadOnly:    true,
			Build: func(a Args) (Request, error) {
				return Request{Method: "GET", Path: "/_cluster/pending_tasks"}, nil
			},
		},
		{
			Name:     "es_get_cluster_info",
			Title:    "集群基本信息",
			Category: "cluster",
			Description: "读取根路径信息：集群名、节点名、版本号、发行版标识（Elasticsearch / OpenSearch）。" +
				"对应 GET /。判断版本兼容性用它。",
			ReadOnly: true,
			Build: func(a Args) (Request, error) {
				return Request{Method: "GET", Path: "/"}, nil
			},
		},
		{
			Name:     "es_list_nodes",
			Title:    "节点列表",
			Category: "cluster",
			Description: "列出节点：名称、IP、角色、是否主节点、堆使用率、内存使用率、CPU、磁盘使用率、版本。" +
				"对应 GET /_cat/nodes?format=json。",
			ReadOnly: true,
			Build: func(a Args) (Request, error) {
				return catPath("nodes",
					"name,ip,node.role,master,heap.percent,ram.percent,cpu,disk.used_percent,version", nil), nil
			},
		},
		{
			Name:        "es_get_node_stats",
			Title:       "节点统计",
			Category:    "cluster",
			Description: "读取节点运行统计（默认全部指标）。对应 GET /_nodes/stats。可指定节点 id 与指标（jvm/fs/os/indices/thread_pool）。",
			ReadOnly:    true,
			Args: []Arg{
				{Name: "node_id", Type: "string", Description: "节点 id 或 _all（默认 _all）"},
				{Name: "metric", Type: "string", Description: "指标名，如 jvm / fs / os / indices / thread_pool（默认空=全部）"},
			},
			Build: func(a Args) (Request, error) {
				node := "_all"
				if a.Has("node_id") {
					value, err := name(a.String("node_id"), "node_id")
					if err != nil {
						return Request{}, err
					}
					node = value
				}
				path := "/_nodes/" + node + "/stats"
				if a.Has("metric") {
					metric, err := name(a.String("metric"), "metric")
					if err != nil {
						return Request{}, err
					}
					path += "/" + metric
				}
				return Request{Method: "GET", Path: path}, nil
			},
		},
		{
			Name:     "es_get_shard_allocation",
			Title:    "分片分配与磁盘",
			Category: "cluster",
			Description: "按节点列出分片数、索引/已用/可用磁盘容量与使用百分比（磁盘水位判断依据）。" +
				"对应 GET /_cat/allocation?format=json。",
			ReadOnly: true,
			Build: func(a Args) (Request, error) {
				return catPath("allocation",
					"node,shards,disk.indices,disk.used,disk.avail,disk.total,disk.percent", nil), nil
			},
		},
		{
			Name:        "es_get_thread_pool_stats",
			Title:       "线程池统计",
			Category:    "cluster",
			Description: "读取各节点线程池的活跃/队列/拒绝计数，用于判断 write/search 是否被拒。对应 GET /_nodes/stats/thread_pool。",
			ReadOnly:    true,
			Build: func(a Args) (Request, error) {
				return Request{Method: "GET", Path: "/_nodes/stats/thread_pool"}, nil
			},
		},
		{
			Name:        "es_explain_allocation",
			Title:       "分片分配解释",
			Category:    "cluster",
			Description: "解释某个分片为什么未分配或为什么留在当前节点。对应 GET /_cluster/allocation/explain。",
			ReadOnly:    true,
			Args: []Arg{
				{Name: "index", Type: "string", Description: "索引名", Required: true},
				{Name: "shard", Type: "integer", Description: "分片编号", Required: true, Min: 0},
				{Name: "primary", Type: "boolean", Description: "是否主分片（默认 false）"},
			},
			Build: func(a Args) (Request, error) {
				index, err := name(a.String("index"), "index")
				if err != nil {
					return Request{}, err
				}
				query := map[string]string{
					"index":   index,
					"shard":   fmt.Sprint(a.Int("shard", 0)),
					"primary": fmt.Sprint(a.Bool("primary")),
				}
				return Request{Method: "GET", Path: "/_cluster/allocation/explain", Query: query}, nil
			},
		},
		{
			Name:     "es_cat",
			Title:    "Cat API",
			Category: "cluster",
			Description: "调用白名单内的 _cat 接口（只读，返回 JSON）。用于 es_list_indices 等工具没覆盖的列组合。" +
				"columns 传逗号分隔的列名；不确定列名时留空。",
			ReadOnly: true,
			Args: []Arg{
				{Name: "api", Type: "string", Required: true, Description: "cat 接口名",
					Enum: []string{"aliases", "allocation", "count", "health", "indices", "master", "nodes",
						"pending_tasks", "recovery", "repositories", "segments", "shards", "snapshots",
						"templates", "thread_pool"}},
				{Name: "columns", Type: "string", Description: "逗号分隔列名（如 health,index,docs.count）"},
			},
			Build: func(a Args) (Request, error) {
				api := a.String("api")
				extra := map[string]string{}
				columns := strings.TrimSpace(a.String("columns"))
				if columns != "" {
					for _, column := range strings.Split(columns, ",") {
						if _, err := keyword(column, "columns"); err != nil {
							return Request{}, err
						}
					}
					extra["h"] = columns
				}
				return catPath(api, "", extra), nil
			},
		},
	}
}
