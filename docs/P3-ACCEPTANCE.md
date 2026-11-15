# P3 阶段验收报告 · ES Studio

> 日期：2026-11-15　分支：`feat/phase-1`　插件：`local.es.studio` **0.2.0**
> 上游：`docs/00-SPEC.md` §4 映射表（5.5.6~5.5.9 / 5.10 / 5.11 / 5.7.3）、§7 P3 验收
> 计划：`docs/03-PLAN-P3.md`（P3-1 ~ P3-12）
> 测试环境：本地真实 Elasticsearch **8.19.23**（单节点 `es-studio-test`，127.0.0.1:19200）

---

## 1. 需求验收项（SPEC §7 P3）

### 1.1 9 个治理 / 运维功能域逐项实测（真实 API）

导航共 **17 项可用**（P1 的 Overview + P2 的 8 项 + P3 的 9 项），P4/P5 项仍带阶段标签且禁用。

| # | 导航项 | 页面 | 真实 API（实测通过） | 写操作（全部需确认） |
|---|---|---|---|---|
| 1 | Allocation | `Allocation.svelte` | `GET /{index}/_settings?flat_settings=true`、`GET /_cluster/allocation/explain` | `PUT /{index}/_settings`（副本数 / `index.routing.allocation.{include,exclude,require}`） |
| 2 | Cluster Settings | `ClusterSettings.svelte` | `GET /_cluster/settings?flat_settings=true`（可选 `include_defaults`） | `PUT /_cluster/settings`（persistent / transient，值留空 = `null` 删除） |
| 3 | Templates | `Templates.svelte` | `GET /_index_template`、`GET /_template`（composable 与 legacy 并列） | `PUT` / `DELETE /_index_template/{n}` |
| 4 | Component Templates | `ComponentTemplates.svelte` | `GET /_component_template` | `PUT` / `DELETE /_component_template/{n}` |
| 5 | Data Streams | `DataStreams.svelte` | `GET /_data_stream` | `POST /{ds}/_rollover`、`DELETE /_data_stream/{ds}` |
| 6 | Lifecycle | `Lifecycle.svelte` | `GET /_ilm/status`、`GET /_ilm/policy`、`GET /_all/_ilm/explain` | `POST /_ilm/{start,stop}`、`PUT`/`DELETE /_ilm/policy/{n}` |
| 7 | Query Profiler | `QueryProfiler.svelte` | `POST /{index}/_search`（`profile: true`） | 无（纯只读） |
| 8 | Snapshots | `Snapshots.svelte` | `GET /_snapshot`、`GET /_snapshot/{repo}/_all` | `PUT /_snapshot/{repo}`、`PUT .../{snap}?wait_for_completion=false`、`POST .../_restore`、`DELETE` |
| 9 | Tasks | `Tasks.svelte` | `GET /_tasks?group_by=none&detailed=true` | `POST /_tasks/{id}/_cancel` |

**真实数据佐证**（截图见 `screenshots/p3-rw/`）：

- Allocation：`metrics-daily`（2 主 2 副本）在单节点上留 5 个未分配副本 → 「Explain unassigned shard」返回 `node_allocation_decisions` 与 `same_shard` 原因，黄色集群状态由真实原因解释，不是写死的文案。
- Cluster Settings：`PERSISTENT` 2 项（`cluster.routing.allocation.enable=all`、`logsdb.prior_logs_usage=true`）、`TRANSIENT` 0 项，均取自真实 `_cluster/settings`。
- Templates / Component Templates：**57** 条索引模板（composable + legacy 并列）与 **73** 条组件模板全部列出，
  `composed_of` 展开为具体组件名（如 `logs@settings` → `logs@mappings, logs@settings, logs@custom, ecs@mappings`）。
- Data Streams：`logs-p3-demo`，`GREEN` / `gen 3` / 模板 `logs-p3-ds-template` / 3 个后备索引。
- Lifecycle：`GET /_ilm/policy` 的 **46** 条策略（内置 + 自建 `p3-demo-policy`），`min_age`、`version`、`modified_date` 全部真实字段。
- Query Profiler：`products` 2 分片 → 每分片 `query/rewrite/collector` 耗时（0.024 / 0.011 / 0.034 ms），查询树 `MatchAllDocsQuery`。
- Snapshots：仓库 `p3-fs-repo`（fs / `D:/tmp/es-studio/snapshots`）、快照 `snap-p3-demo`（`SUCCESS`，1 索引，2/2 分片，0.2s）。
- Tasks：真实异步 reindex（限速 40/s）在列，`Running (s)` 实时增长，`Cancellable=yes`。

