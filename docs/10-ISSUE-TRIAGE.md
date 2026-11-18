# 10 · 今日问题分诊总表（2026-11-17）

> 本文汇总今天确认的三个问题，作为执行入口。**不含监控历史文件大小/保留策略**——该项仅做讨论，不纳入本次范围。
> 详细论证分别见 `07-PERF-LARGE-CLUSTER.md`、`08-UI-AUDIT-AND-FIX-PLAN.md`、`09-PROFILE-ALL-ZERO-BUG.md`。状态（2026-11-18）：A/B/C 已在 0.5.2 全部修复并回归通过，执行记录见 §6。

## 1. 总表

| # | 用户反馈 | 根因（已实证/已定位） | 优先级 | 详细方案 | 改动面 |
|---|----------|----------------------|--------|----------|--------|
| A | 查询剖析拿到的数据都是 0 | 6.x/7.x 的 `profile.shards[i]` 只有复合 `id` + `searches`，无独立 `node_id/index/shard_id`；`services.ts:1662-1666` 把首 key `id` 误认成 nodeId，`node={}` 导致 searches 为空 | **P0（功能失效）** | 09 | 1 个函数 + fixture 单测 |
| B1 | 查询构建器几个下拉框样式不一样 | 全局无 select 视觉规范（`tokens.css:59` 只继承字体），QueryBuilder 同页 2 套 padding + 4 套宽度/字体/语义色，另有 4 页偏离基准 | P1 | 08 P0-1 | 5 个文件 + tokens.css |
| B2 | 查询结果展示不完整，不按一个字段一列 | 固定 `_id/score/_source` 三列，`_source` 被 JS 截 90 字符 + CSS 截 460px；**数据层完整，纯展示层问题** | P1 | 08 P0-2 | QueryBuilder.svelte + 2 个 i18n 键 |
| B3 | 查询剖析文本过长被「…」截断 | `QueryProfiler.svelte:136` 把 node ID 硬截 10 字符；全站另有 8 处 CSS ellipsis + 6 处 JS slice+… | P1 | 08 P0-3 / P1 | QueryProfiler + 通用 CellValue |
| C | 每个页面都很慢，更大的集群直接获取不到信息 | 三层叠加：① 后端 HTTP 客户端默认超时 **10s**（不可调）；② Overview 启动批次混入冗余 `GET /_cat/indices`；③ Shards/IndexList 全量渲染 6037 行无分页 | **P0（大集群不可用）** | 07 | 后端超时体系 + 前端批次 + 分页 |

**版本相关性澄清**：C 是**集群规模问题，与 ES 版本无关**——6.x/7.x/8.x 的大集群都会触发，8.x 小测试集群只是规模太小暴露不出来。A 则相反，是**版本结构差异**：6.x/7.x 中招，8.x/9.x 正常。

## 2. 执行顺序与依赖

```
A（profile 数据映射）  ← 先修，否则 B3 改展示时拿到的 node 本身就是错的
      ↓
B1/B2/B3（UI 一致性与完整展示） ← 与 07 的表格分页合并排期，避免两次回归 106 项 UX
      ↓
C（超时体系 + Overview 瘦身 + 分页）
      ↓
统一回归 → 版本 bump → 打包签名
```

**合并点**：`DataTable.svelte` 的分页改造（07 §3.3）与 QueryBuilder 字段分列表格（08 P0-2）都动表格，放在同一个改造任务里做。

## 3. 各项关键结论速查

### A · 查询剖析全 0（09）
- 实证：ES 6.8/7.17 真实响应均为 `{id: "[nodeId][index][shard]", searches, ...}`；8.x 才有独立字段。
- 验证：真实 6.8 响应下，当前逻辑 queryNanos 合计 **0**，补丁后 **2,422,710 ns**；报告内附可直接替换的回调代码。
- 验收：6.8/7.17/8.19 三版本 profile 表首行数值非 0。

