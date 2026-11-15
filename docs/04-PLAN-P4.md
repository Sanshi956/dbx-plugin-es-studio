# 阶段 4 实施计划（P4 · Monitoring / Diagnostics / Compare）

> 上游：`docs/00-SPEC.md` §4 映射表（5.12 MON / 5.13 DIA / 5.14 CMP）、§7 P4 验收
> 「监控历史为真实周期采集序列；诊断规则可配置；两集群 diff 可导出」。
> 改需求先改 SPEC，本文件只做拆解。

## 0. 本轮范围切分（用户已确认）

P4 一次交付会有 3 个导航项 + 一套新存储 + 一套新 RPC，验收粒度太粗。用户选择：

| 子阶段 | 内容 | 状态 |
|---|---|---|
| **P4-a** | Cluster Metrics / Node Metrics / History（周期采集 + 落盘 + 图表） | 本轮交付，等验收 |
| **P4-b** | Cluster Diagnostics（可配置规则引擎）/ Multi-Cluster Compare（diff 导出） | 已交付（0.3.1），等验收 |

采集方式用户选定：**Go 后台周期采集**（Sidecar 内 goroutine 按间隔采集并落盘，页面只读序列），
并接受为此新增 Go RPC 方法。

## 1. 交付范围（P4-a）

| 导航项 | 页面 | 数据来源 | 是否落盘 |
|---|---|---|---|
| Cluster Metrics | `ClusterMetrics.svelte` | `GET /_cluster/health` + `GET /_cluster/stats` + `GET /_cat/nodes` | 是（每分钟一点） |
| Node Metrics | `NodeMetrics.svelte` | 同一份样本里的 `nodes{}` | 读已落盘序列 |
| History | `History.svelte` | 已落盘序列 → 表格 / CSV | 只读 |

三个页面都不发 ES 请求（除了「立即采样」触发一次采集），图表上的每个点都来自磁盘上的真实样本。

## 2. 架构例外（有意为之，不要"回退"成 es/request）

P2 / P3 的铁律是**不新增 Go RPC**，所有读写走 `es/request` + `es/batch`。P4 必须破这条：

- 时间序列**不能**由页面在打开时算出来 —— 关掉页面就没有序列，且 ES 的累计计数不是时间序列（SPEC §2.2、需求 5.12）。
- Sidecar 是长驻进程，只有它能"每隔 N 秒采一次"。

因此新增 5 个方法，集中在 `backend/internal/transport/monitor.go`：

| 方法 | 作用 |
|---|---|
| `mon/start` | 为某连接启动采集（先立即采一点，再按 ticker 走）；`interval_sec` 夹到 5s~1h |
| `mon/stop` | 停止采集，**已落盘的样本保留** |
| `mon/status` | 采集中/已采集数/上次时间/上次错误/库内点数/字节/文件名 |
| `mon/sample` | 立即采一点（页面上的「立即采样」） |
| `mon/history` | 读序列（`since` + `limit`，默认 720 点，硬上限 5000） |

边界依旧在 Go：采集只调只读端点，权限层不变；`mon/*` 不引入任何写 ES 的能力。

## 3. 存储选型修订（SPEC §2.2 已同步）

SQLite → **JSONL 追加文件**（`backend/internal/storage/series.go`）：

- 纯 Go SQLite 驱动要 vendor ~10 MB，本机未启用 CGO；而 P4 的落盘量级是「每分钟一行」。
- 追加写（同步 flush）→ 进程崩了最后一点也在。
- 三重上限：保留期 7 天 / 20000 点 / 8 MiB；每 64 次追加压缩一次（tmp + rename）。
- 文件名 `local-es-<hash8>.jsonl`：连接 id 做 `sanitize` + sha256 前 4 字节，防路径穿越且可读。
- 好处：出问题能直接用文本编辑器看，截断可恢复。

## 4. 任务拆分

| # | 任务 | 验收 |
|---|---|---|
| P4a-1 | `internal/storage`：JSONL 追加 + 保留期/条数/字节上限 + 压缩 | `go test ./internal/storage` 9 用例全绿 |
| P4a-2 | `internal/core/metrics`：三端点采集 + 解析（对齐真实 8.19 形状） | 单测覆盖真实响应桩；真实集群堆内存非 0 |
| P4a-3 | `internal/collector`：连接粒度的 goroutine、间隔夹取、失败不中断 | `go test ./internal/collector` 7 用例 |
| P4a-4 | `internal/transport/monitor.go` + `sidecar.go` 接线 + capabilities | `go test ./internal/transport` 7 用例 |
| P4a-5 | `main.go` 装配 storage/collector + `pluginVersion` 0.3.0 | `check-manifest.py` 断言两者一致 |
| P4a-6 | 前端服务层 `monitor*` + 类型 | `svelte-check` 0/0 |
| P4a-7 | `MetricChart.svelte`（自绘 SVG，Y 轴取整、hover 读数、x 轴真实时间戳） | 采集中断时曲线出现真实断裂 |
| P4a-8 | Cluster Metrics 页（六卡 + 四图 + 间隔切换 + 立即采样） | 打开即有数据点 |
| P4a-9 | Node Metrics 页（节点 chips 多选 + 三图 + Latest sample 表） | 节点指标来自 `_cat/nodes` 真实值 |
| P4a-10 | History 页（时间范围 + 表格 + 复制 CSV） | 行数与库内点数一致 |
| P4a-11 | 浏览器实测 + smoke 断言 | `browser-ux-test` 75/75、smoke 105/105 |
| P4a-12 | 打包 / 签名 / 包内 exe 回归 + `docs/P4-ACCEPTANCE.md` | 停在用户验收，不擅自进 P4-b |

