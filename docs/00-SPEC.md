# ES Studio — 设计规格（V1.0 / 已确认）

> 本文件是 superpowers 流程第 1 步「头脑风暴」的产出，作为设计文档与下游实施计划的唯一输入。
> 需求基线：《ES Studio —— DBX Elasticsearch 管理插件完整需求规格说明书》V1.0（16 章）。
> 确认日期：2026-11-15。规格变更必须回改本文件，不悄悄改计划。

## 0. 决策记录（用户已拍板）

| # | 决策点 | 结论 |
|---|---|---|
| D1 | 交付节奏 | **按需求第 12 章 5 阶段分批**，每阶段跑测试 + 两段式评审 + 验收后才进入下一阶段 |
| D2 | 本地 ES 测试集群 | **内存可用 ≥3GB 才启动**；此前用官方 dev host + 内存 mock 做单测；启动后设内存守卫 |
| D3 | MCP 形态 | **双通道**：独立 `es-studio-mcp.exe`（STDIO，`es_*` 工具，外部 AI 客户端）+ 官方 `mcp/tools` / `mcp/call`（DBX 内置 AI），共享同一 `internal/core` |
| D4 | 连接管理权威 | **DBX 连接树为权威** + 插件补全 UI 入口（详见 2.3） |

## 1. 环境核查基线（实测，需求 16.1）

| 项 | 实测结论 |
|---|---|
| 插件 CLI | `@dbx-app/plugin-cli` 0.1.10；`create` 支持 `frontend / svelte / rust / go` 四种模板 |
| DBX 宿主 | 本机 `0.6.70-beta.20261112.1`（上轮会话记录）；模板 `engines.dbx >=0.5.68` 满足 |
| 官方 Go SDK | `github.com/t8y2/dbx/plugins/sdk/go/dbx-plugin-sdk`，Sidecar Protocol v1，`stdio-jsonl`；源码已离线留存（12.6 KB），vendor 进仓库保证离线编译 |
| 官方 dev host | `dbx-plugin dev --path . --port 5190` 可用，满足需求 16.2「不得自行模拟 DBX 插件运行环境」 |
| Secret Store | **无前端 Secret API**；`connection-provider` 字段 `binding:"secret"` 写入，凭据仅在 backend lifecycle 请求 hydration；前端只拿 `connectionId` |
| `host.storage` | 单值 256 KiB、**整库 1 MiB**；官方明确「UI state，不是 data sink」 |
| `host.connections:read` | `getConnections()` 返回脱敏摘要（id/name/dialect/connected/readOnly），**不含 host/port/username** |
| MCP 官方支持 | 仅 `mcp/tools` + `mcp/call`（DBX 内置 AI，工具名被改为 `<plugin>__<tool>`）；**无通用外部 MCP Server 托管 API**；manifest 只有一个 `entrypoints.backend.executable` |
| UI 沙箱 | Svelte/Vite 产物可用，必须 `base:"./"`、静态资产全落 `ui/`、禁 CDN 与 Vite dev server 引用；主题用宿主注入 CSS tokens + `data-dbx-theme` |
| 机器资源 | 15.7 GB RAM（确认时可用 1.61 GB / 89.8%）；C: 剩 10 GB，D: 剩 158 GB；ES 8.19.23 在 `D:/software/dev/elasticsearch-8.19.23-windows-x86_64/elasticsearch-8.19.23` |

## 2. 需求优化整理（7 处修正，均已确认）

### 2.1 MCP 拆双通道（需求第 6 章）
官方不支持独立 MCP 进程托管，需求 6.8 已预留「配套独立可执行程序 + 共享核心服务代码」退路，正好命中。
实现为**一个 `internal/core` 包，三个 transport 适配**：

```
internal/core/*   ← 唯一业务实现（连接、ES client、版本、能力、各 service、权限）
   ├── cmd/es-studio-sidecar  → DBX 插件 Sidecar（stdio-jsonl JSON-RPC）
   │      ├── 自定义方法 es/* 给 Workbench UI
   │      └── mcp/tools + mcp/call 给 DBX 内置 AI
   └── cmd/es-studio-mcp      → 独立 STDIO MCP Server（外部 AI 客户端，工具名 es_*）
```

