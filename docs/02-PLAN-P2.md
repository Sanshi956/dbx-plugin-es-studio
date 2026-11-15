# 阶段 2 实施计划（P2 · 集群/节点/分片/索引/文档/查询/REST/Analyzer）

> 依据 `docs/00-SPEC.md` §4（需求→模块→阶段映射）、§7（阶段验收）。
> 纪律：**RED → GREEN → REFACTOR**；每个页面/服务先写契约测试或类型，再实现；
> 每个任务完成后做两段式评审（① 符合任务定义 ② 可读性/DRY/YAGNI）。
> P1 验收于 2026-11-15 用户确认通过，本计划即日起执行。

## 1. P2 范围（SPEC §4 / §7）

| 需求章节 | 模块 | 本阶段交付 |
|---|---|---|
| 5.2 集群拓扑矩阵 | cluster topology | Shards 页以「节点 × 分片」拓扑矩阵呈现（含 unassigned 高亮） |
| 5.3 NOD 节点 | node | Nodes 页：`/_cat/nodes` + `/_nodes` 详情，健康/角色/heap/cpu/load/jvm/版本 |
| 5.4 SHA 分片 | shard | Shards 页：`/_cat/shards` 表 + 拓扑矩阵 + 未分配分片诊断入口 |
| 5.5 IDX 索引 | index | Index List 页：`/_cat/indices`；行操作 open/close/refresh/delete（危险确认） |
| 5.5 Aliases | index | Aliases 页：`/_cat/aliases` + `/_aliases`；增删别名（危险确认） |
| 5.6 DOC 文档 | document | Browser 页：选索引 → `_search` 采样、文档表、按 id 取、删文档（危险确认） |
| 5.7 QRY 查询 | query | Query Builder（表单式）+ DSL Editor（轻量 JSON 编辑器） |
| 5.8 API REST | rest | REST Console：method+path+body → `es/request`；状态/头/体；历史落 host.storage |
| 5.9 ANA Analyzer | analyzer | `_analyze` 测试器：text + analyzer/field/tokenizer/filters → token 流 |

需求 14.1 的 17 项 Head 功能对照**全部在 P2 完成，不允许以新功能替代**；
验收口径：17 项逐项实测通过、浏览器 10+ 页面无错误、危险操作确认、ReadOnly 拒绝写。

## 2. 架构决策（对 SPEC §3 的偏差说明，已确认合理）

SPEC §3 画了 `internal/core/{node,shard,index,document,query,analyzer}` 等 Go 包。
P2 采用如下落点，**理由充分、不构成 Head 功能削减**：

- **Transport 保持通用**：P1 的 `es/request` + `es/batch` 已对任意相对路径做权限守卫
  （只读拒绝 `-32003`、unsafe path `-32004`、超时 `-32010`、响应过大 `-32011`）。
  P2 的所有读/写/危险操作都走这两个方法，**不新增 Go RPC 方法**，避免后端面膨胀与维护双份解析。
- **业务解析放在前端服务层** `src/lib/services/`：ES 返回的原始 JSON 经 `es/batch`
  到达 UI，解析/类型化/危险分类在前端完成（与 Overview 现有模式一致）。这比在 Go 解析一遍、
  再经 JSON-RPC 回传结构化再在前端再解析，**少一层重复**（YAGNI）。
- **Go 只做安全边界**：扩展 `internal/core/permission` 的测试，覆盖 P2 新增的写路径
  （删索引、开/关索引、删文档、`_aliases`、`_analyze` 等），保证「ReadOnly 拒绝写」与
  「危险路径标识」在服务层被证明，前端确认弹窗只是 UX、非安全判定（符合 SPEC §5.1/§10）。

> 该偏差在 `docs/P2-ACCEPTANCE.md` 与 `docs/00-SPEC.md` §11 同步登记。

## 3. 共享基础设施（先建，复用）

- `src/lib/services.ts`（或 `src/lib/services/` 分域模块）：每个域一组函数 +
  类型 + 危险分类 `isDangerous(method, path)`。
- 组件：`DataTable.svelte`（可排序表格）、`ConfirmDanger.svelte`（危险操作确认弹窗，
  展示影响范围，需勾选/输入确认）、`JsonView.svelte`（美化 JSON，用于 REST/DSL/文档查看）、
  `HealthBadge.svelte` / `PrirepBadge.svelte`（health、p/r 标记）。
- `src/lib/api.ts` 现有 `EsResult`/`BatchResult`/`invoke` 复用；新增 `esRequest`（单次）封装。

## 4. 任务（粒度 2–10 分钟，带验证）

