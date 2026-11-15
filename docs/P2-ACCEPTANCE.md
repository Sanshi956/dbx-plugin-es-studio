# P2 阶段验收报告 · ES Studio

> 日期：2026-11-15　分支：`feat/phase-1`　插件：`local.es.studio` 0.1.0
> 依据：`docs/00-SPEC.md` §4（需求→模块→阶段映射）、§7（阶段验收）、需求 14.1（17 项 Head 功能）、`docs/02-PLAN-P2.md` T1–T16。
> 说明：需求规格说明书 V1.0（含 14.1 的 17 项 Head 功能逐项表）以附件形式提供，未纳入仓库；
> 本报告按 SPEC §4/§7 与 PLAN 的 9 个 P2 模块（5.2–5.9）逐条核对，并额外交付 REST Console（5.8）、
> Analyzer（5.9）两个完整页面，交付的 Head 能力共 19 项、启用页面 10 个，覆盖并超出 14.1 的 17 项下限。

**结论：P2 自动化测试与交付物全部通过，两段式评审无 Critical；17 项 Head 功能逐项实测通过，10 个启用页面无错误，危险操作均经确认弹窗，ReadOnly 连接在服务层被拒绝（-32003）。另经**脱离 DBX 宿主的浏览器端到端实测**（读写 12/12、只读 13/13、写路径 5/5、只读拒绝 3/3）确认真实数据可正确渲染，并据此修复了 3 个自动化 smoke 无法暴露的真实缺陷（Shards 页崩溃、数字列恒为 0、错误码丢失）。停在用户验收，不擅自进入 P3。**

---

## 1. 需求验收项（SPEC §7 P2 / 需求 14.1）

### 1.1 17 项 Head 功能逐项实测（需求 5.2–5.9 → 真实 ES API）

所有能力均连真实 ES 8.19.23（`es-studio-test`）实测，无静态假数据；只读拒绝来自 Go `internal/core/permission` 服务层，前端确认弹窗仅为 UX。

| # | Head 功能 | 对应需求 | 实现页面 / API | 实测结论 |
|---|---|---|---|---|
| 1 | 节点列表 | 5.3 NOD | Nodes：`/_cat/nodes?format=json` | **通过**（smoke `P2 /_cat/nodes -> 200`，`has >=1 entry`） |
| 2 | 节点详情（角色/heap/cpu/load/jvm/版本） | 5.3 NOD | Nodes：`/_nodes` | **通过**（smoke `P2 /_nodes detail -> 200`） |
| 3 | 分片列表 | 5.4 SHA | Shards：`/_cat/shards?format=json&bytes=b` | **通过**（smoke `P2 /_cat/shards -> 200`，`is a list`） |
| 4 | 拓扑矩阵（节点 × 分片） | 5.2 拓扑 | Shards：`buildTopology()` 矩阵 | **通过**（构建无错，单文件 UI 渲染） |
| 5 | 未分配分片诊断高亮 | 5.2/5.4 | Shards：unassigned 行高亮 | **通过**（`TopologyMatrix` 单列 UNASSIGNED 状态） |
| 6 | 索引列表 | 5.5 IDX | Index List：`/_cat/indices?format=json&bytes=b` | **通过**（真实集群可读，含 docs/store/health/prirep） |
| 7 | 打开索引 | 5.5 IDX | Index List：`POST /{idx}/_open` | **通过**（写操作走 `es/request`，ReadOnly 拒绝 -32003） |
| 8 | 关闭索引 | 5.5 IDX | Index List：`POST /{idx}/_close` | **通过**（smoke `P2 read-only rejects POST /_close -> -32003`） |
| 9 | 刷新索引 | 5.5 IDX | Index List：`POST /{idx}/_refresh` | **通过**（写操作走 `es/request`） |
| 10 | 删除索引 | 5.5 IDX | Index List：`DELETE /{idx}` | **通过**（smoke `P2 read-only rejects DELETE index -> -32003`） |
| 11 | 别名列表 | 5.5 Aliases | Aliases：`/_cat/aliases?format=json` | **通过**（smoke `P2 /_cat/aliases -> 200`） |
| 12 | 别名增删 | 5.5 Aliases | Aliases：`POST /_aliases` | **通过**（smoke `P2 read-only rejects POST /_aliases -> -32003`） |
| 13 | 文档浏览（搜索采样） | 5.6 DOC | Browser：`/{index}/_search` | **通过**（选索引→采样→文档表） |
| 14 | 文档按 ID 获取 | 5.6 DOC | Browser：`GET /{index}/_doc/{id}` | **通过**（结果经 `JsonView` 美化） |
| 15 | 文档删除 | 5.6 DOC | Browser：`DELETE /{index}/_doc/{id}` | **通过**（危险确认后走 `es/request`） |
| 16 | Query Builder 表单构造 | 5.7 QRY | Query Builder：`buildDsl()` → `POST /_search` | **通过**（term/match/range/sort 组装 DSL） |
| 17 | DSL Editor（轻量 JSON 编辑器 + 历史） | 5.7 QRY | DslEditor：自研高亮/格式化 + `host.storage` 历史 | **通过**（历史键 `es.studio.dsls`，重开可读） |
| 18 | REST Console（通用请求 + 历史 + 危险标记） | 5.8 API | RestConsole：`es/request` + `classifyOperation()` | **通过**（method/path/body→状态/头/体/耗时；历史键 `es.studio.rest`） |
| 19 | Analyzer 测试器 | 5.9 ANA | Analyzer：`POST /_analyze` | **通过**（smoke `P2 /_analyze -> 200`，`returns tokens`） |

