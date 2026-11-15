# ES Studio

DBX 内的专业 Elasticsearch 管理工作台插件。插件 ID `local.es.studio`，版本 `0.5.0`。

已完成 **P1 · 插件基础设施**、**P2 · 日常运维**、**P3 · 治理与运维**、**P4 · 观测与诊断**、
**P5 · MCP 工具服务** 与 **P6 · 国际化 + 多版本兼容**，共 **25 个导航项**全部可用（无占位按钮）。

> 规格与实施计划见 `docs/00-SPEC.md`、`docs/01-PLAN.md`；各阶段验收结论见
> `docs/P1-ACCEPTANCE.md` … `docs/P6-ACCEPTANCE.md`。

## 支持版本

接口形状与页面渲染在真实发行版上实测过（非推断）：

| ES 版本 | 结果 |
|---|---|
| 6.8.23 | 全页渲染，ILM/组件模板/数据流/组合模板按版本门控并说明原因；传统 `_template` 需 `mappings._doc` |
| 7.0.1 / 7.7.1 | 全页渲染；`_index_template`/组件模板/数据流为 400（7.8/7.9 起才存在），页面显示门控说明 |
| 7.17.29 | 全功能（组合模板/组件模板/数据流/快照） |
| 8.19.23 / 9.5.4 | 全功能，基线回归版本 |

能力矩阵按 API 引入版本判定（ILM 6.6 / 组件模板 7.8 / 数据流 7.9 / PIT 7.10 / ES\|QL 8.11），
低于门限的功能不发送请求，直接显示「此集群为 X，需要 Y 或更高版本」。细节见
`docs/P6-ACCEPTANCE.md` §2、§4。

> **Easysearch / OpenSearch 未做版本语义适配**：两者的版本号规则与 ES 不同，
> 上述矩阵仅对 Elasticsearch 成立。

## 架构

```
DBX Host Bridge (window.dbxPlugin)
        │ invoke("es/*")
        ▼
Go Sidecar  backend/main.go  (stdio-jsonl / Protocol v1)
        │  internal/transport → internal/permission → internal/core/{es,connection}
        ▼
Elasticsearch 7.x / 8.x / 9.x · Easysearch · OpenSearch
```

- 前端在隔离 iframe 内，**不直连 ES**；所有请求经 Sidecar，凭据只在 host lifecycle hydration 时进入后端。
- 凭据字段一律 `json:"-"`，不进响应、事件、错误文本或日志。
- UI 主题只用 `src/styles/tokens.css` 的 `--es-*` 变量映射到宿主注入的 `--dbx-*`；
  **界面语言与字号跟随 DBX**（宿主发 `env` 即全量重渲染，字号由 `--dbx-font-size` 驱动，全项目无 `px` 常量）。
- Go SDK 已 vendor 到 `backend/internal/dbxpluginsdk/`，离线可编译。

## 环境要求

| 工具 | 本机路径 / 版本 |
|---|---|
| Node.js | 22+（本机 22.22.2） |
| `@dbx-app/plugin-cli` | 0.1.10（全局） |
| Go | 1.22.12，`D:/software/dev/Go/go/bin` |
| Python | `D:/software/dev/Python/Python313/python.exe`（脚本用） |
| 本地 ES（可选，集成测试） | `D:/software/dev/elasticsearch/elasticsearch-8.19.23` |

所有缓存、构建产物、ES 数据与日志都落在 D 盘，不写 C 盘大文件。

## 常用命令

```bash
npm install

# 一次性校验：go vet + go test + UI 构建 + 沙箱相对路径检查
GO="D:/software/dev/Go/go/bin/go.exe" node scripts/check-baseline.mjs
npm run check                    # svelte-check，0 error / 0 warning

# 低资源本地 ES 8.19.23（可用内存需 ≥ 3GB；内置 <1GB 自动停止守卫）
"D:/software/dev/Python/Python313/python.exe" scripts/es-cluster.py start   # 后台常驻
"D:/software/dev/Python/Python313/python.exe" scripts/es-cluster.py status
"D:/software/dev/Python/Python313/python.exe" scripts/es-cluster.py stop

# 官方 dev host（请用本项目启动器，原因见下）
npm run dev                     # http://127.0.0.1:5190
npm run dev:check               # 另一个终端：24 项端到端断言

# 协议 smoke（含真实 ES 集成）
"D:/software/dev/Python/Python313/python.exe" scripts/smoke-test.py --es-url http://127.0.0.1:19200
```

## Windows 上的官方 dev host 兼容层

官方 CLI 0.1.10 的 Rust 层在 Windows 会把 `--path` 规范化成**扩展长度路径**
`\\?\D:\...`，并把它作为 `project` / `cwd` / 构建输出路径传给 JS dev runtime。
Node 的 `spawn` 与 `CMD.EXE` 都不接受这种 cwd，导致：

- `Cannot start build command`
- `'vite' 不是内部或外部命令`

因此不要直接跑 `dbx-plugin dev --path .`，改用 `npm run dev`：

- `scripts/dev.mjs`：注入兼容层、把 Go 工具链补进 PATH、定位全局 CLI，然后启动官方 dev host。
- `scripts/dev-host.mjs`：作为 `DBX_PLUGIN_DEV_RUNTIME`，剥离 config 里的 `\\?\` 前缀后委托给**未改动**的官方 `runtime.mjs`。在非 Windows 上是空操作。
- Go 不在 PATH 时会报 `Cannot start build command`；`dev.mjs` 自动探测，也可用 `DBX_GO_BIN_DIR` 指定。

`npm run dev:check` 走的是 dev host 的真实 HTTP 协议（bootstrap → 连接 frame →
iframe document → host bridge → Sidecar → 真实 ES），不是另写的模拟器。

## 打包 / 签名 / 安装

```bash
export PATH="/d/software/dev/Go/go/bin:$PATH"

