# P6 验收

版本 `0.5.0`。P6 分两批:P6-a 国际化与字号,P6-b 多版本兼容。
两个问题都来自验收反馈:界面中英文混排、字号写死;版本兼容只有 8.19 实证。

---

## 1. 语言与字号跟随 DBX

### 行为

| 宿主动作 | 插件表现 |
|---|---|
| 初始化消息带 `locale` | boot 时 `setLocale(normalizeLocale(...))`,无需刷新 |
| 设置里切换语言(发 `env` 消息) | runtime 派发 `dbx-plugin-env`,`t()` 所读的 `$state` 变化 → 全部模板即时重渲染 |
| 主题 tokens 带 `--dbx-font-size`(或同义名) | `syncTypeScale` 钉到 `--es-fs-base`,294 处派生字号整体缩放 |
| tokens 不带字号 | CSS 候选链兜底 13px,不留死值 |

### 机制要点

- **一处文案一个键**:`zh-CN.ts` / `en.ts` 各 960 键,键集一致(脚本比对);
  缺键回退中文而不是空白。
- **MCP 工具说明是例外**:中文真源在 Go registry(工具契约),英文放
  `tools.en.ts` 叠加层,`tOr()` 回退到 registry——不让同一句话存在两个真源。
- 字号不留 `px` 常量:全项目 `font-size: NNpx` 已清零(替换为刻度变量)。

### 验证(`scripts/browser-i18n-test.py`,逐页点完 25 个导航项)

| 断言 | en(BRIDGE_LOCALE=en, token 15px) | zh-CN(默认 13px) |
|---|---|---|
| 正文 CJK 字符数 | 25/25 页 = 0 | — |
| title/placeholder/aria-label 无 CJK | 25/25 | — |
| h1 全中文 | — | 25/25 |
| 导航标签全中文 | — | 25/25 |
| body 字号 == 宿主值 | 25/25(15px) | 25/25(13px) |

截图:`screenshots/p6/en-*.png`、`screenshots/p6/zh-CN-*.png`(每页一张)。

### 顺带发现的真 bug(被英文回归暴露)

- MCP 页在英文界面下有 1251 个 CJK 字符 → 引入 `tools.en.ts`。
- Aliases 表单的 `Filter` / `Routing` 标签、Browser 的 `All fields (n)` /
  `Loading fields…`、Analyzer 的 `n tokens` 是漏网硬编码 → 全部键化。
- `Analyzer`/`DSL`/`QueryBuilder` 结果表的 `Score`、分词表 `Token` 键化为
  得分/词元;`_id`/`_source`/`settings`/`mappings` 等 ES 协议词保留原文。

---

## 2. 多版本兼容

### 实证(`scripts/version-probe.py`,自管配置、一次一台)

| 项目 | 6.8.23 | 7.17.29 | 9.5.4 |
|---|---|---|---|
| `_mapping` 布局 | typed(`mappings._doc.properties`) | typeless | typeless |
| `_data_stream` / `_index_template` / `_component_template` | **400** | 200 | 200 |
| `_ilm/status` | 200 | 200 | 200 |
| `_tasks?group_by=none` | 扁平 `tasks[]` | 扁平 | 扁平 |
| `_cluster/stats` 的 `nodes.fs.total_in_bytes` | 有 | 有 | 有 |
| `_cat/nodes` 的 `name` / `node.role` / `master` 键(默认列与显式 `h=` 两种形态) | 有(`name`) | 有(`name`) | 有(`name`) |
| 写已关闭索引 | 400 `reason=closed` | 同 | 同 |

**7.0–7.7 这一档(2026-11-15 18:2x 补测,7.0.1 与 7.7.1 两端各一台):**