### 1.2 危险操作确认（要求 16.4：不允许外壳 / 装饰性按钮）

全部 P3 写操作走同一个 `ConfirmDanger`（勾选后才可点确认），实测覆盖：删除索引、关闭/打开索引、
rollover 数据流、删除数据流、写集群设置、改副本数 / 路由规则、删除 ILM 策略、删除模板、
删除仓库 / 快照、恢复快照、**取消集群任务**。无一处直接执行。

### 1.3 ReadOnly 连接拒绝写（安全边界在 Go 服务层）

`internal/core/permission` 对 P3 全部写路径返回 **-32003**（不是前端隐藏按钮）：`_tasks/*/_cancel`、
`_data_stream/*/_rollover`、`_ilm/policy/*`、`_index_template/*`、`_component_template/*`、
`_cluster/settings`、`_snapshot/*`、`_restore`。只读连接上 `GET /_tasks`、`GET /_ilm/policy` 等读路径正常放行。

### 1.4 浏览器端到端实测（脱离 DBX 宿主，真实 ES + 真实数据）

用 `scripts/browser-bridge.mjs` 拉起真实 Go Sidecar + 注入 `window.dbxPlugin`，Playwright 驱动系统 Chrome
走**产品真实链路**（UI → Sidecar → ES），不经过 DBX 外壳。

| 套件 | 覆盖 | 结果 |
|---|---|---|
| `browser-test.py`（读写连接） | 17 页渲染 + 8 个交互流程（含真实任务列表） | **24/24** |
| `browser-write-test.py --only rw` | 关闭/打开索引、refresh、REST PUT、删除索引、rollover、写集群设置、取消任务 —— 每步回查 ES 真实状态 | **8/8** |
| `browser-write-test.py --only ro` | 4 条只读拒绝 + 资源未被破坏 | **6/6** |
| 控制台报错 | 全部页面 | **0** |

写路径的回查不是"看提示语"：如 rollover 后比对 `_data_stream` 的 `generation` 增量、取消任务后
轮询 `_tasks` 直到该 action 消失、写集群设置后重读 `_cluster/settings` 比对值。

### 1.5 P3 期间由「真实数据」暴露并修复的缺陷

这些问题空跑/单元测试都抓不到，只有接真实 ES + 真实数据才会现形：

| # | 现象 | 根因 | 修复 |
|---|---|---|---|
| 1 | **Tasks 页永远空**（集群明明有 reindex 在跑） | ES 8.x `GET /_tasks?group_by=none` 返回**扁平 `tasks` 数组**，解析器按 `nodes{}.tasks{}` 取 → 恒空 | `fetchTasks` 同时兼容两种形态，并过滤掉本次请求自身产生的 `cluster:monitor/tasks/lists`；smoke 新增形状断言 |
| 2 | Profiler 节点列显示 `aggregations`、耗时恒 0 | 按 7.x `{nodeId:{...}}` 解析，8.x 是扁平 `node_id`/`index`/`shard_id` | 两种形态都支持 |
| 3 | Lifecycle 出现 `Invalid Date` | 把 ISO 字符串当毫秒数塞进 `new Date(number)` | 解析失败回退原字符串 |
| 4 | Templates `Ver` / `Priority` 显示 `2147483647` | ES 用 `Integer.MAX_VALUE` 表示"未设置版本/优先级" | ≥1,000,000 视为未设置，显示 `-` |
| 5 | Shards 页崩溃（`each_key_duplicate`） | 多副本同状态（`metrics-daily` replicas:2 → 两个 `0r:UNASSIGNED`）导致行 key 重复 | 行 key 改为「位置 + 语义」`ShardTag{key,label}` |
| 6 | 所有数字列恒 0（分片数、文档数、耗时） | `_cat/*` 返回字符串，`num()` 只接受 `number` | `num()` 接受数字字符串 |
| 7 | 错误码 -32003 被显示成 -32000 | `single()` 的 catch 丢了 `cause.code` | 保留原始 code |
| 8 | 主按钮 hover 变成白字白底 | `button:hover:not(:disabled)` 特异性 (0,2,1) 压过 `.primary` (0,1,0) | 排除 `.primary` + `brightness(0.92)`（8 个页面统一） |
| 9 | 浏览器搜索要手填字段名（用户反馈） | 无 mapping 驱动的字段候选 | `fetchFields()` 递归解析 nested/multi-field，Search 框旁给 Field 下拉，默认「All fields (n)」 |

