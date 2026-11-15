# P6 · 国际化与多版本兼容

P5 交付后,验收反馈提出了两个明确的问题:

1. 界面中英文混排(导航英文、正文中文),字号写死 13px,没有跟随 DBX;
2. 只对 8.19 做过真实验证,6.x / 7.x / 9.x 的兼容性未经验证,且 Go 侧已有
   的版本能力矩阵前端一处都没用。

P6 分两批交付,都是修既有缺陷,不新增页面。

---

## P6-a 国际化与字号

### 目标

- 一处文案一个键,中英两套;语言切换跟随 DBX,**不刷新页面**。
- 字号不留任何 `px` 常量,基准令牌跟随宿主。

### 做法

**运行时**(`src/lib/i18n.svelte.ts`)

- `t(key, vars)` 读 `$state` 的当前语言。因为读取发生在模板的 effect 内,
  宿主切换语言时 Svelte 5 会自动重渲染调用方 —— 这是"实时切换"的唯一机制,
  不需要重挂载。
- `App.svelte` 在 `ready()` 后 `setLocale(normalizeLocale(host.locale))`,
  并监听 `dbx-plugin-env`(宿主 runtime 在每次 `env` 消息后派发)重复同步。
- 缺键时回退中文而不是显示空串:一种语言落后时不至于让标签消失,同时键仍在
  代码里可被检索。
- `tOr(key, fallback)`:当文案的**真源在代码之外**时使用(MCP 工具说明来自
  Go registry),英文是叠加层,中文回退到 registry 原文。

**词典**(`src/lib/locales/`)

- `zh-CN.ts` / `en.ts`:1040 个键,两侧键集完全一致(提交前用脚本比对)。
- `tools.en.ts`:45 个 MCP 工具的英文标题与说明,单独一层。理由见文件头:
  中文真源在 Go registry,复制进 `zh-CN.ts` 会变成第二个真源。

**字号**(`src/styles/tokens.css`)

- 原来 `body { font-size: 13px }` + 组件里 294 处 `font-size: NNpx`。
- 现在只有一条刻度链:`--es-fs-base` = 宿主 token(候选名链)→ 13px 兜底;
  `2xs…4xl` 全部 `calc()` 派生。294 处硬编码全部替换为刻度变量。
- 宿主 token 名不在公开文档里,且随宿主版本变过,所以 `src/lib/hostTheme.ts`
  在 boot 与每次 `env` 时扫一遍 `theme.tokens`,命中 `--*font|text*size|scale`
  且值为长度时**直接钉** `--es-fs-base`;没命中就退回 CSS 链。

### 验证

`scripts/browser-i18n-test.py` 逐页点击 25 个导航项,机械断言:

| 断言 | en | zh-CN |
|---|---|---|
| 正文无 CJK 字符 | 0/25 违规 | — |
| `title` / `placeholder` / `aria-label` 无 CJK | 0/25 | — |
| `h1` 全部中文 | — | 25/25 |
| 导航标签全部中文 | — | 25/25 |
| `body` 字号 == 宿主 token(15px / 13px) | 25/25 | 25/25 |

首次跑英文时 MCP 页有 1251 个 CJK 字符(工具标题/说明来自 Go),因此引入
`tools.en.ts` 叠加层,并把页面 `h1` 一并键化后复跑通过。

---

## P6-b 多版本兼容

### 目标

版本差异必须来自实证,不能来自文档推演。做法是先起真集群把响应形状录下来,
再让代码按实证写。

### 探测(`scripts/version-probe.py`)

自管配置、自起进程、按插件真实调用集回放,跑完落 `probe.json`。
6.8 需要外挂 JDK 8(发行包不带 JDK),7.17/9.5 用自带 JDK;一次只起一台。

实测结论:

| 项目 | 6.8.23 | 7.17.29 | 9.5.4 |
|---|---|---|---|
| `_mapping` 布局 | **typed**(`mappings._doc.properties`) | typeless | typeless |
| `_data_stream` | **400**(不存在) | 200 | 200 |
| `_index_template` | **400** | 200 | 200 |
| `_component_template` | **400** | 200 | 200 |
| `_ilm/status` | 200(6.6 起就有) | 200 | 200 |
| `_tasks?group_by=none` | 扁平 `tasks[]` | 扁平 | 扁平 |
| `_cluster/stats` `nodes.fs.total_in_bytes` | 有 | 有 | 有 |
| `_cat/nodes` `node.role`+`master` 列 | 有 | 有 | 有 |
| 写已关闭索引 | 400 `reason=closed` | 同 | 同 |

### 代码改动

1. **能力矩阵修正**(`backend/internal/core/es/capability.go`)

   旧实现把 7.x 标成 `TypedAPI:true` 且 `DataStreams:false`、`ILM:false`,
   8.0 就给了 `ESQL`。现在每个门限都是引入该 API 的版本:
   ILM 6.6 · 组件模板 7.8 · 数据流 7.9 · PIT 7.10 · ES|QL 8.11;
   typed 反过来是 `major < 8`。`Engine` 由 `DetectEngine` 传入,不再在调用点覆盖。
   `version_test.go` 改为按"引入版本"边界列表断言(6.8/7.7/7.8/7.9/7.10/7.17/8.0/8.11/8.19/9.0)。

2. **typed mapping**(`fetchFields`)

   只读 `mappings.properties`,遇到 6.x 时字段下拉框直接为空。现在先看
   `mappings.properties`,没有就逐类型下钻。

3. **静默空表 → 明确"不支持"**

   8 处 `if (isError(res)) return []` 会把 400/405/501(接口不存在)当成
   "集群里没有",用户无法区分。现在 `throwIfUnsupported` 对这三种状态抛
   `err.unsupportedApi` 并带上接口名;404 仍视为合法空结果。

4. **门控前移**(`src/lib/capabilities.ts`)

   `GET /` 每连接缓存一次,得到与 Go 同门限的矩阵。数据流页 / 组件模板页在
   调用前判断,版本不够时显示"此集群为 6.8.23,数据流需要 7.9 或更高"并隐藏
   写入口,而不是先打一个 400 再解释。

5. **失败文案**(`failureText`)

   原来 429 只显示 `HTTP 429`。现在优先取 `body.error.reason`(再退
   `root_cause[0].reason`),仍无解释时对 429 给背压提示。

### 已知限制(明确不做)

- **6.x / 7.0–7.7 的传统 `_template`**:模板页只用组合模板(`_index_template`),
  这些版本会显示"接口不可用"。传统模板是另一套形状(`template` + `order`),
  没有实证前不凭猜写适配。
- **Easysearch / OpenSearch**:版本号语义不同(2.x/3.x),能力矩阵只对
  Elasticsearch 成立,矩阵随 `engine` 一起返回,页面文案按 ES 版本表述。
- **MCP 工具说明**:英文叠加层只覆盖 45 个工具,新增工具若忘记补,英文界面
  会回退成中文说明(不会空白)。

---

## 顺带修掉的构建事故

给数据流页加能力门控后,`caps && !caps.componentTemplates` 被压缩成
`$&&!$`,`vite.config.js` 的单文件内联用的是**字符串**替换:

```js
html.replace(/<\/body>/i, `<script>${code}</script></body>`)
```

字符串替换会把 `$&` 展开成匹配到的 `</body>`,于是脚本中间被塞进 `</body>`,
浏览器报 `Unexpected token '<'`,整个工作台白屏且**没有任何一条与插件相关的
报错**。现在改成函数替换,并断言内联后的脚本与源字节一致
(`inlined script was corrupted during insertion`)。

这类事故只在特定压缩结果下出现,所以断言比注释更可靠。