"D:/software/dev/Python/Python313/python.exe" scripts/check-manifest.py
dbx-plugin package .                     # 产出未签名审核候选包，直接装会被 DBX 拒绝
"D:/software/dev/Python/Python313/python.exe" scripts/sign-package.py \
    --package dist/local.es.studio-0.5.0-windows-x64.dbxp \
    --key-id local.es.studio.release      # 产出 .signed.dbxp 并注册本机公钥

"D:/software/dev/Python/Python313/python.exe" scripts/sign-package.py --verify \
    --package dist/local.es.studio-0.5.0-windows-x64.signed.dbxp
"D:/software/dev/Python/Python313/python.exe" scripts/verify-package.py --package dist/local.es.studio-0.5.0-windows-x64.signed.dbxp
"D:/software/dev/Python/Python313/python.exe" scripts/check-manifest.py \
    --package dist/local.es.studio-0.5.0-windows-x64.signed.dbxp
```

安装 `dist/*.signed.dbxp` 到 DBX 插件中心。签名私钥文件
`.dbx-repository-signing-key.env` **不进包、不进 git**，已由 `.gitignore` 排除。

换一台机器：把源码拷过去，在该机跑 `sign-package.py`（会生成新密钥并写入该机 trust store），
或把公钥 `y/qvmEhlUn128fZCUZQdAH3TwArjkKKIAEGz6Ht/naY=` 加入该机
「插件中心 → 自定义仓库信任」。**任何包内文件改动都必须重新 package + sign**。

## 在 DBX 中打开工作台

DBX 只提供「插件中心 → 设置 → 从 URL 安装」入口（本机无「从文件安装」按钮）；
本机验证时在 `dist/` 起一个临时 HTTP 服务（`python -m http.server`），把
`http://127.0.0.1:19321/local.es.studio-0.5.0-windows-x64.signed.dbxp` 填进 URL 框。
下载后必须先在「已安装」里卸载旧版（同版本号不允许覆盖），再重装。

**打开工作台有两种路径，结果不同：**

1. 从插件中心点「打开」、或顶部「更多」菜单 → 工作台 frame **没有绑定连接上下文**，
   Overview 会一直 `connection is not connected`。这是 DBX 的机制，不是 bug。
2. **正确路径（推荐）**：在左侧连接树「新建连接」→ 搜索并选 `ES Studio (Elasticsearch)`
   → 填 `127.0.0.1:19200`、`http`、认证 `none`、超时 10s → 点 **Test Connection**
   （走标准 `connection/action` RPC，绿字 `ES 8.19.23 at http://127.0.0.1:19200`）
   → 保存 → 在连接树里**点该连接节点**打开工作台，Overview 即显示真实集群指标。

> 连接树里的 `elasticsearch`（`192.168.1.240:9200`，内置驱动，无 `plugin_id`）是
> **DBX 自带**的 Elasticsearch 类型，不是插件名，也不是本项目的测试集群。
> 本插件的连接类型显示为 **ES Studio (Elasticsearch)**，与内置项区分。

> **UI 必须是单文件，且内联 `<script>` 必须位于 `<body>` 末尾 `#app` 之后。**
> `ui/index.html` 内联 CSS + JS，不能有 `./assets/*.js` 外链（DBX 用 `read_plugin_ui_entry`
> 把 HTML 放进 opaque-origin iframe，外链解析不到会整页纯白；内联若落在 `<head>` 同步执行，
> 又会因 `#app` 未解析而只显示占位）。`vite.config.js` 的 `dbx-ui-single-file` 在每次构建
> （含 watch）后自动内联并定位脚本；`check-baseline.mjs` / `verify-package.py` 强制断言。
> 若 HTML 已加载但 JS 未跑，页面显示静态占位「ES Studio / Starting workbench…」而非纯白。

## 已知限制

| 项 | 说明 |
|---|---|
| `go test -race` | 本机未启用 CGO / 无对应 C 工具链，暂未执行；普通 `go test` 与 `go vet` 全绿 |
| dev host 连接列表 | 官方 mock host 的 `host.getConnections()` 恒返回 `Mock Connection / sqlite`；UI 以 `context.connectionId` 为准并补一项占位，真实 DBX 不受影响 |
| 浏览器人工验收 | 已在本机真实 DBX 中代验：URL 安装 → 新建连接 → Test Connection → 连接树打开工作台显示真实指标；dev host `host.getConnections()` 仍恒返回 `Mock Connection / sqlite`，UI 以 `context.connectionId` 兜底，真实 DBX 不受影响 |
| 包目标平台 | `dbx-plugin package` 只产当前平台（本机 windows-x64），且永远是未签名候选包 |


## 目录

```
manifest.json / dbx-plugin.toml / package.json / vite.config.js
backend/main.go                    Sidecar 入口（官方 dev host 要求 backend 根目录为 main 包）
backend/internal/core/             es · connection · permission
backend/internal/transport/        JSON-RPC 适配层
backend/internal/dbxpluginsdk/     vendor 官方 Go SDK
src/                               Svelte 5 + TS 源码 → vite build → ui/
scripts/                           构建、集群、dev host、协议与包内回归、签名
docs/                              规格、计划、阶段验收
```
