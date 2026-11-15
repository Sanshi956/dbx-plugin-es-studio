# P4-a 阶段验收报告 · ES Studio 0.3.0（Monitoring）

> 范围：`docs/04-PLAN-P4.md` §0 切分的 **P4-a**（Cluster Metrics / Node Metrics / History）。
> P4-b（Cluster Diagnostics / Multi-Cluster Compare）**未开始**，等本轮验收通过。
> 采集方式：Go 后台周期采集（Sidecar 内 goroutine + 落盘），用户已确认。

## 1. 需求验收项（SPEC §7 P4 拆解）

| SPEC §7 P4 条目 | 本轮状态 |
|---|---|
| 监控历史为**真实周期采集序列** | ✅ 已达成（P4-a） |
| 诊断规则可配置 | ⏸ 未做（P4-b） |
| 两集群 diff 可导出 | ⏸ 未做（P4-b） |

### 1.1 三个页面逐项实测（真实 ES 8.19.23 @ 127.0.0.1:19200）

| 导航项 | 实测结果（截图 `screenshots/p4/`） |
|---|---|
| Cluster Metrics | 打开即 `采集中`，顶栏 `3 点 · 1.2 KB · 覆盖 N 分钟 · local-es-bd98a50d.jsonl`；六卡 `yellow / 1 / 18 / 5 / 37 / 206.1 KB / 33.2% · 0.0%`；四张图都有折线 |
| Node Metrics | chip `es-studio-node-1`；三张图（Heap % / CPU % / Load 1m）；Latest sample `34% · 7% · 0.00 · 82%` |
| History | `最近 1 小时` → 4 行；`当前范围 4 行 · 库内共 4 点 · 1.7 KB · local-es-bd98a50d.jsonl`；CSV 导出可用 |

关键点：**图上每个数字都来自磁盘上的一次真实采集**。`33.2%` 的堆占用来自
`_cluster/stats.nodes.jvm.mem.heap_used_in_bytes`，`34% / 7% / 82%` 来自 `_cat/nodes`
的 `heap.percent / cpu / disk.used_percent`。没有任何 mock 或"示例数据"分支。

### 1.2 真实数据暴露并修复的缺陷（P4-a 期间）

| # | 现象 | 根因 | 修复 |
|---|---|---|---|
| 1 | 集群 Heap / CPU 恒为 0，但 `_cat/nodes` 明明有值 | 按 `nodes.jvm.memory`、`nodes.fs.total` 解析，真实 8.19 是 `nodes.jvm.mem` 与**扁平** `nodes.fs` | `collect.go` 改字段路径，注释标注 "Verified against a live cluster"；两处测试桩同步改 |
| 2 | CPU 只有 1 位小数，7.25 被抹成 7.3 | `parsePercent` 里做了 round1 | 去掉取整，只对堆百分比 round2 |
| 3 | 首次打开 Cluster Metrics 序列为空 | 第一次采集在 goroutine 里，前端 refresh 抢先 | 页面 `bootstrap` 里 0 点则 `sampleNow` 一次再 refresh；轮询 5s→3s |
| 4 | 浏览器里 `mon/*` 报 `connection is not connected` | bridge 只给 `es/request|es/batch` 注入 `direct`，collector 要自己连 | `browser-bridge.mjs` 注入列表加 `mon/start`、`mon/sample` |
| 5 | Cluster Metrics 顶栏出现两个「3 点」 | `span` 里带了点数，`stored` 又带一次 | `span` 改为 `覆盖 N 分钟` |
| 6 | `smoke` 会把样本写进共享临时目录，污染浏览器测试的序列 | `Sidecar()` 没传 env | smoke 改为 `DBX_PLUGIN_DATA_DIR=tempfile.mkdtemp()`，结束后清理 |

### 1.3 有意打破的架构铁律（需要评审确认）

P2/P3 的约定是「不新增 Go RPC，全部走 `es/request` + `es/batch`」。P4 必须破例，
理由是时间序列不能由页面现算（关掉页面就没有序列，ES 的累计计数也不是时间序列）：

- 新增 5 个方法：`mon/start` / `mon/stop` / `mon/status` / `mon/sample` / `mon/history`，
  集中在 `backend/internal/transport/monitor.go`；
- 只调只读端点（`/_cluster/health`、`/_cluster/stats`、`/_cat/nodes`），权限层不变，
  **不引入任何写 ES 的能力**；
- 业务解析仍在前端 `src/lib/services.ts`；Go 只负责「采、存、取」。

### 1.4 存储选型修订（SPEC §2.2 已同步）

原定 SQLite → 改为 **JSONL 追加文件**（`backend/internal/storage/series.go`）：

- 纯 Go SQLite 驱动要 vendor ~10 MB 且未启用 CGO；P4 量级是「每分钟一行」；
- 追加写同步 flush（崩溃不丢最后一点）；保留期 7 天 / 20000 点 / 8 MiB 三重上限；
  每 64 次追加压缩一次（tmp + rename）；
