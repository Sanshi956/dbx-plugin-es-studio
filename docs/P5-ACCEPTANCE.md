# P5 验收记录 · MCP Tool Service（0.4.0）

> 对应 `docs/00-SPEC.md` §7 P5 与 `docs/05-PLAN-P5.md`。
> 本轮验证环境：**两个真实 ES 8.19.23 集群**（`es-studio-test` :19200 / `es-studio-test-b` :19201，
> 本轮按要求关闭内存守卫后同时在线），Windows 11 + Node 22 + Go 1.22 + 系统 Chrome。

## 1. 交付物

| 内容 | 位置 |
|---|---|
| 工具层（44 个只读工具） | `backend/internal/mcp/{registry,tools_cluster,tools_index,tools_search,tools_ops,call,log,grant,seal_*}.go` |
| MCP stdio 协议 | `backend/internal/mcpserver/server.go` |
| 统一入口（`--mcp` / 独立 exe） | `backend/internal/mcpcmd/run.go`、`backend/cmd/es-studio-mcp/main.go`、`backend/main.go`（`--mcp` 分支） |
| Sidecar 方法 | `mcp/tools`、`mcp/call`、`mcp/log`、`mcp/grants`、`mcp/grant`、`mcp/revoke`（`transport/mcp.go`、`transport/mcp_grant.go`） |
| 页面 | `src/pages/McpService.svelte` + `src/lib/mcp.ts` |
| 测试 | `internal/mcp/*_test.go`（21）、`internal/mcpserver/server_test.go`（7）、`scripts/mcp-client-test.py`（32 步）、smoke +20、UX +12 |

## 2. 页面实测（真实集群）

| 区块 | 实测 |
|---|---|
| 摘要 | 工具总数 **45**、写工具 **0**、已授权连接 0、最近调用 18 |
| 工具目录 | 9 个分类（集群 11 / 索引 14 / 文档 4 / 模板 4 / 数据流 2 / 生命周期 3 / 快照 4 / 任务 2 / 高级 1），搜索 `shard` 过滤后仍命中 `es_list_shards` |
| 授权 | 30 天有效期下拉 + 「授权当前连接」→ 一次性令牌 + 可粘贴的 `mcpServers` 配置（含 `--mcp` 与 `ES_STUDIO_MCP_TOKEN`）；授权表只显示指纹与到期时间 |
| 试调用 | 选 `es_get_cluster_health` 直接调通：`GET /_cluster/health · HTTP 200 · 1 ms · 435 B`，返回体里 `"cluster_name": "es-studio-test"`、`"status": "yellow"` |
| 调用日志 | 每次调用一行：时间 / 工具 / 连接 / 状态 / 耗时 / 大小；`mcp-calls.jsonl` 2.6 KB |
| 撤销 | 撤销后授权表清空，旧令牌 `--check` 立即返回退出码 2 |

## 3. MCP 协议端到端（`scripts/mcp-client-test.py`，32/32）

用真实进程 + 真实集群，不用 mock：

1. `mcp/grant` 拿到一次性令牌与客户端配置；公开视图只有指纹，**没有**令牌明文。
2. 错误令牌 `--check` → 退出码 2 且提示「authorise」；正确令牌 → 0。
3. `initialize` 协商 `2024-11-05`；`serverInfo.name = es-studio-mcp`；`notifications/initialized` 不产生响应。
4. `tools/list` → **45** 个工具，全部 `readOnlyHint: true`，全部有 `inputSchema`，
   SPEC §7 点名的 6 个工具都在。
5. 六个工具全部读真实集群：health（`"status"`）、list_indices（`products`）、
   get_index_mapping（`"mappings"`）、search_documents（`"hits"`）、list_shards、call_readonly_api。
6. 写方法（`es_call_readonly_api` + `method: POST`）被参数枚举拒绝；
   未知工具、`../etc/passwd` 索引名、拼错的参数名都返回 `isError: true` 而不是崩溃或静默通过。
