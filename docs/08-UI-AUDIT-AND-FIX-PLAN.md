# 08 · 全站 UI 一致性截断问题审查与修复方案

> 用户反馈：① 查询构建器几个下拉框样式不一样；② 查询构建器结果展示不完整，未按「一个字段一列」展示；③ 查询剖析页文本过长被省略号截断，看不到完整内容；④ 好多页面都这样。
> 本报告覆盖全部 25 个页面 + 8 个组件 + `src/styles/tokens.css`，给出文件级可执行方案。**不含代码改动**，与 `07-PERF-LARGE-CLUSTER.md` 配套执行（两者都涉及表格改造，应合并排期）。

---

## 1. 结论

三个反馈对应三个独立但同源的问题：**项目缺少统一的基础控件与内容展示规范**，各页面各写各的。

| 反馈 | 根因 | 影响范围 |
|------|------|----------|
| 下拉框样式不一样 | 全局没有 select 视觉规范（`tokens.css:59` 只做字体继承），各页面自写 padding/背景/字体；QueryBuilder 同页内 2 套 padding + 4 套宽度/字体/语义色 | QueryBuilder 最明显，另有 4 页偏离基准 |
| 结果未按字段分列 | QueryBuilder 结果表是固定 3 列 `_id / score / _source`，`_source` 被 JS 截 90 字符 + CSS 截 460px，**双重截断** | QueryBuilder；同类预览见 DslEditor / Browser |
| 文本过长看不到完整 | `QueryProfiler.svelte:136` 把 node ID 硬截 10 字符并拼接 `…`，无 title/展开/复制；全站共 **8 处 CSS ellipsis + 6 处 JS slice+… + 6 处数量上限** | 至少 10 个页面，3 处最严重 |

**关键事实：数据层全部完整。** `parseSearch()`（`services.ts:599-618`）保留完整 `hit._source` 对象和 `raw`；`fetchFields()`（`services.ts:446`）已能取到字段清单（含 6.x typed mapping）。所有截断都是**纯展示层选择**，修 UI 无需改后端或数据层。

---

## 2. 下拉框审计

### 2.1 系统性根因

`src/styles/tokens.css` 是项目唯一 CSS 文件，其中唯一涉及 select 的规则：

```css
/* tokens.css:59 */
button, select, input, textarea { font-family: inherit; font-size: inherit; }
```

只继承字体，没有 padding / border / radius / background / color 规范。**不存在可复用的全局控件样式**，8 个页面各写一套。

### 2.2 事实基准

8 个文件采用同一套声明，可作为统一基准：

```css
padding: 6px 10px;
border: 1px solid var(--es-border);
border-radius: 6px;
background: var(--es-surface);
color: var(--es-text);
```

基准文件：`Aliases.svelte:333`、`Browser.svelte:274`、`ClusterSettings.svelte:267`、`RestConsole.svelte:146`、`QueryProfiler.svelte:186`、`Templates.svelte:316`、`DslEditor.svelte:211`（多 `min-width:190px`，属功能合理差异）、`QueryBuilder.svelte:431`。

### 2.3 偏离基准的页面

| 文件:行号 | 差异 |
|-----------|------|
| `ClusterMetrics.svelte:277` | `.bar select` padding `4px 8px`，控件偏矮偏窄 |
| `History.svelte:193` | 裸 `select{}` 标签选择器（全站唯一），padding `5px 8px` |
| `McpService.svelte:387` | padding `5px 8px`；**背景用 `--es-bg`（页面底色）而非 `--es-surface`**（全站唯一），下拉框与页面同色；另有固定 `font-size: var(--es-fs-sm)` |
| `Analyzer.svelte:161` | 唯一给 select 强制 `font-family: var(--es-mono)` + 固定字号 |
| `QueryBuilder.svelte:439-447` | 同页内第二套 padding `6px 8px`，且 4 个条件下拉宽度/字体/颜色各不相同（见 2.4） |

### 2.4 QueryBuilder 专项（用户反馈直接来源）

6 个原生 `<select>`，分两组由两套规则渲染：

