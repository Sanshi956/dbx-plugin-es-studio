# 阶段 5 实施计划（P5 · MCP Tool Service）

> 上游：`docs/00-SPEC.md` §2.1（MCP 拆双通道）、§5.2（独立 MCP Server 凭据）、
> §7 P5 验收「MCP Server 启动、Client 连接、列出工具、六个关键工具全通、未授权连接拒绝、
> 默认无写工具（14.4）」。
> 改需求先改 SPEC，本文件只做拆解。

## 0. 一句话架构

**一套工具层，两个 transport 适配**：

```
internal/mcp/           ← 唯一的工具实现（45 个工具 + 参数校验 + 只读强制 + 审计）
   ├── transport/mcp.go      → Sidecar  mcp/tools | mcp/call | mcp/log | mcp/grant(s) | mcp/revoke
   └── internal/mcpserver/   → MCP stdio  initialize | tools/list | tools/call | ping
```

DBX 内置 AI 走前者，外部 AI 客户端走后者；两者最终都调 `mcp.Caller.Call()`，
不存在「UI 用一套、MCP 用另一套」的情况（需求 6.2）。

## 1. 交付物

| 导航项 | 页面 | 数据来源 |
|---|---|---|
| MCP Tool Service（`mcp`） | `src/pages/McpService.svelte` | `mcp/tools`、`mcp/log`、`mcp/grants` |

页面四块：摘要（工具数 / 写工具 0 / 已授权 / 最近调用）、外部客户端授权（一次性令牌 +
可直接粘贴的客户端配置 + 撤销）、工具目录（9 分类 45 工具 + 搜索 + 试调用）、调用日志。

## 2. 与既有铁律的关系

P3 定的是「不新增 Go RPC」，P4 已经为时间序列破了例（`mon/*`）。P5 再新增 6 个方法，
理由同样是**能力不可替代**：

- 工具目录需要在没有连接时也能列出（`mcp/tools` 不碰 ES）；
- 工具调用要写审计日志、要强制只读、要截断大响应，这些必须在 Go 侧单点实现，
  否则外部 MCP 进程与 UI 会各写一套；
- 授权令牌只能由**持有凭据的进程**（Sidecar）签发，UI 无法自己造。

`mcp/*` 与 `mon/*` 一样只调只读端点，`DescribeRisk` / 权限层不变。

## 3. 工具集（45 个，全部只读）

| 分类 | 数量 | 代表工具 |
|---|---|---|
| 集群 cluster | 11 | `es_get_cluster_health`、`es_get_cluster_stats`、`es_list_nodes`、`es_explain_allocation`、`es_cat` |
| 索引 index | 14 | `es_list_indices`、`es_get_index_mapping`、`es_get_index_settings`、`es_list_shards`、`es_list_aliases`、`es_count_documents` |
| 文档 doc | 4 | `es_search_documents`、`es_get_document`、`es_msearch`、`es_analyze_text` |
| 模板 template | 4 | `es_list_index_templates`、`es_get_component_template` |
| 数据流 stream | 2 | `es_list_data_streams`、`es_get_data_stream` |
| 生命周期 ilm | 3 | `es_get_ilm_status`、`es_list_ilm_policies` |
| 快照 snapshot | 4 | `es_list_snapshot_repositories`、`es_list_snapshots`、`es_get_snapshot_status` |
| 任务 task | 2 | `es_list_tasks`、`es_get_task` |
| 高级 advanced | 1 | `es_call_readonly_api`（只接受 GET/HEAD） |

写工具 **0 个**（需求 14.4）。`es_call_readonly_api` 的 `method` 枚举只有 `GET` / `HEAD`，
且执行前还要过 `permission.Check(ReadOnly: true)`。

## 4. 每层做了什么

### 4.1 工具层（`internal/mcp`）

- `Tool` 声明 name / title / category / description / args / build；`NewRegistry` 在启动时
  校验「名字必须 `es_` 前缀、必须声明 read-only、必须有 builder」，坏工具直接 panic，
  不允许把半成品工具端给客户端。
- `Normalize()`：**拒绝未知参数**（模型把 `index` 拼成 `indexx` 时报错，而不是静默忽略后
  返回全量数据）、必填校验、枚举校验、整数夹取（`size` 10000 → 100）。