### 1.6 测试基建（P3 新增）

- `scripts/es-task-demo.sh start|stop|status`：造一个**真实长跑任务**（`_bulk` 灌 8000 文档 + 限速 40/s 的
  `_reindex?wait_for_completion=false`），让 Tasks 页有真数据可验；`stop` 取消任务并清理两个索引。
  内存占用约 4 MB，符合 D1 资源约束。
- `scripts/seed-es-data.sh` 扩展 P3 夹具：组件模板 `p3-demo-component`、索引模板 `p3-demo-template`、
  数据流 `logs-p3-demo`（含 rollover 到 gen 3）、ILM 策略 `p3-demo-policy`、快照仓库 `p3-fs-repo` + 快照 `snap-p3-demo`。
- `scripts/es-cluster.py`：`clean_stale()` 不再无条件清空 `data/`（会误删 135 个真实数据文件），
  配置模板补 `path.repo`（快照仓库必需）。
- `smoke-test.py` 新增 **28 条 P3 用例**：13 条 P3 端点读 + 形状断言（含 `_tasks` 扁平数组这条回归钉子）+ 9 条 P3 只读拒绝 + 1 条只读放行
（`/_tasks` 只读放行）。61 + 28 = **89**。

---

## 2. PLAN P3-1 ~ P3-12 执行结果

| # | 任务 | 结果 |
|---|---|---|
| P3-1 | 服务层 9 组请求构造 + 解析 | ✅ `services.ts` 追加 9 段，`svelte-check` 0/0 |
| P3-2 | Allocation 页（含 explain） | ✅ 真实 yellow 集群可解释未分配原因 |
| P3-3 | Cluster Settings 页 | ✅ persistent/transient 分栏，写入后回读一致（浏览器实测 wr-07） |
| P3-4 | Templates + Component Templates | ✅ composable 优先、legacy 并列，列表/详情/删除/新建 |
| P3-5 | Data Streams + Lifecycle | ✅ 真实 rollover（gen 3→4 已验），策略/explain/启停 |
| P3-6 | Query Profiler | ✅ 扁平耗时表 + 查询树，非空真实耗时 |
| P3-7 | Snapshots + Tasks | ✅ 仓库/快照/恢复；任务列表/取消（真实 reindex） |
| P3-8 | 导航启用 + 路由 + phase chip | ✅ 17 项可点，chip 显示 `P3 · Governance & Ops` |
| P3-9 | 危险分类 + Go 权限测试 | ✅ `DescribeRisk` 补齐 P3 端点，`go test ./internal/core/permission` 全绿 |
| P3-10 | 造数据 + 浏览器实测 | ✅ 见 §1.4，24/8/6 全绿 |
| P3-11 | 打包 / 签名 / 包内回归 | ✅ 见 §3 验证矩阵 |
| P3-12 | 验收文档 + 提交 | ✅ 本文件 + `git` 提交 |

---

## 3. 验证矩阵汇总（P3 闸口）