- **顶部 `.bar`**（`:431`，padding `6px 10px`）：索引选择 `:304`、排序方向 `:313`。
- **条件区 `.clause`**（`:439`，padding `6px 8px`）：
  - `occur` `:332` — mono 字体、`fs-xs`、宽 `92px`，且按取值染 4 种语义色（`:441-444`：must 绿 / filter 蓝 / should 黄 / must_not 红）；
  - `op` `:336` — 默认字体、`fs-xs`、宽 `168px`；
  - `field` `:340` — mono 字体、`fs-xs`、`flex:1 1 200px`；
  - `value` `:353` — 默认字体、宽 `150px`，**仅布尔字段时才渲染为下拉**，其它情况是 `<input class="value">`（`:358`）。

用户看到的「几个下拉框样式不一样」是上述三重叠加：同页两种 padding、四种宽度、两种字体，以及同一位置一会儿是输入框一会儿是下拉框。无自定义 dropdown/button+menu 混用，也无 inline style 或 `appearance` 覆盖。

---

## 3. 内容截断审计

### 3.1 用户点名的两处

**QueryBuilder 结果表**（`QueryBuilder.svelte:390-401`）

```
表头固定 3 列：_id | score | _source        (:393)
_source 单元格 = preview(hit.source)        (:396)
preview(): JSON.stringify → 截 90 字符 + "…" (:247-254)
CSS: .docs td.src max-width:460px; overflow:hidden; text-overflow:ellipsis (:463)
```

即 **JS 截 90 字符 + CSS 再截 460px 的双重截断**，无 `title`、无逐条展开、无复制；只有「展开原始响应」按钮（`:400`）能看整块 JSON，但不是字段分列。

**QueryProfiler 分片表**（`QueryProfiler.svelte:129-144`）

```
7 个固定列：shard / index / node / query / rewrite / collector / nodes
node 列：{shard.node.slice(0, 10)}…        (:136)
```

node ID 永久只剩 10 字符，无 title/复制/展开。查询树区的 `description`（`:156`）实际是完整换行的，未被截断——用户说的「三个点」就是 `:136` 这一处以及结果区的 `_source` 预览。

### 3.2 其它页面

**CSS 省略号（8 处）**

| 文件:行号 | 对象 | 缓解手段 |
|-----------|------|----------|
| `components/Inspector.svelte:67` | 详情弹窗标题 | 内容区完整可滚动 |
| `Aliases.svelte:353` | filter 表达式 | `:285` 有 `title=row.filter` |
| `Browser.svelte:289` | 文档 `_source` 列 | `:225` view 按钮 → JsonView |
| `DslEditor.svelte:227` / `:231` | `_source` 列 / DSL 历史 | 仅整体 showRaw |
| `QueryBuilder.svelte:422` / `:463` | 标签名 / `_source` 列 | 仅整体 showRaw |
| `RestConsole.svelte:162` | REST 历史项 | 点击回填编辑器 |

**JS `slice()+…`（6 处）**

| 文件:行号 | 对象 | 缓解手段 |
|-----------|------|----------|
| `QueryProfiler.svelte:136` | node ID 截 10 字符 | **无** |
| `QueryBuilder.svelte:250` | `_source` 截 90 字符 | 仅整体 showRaw |
| `Browser.svelte:148` | `_source` 截 80 字符 | view → JsonView |
| `DslEditor.svelte:131` | `_source` 截 80 字符 | 仅整体 showRaw |
| `Snapshots.svelte:309` | 索引列表截 3 项 | **无** |
| `McpService.svelte:247` | token 截 12 字符 | `:244` 复制按钮 + `:249` 完整 textarea |

**数量上限（6 处，多数有提示）**：`Diagnostics.svelte:209`（targets 3 项）、`ClusterSettings.svelte:224`（默认设置 200 行）、`ShardDiagnosis.svelte:72`（EXPLAIN_CAP）、`History.svelte:66`（500 行 + 复制 CSV）、`DslEditor.svelte:120`（历史 10 条）、`RestConsole.svelte:58`（历史 12 条）。这些有「等 N 项」提示或导出手段，优先级低于纯文本硬截。

**无 `-webkit-line-clamp`**：全库 0 处。

### 3.3 现有完整查看组件（可直接复用）

