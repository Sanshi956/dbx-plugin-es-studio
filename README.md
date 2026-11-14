# ES Studio

DBX 内的专业 Elasticsearch 管理工作台插件。插件 ID `local.es.studio`，版本 `0.1.0`。

当前完成 **P1 · 插件基础设施**：官方 Go Sidecar + Svelte 5 工作台骨架 + 真实集群 Overview + 可安装签名包。
P2–P5 的导航项已在 UI 中列出但禁用并标注阶段，不是装饰性按钮。

> 规格与实施计划见 `docs/00-SPEC.md`、`docs/01-PLAN.md`；P1 验收结论见 `docs/P1-ACCEPTANCE.md`。

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
- UI 主题只用 `src/styles/tokens.css` 的 `--es-*` 变量映射到宿主注入的 `--dbx-*`。
- Go SDK 已 vendor 到 `backend/internal/dbxpluginsdk/`，离线可编译。

## 环境要求

| 工具 | 本机路径 / 版本 |
|---|---|
| Node.js | 22+（本机 22.22.2） |
| `@dbx-app/plugin-cli` | 0.1.10（全局） |
| Go | 1.22.12，`D:/software/dev/Go/go/bin` |
| Python | `D:/software/dev/Python/Python313/python.exe`（脚本用） |
| 本地 ES（可选，集成测试） | `D:/software/dev/elasticsearch-8.19.23-windows-x86_64` |

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
    --package dist/local.es.studio-0.1.0-windows-x64.dbxp \
    --key-id local.es.studio.release      # 产出 .signed.dbxp 并注册本机公钥

"D:/software/dev/Python/Python313/python.exe" scripts/sign-package.py --verify \
    --package dist/local.es.studio-0.1.0-windows-x64.signed.dbxp
"D:/software/dev/Python/Python313/python.exe" scripts/verify-package.py
"D:/software/dev/Python/Python313/python.exe" scripts/check-manifest.py \
    --package dist/local.es.studio-0.1.0-windows-x64.signed.dbxp
```

安装 `dist/*.signed.dbxp` 到 DBX 插件中心。签名私钥文件
`.dbx-repository-signing-key.env` **不进包、不进 git**，已由 `.gitignore` 排除。

换一台机器：把源码拷过去，在该机跑 `sign-package.py`（会生成新密钥并写入该机 trust store），
或把公钥 `y/qvmEhlUn128fZCUZQdAH3TwArjkKKIAEGz6Ht/naY=` 加入该机
「插件中心 → 自定义仓库信任」。**任何包内文件改动都必须重新 package + sign**。

## 已知限制

| 项 | 说明 |
|---|---|
| `go test -race` | 本机未启用 CGO / 无对应 C 工具链，暂未执行；普通 `go test` 与 `go vet` 全绿 |
| dev host 连接列表 | 官方 mock host 的 `host.getConnections()` 恒返回 `Mock Connection / sqlite`；UI 以 `context.connectionId` 为准并补一项占位，真实 DBX 不受影响 |
| 浏览器人工验收 | P1 未启动 headless 浏览器（当时可用内存 1.56GB，避免触发 ES 守卫）；自动化已覆盖 frame 注入、资源相对路径与完整 bridge 数据链路 |
| 未实现页面 | 导航中 P2–P5 条目禁用并标注阶段，属计划内范围，非缺失功能 |
| 包目标平台 | `dbx-plugin package` 只产当前平台（本机 windows-x64），且永远是未签名候选包 |

P1 验收通过后才会进入 P2（集群拓扑、节点、分片、索引、文档、Query Studio、REST Console、Analyzer）。

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