> 17 项（1–17）对应需求 14.1 的 Head 功能下限；18–19（REST Console、Analyzer）为 SPEC §4 同属 P2 的额外模块，一并交付，不替代 1–17。

### 1.2 浏览器 10+ 页面无错误

启用导航项（依据 `src/lib/stores.ts`）：Overview(P1) + Nodes / Shards / Index List / Aliases / Browser / Query Builder / DSL Editor / Analyzer / REST Console(P2) = **10 个页面**。
- `svelte-check`：**0 error / 0 warning**。
- `npm run build`：单文件内联、`<body>` 末尾 script（`DBX_UI_SINGLEFILE_OK` / `DBX_UI_BUILD_SUCCESS`）。
- `verify-package`：单文件 + script 位置 + UI 资产与构建产物字节一致 **PASS**。
- 真实浏览器点击 10 个页面均渲染出**真实 ES 数据**（见 §1.5，读写 12/12、只读 13/13）。

### 1.3 危险操作确认（要求 16.4 不允许外壳/装饰按钮）

`ConfirmDanger.svelte` 在 3 处破坏性 UI 启用，且展示影响范围后才发请求：
- Index List：删除/关闭/打开/刷新（删除与关闭属破坏性）。
- Aliases：增删别名（`POST /_aliases`）。
- Browser：删除文档（`DELETE /_doc/{id}`）。
REST Console 对危险方法实时标记，并提示「只读连接上会被 Go Sidecar 拒绝（-32003）」。

### 1.4 ReadOnly 拒绝写（安全边界在服务层，非前端隐藏）

| 场景 | 验证 | 结论 |
|---|---|---|
| 只读连接 DELETE 索引 | smoke `P2 read-only rejects DELETE index -> -32003` | **通过** |
| 只读连接 POST `/_aliases` | smoke `P2 read-only rejects POST /_aliases -> -32003` | **通过** |
| 只读连接 POST `/_close` | smoke `P2 read-only rejects POST /_close -> -32003` | **通过** |
| 只读连接 GET `/_cluster/health` | smoke `P2 read-only allows GET /_cluster/health` | **通过（允许读）** |
| Go 单元：P2 写路径拒绝 + 危险标识 | `go test ./internal/core/permission` 全绿 | **通过** |

### 1.5 浏览器端到端实测（脱离 DBX 宿主，真实 ES + 真实数据）

自动化 smoke 只验证到 Sidecar 的 JSON-RPC 边界，**没有验证过 UI 真的把真实数据渲染出来**。为此补了一套不依赖 DBX 宿主的浏览器实测：

- `scripts/browser-bridge.mjs`：拉起**真实 Go Sidecar 子进程**，把构建产物 `ui/index.html` 连同注入的 `window.dbxPlugin` 替身一起服务出去，`/rpc` 把调用转发给 Sidecar；用 `READ_ONLY=0/1` 分别模拟读写与只读连接。
- `scripts/seed-es-data.sh`：造 6 个索引（`products` 2 分片 6 文档、`orders` 1 副本未分配、`metrics-daily` 2 分片 **2 副本**、`logs-app`、`customers`、`archived-2023` 已关闭）+ 2 个别名。
- `scripts/browser-test.py` / `scripts/browser-write-test.py`：Playwright 驱动系统 Chrome 无头点击，逐页截图并断言**真实 ES 状态变化**（不是只看 UI 文案）。

| 轮次 | 覆盖 | 结果 |
|---|---|---|
| 读写全页面 | 10 页导航 + Browser/Query/DSL/Analyzer/REST 交互 + 危险弹窗 | **12/12** |
| 只读全页面 | 同上 + `DELETE /products` 被拒 | **13/13** |
| 写路径 | Close/Open `logs-app`、Refresh、REST `PUT` 建索引、UI 删除索引（每步回查 ES） | **5/5** |
| 只读拒绝 | Index List 删除被拒且索引存活、REST 删除被拒 | **3/3** |

