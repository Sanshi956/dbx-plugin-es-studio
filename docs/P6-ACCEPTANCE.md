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
| `_cat/nodes` 的 `node.role`+`master` 列 | 有 | 有 | 有 |
| 写已关闭索引 | 400 `reason=closed` | 同 | 同 |

原始报告:`D:/tmp/es-probe/elasticsearch-*/probe.json`。

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

## 4. 全量验证

| 项 | 结果 |
|---|---|
| `svelte-check` | 0 errors / 0 warnings |
| `go test ./...`(含新能力矩阵边界) | 全绿 |
| Sidecar smoke(真实 ES 8.19) | 125/125 |
| 浏览器主回归(真实 ES,读写) | 23/23,控制台 0 报错 |
| 浏览器 UX 回归 | 106/106 |
| 浏览器写路径(每步回查 ES 真实状态) | 7/7;只读拒绝 4/4 |
| i18n 全页×双语 | 25+25 全过,字号跟随 15px/13px |
| check-manifest | 53(含包 107) |
| verify-package / 签名 | 17/17 / SIGNATURE VALID |

> 界面语言由英文脚手架改为中文后,三个浏览器脚本的选择器与期望文案同步翻译
> ——否则会把"文案变了"误判成"功能坏了"。本轮绝大多数调试时间花在这里,
> 也是 106/106 与 7/7 从 0/18、3/7 涨上来的原因。

---

## 5. 已知限制

- **6.x / 7.0–7.7 传统 `_template` 未适配**:模板页只支持组合模板,这些版本
  会看到"接口不可用"。传统模板形状(`template`+`order`)未实证,不凭猜写。
- **Easysearch / OpenSearch**:版本号语义与 ES 不同,矩阵只对 ES 成立;
  `engine` 随矩阵返回,页面文案按 ES 版本表述。
- **新增 MCP 工具若忘补英文**,英文界面回退中文说明(不空白,可发现)。
- 语言/字号跟随依赖宿主 `env` 消息与 theme tokens;若宿主不发 `env`,
  语言在下次打开时生效。

## 6. 如何复现

```bash
npm run build && npx svelte-check --tsconfig ./tsconfig.json
export PATH=/d/software/dev/Go/go/bin:$PATH && (cd backend && go test ./...)
"D:/software/dev/Python/Python313/python.exe" scripts/smoke-test.py --sidecar bin/es-studio-sidecar.exe
# 双语言浏览器回归(两个桥:5210 en + 15px token,5211 zh)
node scripts/browser-bridge.mjs &   # BRIDGE_LOCALE / BRIDGE_TOKENS 见脚本头部
"D:/software/dev/Python/Python313/python.exe" scripts/browser-i18n-test.py --port 5210 --locale en --expect-font 15px
"D:/software/dev/Python/Python313/python.exe" scripts/browser-i18n-test.py --port 5211 --locale zh-CN --expect-font 13px
# 多版本实证(一次一台)
python scripts/version-probe.py --home D:/tmp/es-versions/elasticsearch-6.8.23 --port 19202
```