| 项 | 命令/动作 | 结果 |
|---|---|---|
| 前端类型检查 | `npm run check` | svelte-check **0 error / 0 warning** |
| 前端构建 | `npm run build` | `DBX_UI_BUILD_SUCCESS`；单文件；`<body>` 末尾 script |
| Go 单元 + vet | `go test ./...` / `go vet ./...` | 全部包通过；vet 干净 |
| 清单预检（源码） | `check-manifest.py` | **52 runtime checks** + schema |
| 清单预检（含包） | `check-manifest.py --package` | **105 runtime checks** + schema |
| 真实 ES 集成 smoke | `smoke-test.py --es-url ...` | **89 passed, 0 failed**（P2 为 61，新增 28 条 P3） |
| 签名验签 | `sign-package.py --verify` | **SIGNATURE VALID**（key_id=local.es.studio.release） |
| 包内完整性 | `verify-package.py --package` | **17 passed, 0 failed** |
| 包内 exe 复跑 smoke | `smoke-test.py --sidecar .dbx-dev/verify/es-studio-sidecar.exe` | **89 passed, 0 failed** |
| 浏览器实测（读写，真实数据） | `browser-test.py --out screenshots/p3-rw` | **24/24**，控制台 0 报错 |
| 浏览器写路径（回查 ES 状态） | `browser-write-test.py --only rw` | **8/8** |
| 浏览器只读拒绝 | `browser-write-test.py --only ro` | **6/6** |

---

## 4. 两段式评审

**① 符合任务定义**：9 个 P3 功能域全部读真实 API，无 mock、无占位按钮；全部写操作经确认弹窗；
只读连接在 Go 服务层拒绝（-32003）；导航 17 项可用，P4/P5 仍标注并禁用。浏览器实测暴露 9 个真实缺陷并全部修复。

**② 可读性 / DRY / YAGNI**：
- 继续复用 P1 的通用 `es/request` + `es/batch`，**P3 未新增任何 Go RPC 方法**（9 个功能域只在前端 `services.ts` 追加解析），
  与 P2 架构决策一致：Go 只做安全边界，业务解析在 UI 侧，避免后端面膨胀与双份解析。
- 表格/空态/错误态复用 P2 的 `DataTable` / `StateBlock` / `ConfirmDanger` / `JsonView`，无重复实现。
- 关键 RPC 形状不靠"记得"：`/_tasks?group_by=none` 的扁平数组在 smoke 里用断言钉死，防回归。
- 版本号由 `manifest.json` 单点驱动（smoke 与 verify-package 改为读 manifest 比对，不再硬编码），避免下次改版本又漏改测试。

**评审结论：无 Critical。**

**已知限制（不阻塞验收）**：
- Allocation 页只展示显式设置的 `include/exclude/require`，ES 8.x 自动写入的 `include._tier_preference` 未单列；
  需要看分层原因的用户走「Explain unassigned shard」（`node_allocation_decisions` 内含 `node_version` / `same_shard` 等）。
- Tasks 的 `Node` 列显示节点 id（非 name），与 `Task id` 前缀一致，长短不影响取消操作。
- Snapshots 的 `PUT /_snapshot/{repo}` 需目标节点已配置 `path.repo`，否则 ES 返回 `repository_verification_exception`，
  页面原样透出该错误文本（未做友好化包装）。

---

## 5. 交付物与阶段闸口

- 可安装包：`dist/local.es.studio-0.2.0-windows-x64.signed.dbxp`（4,425,944 B），`SIGNATURE VALID`，
  信任公钥已注册至 `%APPDATA%/com.dbx.app/plugins/.trust/keys.json`。
- **版本号从 0.1.0 升至 0.2.0**：DBX 不允许同版本号覆盖安装，升版本才能与已装的 P2 包区分；旧 0.1.0 可保留或卸载。
- 文档：`docs/P3-ACCEPTANCE.md`（本文件）、`docs/03-PLAN-P3.md`。
- 测试：`scripts/smoke-test.py`（89/89）、`backend/internal/core/permission/guard_test.go`（含 P3 写路径）。
- 浏览器实测工具链：`scripts/browser-bridge.mjs`、`scripts/seed-es-data.sh`、`scripts/es-task-demo.sh`、
  `scripts/browser-test.py`、`scripts/browser-write-test.py`；截图 `screenshots/p3-rw`、`screenshots/p3-write`、`screenshots/p3-ro`。

**阶段闸口（进入 P4 前）：**
1. ✅ P3-1 ~ P3-12 完成，验证矩阵全绿；
2. ✅ 两段式评审无 Critical；
3. ✅ 可安装 `.signed.dbxp` 产出且验签通过、包内 exe 复跑 smoke 全绿；
4. ⏳ **用户明确验收 P3（D1 分批验收）**。

