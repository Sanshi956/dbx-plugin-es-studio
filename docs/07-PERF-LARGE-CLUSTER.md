# 07 · 大集群性能诊断报告

> 现象来源：用户实测连接 6.x 集群（6037 分片），**每个页面都很慢，更大的集群直接获取不到信息**。
> 本报告只做诊断与方案设计，不含代码改动。落版本时按 §4 的实施清单拆任务执行。
> 基线版本：0.5.1（e3a5db4）。

## 1. 结论

大集群慢/拿不到数据是**三层瓶颈叠加**，任何一层单独修都只能缓解：

| 层 | 根因 | 用户感知 |
|----|------|----------|
| 后端超时 | sidecar→ES 的 HTTP 客户端超时默认 **10 秒**且按连接写死 | O(索引数) 的接口必然超时 → **「直接获取不到信息」的真正原因** |
| 冗余重请求 | Overview 启动批次混入 `GET /_cat/indices` | boot 必经路径拖慢几十秒 → **「每个页面都很慢」** |
| 全量渲染 | Shards/IndexList 无分页无虚拟化 | 数据即使回来页面也卡死 |

关键矛盾：本插件的核心数据面 API（`_cat/shards`、`_cat/indices`、`/_all/_settings`）响应体都随**索引数×分片数**线性膨胀。这是**集群规模问题，与 ES 版本无关**——任何版本的大集群（几千索引/几千分片）都会触发；8.x 测试集群（几十索引）只是规模太小，暴露不出来。用户实测的 6.x 集群（6037 分片）是一个具体样本，不应据此把问题理解为 6.x 兼容性缺陷。

## 2. 根因明细

### 2.1 后端 HTTP 客户端超时 10s（P0，"获取不到信息"的真因）

- `backend/internal/core/connection/manager.go:164-174`：`Timeout` 取连接配置的 `query_timeout_secs` / `timeout_secs`，**未配置时兜底 10s**。
- `backend/internal/core/es/client.go:97`：ES 客户端同样兜底 `timeout = 10 * time.Second`。
- 该超时是 `http.Client.Timeout`（覆盖整个请求生命周期），`/_cat/indices` 在几千索引的大集群上（不限 ES 版本）单次普遍 10~40s → 必超时，返回 `ErrTimeout`（`-32003` 链路）。
- 前端 `api.ts` invoke 固定 `timeoutMs: 30000` 也偏紧：`es/batch` 内 5 个请求串行，最坏 5×单请求时长，30s 不够。
- 连接表单目前没有暴露超时字段（`connection_action.go:204` 只在测试连接时读取 `timeout_secs`）。

**佐证**：用户 6037 分片集群"每个页面都很慢"说明请求大多能回来（慢但活着）；"更多的直接获取不到信息"正是超时表现——集群越大越必然超时。

### 2.2 Overview boot 批次混入 `_cat/indices`（P0，"每个页面都慢"的真因）

- `src/lib/api.ts:68-78` `fetchOverview` 批量 5 个请求：`GET /`、`/_cluster/health`、`/_cluster/stats`、`/_cat/nodes`、**`/_cat/indices?format=json&bytes=b`**。
- `_cat/indices` 是 ES 最重的 API 之一：对**每个索引**做一次 stats 聚合。几千索引的大集群上（不限 ES 版本），这一条请求的耗时远超批次其余 4 条之和。
- 而它只被用来取 `indexCount` 兜底值（`api.ts:109`）——`indexCount`、`docCount`、`storeBytes` **`_cluster/stats` 全都有**（`stats.indices.count / docs.count / store.size_in_bytes`）。这条请求是纯冗余。
- Overview 是 boot 锚点页（宿主等「集群概览」），这条冗余直接拖慢插件启动。

### 2.3 Shards / IndexList 全量渲染（P1，"数据回来了也卡"）

- `src/pages/Shards.svelte:191-244`：拓扑矩阵渲染**全部索引 × 全部节点**（6037 分片 ≈ 数千行 × 数十列，每格若干 shard chip），明细表渲染全部 6037 行 × 9 列，**无分页、无虚拟滚动**。
- `$derived(buildTopology(filteredShards))`：过滤框每敲一个字符对 6037 行重建整个矩阵对象。
- `summarizeShards` 对全量数组做 5 次完整 `filter`（小开销，可顺手合并为一次遍历）。
- `src/pages/IndexList.svelte`：索引列表全量渲染，无分页。
- `src/components/DataTable.svelte`：通用表格组件同样无分页（Nodes/Templates/Aliases/ILM 等 10+ 页面共用）。

### 2.4 次要放大器（P2）

| 位置 | 问题 |
|------|------|
| `Shards.svelte:70-74` | inFlight>0 时**每 2 秒**重拉全量 `_cat/shards`（大集群上等于每 2 秒一次超时边缘请求，还会与迁移期集群负载互相放大） |
| `services.ts:976`（fetchAllocation / 分片设置页） | `/_all/_settings?flat_settings=true`：每个索引一段 settings，响应体 O(索引数)，大集群上超时高发 |
| `services.ts:521`（fetchAliases） | `GET /_alias` 全量：每个索引×每个别名一个 filter 对象，响应体最大可达数十 MB |
| `services.ts:1551`（fetchIlmExplain） | `/_all/_ilm/explain`：每个索引一条 explain |
| `services.ts:1848`（fetchTasks） | `detailed=true` 会展开每个 task 的 status（大集群上 `/​_tasks` 响应体暴涨） |
| `diagnostics.ts:253`（分片诊断） | 全量 `_cat/shards`（h= 裁剪过列，行数仍 O(分片数)，仅作为输入用，问题较轻） |