严格满足需求 6.2「UI 与 MCP 共用 Core Service，禁止两套业务逻辑」，且不绕过官方生命周期。
`mcp/call` 与独立 MCP 的 `tools/call` 都走同一 `Permission Layer` → `core`。

### 2.2 持久化分层（需求第 8 章）
若按字面把监控/诊断/请求/查询/MCP 历史都放 `host.storage`，会直接爆 1 MiB 上限。

| 数据 | 落点 |
|---|---|
| 连接配置 / 密码 / API Key / Token | **DBX Secret Store**（`binding:"secret"`），插件不存明文 |
| UI 偏好：主题、布局、当前连接 ID、展开态 | `window.dbxPlugin.storage`（<1 MiB） |
| 请求历史 / 收藏 API / 查询历史 / DSL 历史 | Sidecar `DBX_PLUGIN_DATA_DIR`（JSONL 追加文件） |
| 监控历史 / 诊断历史 / MCP 调用日志 / 对比报告 | Sidecar `DBX_PLUGIN_DATA_DIR`（JSONL 追加文件，带保留期与容量上限） |
| 缓存、导出临时文件 | Sidecar data dir，**全部位于 D 盘** |

`DBX_PLUGIN_DATA_DIR` 由宿主注入、跨升级保留；SDK 提供 `DataDir()` / `EnsureDataDir()`，首次写入前自行创建。
监控历史必须**周期采集后自己落库**，不能把 ES 累计计数当时间序列（需求 5.12 明确）。

> **P4 落地修订（2026-11-15，已实施）**：原定 SQLite 改为 **JSONL 追加文件**。
> 理由：纯 Go SQLite 驱动（`modernc.org/sqlite`）需 vendor ~10 MB 且本项目未启用 CGO；
> 而 P4 落盘量级是「每分钟一行的日志」，不需要索引与事务。实现见
> `backend/internal/storage/series.go`：追加写 + 保留期/条数/字节三重上限 + 每 64 次追加压缩一次，
> 文件可用文本编辑器直读、截断可恢复。诊断历史与对比报告（P4-b）沿用同一存储。

### 2.3 连接管理（需求 5.1，D4）
- **权威**：DBX 连接树。连接 CRUD 表单由宿主渲染并拥有生命周期；凭据、TLS、只读标记由宿主管理。
- **插件补全**：测试连接、复制连接、默认连接、环境标签/颜色、分组、多集群切换、连接状态检测，全部在插件 UI 内实现；元数据存 Sidecar data dir。
- **新增/编辑/删除**：插件 UI 提供入口，通过官方 API 打开 DBX 连接对话框（`host.workbench` / command），不自建第二套表单。
- **endpoint 展示**：`getConnections()` 脱敏，改由 Sidecar 在会话建立后返回**脱敏地址**（不含账号密码），UI 不直接读连接配置。
- 结果：需求 5.1 所有条目不缺失，且密码永不进入前端、日志、请求历史。

### 2.4 REST Console 安全（需求 5.8 / 10.2）
`host.network:https://host` 只是 CSP `connect-src` 白名单（最多 64 origin），**不是 Sidecar 网络防火墙**，且用户集群多为 http 且未开 CORS。
因此所有请求经 Sidecar 发出；Sidecar 强制：只接受相对路径、拒绝含 scheme/host 的输入、拒绝 `..`、method 白名单、超时与响应体积上限、危险方法标记与二次确认。前端不做安全判定。

### 2.5 技术栈裁剪（需求 7.2，官方模板优先）
- 采用官方 `svelte` 模板（Svelte 5 + Vite 7）+ TypeScript，与官方 `go` 模板合并；官方无组合模板，P1 必须验证 `[backend]` 与 `[dev] ui_build` 共存及打包。
- Vite 必须设 `base:"./"`（官方模板未设，是已知坑），否则 code-split chunk 在沙箱内 404。
- Monaco / ECharts 改为**按需静态打包**；P2 的 JSON 编辑器先用轻量自研（高亮/缩进/校验/错误定位/搜索/历史/模板），避免数 MB 资产与沙箱加载风险。需求 7.2 为「推荐」且写明「官方模板优先」，不构成削减。
- 主题变量集中在 `src/styles/tokens.css`，业务组件不写死颜色（需求 9.4）。