- 路径只有两处用户输入能进来：索引/模板名（白名单字符 + 拒 `..`）与文档 id（`url.PathEscape`）。
- `Call()` 的执行顺序：找工具 → 校验参数 → 构建请求 → **权限层只读复查** → 发请求 →
  渲染（大响应截断并标注）→ 审计。
- 审计（`mcp-calls.jsonl`）只记「工具名 / 连接 / 状态 / 耗时 / 字节 / 参数名」，
  **不记参数值**：查询词与文档 id 属于用户数据。

### 4.2 授权（`internal/mcp/grant.go` + `seal_*.go`）

需求 8 允许「单独的安全凭据提供器」，SPEC §5.2 要求不读 DBX 内部数据。落地：

1. 用户在页面点「授权当前连接」→ Sidecar 用**当前会话**的 endpoint + auth 组一个 `Grant`；
2. `Grant` 用 **Windows DPAPI** 加密（`CryptProtectData`，额外 entropy `local.es.studio/mcp-grant/v1`），
   只有当前用户能解；非 Windows 退化为 0600 的 AES-GCM 密钥文件（已在代码与文档标注差异）；
3. 返回一次性令牌 + 客户端配置；令牌不落明文，页面关掉就看不到，重新授权即轮换；
4. MCP 进程只读自己的 grant 文件，**不接受客户端传来的 URL 或 connection_id**；
5. 默认 30 天过期，可撤销。

### 4.3 MCP 传输（`internal/mcpserver`）

- 换行分隔的 JSON-RPC 2.0；支持 `initialize`（协议版本协商）、`notifications/initialized`、
  `ping`、`tools/list`、`tools/call`。
- 工具失败按 MCP 约定返回 `result.isError=true`，不走协议错误通道，便于模型自己纠错；
  只有「方法不存在 / 参数 JSON 坏了 / 找不到工具」才用 `error`。
- stdout 只走协议，诊断全部 stderr。
- 同一份代码有两个入口：`es-studio-sidecar --mcp`（包内交付）与
  `cmd/es-studio-mcp`（独立 exe，便于一个客户端一个进程）。

## 5. 为什么不是第二个 exe 进包

manifest 只允许一个 `entrypoints.backend.executable`，且 `dbx-plugin package` 只产出这一个
可执行文件。因此**交付路径是 `--mcp`**：用户客户端配置里 `command` 指向已安装的
`es-studio-sidecar.exe`，`args: ["--mcp","--data-dir", …]`。独立 exe 仍然可构建，供高级用户使用。
这条已登记到 SPEC §10。

## 6. 验收

| # | 验收项 | 证据 |
|---|---|---|
| P5-1 | 工具层 45 个工具、全部只读、schema 合法 | `go test ./internal/mcp` |
| P5-2 | 工具执行强制只读（构造 DELETE 工具必须被拦） | `TestCallRefusesWriteToolsByConstruction` |
| P5-3 | 参数校验拒绝未知参数 / 路径穿越 / 越界 size | `TestNormalize*`、`TestReadOnlyAPI*` |
| P5-4 | 大响应截断并标注，审计只记参数名 | `TestTruncationIsExplicit`、`TestCallWritesAuditRows*` |
| P5-5 | grant 加解密往返、轮换、撤销、过期、跨用户跳过、文件无明文 | `grant_test.go` |
| P5-6 | MCP 协议：版本协商、tools/list、tools/call、未知方法、坏 JSON、通知不回复 | `mcpserver/server_test.go` |
| P5-7 | 真机端到端：授权 → 起进程 → 六个工具 → 写被拒 → 审计 → 撤销失效 | `scripts/mcp-client-test.py`（32 项） |
| P5-8 | Sidecar 侧 `mcp/*` 走真实 ES | `scripts/smoke-test.py`（+20 项） |
| P5-9 | 页面：目录 40+、搜索、授权配置、试调用真实集群、日志、撤销 | `scripts/browser-ux-test.py`（+12 项） |

## 7. 已知限制

- 审计文件由 Sidecar 与 MCP 进程共用（同一 `.jsonl`）：追加是单次写入，压缩用 tmp+rename，
  极端并发下可能丢极少量旧行，不会损坏文件。
- 非 Windows 的 grant 加密强度低于 DPAPI（无 keyring 依赖可用的等价物），已在代码注释与验收文档写明。
- MCP 第一版只有 stdio，不做 Streamable HTTP（SPEC §8 明确不做）。