### 2.5 已排除的怀疑

- **不是渲染引擎问题**：Svelte 5 keyed each 用位置键，无重复键崩溃（memory 已验证）。
- **不是门控/能力探测问题**：`clusterCapabilities()` 只打一次 `GET /`，开销可忽略。
- **不是特定 ES 版本的兼容问题**：慢与 6.x/7.x/8.x 无关，纯粹由集群规模决定；`legacyTypedMappings = major<7` 等版本分支只影响解析，不影响耗时。

## 3. 修复方案

### 3.1 P0-1 超时体系重做（后端）

1. `manager.go` / `client.go`：默认超时 **10s → 60s**。对端宕机时 TCP dial 仍快速失败，不受影响。
2. `es/request` / `es/batch` RPC 参数新增可选 `timeout_secs`（上限 clamp 到 300s），前端按页面类型传值：
   - Overview/Nodes 等轻页：默认；
   - Shards/IndexList/Settings 等重页：60~120s。
3. 前端 `api.ts`：`invoke` 超时与请求类型联动（`es/batch` ≥ 单请求 × 请求数），`timeoutMs` 提到 120s 量级。
4. 连接表单暴露 `timeout_secs`（可选），文档写入 `docs/remote-ops` 同级约定。

**风险**：`-32003` 超时错误码语义不变；只放大上限，不改变失败判定逻辑。

### 3.2 P0-2 Overview 去掉 `_cat/indices`（前端，一行级改动）

- `fetchOverview` 批次删掉第 5 条请求，`indexCount` 直接用 `stats.indices.count`（原 fallback `indices.length` 一并删除）。
- 同步更新 `Overview.svelte` loading 文案、`labels` 数组、i18n 两份 locale（如有涉及）、browser 测试断言。
- 预期收益：boot 路径从「最重 API 决定总时长」变为「`_cluster/stats` 决定总时长」，实测一般快一个数量级。

### 3.3 P1 分页与渲染裁剪（前端）

1. `DataTable.svelte` 加可选 `pageSize` prop（默认 0 = 关闭，不惊动现有小数据页面），启用时渲染"第 x/y 页 · 共 N 行"脚标 + 上一页/下一页。
2. Shards 页：
   - 明细表启用分页（100/页）；
   - 拓扑矩阵同样分页（按索引行分页，一页 ~50 个索引）；
   - `buildTopology` 只在「矩阵页需要的行切片」上跑（先用 filter 后切片再 build）。
3. IndexList 启用 DataTable 分页或等效方案。
4. `summarizeShards` 合并为单次遍历。

**风险**：browser-ux-test（106 项）中断言行数的用例需要核对分页后的可见行数；用 `pageSize` 显式传参的页面才受影响。

### 3.4 P2 重页裁剪（后端+前端，可后置）

| 页面 | 方案 |
|------|------|
| Shards 轮询 | inFlight>0 时对大集群（上次返回行数 > 1000）降频到 10s，并缩短为只拉 `_cluster/health` 判断 inFlight 是否清零后再全量拉 |
| 分片设置页 | `/_all/_settings` 改为 `/_cat/indices` 的 h= 列 + 按需单索引 `/{index}/_settings`（展开行时再拉） |
| Aliases | 保留全量但作为重页传大超时；后续可改 `/_cat/aliases` + 按需 `/_alias/{index}` |
| ILM explain | 同上，加"仅显示受管索引"过滤（`/_all/_ilm/explain?only_managed=true`，7.5+ 支持，6.x 需降级为全量） |
| Tasks | `detailed=true` → 默认 `detailed=false`，展开单行时再查详情 |

## 4. 实施清单（供下个版本拆任务）

- [ ] P0-1 后端默认超时 60s + RPC `timeout_secs` 参数 + go test
- [ ] P0-1 前端 invoke 超时联动 + 重页传大超时
- [ ] P0-2 Overview 去掉 `_cat/indices`（api.ts / Overview.svelte / locales / 测试）
- [ ] P1 DataTable 分页 prop + Shards 明细/矩阵分页 + IndexList 分页
- [ ] P1 summarizeShards 单次遍历
- [ ] P2 Shards 轮询降频 + 重页裁剪（可拆独立版本）
- [ ] 回归链：browser-test / browser-ux-test / browser-render-check / browser-write-test / go test / check-manifest / smoke-test
- [ ] 版本号 bump（manifest.json + backend/main.go）与发布包

## 5. 验收标准（大集群场景）

1. 6037 分片集群（**不限 ES 版本**）：Overview 3s 内出全量指标；Shards/IndexList 明细可翻页浏览，过滤输入不卡顿。
2. 更大规模集群：所有页面**给出数据或给出可读错误**，不再出现静默超时空态。
3. 8.19 小集群基线不回归：0.5.1 的 23+106+25+7+4+32 项测试全绿。