### 2.6 节奏与验收（需求第 12 / 14 / 16 章）
需求自带的 5 阶段与 18 步执行顺序直接作为实施计划骨架；每阶段 = 规格 → RED 测试 → GREEN 实现 → 两段式评审 → 验收。
需求 16.4「不允许只创建页面外壳」「不允许装饰性按钮」「不允许静态假数据」通过每阶段验收清单逐项核对。

### 2.7 项目身份与交付链
- 插件 ID `local.es.studio`（名称 ES Studio），与 DBX 里已装的旧 `local.es.head` 并存不冲突；旧插件是否卸载由用户决定。
- 签名沿用自定义仓库 Ed25519（本机 trust store 已有 `local.es.head.release`），新包用新 key id `local.es.studio.release`。
- 交付链：`npm run build` → `go vet/test/build` → `check-manifest.py` → `dbx-plugin package .` → `sign-package.py` → `--verify` + 包内 Sidecar 回归。任何包内文件改动必须重打包重签。

## 3. 分层架构与目录

```
DBX Host
 ├─ Workbench UI (ui/，Svelte+TS，隔离 iframe)
 │     └─ window.dbxPlugin.invoke("es/*") ──┐
 └─ Host Bridge / Secret Store              │
                                            ▼
        cmd/es-studio-sidecar  ──────── internal/transport
        (stdio-jsonl, mcp/tools, mcp/call)    │
                                              ▼
        internal/permission  ─────────  internal/core  ────── internal/storage
        (只读/路径/连接白名单/超时)      │  connection · es · cluster · node
                                        │  shard · index · document · query
        cmd/es-studio-mcp (独立 STDIO) ─┘  snapshot · task · compare · diagnostics
                                              ▼
                                  Elasticsearch 7/8/9 · Easysearch
```

```
backend/
  go.mod
  cmd/es-studio-sidecar/main.go     插件 Sidecar（唯一 backend executable）
  cmd/es-studio-mcp/main.go         独立 MCP Server（配套 exe，随源码交付）
  internal/dbxpluginsdk/            vendor 官方 Go SDK（不动）
  internal/core/connection/         连接解析、认证、会话（按 connection.id）
  internal/core/es/                 HTTP client、EngineAdapter、version、capability
  internal/core/{cluster,node,shard,index,document,query,snapshot,task,compare,diagnostics}/
  internal/core/permission/          只读模式、危险操作、路径守卫
  internal/storage/                  SQLite + 保留期/容量上限
  internal/mcp/                     tool registry、JSON Schema、permission、stdio transport
  internal/transport/               JSON-RPC dispatch（UI 与 MCP 共用 core 的适配层）
ui-src/ (Svelte + TS)  → vite build → ui/
scripts/                 build / check-manifest / sign-package / mock-es / smoke-test
docs/                    00-SPEC.md · 01-PLAN.md · 验收记录 · API 兼容性清单
```

## 4. 需求 → 模块 → 阶段 映射

| 需求章节 | 模块 | 阶段 |
|---|---|---|
| 5.1 CON 连接管理 / 5.2 CLU 集群总览（基本信息、健康） | core/connection, core/cluster, UI Overview | **P1** |
| 5.2 集群拓扑矩阵、5.3 NOD 节点、5.4 SHA 分片、5.5 IDX 索引、5.6 DOC 文档、5.7 QRY 查询、5.8 API REST、5.9 ANA Analyzer | 对应 core service + UI 页面 | **P2** |
| 5.5.6~5.5.9 Templates / Component Templates / Data Streams / ILM、5.10 SNP 快照、5.11 TSK 任务、5.7.3 Query Profiler | core 扩展 + UI | **P3** |
| 5.12 MON 监控、5.13 DIA 诊断、5.14 CMP 多集群对比、Allocation Explain | storage + 规则引擎 + diff | **P4** |
| 6 MCP 全章（40+ 只读工具、权限、日志、配置） | internal/mcp + cmd/es-studio-mcp + mcp/tools | **P5** |

需求 14.1 的 17 项 Head 功能对照**全部在 P2 完成，不允许以新功能替代**。

## 5. 安全设计（需求第 10 章）

