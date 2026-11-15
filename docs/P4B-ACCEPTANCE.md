# P4-b 阶段验收报告 · ES Studio 0.3.1（Diagnostics + Compare）

> 范围：`docs/04-PLAN-P4.md` §0 的 **P4-b**（Cluster Diagnostics / Multi-Cluster Compare）。
> P4-a（Monitoring）已在 0.3.0 交付，本文件只覆盖 P4-b。
> 结论：SPEC §7 P4 的两个条件（诊断规则可配置、两集群 diff 可导出）已达成。

## 1. 需求验收项

| SPEC §7 P4 条目 | 状态 | 证据 |
|---|---|---|
| 诊断规则可配置 | ✅ | 13 条规则，阈值即时生效，开关即时生效，配置持久化 |
| 两集群 diff 可导出 | ✅ | 22 行对比，差异标记，CSV 导出 |

### 1.1 Cluster Diagnostics（真实 ES 8.19.23）

打开即命中 3 条结论，全部指向真实数字：

| 级别 | 结论 | 依据 |
|---|---|---|
| 警告 | 集群黄灯：存在未分配分片 | `status=yellow · 未分配分片 5 · 活跃分片 19` |
| 警告 | 分片未分配 | `未分配 5（CLUSTER_RECOVERED×5）` |
| 警告 | 单节点集群仍配置副本 | `1 个节点，2 个索引副本数 ≥1：metrics-daily(2)、orders(1)` |

可配置性实测（同一快照，不重新采集）：

| 操作 | 结果 |
|---|---|
| 磁盘阈值 85 → 50 | 立刻多出「磁盘接近水位线」：`es-studio-node-1 81.5% ≥ 阈值 50%（high 水位 90%）` |
| 阈值 50 → 85 | 该结论消失（81.5% < 85） |
| 关闭「单节点集群仍配置副本」 | 该结论消失，其余保留 |
| 点「恢复默认」 | 结论回来，13/13 规则重新启用 |
| 刷新页面 | 阈值与开关都保留（走 `host.storage`） |

### 1.2 Multi-Cluster Compare

左侧为本机真实集群，右侧为 `scripts/mock-cluster.py`（**测试替身**：本机只跑得起一个 ES）。
页面不区分真假，走的是同一条 `es/batch`；真实两集群场景下路径完全一致。

实测输出：`17 项不同 · 5 项相同`

| 分组 | 项目 | 左 | 右 |
|---|---|---|---|
| 概览 | 集群名 | es-studio-test | es-mirror-remote |
| 概览 | 版本 | 8.19.23 | 8.15.0 |
| 概览 | 健康状态 | yellow | green |
| 节点 | 节点数 / 数据节点 / 主节点 | 1 / 1 / 1 | 2 / 2 / 1 |
| 分片与容量 | 活跃分片 · 未分配 | 19 · 5 | 40 · 0 |
| 分片与容量 | 文档数 · 存储 | 37 · 206.3 KB | 1,234,567 · 12.5 MB |
| 关键设置 | 磁盘 low 水位 | 85% | 80% |
| 关键设置 | 单节点分片上限 | 1000 | 500 |

差异行带 `tr.diff`（左侧黄色竖条 + 加粗），相同行保持常规样式；`复制 CSV` 成功或展开文本兜底。

## 2. 真实数据暴露并修复的缺陷

| # | 现象 | 根因 | 修复 |
|---|---|---|---|
| 1 | 诊断页显示「可检查索引 0」，单节点副本规则永不命中 | `filter_path` 用点号路径匹配，而 `flat_settings=true` 的键**本身就叫** `index.number_of_replicas` → 返回 `{}` | 改用 `?name=index.number_of_replicas,...`（8.19 实测可用） |
| 2 | 对比页「主节点 2 vs 2」 | 用角色字母 `includes("m")` 判断，`cdfhilmrstw` 里的 m 是 master，但投票节点也带 m | 优先用 `_cat/nodes` 的 `master` 列（`*`/`-`），角色字母只做兜底 |
| 3 | 诊断「索引 14」与对比「索引数 17」互相矛盾 | 前者来自 `_all/_settings`（用户索引），后者来自 `_cluster/stats`（含系统索引） | 分别标注语义：`可检查索引` / `索引数（含系统索引）`，不硬凑成一个数 |
| 4 | 「配置持久化」无法验证 | bridge shim 的 `host.storage` 是内存对象，刷新即丢 | shim 改 `sessionStorage`（贴近真实 `host.storage` 的持久语义） |
| 5 | 纯协调节点会被算成数据节点 | `roles.includes("d") \|\| roles === ""` | 去掉 `=== ""` 分支 |

## 3. 架构

- **没有新增 Go RPC**：诊断与对比都是「读一次 → 本地算」；`mon/*` 仍是 P4 唯一的例外（理由见 `04-PLAN-P4.md` §2）。
- `evaluate(snapshot, configs)` 与 `diffFacts(left, right)` 都是纯函数，同一输入必得同一输出。
- 规则 id 落 `host.storage`，**改名等于破坏用户配置**（代码内有注释约束）。
- 诊断只产「结论/依据/建议」，没有任何自动修复入口（SPEC §5.6「不得把推测写成确定事实」）。

## 4. 验证矩阵（P4-b 闸口）

| 项 | 结果 |
|---|---|
| `npm run check`（svelte-check） | **0 errors, 0 warnings** |
| `go vet ./...` + `go test ./...` | 全绿（59 用例，P4-b 未改 Go 逻辑） |
| `scripts/check-manifest.py` | PASS: **53** |
| `scripts/check-manifest.py --package` | PASS: **107**（manifest 与 `main.go` 同为 0.3.1） |
| `scripts/smoke-test.py`（bin / 包内 exe） | **105 / 105** 两次 |
| `scripts/sign-package.py --verify` | **SIGNATURE VALID** |
| `scripts/verify-package.py` | **17 / 17** |
| `scripts/browser-test.py`（主回归） | **23 / 23**，控制台 0 报错 |
| `scripts/browser-ux-test.py` | **90 / 90**（新增 15 条：诊断 9 + 对比 6），控制台 0 报错 |
| `scripts/browser-write-test.py --rw` | **7 / 7** |
| `scripts/browser-write-test.py --ro` | **4 / 4** |

## 5. 交付物

- 包：`dist/local.es.studio-0.3.1-windows-x64.signed.dbxp`（4,519,554 B，已签名）。
- 安装：DBX 插件中心 → 设置 → 从 URL 安装（同版本先卸载）；本地静态服务 `cd dist && python -m http.server 19321`。
- 导航：**24 项可用**（P1 1 + P2 9 + P3 9 + P4 5）；只剩 P5 的 `MCP Tool Service` 仍是占位。

## 6. 已知限制

1. 本机只能跑一个 ES，「右侧集群」在测试里是 `scripts/mock-cluster.py` 测试替身；真实双集群需用户环境。
2. 对比范围是**集群级事实**（22 行），不做逐索引 diff。
3. CPU / 堆是瞬时快照，规则里已注明「需结合监控曲线确认是否长期」。
4. `/_ilm/status` 在部分发行版不存在，此时 ILM 规则自动跳过（不报错、不误报）。