7. `stdin` 关闭后进程退出码 0。
8. 审计日志记录了外部客户端的调用，**不含** `match_all`（只记参数名），并记录了失败调用。
9. `mcp/revoke` 后旧令牌立即失效。

## 4. 回归矩阵（全绿）

| 项 | 结果 |
|---|---|
| svelte-check | **0 errors / 0 warnings** |
| `go vet ./...` | 通过 |
| `go test ./...` | **87** 个用例（P5 新增 28） |
| smoke（bin） | **125/125**（P5 新增 20） |
| smoke（包内 exe） | **125/125** |
| `mcp-client-test.py`（bin / 包内 exe） | **32/32 / 32/32** |
| 浏览器主回归 | **23/23**，控制台 0 报错 |
| 浏览器 UX | **106/106**（MCP 新增 12） |
| 写路径 / 只读拒绝 | **7/7 / 4/4** |
| check-manifest | 53（含包 107） |
| `verify-package.py` | **17/17** |
| 签名 | **SIGNATURE VALID**（`local.es.studio.release`） |
| 包体积 | 4,665,294 B |

包：`dist/local.es.studio-0.4.0-windows-x64.signed.dbxp`。

## 5. 双集群验证（本轮按用户要求忽略内存守卫）

- `scripts/es-cluster.py` 支持 `--root/--port/--cluster-name/--node-name`，
  且 `cluster_pids()` 改为按 `-Des.path.conf` **精确定位本实例**，
  因此 `stop` 一个实例不会误杀另一个（之前按 `es-studio` 子串匹配会两家一起杀）。
- 同时运行：`es-studio-test`（:19200，既有数据）与 `es-studio-test-b`（:19201，独立数据目录）。
- 集群 B 被人为做出真实差异：`cluster.name`、`cluster.routing.allocation.disk.watermark.low=80%` /
  `high=88%`、`customers.refresh_interval=-1`、`logs-app.blocks.write=true`、额外 400 篇文档。
- 对比页实测：**9 项不同 / 13 项相同**，`文档数 38 vs 424`、`索引数 18 vs 9`、
  `磁盘 low 水位 85% vs 80%`。UX 断言已改为**从两侧 HTTP 读事实再核对页面**（不再硬编码 8.15.0）。
- 结论：Multi-Cluster Compare 对「两个真集群」成立，不再依赖 `scripts/mock-cluster.py`
  （该替身保留，作为只有一台 ES 时的降级路径，测试输出会打印 `right side = real|mock`）。

## 6. 与规格的差异（透明记录）

| 项 | 说明 |
|---|---|
| 包内不额外放 MCP exe | manifest 只允许一个 backend executable，`dbx-plugin package` 只产出它 → 交付 `--mcp` 模式；`cmd/es-studio-mcp` 仍可独立构建 |
| 非 Windows 的 grant 加密 | 无 keyring 依赖可用等价物，退化为 0600 密钥文件 + AES-GCM，强度低于 DPAPI，已在代码与文档写明 |
| 审计文件跨进程共用 | Sidecar 与 MCP 进程追加同一 `.jsonl`；追加是单次写入、压缩用 tmp+rename，极端并发可能丢极少量旧行，不会损坏文件 |
| Streamable HTTP | 第一版不做（SPEC §8 明确不做），只提供 stdio |

## 7. 用户验收建议关注

1. 在 DBX 里打开一个连接 → MCP Tool Service → 授权 → 把配置粘到你常用的 MCP 客户端，试问
   「这个集群健康吗，有哪些索引」。若客户端不认 `--data-dir`，说明你想用独立进程模式，
   告诉我要不要为某些客户端加一个只读的 `stdio` 包装脚本。
2. 工具集是否够用：现在 44 个，缺的是 `_msearch/template`、`_scripts`、`_nodes/hot_threads` 之类。
   哪些该进第一版，你点一下我再补。
3. 授权有效期默认 30 天，是否要改短（安全）或加「永不过期」（方便）。