- 文件名 `local-es-<hash8>.jsonl`（id `sanitize` + sha256 前 4 字节，防路径穿越且可读）；
- 出问题可用文本编辑器直读，截断可恢复。

## 2. PLAN P4a-1 ~ P4a-12 执行结果

| # | 任务 | 结果 |
|---|---|---|
| P4a-1 | `internal/storage` JSONL 存储 | ✅ 9 个单测 |
| P4a-2 | `internal/core/metrics` 三端点采集 + 解析 | ✅ 3 个单测 |
| P4a-3 | `internal/collector` 连接粒度 goroutine | ✅ 7 个单测 |
| P4a-4 | `transport/monitor.go` + capabilities | ✅ 7 个单测 |
| P4a-5 | `main.go` 装配 + `pluginVersion` 0.3.0 | ✅ `check-manifest` 断言两者一致 |
| P4a-6 | 前端 `monitor*` 服务层 + 类型 | ✅ `svelte-check` 0/0 |
| P4a-7 | `MetricChart.svelte`（自绘 SVG） | ✅ Y 轴取整（20/40/250KB 而非 21.3/43）、hover 读数 |
| P4a-8 | Cluster Metrics 页 | ✅ 六卡 + 四图 + 间隔切换 + 立即采样 |
| P4a-9 | Node Metrics 页 | ✅ 节点 chips 多选 + 三图 + Latest sample |
| P4a-10 | History 页 | ✅ 时间范围 + 表格 + CSV |
| P4a-11 | 浏览器实测 + smoke 断言 | ✅ ux 75/75、smoke 105/105 |
| P4a-12 | 打包 / 签名 / 包内回归 + 本文档 | ✅ 停在本验收 |

## 3. 验证矩阵汇总（P4-a 闸口）

| 项 | 结果 |
|---|---|
| `npm run check`（svelte-check） | **0 errors, 0 warnings** |
| `go vet ./...` + `go test ./...` | 全绿，**59** 个用例（P4 新增 26 个） |
| `scripts/check-manifest.py` | PASS: **53** runtime checks |
| `scripts/check-manifest.py --package` | PASS: **107** runtime checks（manifest 与 `main.go` 版本一致） |
| `scripts/smoke-test.py --sidecar bin/...` | **105 / 105**（新增 16 条 P4 断言） |
| `scripts/sign-package.py --verify` | **SIGNATURE VALID**（key_id=local.es.studio.release） |
| `scripts/verify-package.py` | **17 / 17** |
| 包内 exe `smoke-test.py` | **105 / 105** |
| `scripts/browser-test.py`（主回归） | **23 / 23**，控制台 0 报错 |
| `scripts/browser-ux-test.py` | **75 / 75**（含 12 条监控断言），控制台 0 报错 |
| `scripts/browser-write-test.py --rw` | **7 / 7**（每步回查真实 ES 状态） |
| `scripts/browser-write-test.py --ro` | **4 / 4**（只读拒绝 -32003） |

## 4. 两段式评审

**第一段（实现自检）**：三页全部读真实序列；`mon/*` 五个方法的错误码符合约定
（`-32602` 缺 `connection_id`、`-32001` 存储不可用）；采集失败只记 `lastError` 并继续采集，
不因一次瞬时错误断流；间隔夹到 5s~1h（smoke 有断言）。

**第二段（用户验收）**：见 §6，**不擅自进入 P4-b**。

## 5. 已知限制（本轮不做，需说明）

1. **采集只在 Sidecar 存活期间进行**：DBX 关闭后没有进程采集；没有凭据也无法自恢复
   （连接凭据只存在于会话中），因此不做「开机自动采集」。
2. **序列按连接 id 分文件**，删除连接不删历史文件（避免误删），保留期到期自动清理。
3. **保留期 / 容量上限暂不可在 UI 配置**（固定 7 天 / 20000 点 / 8 MiB）。
4. **单节点集群**下 `relocating / initializing` 恒 0；多分片迁移曲线的验证需要 ≥2 节点。
5. 未启用 CGO，`go test -race` 仍未跑（与 P1~P3 一致）。

## 6. 交付物与阶段闸口

- 交付包：`dist/local.es.studio-0.3.0-windows-x64.signed.dbxp`（4.51 MB，已签名）。
- 安装路径：DBX 插件中心 → 设置 → 从 URL 安装；**同版本需先卸载**。
  本地静态服务：`cd dist && python -m http.server 19321`。
- 正确打开方式：连接树「新建连接」→ 选 `ES Studio (Elasticsearch)` → 填
  `127.0.0.1:19200 / http / none` → Test → 保存 → 点连接节点 → `Monitoring / Cluster Metrics`。
- 闸口：**等用户确认 P4-a 后，再开 P4-b（诊断规则 + 多集群对比）**。