1. **凭据**：只走 DBX Secret Store；Sidecar 结构体凭据字段 `json:"-"`，不进 events / context / error / log / 请求历史 / 导出配置。
2. **独立 MCP Server 凭据**（需求 8 允许的「单独安全凭据提供器」）：**不读 DBX 内部数据库或文件**。由用户在插件 MCP 设置页对**指定 DBX 连接显式授权**后，Sidecar 把该连接的授权令牌写入 OS 级加密库（Windows DPAPI，仅当前用户可解）；MCP Server 只凭令牌向已授权连接发起请求，工具不接受任意 URL / 任意 connection_id。
3. **只读模式**：在 `internal/permission` 服务层强制校验，不是前端隐藏按钮；所有写操作（DELETE/PUT/POST 危险端点、索引删除、reroute、restore）先过确认 + 影响范围展示。
4. **MCP 默认全只读**：工具 `annotations.readOnlyHint=true`，`size` 上限、查询超时、返回条数上限、调用日志、连接白名单；写工具（`es_create_index` 等）第一版不开放。
5. **REST Console**：只相对路径、只当前授权集群、method 白名单、超时与响应上限、危险操作标识。
6. **诊断**：确定性规则引擎，只给建议不自动修复；不得把推测写成确定事实。

## 6. 版本适配（需求 7.4）

`internal/core/es/version.go` 识别 `version.number` + 发行版（Elasticsearch / Easysearch / OpenSearch）；
`capability.go` 维护能力矩阵，UI 按能力隐藏/禁用不支持的功能并给出清晰提示。
优先 7.x / 8.x / 9.x；typed API 与 `_doc`、ILM/ISM 差异、Data Stream 支持等按能力开关处理。
Easysearch 兼容必须经真实 API 验证，不得凭接口名相似宣称兼容。

## 7. 阶段验收

| 阶段 | 验收（对应需求） |
|---|---|
| P1 | 官方模板骨架可 `dbx-plugin package` 出可安装 `.dbxp`；Workbench 启动；能建立 ES 连接并读到**真实**集群版本/名称/健康；Go 单测 + 协议 smoke 全绿（12.1 / 14.3） |
| P2 | 需求 14.1 的 17 项逐项实测通过；浏览器 10+ 页面无错误；危险操作确认；ReadOnly 拒绝写 |
| P3 | Templates / Data Streams / ILM / Snapshot / Task / Profiler 真实 API 验证 |
| P4 | 监控历史为真实周期采集序列；诊断规则可配置；两集群 diff 可导出 |
| P5 | MCP Server 启动、Client 连接、列出工具、`es_get_cluster_health` / `es_list_indices` / `es_get_index_mapping` / `es_search_documents` / `es_list_shards` / `es_call_readonly_api` 全通；未授权连接拒绝；默认无写工具（14.4） |

## 8. 明确不做的范围（需求 2.2 + 第一版边界）

内置 AI 聊天、大模型诊断、自然语言生成 DSL、AI 自主运维、定时自动化平台、独立账号体系、重复实现 DBX 通用工作区、独立 ES 服务端；
第一版额外不做：MCP 写工具、Streamable HTTP Transport（默认关闭）、自动修复、跨集群配置自动同步。
**推荐项裁剪**：Monaco 换轻量自研编辑器、ECharts 按需静态打包（需求 7.2 为推荐）。
以上均不构成对需求 14.1 Head 功能的削减。

## 9. 资源安全（用户强调，硬约束）

- **内存守卫**：启动本地 ES 前检查可用内存 ≥3 GB；启动后持续采样，可用 <1 GB 时自动停止 ES 并报告。确认规格时为 1.61 GB；P1 执行时用户授权关闭 Chrome / PyCharm 后可用内存达 3.33 GB，**按 D2 阈值启动了真实 ES**（见 §11）。
- **ES 低配**：`jvm.options.d/es-studio.options` 设 `-Xms384m -Xmx384m`；`discovery.type: single-node`；仅监听 127.0.0.1；关闭 security / ML；`path.data`、`path.logs` 指向 D 盘；放宽磁盘水位避免测试期变 red；只建 2~3 个小索引。
- **磁盘**：npm cache、Go build cache、ES 数据与日志、构建产物一律 D 盘；C 盘剩余 10 GB 不再写入大文件。
- **采集频率**：监控默认 60s、保留 7 天、条数上限；自动刷新前一次未完成不重复触发（需求 5.2.5 / 11.1）。

