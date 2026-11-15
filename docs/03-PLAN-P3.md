# 阶段 3 实施计划（P3 · Governance & Operations）

> 上游：`docs/00-SPEC.md` §4 映射表（5.5.6~5.5.9 / 5.10 / 5.11 / 5.7.3）、§7 P3 验收
> 「Templates / Data Streams / ILM / Snapshot / Task / Profiler 真实 API 验证」。
> 改需求先改 SPEC，本文件只做拆解。

## 1. 交付范围（导航中的 8 个 P3 项）

| 导航项 | 页面 | 真实 API | 写操作（需确认） |
|---|---|---|---|
| Allocation | `Allocation.svelte` | `GET /_settings?flat_settings`、`GET /_cluster/allocation/explain` | `PUT /{index}/_settings`（副本数 / `index.routing.allocation.*`） |
| Cluster Settings | `ClusterSettings.svelte` | `GET /_cluster/settings?flat_settings` | `PUT /_cluster/settings`（persistent / transient，可设 null 删除） |
| Templates | `Templates.svelte` | `GET /_index_template`、`GET /_template` | `PUT /_index_template/{n}`、`DELETE` |
| Component Templates | `ComponentTemplates.svelte` | `GET /_component_template` | `PUT`、`DELETE` |
| Data Streams | `DataStreams.svelte` | `GET /_data_stream` | `POST /{ds}/_rollover`、`DELETE /_data_stream/{ds}` |
| Lifecycle | `Lifecycle.svelte` | `GET /_ilm/status`、`GET /_ilm/policy`、`GET /_ilm/explain` | `POST /_ilm/start|stop`、`PUT/DELETE /_ilm/policy/{n}` |
| Query Profiler | `QueryProfiler.svelte` | `POST /{index}/_search`（`profile: true`） | 无（纯只读） |
| Snapshots | `Snapshots.svelte` | `GET /_snapshot`、`GET /_snapshot/{repo}/_all` | `PUT /_snapshot/{repo}`、`PUT .../{snap}`、`POST .../_restore`、`DELETE` |
| Tasks | `Tasks.svelte` | `GET /_tasks?group_by=none&detailed=true`（8.x 返回扁平 `tasks` 数组） | `POST /_tasks/{id}/_cancel` |

## 2. 架构约束（沿用 P2 决策，不回退）

- 只走通用 `es/request` + `es/batch`；不新增 Go RPC 方法。业务解析继续放前端
  `src/lib/services.ts`（P3 追加一段，复用 `single` / `batch` / `isError` / `record`）。
- 安全边界仍在 Go：只读连接写操作由 `internal/core/permission` 拒绝（-32003）。
  P3 只在 `DescribeRisk` 里补齐新端点的展示名与影响范围。
- 危险操作一律走 `ConfirmDanger`（勾选确认后才可点），不提供"装饰性"按钮。
- 关闭索引 / 无 mapping 等不可读资源必须给空态，不得抛错。

## 3. 任务拆分

| # | 任务 | 验收 |
|---|---|---|
| P3-1 | 服务层：allocation / cluster-settings / index-template / component-template / data-stream / ilm / profile / snapshot / task 的请求构造 + 解析 | `svelte-check` 0/0，类型齐全 |
| P3-2 | Allocation 页（含未分配分片 explain） | 真实集群 yellow 场景可解释原因 |
| P3-3 | Cluster Settings 页（persistent/transient 分栏 + 修改/删除） | 写入后回读一致 |
| P3-4 | Templates + Component Templates 页（composable 优先，legacy `_template` 并列） | 列表/详情/删除/新建 |
| P3-5 | Data Streams + Lifecycle 页（状态、策略、explain、rollover、删除） | 真实数据流可 rollover |
| P3-6 | Query Profiler 页（profile 树扁平化为耗时表） | 展示每分片 query 耗时，非空 |
| P3-7 | Snapshots + Tasks 页（仓库/快照/恢复；任务列表/取消） | 均可读真实状态 |
| P3-8 | 导航启用 + 路由 + phase chip → `P3 · Governance & Ops` | 8 项可点，无占位 |
| P3-9 | 危险分类补齐 + Go 权限测试（只读拒绝 P3 写路径） | `go test ./internal/core/permission` 全绿 |
| P3-10 | 造 P3 数据（数据流/ILM 策略/模板/快照仓库）+ 浏览器实测 | 新增页面全部渲染真实数据 |
| P3-11 | 打包 / 签名 / 包内 exe 回归 | `SIGNATURE VALID`、`verify-package` 全 PASS、smoke 全绿 |
| P3-12 | `docs/P3-ACCEPTANCE.md` + 提交 | 停在用户验收，不擅自进 P4 |

## 4. 阶段闸口

1. 8 个 P3 页面全部读**真实 API**，无 mock、无占位按钮；
2. 写操作全部经确认弹窗，只读连接在服务层被拒（-32003）；
3. 浏览器实测（真实 ES + 真实数据）全绿；`svelte-check` 0 error 0 warning；
4. 签名包可安装 + `verify-package` 全 PASS；
5. 用户确认后再进 P4（D1 分批验收）。