> **不擅自进入 P4。** 待用户在 DBX Plugin Center 安装 `local.es.studio-0.2.0-windows-x64.signed.dbxp`
> 并实测确认后，再开启 P4（Cluster Metrics / Node Metrics / History / Cluster Diagnostics / Multi-Cluster Compare）。

---

## 6. 0.2.1 · 0.1.0 验收反馈修复

用户在 0.1.0（P2 包）验收时逐条提了 11 个体验问题。全部按「先看真实数据、再改、
再回查」的方式处理，并顺手把「同一类需要手填的地方」全局排查了一遍（见 §6.2）。

### 6.1 逐条处理

| # | 反馈 | 原因 | 处理 | 验证 |
|---|---|---|---|---|
| 1 | Shards & Topology 没看懂，分配中 / 迁移中的分片没体现 | `_cat/shards` 的 `INITIALIZING`/`RELOCATING` 被归到灰色 `other`，页面也没有任何在途统计；未分配还没给原因 | 顶部四张状态卡（已就绪/分配中/迁移中/未分配，带真实计数与解释）、在途横幅、拓扑里分配中=蓝/迁移中=琥珀、表格新增「未分配原因」「已等待」列；**有在途分片时每 2 秒自动刷新**（可关） | 用真实集群制造了一次 `INITIALIZING`：把 66MB 索引快照后以 `indices.recovery.max_bytes_per_sec=1mb` 限速恢复，页面显示「分配中 1」、`big-restored 0p:INITIALIZING` 蓝色徽标、横幅「集群正在搬运 1 个分片副本」，时间戳 12:56:29→12:56:31 证明自动刷新生效（截图 `screenshots/ux/ux-shards-initializing*.png`） |
| 2 | Aliases 不能筛选 | 页面没有筛选控件 | 加筛选框（别名 / 索引 / 过滤条件三处一起匹配）+「只看带过滤条件的」开关 + `n / total` 计数 | UX 用例：输入 `prod` 后 2→1 行；输入不存在的词出空态 |
| 3 | Aliases 新增别名时原索引名要自己填 | 裸 `<input>` | 改成索引下拉，选项里带「该索引已有 N 个别名」，没选索引时按钮禁用 | UX 用例断言下拉存在且含真实索引 |
| 4 | 数据列 Filter 是做什么的 | `_cat/aliases` 用字面量 `-` 表示"无"，页面把 `-` 原样显示，看着像无意义的一列 | 语义澄清：表头「Filter（别名过滤条件）」、页面说明解释 alias filter 的作用与写入约束、有过滤时渲染黄色 `有过滤条件` 标签 + 表达式（悬浮看全文）、无过滤显示「无（整个索引可见）」；同时补 `write` 与 `routing` 列 | `fetchAliases` 不再把 `-` 当值；UX 用例断言 header 里有解释文案 |
| 5 | Document Browser 索引后面的 Search 是什么意思 | 输入框只标 `Search`，用户不知道它会发什么查询 | 标签改 `Keyword（关键字）`、字段下拉改 `Search in`、按钮改 `Search documents`，并在下方实时显示**将要执行的查询**：`match_all` / `multi_match「kw」on 全部 N 个字段` / `match「kw」on name（text）` | UX 用例断言三种状态下计划文案依次变化，搜到结果 |
| 6 | Query Builder 不能组合 must / must_not / should | `buildDsl` 写死 term/range→filter、match→must，没有排除能力 | 每条条件自带 occur 下拉（must / filter / should / must_not，各带中文说明），bool 里分别成组；出现 should 时自动补 `minimum_should_match`（默认 1，可改） | 真实集群计数断言：`must_not category=peripheral` → 3/6；`should display + should accessory`（msm=1）→ 3/6；断言生成的 DSL 里有 `bool.should` |
| 7 | 更换索引不清理查询条件 | 切索引只换 `index`，条件原样留着 → 换索引后静默 0 命中，像查询写错了 | 切换索引即清空该 tab 的条件/排序/结果并重新拉字段，页面有明确提示 | UX 用例：切到 orders 后 `.clause` 数量为 0 |
| 8 | 建议多 tab，一个 tab 处理一个索引 | 单实例状态，换索引会互相覆盖 | 加了 tab 条：每个 tab 独立保存索引、条件、size、排序、msm 和**自己的结果**；`+ 新 tab` 自动选一个还没用的索引，`×` 关闭（至少留一个） | UX 用例：新 tab 不继承旧条件；两个 tab 分别显示 products / orders |
| 9 | 查询条件应按字段类型，而不是固定几种 | 操作符是写死的 5 项，`long` 上也能选 `match` | 操作符由 mapping 类型推导：数值/日期→range/term/exists；boolean→term(true/false)/exists；keyword→term/wildcard/prefix/exists；text→match/match_phrase/term/prefix/wildcard/exists；object/nested→exists。改字段时**同时把操作符重置为该类型的默认值**（否则会留下 term 打在 text 上、静默 0 命中——这次就是靠浏览器实测抓到的） | UX 用例：`price` 的操作符里有 range、没有 match；`name` 里有 match 与 match_phrase |
| 10 | DSL Editor 索引也要自己写 | 裸 `<input>` | 改索引下拉（`All indices (*)` + 真实索引） | UX 用例断言下拉存在 |
| 11 | 条件输入框按 Tab 光标会跳到下面的数据组件 | `textarea` 没拦 Tab，浏览器默认把焦点移走 | Tab 插入 2 空格并保持焦点；`Esc` 后 Tab 才跳出编辑器；`Ctrl/⌘+Enter` 直接运行，页面上写明这些快捷键 | UX 用例：Tab 后内容长度 +2 且 `document.activeElement` 仍是 TEXTAREA；`Ctrl+Enter` 命中 products 6 条 |