## 10. 需求偏差登记（透明记录）

| 需求原文 | 实际 | 原因 |
|---|---|---|
| 6.3 独立 MCP Server 集成进插件 | 独立 exe 交付（需求 6.8 允许的备选） | 官方 manifest 仅支持一个 backend executable，且无 MCP 进程托管 API |
| 8「连接密码持久化」 | 由 DBX Secret Store 承担，插件不持久化 | 官方无前端 Secret API，且需求 2.3 禁止绕过权限体系 |
| 7.2 Monaco / ECharts | 自研轻量编辑器 + 按需静态图表 | 沙箱禁 CDN、包体积与官方模板优先原则 |
| 5.1.1 新增/编辑/删除连接 | 插件 UI 提供入口，跳转到 DBX 官方连接对话框 | 宿主拥有连接生命周期（D4 已确认） |

## 11. 实施偏差登记（P1 收尾补记）

规格与终态架构不变，以下仅为 P1 实施过程中相对文字计划的实际落点，
详细证据见 `docs/P1-ACCEPTANCE.md` §6。

| 原计划 | 实际 | 原因 |
|---|---|---|
| P1 不启动真实 ES（§9 当时内存 1.61 GB） | 启动真实 ES 8.19.23 并跑 11 项集成 | 关闭 Chrome / PyCharm 后可用内存 3.33 GB，满足 D2 ≥3 GB |
| `backend/cmd/es-studio-sidecar/main.go` | `backend/main.go` | 官方 dev host 在 backend 根目录执行 `go build`，main 包必须在根 |
| `entrypoints.backend.executable` 写平台子目录 | 写 `bin/es-studio-sidecar`，CLI 打包时自动落 `bin/windows-x64/*.exe` | 依 CLI 0.1.10 实际行为，包内回归已验证 |
| T15/T16 用 `scripts/mock-es.py` 驱动 Overview | 用真实 ES + `scripts/dev-host-check.mjs` 走官方 dev host HTTP 协议 | 内存达标后采用更强验证，且未引入静态假数据 |
| §3 架构图中的 `cmd/es-studio-mcp` | P1 未实现，属 P5 | 架构图画的是终态，按 D1 分阶段交付 |
| 直接 `dbx-plugin dev --path .` | 用 `npm run dev`（`scripts/dev.mjs` + `dev-host.mjs`） | 官方 CLI 0.1.10 在 Windows 传 `\\?\` 扩展长度路径作 cwd，构建命令必失败；兼容层只剥离前缀，不修改全局 CLI |
| Vite 默认多文件 UI 产物（`./assets/*.js` 外链） | `ui/index.html` **单文件内联** CSS + JS，且内联 `<script>` 位于 `<body>` 末尾 `#app` 之后 | DBX 通过 `read_plugin_ui_entry` 把 HTML 放入 opaque-origin iframe，外链资源解析不到 → 工作台纯白；首版内联落 `<head>` 同步执行又导致 `#app` 未解析、`mount` 抛错、只显示占位，二次修正脚本位置；`vite.config.js` 构建后自动内联，`check-baseline.mjs` / `verify-package.py` 强制断言 |
| connection-provider `label: "Elasticsearch"` | `label: "ES Studio (Elasticsearch)"` | DBX 原生内置 Elasticsearch 连接类型，同名会让「新建连接」里无法区分插件入口与内置驱动 |
| 仅实现自定义 `connection/test` / `connect` / `disconnect` RPC | 额外实现标准 `connection/action` RPC（`backend/internal/transport/connection_action.go`） | manifest 自加 `actions` 字段使 DBX 走标准 `connection/action`，否则「Test Connection」报 `Unknown method`；实测 DBX 传 `action.id` + `connection{host,port,external_config,runtime}` |
| 本机 DBX 端到端验证交给用户 | 由 AI 在本机真实 DBX 中代验（URL 安装 → 新建连接 → Test Connection → 连接树打开工作台显示真实指标） | DBX 以更高完整性级别运行，模拟输入被 UIPI 拦截；改用 `AttachThreadInput` 抢前台 + 插件中心「从 URL 安装」完成整条链路 |

`go test -race` 因本机未启用 CGO 未执行，已登记为 P1-05 待补，不阻断阶段闸口。