## 5. 阶段闸口（P4-a）

1. 三个页面上的每个数字都能追到一次真实采集，**没有 mock、没有"示例数据"**；
2. 采集中断（停采集/关连接）在图上表现为真实断裂，不插值、不补点；
3. 序列落在 `DBX_PLUGIN_DATA_DIR`（宿主注入；dev host / bridge 无该变量时回退系统临时目录）；
4. `svelte-check` 0 error 0 warning；`go vet` + `go test ./...` 全绿；
5. 签名包可安装 + `verify-package` 全 PASS + 包内 exe smoke 全绿；
6. 用户确认后才进 P4-b（D1 分批验收）。

## 6. P4-b 实施（0.3.1，已交付）

同样**不新增 Go RPC**：诊断与对比都是「读一次 → 本地算」，`mon/*` 仍是 P4 唯一的架构例外。

### 6.1 Cluster Diagnostics（`src/lib/diagnostics.ts` + `Diagnostics.svelte`）

一次 `es/batch` 取快照：`/_cluster/health`、`/_cat/nodes?h=name,node.role,master,heap.percent,cpu,disk.used_percent`、
`/_all/_settings?name=index.number_of_replicas,index.refresh_interval,index.blocks.*`、
`/_cat/shards?h=...,unassigned.reason`、`/_cluster/settings?flat_settings`、`/_ilm/status`（缺失即视为不可用）。

13 条确定性规则：`health-red`、`health-yellow`、`unassigned-shards`、`single-node-replicas`、
`replicas-exceed-nodes`、`disk-watermark`、`heap-pressure`、`cpu-saturation`、`shards-per-node`、
`ilm-stopped`、`refresh-disabled`、`write-blocked`、`delayed-unassigned`。

- `evaluate(snapshot, configs)` 是**纯函数**：同一快照 + 同一配置 ⇒ 同一结论；
- 4 条带阈值（磁盘 85 / 堆 85 / CPU 90 / 每节点分片 600），改阈值**立即重算**，不重新采集；
- 开关与阈值存 `host.storage`（key `es.studio.diagnostics.rules`），有「恢复默认」；
- 只输出「结论 / 依据 / 建议」，不提供任何自动修复按钮（SPEC §5.6）。

### 6.2 Multi-Cluster Compare（`src/lib/compare.ts` + `Compare.svelte`）

每侧 4 个只读请求：`/_cluster/health`、`/_cluster/stats`、`/_cat/nodes?h=name,node.role,master,heap.percent`、
`/_cluster/settings?flat_settings`。归约成 `ClusterFacts`，再 `diffFacts` 出 22 行（概览 / 节点 / 分片与容量 / 关键设置）。

- `same` 由**原始值**比较得出，展示串只负责格式化 —— 格式化永不制造假差异；
- 关键设置行在未显式设置时按 ES 默认值展示（`all` / `85%` / `90%` / `95%` / `false` / `1000`）并标注来源 key；
- 第二连接必须是**已连接会话**（Sidecar 里没有它的会话就报 `connection is not connected`），
  页面把该错误翻成「先在 DBX 连接树里打开该连接」，不整页报错；
- `toCsv(rows, 左标签, 右标签)` 导出，剪贴板被拒时展开文本框兜底。

### 6.3 任务拆分与验收

| # | 任务 | 验收 |
|---|---|---|
| P4b-1 | `diagnostics.ts` 规则表 + 快照 + 纯函数评估 + 配置归一化 | 阈值/开关改动即时生效 |
| P4b-2 | `Diagnostics.svelte` 摘要 + 结论表 + 规则面板 | 真实集群命中 3 条（黄灯 / 未分配 / 单节点副本） |
| P4b-3 | `compare.ts` 事实归约 + diff + CSV | 两侧真实差异 17 项、相同 5 项 |
| P4b-4 | `Compare.svelte` 选连接 + 差异高亮 + 导出 | 差异行带 `tr.diff` 标记 |
| P4b-5 | 导航启用 + 路由 + 版本 0.3.1 | 22 个导航项全部可用 |
| P4b-6 | 浏览器实测（ux 90/90）+ 打包签名 | 见 `docs/P4B-ACCEPTANCE.md` |

### 6.4 已知限制

- 本机只能跑一个 ES，对比的「右侧」在测试里由 `scripts/mock-cluster.py`（测试替身）提供；
  页面不区分真假集群，走的还是真实 `es/batch` 路径。
- 对比只看**集群级事实**，不做索引级逐条 diff（索引数可能上百，逐条 diff 需要另做过滤 UI）。
