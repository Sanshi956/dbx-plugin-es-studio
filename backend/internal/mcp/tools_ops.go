package mcp

import "strings"

// opsTools cover the operational objects: templates, data streams, ILM,
// snapshots, tasks. All read-only; nothing here changes state.
func opsTools() []Tool {
	return []Tool{
		{
			Name:        "es_list_index_templates",
			Title:       "索引模板列表",
			Category:    "template",
			Description: "列出 composable index templates（含优先级与 index_patterns）。对应 GET /_index_template。",
			ReadOnly:    true,
			Build: func(a Args) (Request, error) {
				return Request{Method: "GET", Path: "/_index_template"}, nil
			},
		},
		{
			Name:        "es_get_index_template",
			Title:       "索引模板详情",
			Category:    "template",
			Description: "读取单个（或通配）索引模板的定义。对应 GET /_index_template/{name}。",
			ReadOnly:    true,
			Args: []Arg{
				{Name: "name", Type: "string", Required: true, Description: "模板名（支持通配符）"},
			},
			Build: func(a Args) (Request, error) {
				value, err := name(a.String("name"), "name")
				if err != nil {
					return Request{}, err
				}
				return Request{Method: "GET", Path: "/_index_template/" + value}, nil
			},
		},
		{
			Name:        "es_list_component_templates",
			Title:       "组件模板列表",
			Category:    "template",
			Description: "列出组件模板（可被索引模板组合复用）。对应 GET /_component_template。",
			ReadOnly:    true,
			Build: func(a Args) (Request, error) {
				return Request{Method: "GET", Path: "/_component_template"}, nil
			},
		},
		{
			Name:        "es_get_component_template",
			Title:       "组件模板详情",
			Category:    "template",
			Description: "读取单个组件模板。对应 GET /_component_template/{name}。",
			ReadOnly:    true,
			Args: []Arg{
				{Name: "name", Type: "string", Required: true, Description: "组件模板名"},
			},
			Build: func(a Args) (Request, error) {
				value, err := name(a.String("name"), "name")
				if err != nil {
					return Request{}, err
				}
				return Request{Method: "GET", Path: "/_component_template/" + value}, nil
			},
		},
		{
			Name:        "es_list_data_streams",
			Title:       "数据流列表",
			Category:    "stream",
			Description: "列出数据流及其后备索引、写入索引、代数与模板。对应 GET /_data_stream。",
			ReadOnly:    true,
			Build: func(a Args) (Request, error) {
				return Request{Method: "GET", Path: "/_data_stream"}, nil
			},
		},
		{
			Name:        "es_get_data_stream",
			Title:       "数据流详情",
			Category:    "stream",
			Description: "读取单个数据流。对应 GET /_data_stream/{name}。",
			ReadOnly:    true,
			Args: []Arg{
				{Name: "name", Type: "string", Required: true, Description: "数据流名"},
			},
			Build: func(a Args) (Request, error) {
				value, err := name(a.String("name"), "name")
				if err != nil {
					return Request{}, err
				}
				return Request{Method: "GET", Path: "/_data_stream/" + value}, nil
			},
		},
		{
			Name:     "es_get_ilm_status",
			Title:    "ILM 运行状态",
			Category: "ilm",
			Description: "读取 ILM 全局开关（RUNNING / STOPPING / STOPPED）。注意正确路径是 GET /_ilm/status，" +
				"写成 /_all/_ilm/status 会 400。",
			ReadOnly: true,
			Build: func(a Args) (Request, error) {
				return Request{Method: "GET", Path: "/_ilm/status"}, nil
			},
		},
		{
			Name:        "es_list_ilm_policies",
			Title:       "ILM 策略列表",
			Category:    "ilm",
			Description: "列出全部 ILM 策略及其阶段动作。对应 GET /_ilm/policy（返回「策略名→策略」的顶层映射）。",
			ReadOnly:    true,
			Build: func(a Args) (Request, error) {
				return Request{Method: "GET", Path: "/_ilm/policy"}, nil
			},
		},
		{
			Name:        "es_get_ilm_policy",
			Title:       "ILM 策略详情",
			Category:    "ilm",
			Description: "读取单个 ILM 策略。对应 GET /_ilm/policy/{name}。",
			ReadOnly:    true,
			Args: []Arg{
				{Name: "name", Type: "string", Required: true, Description: "策略名"},
			},
			Build: func(a Args) (Request, error) {
				value, err := name(a.String("name"), "name")
				if err != nil {
					return Request{}, err
				}
				return Request{Method: "GET", Path: "/_ilm/policy/" + value}, nil
			},
		},
		{
			Name:        "es_list_snapshot_repositories",
			Title:       "快照仓库",
			Category:    "snapshot",
			Description: "列出已注册的快照仓库及其类型与设置。对应 GET /_snapshot。",
			ReadOnly:    true,
			Build: func(a Args) (Request, error) {
				return Request{Method: "GET", Path: "/_snapshot"}, nil
			},
		},
		{
			Name:        "es_list_snapshots",
			Title:       "快照列表",
			Category:    "snapshot",
			Description: "列出某仓库下的快照（状态、索引、起止时间）。对应 GET /_snapshot/{repository}/_all。",
			ReadOnly:    true,
			Args: []Arg{
				{Name: "repository", Type: "string", Required: true, Description: "仓库名"},
			},
			Build: func(a Args) (Request, error) {
				repo, err := name(a.String("repository"), "repository")
				if err != nil {
					return Request{}, err
				}
				return Request{Method: "GET", Path: "/_snapshot/" + repo + "/_all"}, nil
			},
		},
		{
			Name:        "es_get_snapshot",
			Title:       "快照详情",
			Category:    "snapshot",
			Description: "读取单个快照的详情。对应 GET /_snapshot/{repository}/{snapshot}。",
			ReadOnly:    true,
			Args: []Arg{
				{Name: "repository", Type: "string", Required: true, Description: "仓库名"},
				{Name: "snapshot", Type: "string", Required: true, Description: "快照名"},
			},
			Build: func(a Args) (Request, error) {
				repo, err := name(a.String("repository"), "repository")
				if err != nil {
					return Request{}, err
				}
				snap, err := name(a.String("snapshot"), "snapshot")
				if err != nil {
					return Request{}, err
				}
				return Request{Method: "GET", Path: "/_snapshot/" + repo + "/" + snap}, nil
			},
		},
		{
			Name:        "es_get_snapshot_status",
			Title:       "快照运行状态",
			Category:    "snapshot",
			Description: "读取进行中的快照进度。对应 GET /_snapshot/_status。",
			ReadOnly:    true,
			Build: func(a Args) (Request, error) {
				return Request{Method: "GET", Path: "/_snapshot/_status"}, nil
			},
		},
		{
			Name:     "es_list_tasks",
			Title:    "运行中任务",
			Category: "task",
			Description: "列出集群中正在运行的任务。对应 GET /_tasks?group_by=none。" +
				"ES 8 返回扁平 tasks 数组，且会包含本次查询自身（cluster:monitor/tasks/lists），分析时需自行过滤。",
			ReadOnly: true,
			Build: func(a Args) (Request, error) {
				return Request{Method: "GET", Path: "/_tasks", Query: map[string]string{"group_by": "none"}}, nil
			},
		},
		{
			Name:        "es_get_task",
			Title:       "任务详情",
			Category:    "task",
			Description: "按 task id（形如 nodeId:123）读取任务详情。对应 GET /_tasks/{task_id}。",
			ReadOnly:    true,
			Args: []Arg{
				{Name: "task_id", Type: "string", Required: true, Description: "任务 id，如 AbC123:456"},
			},
			Build: func(a Args) (Request, error) {
				value, err := segment(a.String("task_id"), "task_id", taskChars)
				if err != nil {
					return Request{}, err
				}
				return Request{Method: "GET", Path: "/_tasks/" + value}, nil
			},
		},
		{
			Name:     "es_call_readonly_api",
			Title:    "只读 API 透传",
			Category: "advanced",
			Description: "对任意只读 ES 端点发 GET/HEAD 请求（用于上面工具未覆盖的 API）。" +
				"path 必须是相对路径且以 / 开头；非只读方法一律拒绝。query 里的值必须是字符串。",
			ReadOnly: true,
			Args: []Arg{
				{Name: "path", Type: "string", Required: true, Description: "相对路径，如 /_cluster/health?level=indices 的 /_cluster/health 部分"},
				{Name: "query", Type: "object", Description: "查询参数对象，如 {\"level\":\"indices\"}"},
				{Name: "method", Type: "string", Description: "GET 或 HEAD（默认 GET）", Default: "GET", Enum: []string{"GET", "HEAD"}},
			},
			Build: func(a Args) (Request, error) {
				path := a.String("path")
				if !strings.HasPrefix(path, "/") {
					return Request{}, invalid("path must start with /")
				}
				query := map[string]string{}
				for key, value := range a.Object("query") {
					text, ok := value.(string)
					if !ok {
						return Request{}, invalid("query values must be strings (%s)", key)
					}
					if _, err := keyword(key, "query key"); err != nil {
						return Request{}, err
					}
					query[key] = text
				}
				method := a.String("method")
				if method == "" {
					method = "GET"
				}
				return Request{Method: method, Path: path, Query: query}, nil
			},
		},
	}
}