| 组件 | 能力 | 当前使用者 |
|------|------|-----------|
| `JsonView.svelte` | 完整 `JSON.stringify(value,null,2)`，`pre-wrap` + `break-word` + 横向滚动，内容不截断；**无复制按钮** | Browser / ComponentTemplates / DslEditor / QueryBuilder / QueryProfiler / RestConsole / Templates |
| `Inspector.svelte` | 模态详情，`.body{overflow:auto}`，Esc 关闭；**无复制按钮**；仅 h3 标题省略号 | Templates / ComponentTemplates |
| `DataTable.svelte` | `td{white-space:nowrap}` 但无 ellipsis/宽度上限，容器横向滚动可看全；支持排序；无展开/复制 | 仅 Nodes |

**正面范例**：Browser 同样预览截断（`:148`/`:289`），但配了逐条 view 按钮 → JsonView，内容可达。问题页面缺的不是组件，而是复用组件的入口。

---

## 4. 可执行修复方案

### P0-1 统一控件样式（小改动，收益覆盖全站）

1. `src/styles/tokens.css`：新增全局基础规则（用 token 变量，**禁止出现 `font-size:NNpx`**）：

   ```css
   select, input:not([type=checkbox]):not([type=radio]), textarea {
     padding: 6px 10px;
     border: 1px solid var(--es-border);
     border-radius: 6px;
     background: var(--es-surface);
     color: var(--es-text);
     font-family: inherit;
     font-size: inherit;
   }
   ```

2. 删除 2.3 表中 4 个页面的本地 select 视觉声明，只保留功能性差异（如 DslEditor 的 `min-width`、Analyzer 的 datalist 输入）。`History.svelte:193` 的裸 `select{}` 选择器必须改掉——它会命中表内任何 select。
3. 修正 `McpService.svelte:387` 的 `--es-bg` → `--es-surface`。
4. `.bar` / `.clause` 等容器选择器改为只设布局（gap/flex），不重复设控件视觉。
5. **类名撞名检查**：新增/调整 class 前 grep 全库（历史 bug：`.tools{display:flex}` 命中 `<table class="tools">` 导致表头悬空），新类统一用 `es-` 前缀。

### P0-2 QueryBuilder 结果按字段分列

数据层零改动。改动集中在 `QueryBuilder.svelte`：

1. 新增 `$derived` 列定义 `$derived.by(() => ...)`：
   - 基准列来自 `fieldsOf(active)`（已返回的 `FieldInfo[]`，`:320` 使用同一个来源）；
   - 再遍历 `active.hits`，把命中记录里出现但 mapping 没有的字段追加进列集合（兼容 `_source` 动态字段和 6.x typed mapping）；
   - 顺序：mapping 顺序优先 → 额外字段按首次出现顺序；
   - 固定列 `_id`、`score` 保留在最前。
2. 表头由 `:393` 的固定三列改为动态 `{#each columns as col (col.key)}`，字段列显示 `字段名 · 类型`（类型取 `fieldTypeOf`）。
3. 表格容器加 `overflow-x:auto`，`td { white-space:nowrap }`——列多时横向滚动，**不截断内容**（参照 `DataTable.svelte:95-97` 的做法）。
4. 单元格值渲染：标量直接输出；对象/数组用紧凑 JSON 单行显示，**不加 `slice`**；超长文本靠横向滚动查看。
5. 每行末尾增加「详情」按钮 → `JsonView value={hit.source}`（复用现有组件，放在行下方或弹层），并保留现有「展开原始响应」按钮作为整体 JSON 入口。
6. 删除 `preview()`（`:247-254`）与 `.docs td.src` 的 ellipsis 规则（`:463`）。
7. **字段极多时的取舍**：若列数 > 30，默认显示前 30 列 + 「显示全部 N 列」开关（i18n 新增 1 键，zh-CN/en 各 1 键，两边键集必须一致）。
8. 布尔字段的 `value` 控件（`:353` select vs `:358` input）统一视觉：两者套用 P0-1 的全局规则并设同宽 `width:150px`，差异只体现在控件类型，不体现在边框/内距/背景。

### P0-3 查询剖析页 node ID 完整展示