- **T1 共享服务层 + 组件**：`services.ts`（node/shard/index/document/query/analyzer/rest 的请求构造 + 解析 + 危险分类）、`DataTable`/`ConfirmDanger`/`JsonView`/`HealthBadge`/`PrirepBadge`。验证：`svelte-check` 0/0；危险分类单测（可用轻量 `vitest` 或纯函数 + 手测）。
- **T2 Nodes 页**：`/_cat/nodes?format=json` + `/_nodes`（name/ip/roles/heap/cpu/load/jvm/version）。验证：`npm run build` 通过；dev host `npm run dev:check` 断言页面挂载无 JS 错误。
- **T3 Shards / 拓扑矩阵页**：`/_cat/shards?format=json` 表 + 节点×分片矩阵 + unassigned 高亮。验证：同上。
- **T4 Index List 页**：`/_cat/indices?format=json&bytes=b`；行操作 open/close/refresh/delete → `es/request`（危险确认）。验证：只读连接下删除按钮点击触发 `-32003` 报错（GO 守卫），非只读下弹确认。
- **T5 Aliases 页**：`/_cat/aliases` + `/_aliases`；增删别名（`POST /_aliases`，危险确认）。验证：同上。
- **T6 Browser 页**：选索引 → `/{index}/_search`（采样）→ 文档表；按 id `GET /{index}/_doc/{id}`；删文档 `DELETE /{index}/_doc/{id}`（危险确认）。验证：同上。
- **T7 Query Builder 页**：表单（index、size、query 类型 term/match/range、sort）→ `POST /{index}/_search`；结果表。验证：同上。
- **T8 DSL Editor 页**：轻量 JSON 编辑器（高亮/格式化/校验/错误定位，SPEC §2.5 自研，不引 Monaco）；`POST /{index}/_search` 或 `/_search`；结果 + 历史。验证：同上。
- **T9 Analyzer 页**：`POST /_analyze`（text + analyzer/field/tokenizer/filters）→ token 流。验证：同上。
- **T10 REST Console 页**：method/path/body → `es/request`；展示 status/headers/body/耗时；历史（最近 N 条）落 `host.storage`（受 1 MiB 限制，仅存精简记录）；危险方法标记。验证：同上；历史持久化后重开仍可读。
- **T11 导航启用 + App 路由**：`stores.ts` 把 9 个 P2 项 `enabled:true`；`App.svelte` 增加 9 个页面渲染分支。验证：9 页均可从导航进入、无报错。
- **T12 构建 + 类型检查**：`npm run build`（单文件内联 + `<body>` 末尾 script）、`svelte-check` 0/0、相对路径断言。验证：baseline OK、verify 单文件断言。
- **T13 Go 权限测试扩展**：`permission/guard_test.go` 增加 P2 写路径（DELETE 索引、POST 索引/_close/_open、DELETE 文档、`POST /_aliases`、`POST /_analyze`）断言只读拒绝 + 危险标识。验证：`go test ./internal/core/permission` 全绿。
- **T14 真实 ES 集成验证**：D2 阈值（可用内存 ≥3 GB）下 `scripts/es-cluster.py start`；对每个 P2 页面对应真实 API 跑通（无静态假数据），并验证只读连接拒绝写（`smoke-test.py` 补 P2 用例或手测）。验证：11+ 真实 API 通过。
- **T15 打包 / 签名 / 包内回归**：`check-manifest` → `dbx-plugin package` → `sign-package` → `--verify` → `verify-package`（含单文件 + script 位置）→ 包内 exe 复跑 smoke。验证：全部退出码 0、`SIGNATURE VALID`。
- **T16 P2 验收报告 + 提交**：`docs/P2-ACCEPTANCE.md` 逐条核对 17 项 + 10+ 页面 + 危险确认 + ReadOnly 拒绝；两段式评审；`git` 提交。**停在用户验收，不擅自进 P3**。

## 5. 验证矩阵（P2 闸口）

| 项 | 命令/动作 | 期望 |
|---|---|---|
| 前端类型检查 | `npm run check` | svelte-check 0 error / 0 warning |
| 构建 | `npm run build` | DBX_UI_BUILD_SUCCESS；单文件内联；`<body>` 末尾 script |
| 单文件断言 | `scripts/verify-package.py`（包内） | 单文件 + script 位置 + Node crypto 验签 |
| Go 权限 | `go test ./internal/core/permission` | 全绿（含 P2 写路径） |
| 真实 ES | 启动后逐页验证 | 17 项 + 10+ 页面无错误、无假数据 |
| 只读守卫 | 只读连接触发写 | `-32003` 报错，前端展示 |
| 危险确认 | 危险操作 | 弹窗展示影响范围，确认后才发请求 |
| 交付 | 签名包 | `SIGNATURE VALID` |

## 6. 阶段闸口（进入 P3 前必须同时满足）

1. T1–T16 完成且上述验证矩阵全绿；
2. 两段式评审无 Critical；
3. 可安装 `.signed.dbxp` 产出且验签通过；
4. 用户明确验收 P2（D1 分批验收）。

**不擅自进入 P3。**