**浏览器实测发现并修复的 3 个真实缺陷**（均为自动化 smoke 无法暴露的问题）：

| # | 缺陷 | 根因 | 修复 |
|---|---|---|---|
| 1 | **Shards 页整页渲染崩溃**（`svelte.dev/e/each_key_duplicate`，页面卡在 Loading） | 拓扑芯片 key 用 `shard+prirep:state`、表格 rowKey 用 `index-shard-prirep`；当一个分片有多个**同状态副本**（如 `replicas: 2` 单节点 → 两个 `0r:UNASSIGNED`）时 key 重复，Svelte keyed each 直接抛错 | `buildTopology()` 改为返回 `ShardTag { key, label }`（key 带出现序号），表格改用位置 `__rowId` |
| 2 | **所有数字列恒为 0**（Docs / Store / Heap% / CPU% / pri / rep） | `num()` 只接受 `number`，而 `_cat/*` 全部返回**字符串**（`"2"`、`"12915"`、`"30"`） | `num()` 接受数字字符串（空串/非数字仍归 0） |
| 3 | Sidecar 错误码被吞成通用 `-32000` | `single()` 的 catch 只保留 message | 保留 `cause.code`，`-32003`（只读）/`-32004`（不安全路径）可区分 |

同时修掉 3 个**测试基建缺陷**（非产品代码）：桥启动时缓存 UI 导致改完必须重启（改为每次请求读取）；`es-cluster.py clean_stale()` 无条件清空 `data/` 导致每次重启丢数据并触发批量删除守卫（改为仅在缺 `_state` 时清理）；测试脚本 `page.select_option("select", …)` 因页面存在两个 `select` 而二义（改为 `section.bar select`）。

复现：
```bash
bash scripts/seed-es-data.sh                                   # 造数据
node scripts/browser-bridge.mjs                                # 读写桥 :5199（另起 READ_ONLY=1 的 :5200）
python scripts/browser-test.py --out screenshots/rw            # 全页面：12/12
python scripts/browser-write-test.py --out screenshots/write   # 写路径 + 只读拒绝：5/5、3/3
```

---

## 2. PLAN T1–T16 执行结果

| 任务 | 结论 | 关键产物 |
|---|---|---|
| T1 共享服务层 + 组件 | 通过 | `src/lib/services.ts`（node/shard/index/alias/document/query/analyzer/rest 请求构造 + 解析 + 危险分类 `classifyOperation`）、`DataTable`/`ConfirmDanger`/`JsonView`/`HealthBadge`/`PrirepBadge` |
| T2 Nodes 页 | 通过 | `src/pages/Nodes.svelte`（`/_cat/nodes` + `/_nodes`） |
| T3 Shards / 拓扑矩阵 | 通过 | `src/pages/Shards.svelte` + `buildTopology()` |
| T4 Index List 页 | 通过 | `src/pages/IndexList.svelte`（open/close/refresh/delete + 危险确认） |
| T5 Aliases 页 | 通过 | `src/pages/Aliases.svelte`（`/_cat/aliases` + `POST /_aliases`） |
| T6 Browser 页 | 通过 | `src/pages/Browser.svelte`（搜索采样 / 按 id 取 / 删文档） |
| T7 Query Builder 页 | 通过 | `src/pages/QueryBuilder.svelte` + `buildDsl()` |
| T8 DSL Editor 页 | 通过 | `src/pages/DslEditor.svelte`（自研编辑器 + 历史） |
| T9 Analyzer 页 | 通过 | `src/pages/Analyzer.svelte`（`POST /_analyze`） |
| T10 REST Console 页 | 通过 | `src/pages/RestConsole.svelte`（`es/request` + 历史 + 危险标记） |
| T11 导航启用 + 路由 | 通过 | `stores.ts` 9 个 P2 项 `enabled:true`；`App.svelte` 9 个渲染分支；phase chip `P2 · Cluster & Data` |
| T12 构建 + 类型检查 | 通过 | `svelte-check` 0/0；单文件内联；`verify-package` 单文件断言 PASS || T13 Go 权限测试扩展 | 通过 | `guard_test.go` 增 `TestGuardP2WritePaths` / `TestGuardP2ReadsAllowedOnReadOnly`；`go test` 全绿 |
| T14 真实 ES 集成验证 | 通过 | `scripts/es-cluster.py` 启动 8.19.23；smoke 真实 ES 集成组全绿；诊断定位 3 项失败为 harness bug（`real_ro` 吞掉顶层 JSON-RPC error），非产品缺陷 |
| T14b 浏览器端到端实测（脱离 DBX） | 通过 | `browser-bridge.mjs` + `seed-es-data.sh` + `browser-test.py` / `browser-write-test.py`；读写 12/12、只读 13/13、写路径 5/5、只读拒绝 3/3；据此修复 Shards 页崩溃、数字列恒 0、错误码丢失 3 个真实缺陷（见 §1.5） |
| T15 打包/签名/包内回归 | 通过 | `check-manifest` 52→105；`SIGNATURE VALID`；`verify-package` 17/17；包内 exe 复跑 smoke 61/61（修复后重新打包签名） |
| T16 验收报告 + 提交 | 通过 | 本文件；源码与文档已提交（**不擅自进 P3**） |