1. `QueryProfiler.svelte:136` 改为 `{shard.node}` 或 `{shard.node.slice(0,10)}…` 删除——依据 `td` 可换行的前提下直接完整显示；同时加 `title={shard.node}`。
2. 若需保持表格紧凑：node 列固定宽度 + `word-break:break-all`（ES node ID 是长哈希，可安全断行），**不使用 ellipsis**。
3. 对照检查同一表格其它列：query/rewrite/collector 是毫秒数字，index 名完整，无需动。

### P1 通用「完整内容查看」能力（消灭「三个点却没处看」）

1. 新增 `src/components/CellValue.svelte`：短值直出；长值显示为可点击文本，点击后用 `Inspector` 或行内 `JsonView` 展示完整内容，附「复制」按钮。给出 `max` prop 控制摘要长度，但**摘要之外必须有可达的完整内容**。
2. 接入优先顺序（按 3.2 严重度）：
   - `DslEditor.svelte:131/227` — `_source` 预览和文档列（与 QueryBuilder 同类问题）；
   - `Snapshots.svelte:309` — 索引列表截 3 项，补「查看全部 N 个索引」；
   - `Aliases.svelte:353`、`RestConsole.svelte:162`、`QueryBuilder.svelte:422` — 已有间接手段，补 `title=` 或复制即可；
   - `components/Inspector.svelte:67` — 标题 ellipsis 补 `title=`。
3. 给 `JsonView.svelte` 和 `Inspector.svelte` 加「复制内容」按钮（当前两者均无），避免"看到了但取不出来"。
4. 数量上限类（Diagnostics/ClusterSettings/ShardDiagnosis/History）已有提示或导出，保持现状；只补齐 Snapshots 的查看入口。

### P2 与 07 号性能报告合并的表格改造

`DataTable.svelte` 的分页改造（07 号报告 §3.3）与本报告 P0-2 的字段分列都涉及表格组件。**建议合并为一个表格改造任务**：先做 P0-2 的 QueryBuilder 专项（专用动态列），再给 DataTable 加 `pageSize` prop 供 Shards/IndexList 使用，避免两次回归 106 项 UX 测试。

---

## 5. 实施清单

- [x] P0-1 `tokens.css` 全局控件规则 + 4 页本地样式清理 + `--es-bg` 修正 + 撞名 grep
- [x] P0-2 QueryBuilder 动态字段列 + 逐行详情 + 删双重截断 + 新 i18n 键（zh-CN/en 同步）
- [x] P0-3 QueryProfiler node 列完整展示 + title
- [x] P1 JsonView/Inspector 复制按钮 + 逐行详情/title 接入（未单独抽 `CellValue.svelte`，由现有组件组合覆盖）
- [x] P2 与 07 号报告合并的 DataTable 分页改造
- [x] 同步更新 `scripts/browser-ux-test.mjs` 等浏览器脚本的 CSS 选择器（UI 文案/结构改动后必须同步）
- [x] 测试链：`npm run check`（svelte-check 0/0）、browser-test、browser-ux-test（106）、browser-render-check（25 页 0 错）、browser-i18n-test（25+25，键集一致）、go test
- [x] 版本号 bump（manifest.json + backend/main.go pluginVersion）+ 打包签名链

## 6. 验收标准

1. **下拉框**：QueryBuilder 页 6 个 select 与 QueryProfiler/IndexList 等页的 select 计算样式一致（padding/border/background/font-family 一致）；occur 的语义色仅作为取值指示保留。
2. **结果展示**：6.x 与 8.x 集群上，QueryBuilder 执行 `_search` 后，每个 `_source` 字段独占一列；字段数 > 30 时有「显示全部」开关；任意单元格内容可通过逐行详情看到完整 JSON 并复制。
3. **剖析页**：node ID 完整可见或可换行查看，无 `…` 硬截；查询树 description 保持完整。
4. **全站**：随机抽查 10 个含 `slice()+…` 或 ellipsis 的位置，每处都能通过 title/展开/详情/复制中的至少一种方式获取完整内容。
5. **不回归**：0.5.1 基线全绿（23/106/25/7/4/25+25/32 项），25 页渲染 0 错，zh-CN/en 键集一致。