### B · UI 一致性与截断（08）
- 基准样式：`6px 10px + --es-border + 6px 圆角 + --es-surface`，8 个页面已采用；偏离项为 ClusterMetrics/History/McpService/Analyzer/QueryBuilder。
- 字段分列的数据来源已就绪：`parseSearch()` 保留完整 `source`，`fetchFields()` 给出字段清单（含 6.x typed mapping）。字段 > 30 时提供「显示全部」开关。
- 完整查看组件 `JsonView`/`Inspector` 已存在，缺的是接入入口和复制按钮；Browser 是正面范例（逐条 view 按钮）。

### C · 大集群性能（07）
- 真因排序：10s 硬超时（拿不到数据）> Overview 冗余 `_cat/indices`（启动慢）> 全量渲染（有数据也卡）。
- Overview 要的 indexCount/docCount/storeBytes 在 `_cluster/stats` 里全都有，`_cat/indices` 是纯冗余。
- 超时方案：默认 10s→60s + RPC 可选 `timeout_secs` + 前端 invoke 联动；前端 30s 不是瓶颈（后端先死）。

## 4. 统一验收链

```
go test ./...
npm run check                # svelte-check 0/0
browser-test                 # 主回归 23/23
browser-ux-test              # 106/106（UI 结构改动后同步选择器）
browser-render-check         # 25 页 0 错
browser-write-test --rw/--ro # 7/7 + 4/4
browser-i18n-test --locale   # 25+25，zh-CN/en 键集一致
mcp-client-test              # 32/32
check-manifest / verify-package / smoke-test --sidecar
```

版本 bump 两处：`manifest.json` + `backend/main.go` 的 `pluginVersion`。出包前核对包时间晚于最后一次源码改动。

## 5. 不在本次范围

- **集群监控历史文件大小/保留策略**：今天讨论过但未立项。现状是 7 天 / 20000 条 / 8MB 三重封顶、每 64 次 append 惰性压缩；默认保留天数可后续单独成项，需同步处理字节上限与条数上限对保留期的挤压、以及全文件读取 + 3 秒轮询的 IO 压力。

## 6. 0.5.2 执行结果（2026-11-18）

| 项 | 结果 | 关键改动 |
|---|---|---|
| A 查询剖析全 0 | 已修复 | `src/lib/profile-parse.ts` 解析 6/7 复合 id；6.8/7.17/8.19 fixture 单测 5/5 |
| B1 控件样式 | 已统一 | `tokens.css` 单一控件基准 + 统一 focus 环 + 统一 `calc(1.5em + 14px)` 高度；删除 12 处页面级重复 chrome；Combobox 替换全部 datalist |
| B2 结果字段分列 | 已修复 | QueryBuilder 动态列 + 逐行详情 + >30 列开关；删除 90 字符/460px 双重截断 |
| B3 文本截断 | 已修复 | QueryProfiler node 完整展示；JsonView/Inspector 复制按钮；各处补 title/详情入口 |
| C 大集群性能 | 已修复 | 后端超时 10s→60s、RPC `timeout_secs`、前端 120s；Overview 去掉冗余 `_cat/indices`；Shards/IndexList/拓扑矩阵分页 |
| CI / 发布 | 已接入 | `.github/workflows/ci.yml` + `plugin-release.yml`（官方 reusable `plugin-cli-v0.1.10`） |
| README | 已同步 | 版本 0.5.2、CI 章节、写操作与只读保护、已知限制 |

验证基线：svelte-check 0/0、Go test/vet 全绿、fixture 5/5、大集群验收 PASS、主回归 23/23、UX 106/106、25 页渲染 0 错误、i18n 25+25、MCP 32/32、写 7/7、只读 4/4、smoke 125/125、verify 17/17、manifest 111。
仍保留的非阻塞项：未单独抽象 `CellValue.svelte`（由 JsonView/Inspector 复制 + 逐行详情 + title 覆盖）；Snapshots 索引列表仍只显示前 3 项但带完整 `title`。