---

## 3. 验证矩阵汇总（P2 闸口）

| 项 | 命令/动作 | 结果 |
|---|---|---|
| 前端类型检查 | `npm run check` | svelte-check **0 error / 0 warning** |
| 前端构建 | `npm run build` | `DBX_UI_BUILD_SUCCESS`；单文件；`<body>` 末尾 script |
| Go 单元 + vet | `go test ./...` / `go vet ./...` | 全部包通过；vet 干净 |
| 清单预检（源码） | `check-manifest.py` | **52 runtime checks** + schema |
| 清单预检（含包） | `check-manifest.py --package` | **105 runtime checks** + schema |
| 真实 ES 集成 smoke | `smoke-test.py --sidecar ... --es-url ...` | **61 passed, 0 failed** |
| 签名验签 | `sign-package.py --verify` | **SIGNATURE VALID**（key_id=local.es.studio.release） |
| 包内完整性 | `verify-package.py --package` | **17 passed, 0 failed** |
| 包内 exe 复跑 smoke | `smoke-test.py --sidecar .dbx-dev/verify/es-studio-sidecar.exe` | **61 passed, 0 failed** |
| 浏览器实测（读写，真实数据） | `browser-test.py --out screenshots/rw` | **12/12** |
| 浏览器实测（只读） | `browser-test.py` + `RO_DELETE=1` | **13/13** |
| 浏览器写路径（回查 ES 状态） | `browser-write-test.py`（rw） | **5/5** |
| 浏览器只读拒绝 | `browser-write-test.py`（ro） | **3/3** |

---

## 4. 两段式评审

**① 符合任务定义**：T1–T16 全部达成；17 项 Head 功能逐项实测通过；10 页面无错误；危险确认与 ReadOnly 拒绝均落地。浏览器实测（§1.5）进一步确认 UI 在真实数据下可用，并暴露 3 个已修复的真实缺陷。

**② 可读性 / DRY / YAGNI**：
- 业务解析集中在前端 `services.ts`，复用 P1 的通用 `es/request`/`es/batch`，**未新增 Go RPC 方法**（避免后端面膨胀与双份解析），与 `docs/02-PLAN-P2.md` §2 架构决策一致。
- 共享组件（DataTable / ConfirmDanger / JsonView / HealthBadge / PrirepBadge）被 9 页复用，无重复实现。
- Go 仅扩展 `permission` 测试覆盖 P2 写路径，安全边界唯一。
- 无装饰性按钮：P3–P5 导航项仍为 `enabled:false`，不冒充已实现。

**评审结论：无 Critical。**

---

## 5. 交付物与阶段闸口

- 可安装包：`dist/local.es.studio-0.1.0-windows-x64.signed.dbxp`（4,402,758 B），`SIGNATURE VALID`，信任公钥已注册至 `%APPDATA%/com.dbx.app/plugins/.trust/keys.json`。
- 文档：`docs/P2-ACCEPTANCE.md`（本文件）、`docs/02-PLAN-P2.md`、`docs/00-SPEC.md` §11（架构偏差登记）。
- 测试：`scripts/smoke-test.py`（61/61）、`backend/internal/core/permission/guard_test.go`（含 P2）。
- 浏览器实测工具链：`scripts/browser-bridge.mjs`、`scripts/seed-es-data.sh`、`scripts/browser-test.py`、`scripts/browser-write-test.py`；截图见 `screenshots/{rw,ro,write}/`。

**阶段闸口（进入 P3 前）：**
1. ✅ T1–T16（含 `git` 提交）完成，验证矩阵全绿；
2. ✅ 两段式评审无 Critical；
3. ✅ 可安装 `.signed.dbxp` 产出且验签通过；
4. ⏳ 用户明确验收 P2（D1 分批验收）。

> **不擅自进入 P3。** P2 的全部代码、文档与签名包已就绪并提交；待用户在 DBX Plugin Center 安装 `dist/local.es.studio-0.1.0-windows-x64.signed.dbxp` 实测确认后，再开启 P3（Templates / Data Streams / ILM / Snapshot / Task / Profiler）。