| 项目 | 7.0.1 | 7.7.1 |
|---|---|---|
| `_mapping` 布局 | typeless | typeless |
| `_data_stream` / `_index_template` / `_component_template` | **400**(GET)/**405**(PUT) | 同 |
| `_cat/nodes` 的 `name`/`node.role`/`master` | 有 | 有 |
| `_tasks?group_by=none` | 扁平 `tasks[]` | 扁平 |
| typeless 建索引(`mappings.properties`) | 200 | 200 |

两端形状完全一致 ⇒ `TypedAPI = major<8` 对整段 7.x 成立,
先前"7.0–7.6 的 `_mapping` 可能带回类型层"的担心**不成立**(`include_type_name`
只影响写侧,7.0 起 typeless body 已默认接受)。

节点名的原始键已落盘(`catNodes.*.keys`):`name` 是唯一的节点名列键,
默认列与显式 `h=` 两种形态在 6.8 / 7.0 / 7.7 / 7.17 / 8.19 / 9.5 上一致,
因此 `parseNodes()` 与 `fetchNodes()` 只认 `name` 是安全的,**不需要 `node.name` 兜底**。

原始报告:`D:/tmp/es-probe/recheck-6.8.23/probe.json`、`D:/tmp/es-probe/recheck-9.5.4/probe.json`、
`D:/tmp/es-probe/elasticsearch-7.17.29/probe.json`(2026-11-15 18:01 复跑)。
⚠️ `D:/tmp/es-probe/elasticsearch-6.8.23/probe.json` 与 `elasticsearch-9.5.4/probe.json`
是 16:15 旧版脚本的产物,其 `_cat/nodes` 行恒为 FAIL(旧 `must` 写成 `["name"]`,
对数组响应永远取不到,与真实形状无关),**不可作证据**。

### 传统模板(`_template`)实证(2026-11-15 19:2x 补测)

此前文档写「6.x / 7.0–7.7 传统 `_template` 未适配」——**这条是错的**,现纠正。
四个版本的实测结果(直接 curl + UI 走查):

| 版本 | `GET/_template` | typeless body(`mappings.properties`) | typed body(`mappings._doc`) |
|---|---|---|---|
| 6.8.23 | 200 | **400** `Root mapping definition has unsupported parameters` | **200** |
| 7.7.1 | 200 | **200** | **400** `The mapping definition cannot be nested under a type [_doc]` |
| 8.19.23 | 200 | 200 | **400** `cannot be nested under a type [_doc]` |
| 9.5.4 | 200(空集群为 `{}`) | 200 | **400** 同上 |

两条由此确证的事实:

1. **`_template` 端点从 6.8 到 9.5 一直可用**(9.x 上是空对象而不是 404),
   所以它不属于"未适配"——页面原本就同时读 `_index_template` 与 `_template`。
2. **mapping 类型层的分界在 7.0,不在 8.0**:6.x 必须 `mappings._doc`,
   7.x 起必须不带类型层。原先按 `typedApi = major<8` 判断,会把 7.x 判成
   "需要类型层",在 7.7.1 上实测直接 400。已改用独立能力位
   `legacyTypedMappings = major<7`(`src/lib/capabilities.ts`)。

同时修掉三个真实缺陷(均在本轮 UI 走查中复现):

| 现象 | 根因 | 修法 |
|---|---|---|
| 6.8 上创建传统模板一律失败,提示只有「写入失败:HTTP 400」 | 草稿是 composable 形状(`priority` + `template` 包装 + typeless mappings),6.x 三条全不接受 | 新增 `normalizeLegacyTemplate()`:展开 `template`、`priority`→`order`、去 `composed_of`、按版本加/去 `_doc`;toast 明示改了哪几项 |
| 失败原因丢失 | 页面取 `res.error?.message`,而 ES 的 400 原因在 `body.error.reason` | 统一用 `failureText()`,提示变成 `unknown key [priority] in the template · HTTP 400` |
| 集群连不上时页面显示空表 | `throwIfUnsupported()` 只认 400/405/501,`status:0` 被当成"没有数据" | 传输层错误(`res.error` 且 `status<400`)改为抛错,页面显示红块 + 原始原因 |

6.8 / 7.7.1 / 8.19 三台都跑通了「新建 → 列表出现 → 查看弹层 → 删除」:
6.8 走 typed 分支、7.7 与 8.19 走解包分支,控制台 0 报错。

### 代码改动

1. **能力矩阵按引入版本重写**(Go `capability.go`):ILM 6.6 · 组件模板 7.8 ·
   数据流 7.9 · PIT 7.10 · ES|QL 8.11;typed = `major<8`。`Engine` 由
   `DetectEngine` 传入。单测改为按 6.8/7.7/7.8/7.9/7.10/7.17/8.0/8.11/8.19/9.0
   边界逐项断言(旧断言"7.x 无数据流"是错的,已改)。
2. **typed mapping**:字段下拉先读 `mappings.properties`,没有再逐类型下钻,
   6.x 不再是空下拉。
3. **静默空表 → 明确不支持**:8 处 `isError → []` 中 7 处改为
   `throwIfUnsupported`(400/405/501 抛 `err.unsupportedApi` 带接口名);
   `fetchFields` 仍对缺失/关闭索引返回 `[]`。
4. **门控前移**:`clusterCapabilities()`(GET / 每连接缓存一次)让数据流页 /
   组件模板页在发请求前就显示"此集群为 6.8.23,数据流需要 7.9 或更高",
   并隐藏写入口。
5. **失败文案**:`failureText()` 优先 `body.error.reason`/`root_cause`,429
   无解释时给背压提示,不再只显示 `HTTP 429`。

### 被回归暴露的第三类 bug(真 bug,与 i18n 无关)

`analyzeText()` 永远 POST `/_analyze`,页面算好的 `/{index}/_analyze` 是死代码
——**按字段分析从未生效过**(ES 返回 "analysis based on a specific field
requires an index")。P3 验收用例恰好没带字段所以没暴露。已修:索引进入路径。

---

## 3. 构建链修复(高危)

`vite.config.js` 单文件内联用**字符串** `replace`,压缩产物里的 `$&`
(`caps && !caps.x` → `$&&!$`)被展开成匹配到的 `</body>`,脚本解析失败,
工作台白屏且无插件侧报错。已改函数替换,并断言内联字节与源一致。

---

## 4. 跨版本 UI 验证(2026-11-15 18:0x 补测)

此前只有 8.19 跑过 UI,其余版本停留在接口形状探测。本轮用同一套夹具与脚本,
把 UI 真正跑到五个非 8.x 集群上(每集群一个真实 ES + `browser-bridge.mjs`):

| 版本 | 主回归 | UX 套件 | 写路径 | 只读拒绝 | 控制台报错 | 说明 |
|---|---|---|---|---|---|---|
| 7.0.1 | 20/23* | 95/98* | 6/7* | 未跑 | 0 | 渲染 25/25;*失败项均为 7.8/7.9/7.11 夹具或能力缺失 |
| 7.7.1 | 20/23* | 未跑 | 未跑 | 未跑 | 0 | 渲染 25/25;*同为三个门控页 |
| 7.17.29 | 23/23 | 106/106 | 7/7 | 4/4 | 0 | 全夹具(数据流/组件模板/快照)可用 |
| 9.5.4 | 23/23 | **111/111** | 7/7 | 4/4 | 0 | 含新增的传统模板全流程 5 项(见下) |
| 6.8.23 | 25/25 页渲染+门控 | 不适用 | 不适用 | 不适用 | 0 | 三套套件的夹具物理不成立,改用渲染检查 |

### 这些"失败"是怎么处理的(逐条定性,不含推测)

旧口径的失败有三类,处理方式不同,没有一类是"记一笔就放过":

| 类别 | 例子 | 判定依据 | 处理 |
|---|---|---|---|
| 夹具在本版本物理不存在 | 索引模板/组件模板/数据流的 View 断言等 `p3-demo-*`;写路径的 rollover 需 7.9 | 直接对集群 `curl` 该端点:7.0/7.7 上 `_index_template` 400、`_data_stream` 400 | **改套件,不改结论**:`PAGES` 增加门控兜底文案,命中时记录 `gated_by` 而不是判 FAIL;模板/组件模板/数据流三页若出现门控文案则断言文案正确 |
| 需要更高版本的字段 | `orders-hidden` 的 `is_hidden`(7.11) | 该字段让**整个 `_aliases` actions 数组** 400,导致三个别名一个都没建成 | 夹具拆成两个请求;套件里该段的 `except` 不再复用别的用例标签 |
| 测试自身缺陷 | `probe.json` 的 `_cat/nodes` 恒 FAIL;`Aliases 筛选生效` 误报 | 用合成响应跑 `check()/dig()`,PASS;改 `must=["0.name"]` 后三台全绿 | 修脚本;`_cat/nodes` 拆两种形态并把原始键落盘,FAIL 自带解释 |

由此**修掉的两个测试可信度问题**(比功能 bug 更值得记):

1. 之前的逐页渲染检查是 `%TEMP%` 里的一次性脚本,结论无法复现 → 已入库为
   `scripts/browser-render-check.py`(断言 25 页 `h1`、控制台 0 错、门控文案含版本号),
   6.8 与 7.7 各跑一遍:25/25、0 报错。
2. 套件把"预期的版本差异"报成 FAIL,真回归会淹没在噪声里 → 主回归与 UX 套件
   都改成门控感知;9.5 上复跑仍 23/23 与 111/111,证明改动没有放水。

另外本轮走查顺手抓到 4 个真缺陷(2 个页面行为、2 个错误呈现),修法与证据见 §2
「传统模板实证」小节;其中「传输失败被渲染成空表」是靠 7.0 集群中途被回收才暴露的。

7.0.1 的 3 项失败逐条定性(均已手工复现确认非插件缺陷):
① 索引模板 / 组件模板的 View 弹层 —— `p3-demo-*` 夹具需 7.8,建不出来;
② Aliases 的 hidden 标记 —— `is_hidden` 建别名需 7.11,`orders-hidden` 不存在。
别名筛选本身在 7.0.1 上可用:手工复现 3 行 → 筛选 `prod-eu` → 1 行,
filter/routing 列正常渲染。

- 6.8 的"不适用":三套套件的期望文本(`p3-demo-*` 夹具)在 6.8 上物理不成立,
  失败只说明夹具没有,不代表缺陷。改用逐页渲染检查(临时脚本,未入库):
  25 页全部渲染出 `h1`、0 控制台错误;数据流页实测显示
  「此集群为 6.8.23,数据流需要 Elasticsearch 7.9 或更高版本。」
  组件模板页同理(7.8)。截图 `/tmp/render68-*.png`。
- 7.x 的门控切换点(7.8/7.9)现在两端都有实证:7.0.1 / 7.7.1 在门限之下(三个接口
  400 且页面显示"需要 7.9/7.8 或更高版本"),7.17.29 在门限之上(全功能)。
  7.7.1 主回归失败的 3 项同样是这三个门控页,与设计一致,不是缺陷。

### 顺带修掉的工具链问题(为跨版本复用)

- `es-cluster.py` 配置模板按主版本生成:7.x 拒绝 `xpack.security.enrollment.enabled`
  (unknown setting,拒绝启动);6.x 无 `discovery.type`,需 zen discovery;
  6.x 发行包不带 JDK,`ES_JAVA_HOME` 改为仅在 bundled `jdk/` 存在时设置,
  否则回退 `ES_STUDIO_JAVA_HOME`/`JAVA_HOME`(Java 8-11)。
- `seed-es-data.sh` 快照仓库路径原硬编码 `D:/tmp/es-studio/snapshots`,
  第二实例上 PUT 500(location 不在 `path.repo`);改 `ES_SNAPSHOT_DIR` 可覆盖。

---

## 5. 全量基线(ES 8.19.23,0.5.0 签名包)

| 项 | 结果 |
|---|---|
| `svelte-check` | 0 errors / 0 warnings |
| `go test ./...`(含新能力矩阵边界) | 全绿 |
| Sidecar smoke(真实 ES 8.19) | 125/125 |
| 浏览器主回归(真实 ES,读写) | 23/23,控制台 0 报错 |
| 浏览器 UX 回归 | **111/111**(新增传统模板全流程 5 项) |
| 浏览器写路径(每步回查 ES 真实状态) | 7/7;只读拒绝 4/4 |
| i18n 全页×双语 | 25+25 全过,字号跟随 15px/13px |
| check-manifest | 53(含包 107) |
| verify-package / 签名 | 17/17 / SIGNATURE VALID |

> 界面语言由英文脚手架改为中文后,三个浏览器脚本的选择器与期望文案同步翻译
> ——否则会把"文案变了"误判成"功能坏了"。本轮绝大多数调试时间花在这里,
> 也是 106/106 与 7/7 从 0/18、3/7 涨上来的原因。

---

## 6. 已知限制

- **传统 `_template` 已支持**(6.8.23 / 7.7.1 / 8.19.23 / 9.5.4 实测):端点
  全程可用,页面同时读 `_index_template` 与 `_template`。类型层分界在 **7.0**
  (6.x 需 `mappings._doc`,7.x 起必须不带),写入前由 `normalizeLegacyTemplate()`
  按版本自动转换并在 toast 里说明改了什么。原文写的"未适配"是错的,已纠正。
- **7.0–7.7 已实证**(7.0.1 + 7.7.1 两端):接口形状与 UI 渲染均与 7.17 同族,
  三个门限接口返回 400/405、`_mapping` typeless。
  7.0.1 的门控**截图**已补测(2026-11-15 20:12):真实 7.0.1 集群 + 逐页渲染检查
  25/25、0 控制台错误,三张门控证据图在 `screenshots/p6/7.0-{index-templates,
  component-templates,data-streams}.png`,门控原句:
  「此集群为 7.0.1，组合模板（_index_template）需要 Elasticsearch 7.8 或更高；
  下方只显示传统模板（_template）。」/「组件模板需要 Elasticsearch 7.8 或更高版本。」
  /「数据流需要 Elasticsearch 7.9 或更高版本。」索引模板页下方同时正常渲染
  legacy 模板列表。复现命令见 §7(`--gate-out` 会保存门控页整页图)。
- **9.x 的传统模板**:`GET /_template` 返回 `{}` 而非 404,说明端点仍在;
  但官方已标注弃用,若未来移除,页面会自动转为"只显示组合模板"(门控文案已就绪)。
- **Easysearch / OpenSearch**:版本号语义与 ES 不同,矩阵只对 ES 成立;
  `engine` 随矩阵返回,页面文案按 ES 版本表述。
- **新增 MCP 工具若忘补英文**,英文界面回退中文说明(不空白,可发现)。
- 语言/字号跟随依赖宿主 `env` 消息与 theme tokens;若宿主不发 `env`,
  语言在下次打开时生效。

## 7. 如何复现

```bash
npm run build && npx svelte-check --tsconfig ./tsconfig.json
export PATH=/d/software/dev/Go/go/bin:$PATH && (cd backend && go test ./...)
"D:/software/dev/Python/Python313/python.exe" scripts/smoke-test.py --sidecar bin/es-studio-sidecar.exe
# 双语言浏览器回归(两个桥:5210 en + 15px token,5211 zh)
node scripts/browser-bridge.mjs &   # BRIDGE_LOCALE / BRIDGE_TOKENS 见脚本头部
"D:/software/dev/Python/Python313/python.exe" scripts/browser-i18n-test.py --port 5210 --locale en --expect-font 15px
"D:/software/dev/Python/Python313/python.exe" scripts/browser-i18n-test.py --port 5211 --locale zh-CN --expect-font 13px
# 多版本实证(一次一台;6.8 发行包不带 JDK,用 ES_STUDIO_JAVA_HOME 指 Java 8)
python scripts/version-probe.py --home D:/tmp/es-versions/elasticsearch-6.8.23 --port 19202
# 跨版本 UI(每台独立 root/端口/数据目录,夹具路径要跟 path.repo 对齐)
ES_HOME=D:/tmp/es-versions/elasticsearch-7.7.1 ES_STUDIO_TEST_ROOT=D:/tmp/es-771 \
  ES_STUDIO_TEST_PORT=19213 python scripts/es-cluster.py start
ES_URL=http://127.0.0.1:19213 ES_SNAPSHOT_DIR=D:/tmp/es-771/snapshots bash scripts/seed-es-data.sh
ES_URL=http://127.0.0.1:19213 BRIDGE_PORT=5199 DBX_PLUGIN_DATA_DIR=.dbx-dev/plugin-data-771 \
  node scripts/browser-bridge.mjs
"D:/software/dev/Python/Python313/python.exe" scripts/browser-test.py \
  --base-url http://127.0.0.1:5199
# 夹具在本版本不成立的集群(6.8/7.0/7.7):改用逐页渲染检查
# --gate-out 会额外保存门控页整页图并打印门控原句
"D:/software/dev/Python/Python313/python.exe" scripts/browser-render-check.py \
  --base-url http://127.0.0.1:5199 --expect-version 7.0 --gate-out screenshots/p6
```

注意:本机后台起的 ES 会被环境回收,`es-cluster.py start`、`browser-bridge.mjs`
与被测脚本应在**同一轮**内跑完(`version-probe.py` 自带这个闭环);桥接必须用
工具级常驻(前台 `&` 起的进程在命令返回后即被回收)。