### 6.2 主动补齐的同类问题（用户说「这种类似的地方不要我提醒了」）

| 位置 | 原来 | 现在 |
|---|---|---|
| Analyzer · Context index | 裸输入框 | 索引下拉（含「不指定索引」用全局 `_analyze`） |
| Analyzer · Field | 裸输入框，靠记忆拼字段名 | 下拉，选项来自所选索引的 mapping（带类型），换索引自动清空 |
| Analyzer · Analyzer / Tokenizer / Filters | 裸输入框 | 仍可自由输入，但补了 ES 内置候选 datalist |
| Snapshots · 创建快照的索引 | 手填 `products,orders` | 多选列表（不选 = 全部 `*`） |

### 6.3 验证矩阵（0.2.1）

| 项 | 命令 | 结果 |
|---|---|---|
| 类型检查 / 构建 | `npm run check` / `npm run build` | **0 error 0 warning**；单文件 UI |
| Go | `go test ./...` / `go vet ./...` | 全绿 |
| 浏览器主回归（启动 + 15 页 + 8 个交互流程） | `browser-test.py` | **23/23**，控制台 0 报错（另有 1 条真实任务用例需 `scripts/es-task-demo.sh` 夹具，单独跑过） |
| 浏览器体验回归（本轮 11 项 + 2 处补齐） | `browser-ux-test.py` | **31/31**，控制台 0 报错 |
| 浏览器写路径 / 只读拒绝 | `browser-write-test.py --only rw|ro` | **8/8**、**6/6** |
| 真实 ES smoke | `smoke-test.py` | **89/89** |
| 签名 / 包内完整性 / 包内 exe | `sign-package.py --verify`、`verify-package.py`、`smoke-test.py --sidecar` | **SIGNATURE VALID**、**17/17**、**89/89** |
| 清单预检 | `check-manifest.py`（源码 / 含包） | **52** / **105** |

**新增回归脚本**：`scripts/browser-ux-test.py`（31 条断言），把用户提的每一条都钉成用例，
下次改这几个页面会立刻红。

### 6.4 已知限制（本轮未做，需要更多节点/时间才有意义）

- **RELOCATING（迁移中）无法在本机实测**：单节点集群不会发生分片迁移。代码路径与
  INITIALIZING 完全一致（同一个 `stateText`/`stateClass` 映射，同一套配色与计数），
  INITIALIZING 已用真实分片验证；要真验 RELOCATING 需 ≥2 个节点（`_cluster/reroute` move）。
- Query Builder 的 `range` 目前只给 gte/lte 两个输入（gt/lt 在 `buildDsl` 里支持、UI 未暴露）。
- 快照创建的索引多选没有「全选/反选」按钮，索引特别多时需要按住 Ctrl 多选。
