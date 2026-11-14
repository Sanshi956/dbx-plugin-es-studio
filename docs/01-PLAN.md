# 阶段 1 实施计划（P1 · 插件基础设施）

> 依据 `docs/00-SPEC.md`。任务粒度 2–5 分钟，每个任务都带「怎么知道做完了」。
> 纪律：**RED → GREEN → REFACTOR**，测试前写的业务代码一律删除；每个任务完成后做两段式评审
> （① 是否符合本任务定义 ② 可读性 / DRY / YAGNI），Critical 阻断推进。
> 需求 12 章第一阶段 10 项全部映射到下面 T2–T17。

## 前置约束

- 工作目录 `D:/program_code/work_buddy_space/dbx-plugin-es-head`（已清空，仅保留 `.workbuddy/`）。
- `export PATH=/d/software/dev/Go/go/bin:$PATH`，Go 1.22.12；Node 22；Python `D:/software/dev/Python/Python313/python.exe`。
- 所有缓存 / 构建产物放 D 盘：`GOCACHE=D:/tmp/go-build`、`npm_config_cache=D:/tmp/npm-cache`。
- **本阶段不启动真实 ES**（D2：可用内存 1.61 GB < 3 GB）。集成验证用 Go `httptest` + 官方 `dbx-plugin dev`，
  这是需求 13 的单元/异常测试，不是「静态假数据冒充正式功能」；真实 ES 8.19 集成测试在内存达标后补跑并登记。
- 禁止自建 DBX 模拟运行环境：只用官方 `dbx-plugin dev --path . --port 5190`。

---

### 任务 T1：开隔离工作区
- 文件路径：项目根 `.git/`、`docs/00-SPEC.md`
- 要做的：`git init`；配置 `user.name/email`（仅本仓库）；提交基线 `chore: spec baseline`（只含 `docs/00-SPEC.md` 与本计划 + `.gitignore`）；新建并切到分支 `feat/phase-1`。`.gitignore` 至少含 `/dist/ /bin/ /ui/ /node_modules/ /.dbx-dev/ /scripts/__pycache__/ .dbx-repository-signing-key.env *.log`。
- 验证：`git status --short` 干净；`git branch --show-current` = `feat/phase-1`；`git log --oneline -1` 为基线提交。

### 任务 T2：合并官方骨架 + vendor SDK
- 文件路径：`manifest.json`、`dbx-plugin.toml`、`backend/go.mod`、`backend/internal/dbxpluginsdk/{go.mod,sdk.go}`、`package.json`、`vite.config.js`、`tsconfig.json`、`index.html`、`src/main.ts`
- 要做的：
  1. 用 `dbx-plugin create probe --template go` 与 `--template svelte` 生成到临时目录，取其 `manifest.json` / `dbx-plugin.toml` / `package.json` / `vite.config.js` 结构合并，**不照抄 demo 业务代码**。
  2. `dbx-plugin.toml` 同时含 `[backend] language="go" directory="backend" binary="es-studio-sidecar"`、`[package] include=["assets","ui"]`、`[dev] ui_build=["npm","run","build"]`。
  3. 把 `/tmp/dbxsdk/{go.mod,sdk.go}` 复制为 `backend/internal/dbxpluginsdk/`，`backend/go.mod` 用 `replace github.com/t8y2/dbx/plugins/sdk/go/dbx-plugin-sdk => ./internal/dbxpluginsdk`，保证离线编译。
  4. `vite.config.js` 设 `base: "./"`, `build.outDir="ui"`, `emptyOutDir=true`，并保留 `DBX_UI_BUILD_SUCCESS` 的 `closeBundle` 信号。
  5. `tsconfig.json` 开 `strict`, `target ES2022`, `moduleResolution bundler`；`src/main.ts` 为入口。
- 验证：`go list ./...` 无网络请求且成功；`npm install --cache D:/tmp/npm-cache` 成功；`npm run build` 输出 `DBX_UI_BUILD_SUCCESS` 且 `ui/index.html` 内资源引用均为相对路径（grep 不得出现 `src="/assets`）。

### 任务 T3：干净测试基线
- 文件路径：`backend/Makefile` 或 `scripts/verify.sh`（可选）、`scripts/check-baseline.mjs`
- 要做的：建立一条命令同时跑 `go vet ./... && go test ./... && npm run build`；此时 `go test` 应为「no test files」的干净基线。
- 验证：`scripts/check-baseline.mjs` 退出码 0；`go test ./...` 显示 no test files（不是失败）；记录基线输出。

### 任务 T4（RED）：版本与能力检测契约
- 文件路径：`backend/internal/core/es/version_test.go`
- 要做的：写表驱动测试，断言 `ParseVersion("8.19.23")` → major=8、minor=19；`DetectEngine` 对 `distribution`/`version.distribution` 识别 elasticsearch / easysearch / opensearch；`Capabilities(v)` 对 7.x 返回 `typedAPI=true, dataStreams=false`，8.x/9.x 返回 `typedAPI=false, dataStreams=true`；未知版本返回 `ErrUnknownVersion`。
- 验证：`go test ./internal/core/es -run TestVersion` **必须失败**（包不存在 / 未定义）。

### 任务 T5（GREEN）：实现版本与能力
- 文件路径：`backend/internal/core/es/version.go`、`capability.go`
- 要做的：只写让 T4 通过的最小实现；`Capabilities` 用显式能力矩阵结构体，不在业务页面写死版本判断。
- 验证：`go test ./internal/core/es -run TestVersion` 全绿；`go vet ./internal/core/es` 通过。

### 任务 T6（RED）：ES HTTP Client 契约
- 文件路径：`backend/internal/core/es/client_test.go`
- 要做的：用 `httptest.NewServer` 写测试并断言：① 相对路径 `GET /_cluster/health` 原样送达并携带 query；② 绝对路径 / 含 scheme / 含 `..` 的路径被 `ErrUnsafePath` 拒绝；③ Basic / ApiKey / Bearer 三种认证头正确且**不出现在任何错误文本或日志**；④ 超时返回 `ErrTimeout`；⑤ 4xx/5xx 原样返回状态码与 body，不吞错；⑥ 响应体超过上限返回 `ErrResponseTooLarge`。
- 验证：`go test ./internal/core/es -run TestClient` **必须失败**。

### 任务 T7（GREEN）：实现 ES Client
- 文件路径：`backend/internal/core/es/client.go`
- 要做的：`Do(ctx, Request)`，`Request{Method, Path, Query, Body, Headers}`；路径守卫在发请求**之前**；认证头由 `Auth` 结构注入，结构体凭据字段全部 `json:"-"`；`http.Client` 带 Timeout；用 `io.LimitReader` 限响应体积。
- 验证：T6 全绿；`go test -race ./internal/core/es` 通过。

### 任务 T8（RED）：连接解析与会话契约
- 文件路径：`backend/internal/core/connection/manager_test.go`
- 要做的：测试生命周期 payload（`provider`/`connection`/`runtime`）解析：host/port/scheme 优先级、4 种认证、TLS、超时、只读；断言 `Session` 按 `connection.id` 缓存且幂等 connect/disconnect；断言 `json.Marshal(session)` **不含** password/apiKey/token；断言凭据字段不进 `String()`/`Error()`。
- 验证：`go test ./internal/core/connection` **必须失败**。

### 任务 T9（GREEN）：实现连接管理
- 文件路径：`backend/internal/core/connection/manager.go`、`auth.go`
- 要做的：`ParseLifecycle` 宽松解析宿主字段（兼容 `external_config`、`secrets`、`runtime.host/port`）；`Manager` 用 `sync.RWMutex` + `map[string]*Session`；凭据字段 `json:"-"`；`Secret` 只来自 lifecycle hydration，不自行读 DBX 内部文件。
- 验证：T8 全绿；`go test -race ./internal/core/connection` 通过。

### 任务 T10（RED）：权限守卫契约
- 文件路径：`backend/internal/core/permission/guard_test.go`
- 要做的：断言只读连接拒绝 PUT/POST/DELETE、`_cluster/settings` 修改、`_reindex`、`_reroute`；断言 `ValidatePath` 拒绝 `http://`、`//host`、`../`、含 `@` 的用户信息；断言危险操作返回结构化 `Risk{Name,Target,Scope,Reason}` 供 UI 确认；断言允许 GET/HEAD 与安全诊断 API。
- 验证：`go test ./internal/core/permission` **必须失败**。

### 任务 T11（GREEN）：实现权限守卫
- 文件路径：`backend/internal/core/permission/guard.go`
- 要做的：`Check(ctx, conn, req)` 在**服务层**强制只读与路径校验；`DescribeRisk` 返回结构化风险说明；前端只负责展示，不做安全判定。
- 验证：T10 全绿；`go test -race ./internal/core/permission` 通过。

### 任务 T12（RED）：Sidecar 协议契约
- 文件路径：`backend/internal/transport/sidecar_test.go`、`scripts/smoke-test.py`
- 要做的：断言 `plugin/initialize` 返回与 manifest 一致的 id/version/protocolVersion；`es/ping`；`connection/test` 打到 `httptest` 集群并返回真实 version/cluster_name；`connection/connect` / `disconnect` 幂等；未知方法 `-32601`；凭据不出现在任何响应；`smoke-test.py` 覆盖 400/401/403/404/500、超时、无效 JSON、只读拒绝写、并发。
- 验证：`go test ./internal/transport` 与 `python scripts/smoke-test.py --es-url <httptest>` **必须失败**。

### 任务 T13（GREEN）：实现 Sidecar 与生命周期
- 文件路径：`backend/internal/transport/sidecar.go`、`backend/cmd/es-studio-sidecar/main.go`
- 要做的：`main.go` 用官方 `dbxpluginsdk.NewServer(Metadata{ID:"local.es.studio",Version,Capabilities}, handler).Serve()`；handler 分发到 `core`，方法：`es/ping`、`es/request`、`es/batch`、`es/test`、`connection/test|connect|disconnect`；`stdout` 只走协议、日志全到 `stderr`；所有响应复用请求 id。
- 验证：T12 全绿；`go vet ./...`；`smoke-test.py` 全绿；无 secret 泄漏（grep stderr 与响应）。

### 任务 T14：manifest 契约与预检
- 文件路径：`manifest.json`、`assets/plugin.svg`、`assets/connection.svg`、`scripts/check-manifest.py`、`scripts/manifest.schema.json`
- 要做的：`id=local.es.studio`、`name=ES Studio`、`version=0.1.0`、`publisher=xihonglei`；`engines={dbx:">=0.5.68",host_api:"1"}`；`permissions` 最小化：`host.workbench / host.storage / host.connections:read / host.events`；`entrypoints.backend={protocol_versions:[1],transport:"stdio-jsonl",executable:"bin/windows-x64/es-studio-sidecar.exe"}`、`ui={root:"ui",entry:"ui/index.html"}`；`connection-provider` 的 `database_type="elasticsearch"`，字段覆盖需求 5.1.2（名称/环境/协议/主机/端口/认证方式/用户名/密码/APIKey/Token/TLS/超时/描述/只读），密码与 token 字段 `binding:"secret"`；`workbench` contribution；`context-menu` 打开工作台。恢复 `check-manifest.py` 的 schema + 运行时规则预检（含 `menus.group` 白名单）。
- 验证：`python scripts/check-manifest.py` 通过；`dbx-plugin package .` 成功产出 `.dbxp`；包内 manifest 的 executable 与实际 exe 路径一致。

### 任务 T15：Workbench UI 骨架（阶段 1 可见范围）
- 文件路径：`src/main.ts`、`src/App.svelte`、`src/lib/bridge.ts`、`src/lib/api.ts`、`src/lib/stores.ts`、`src/styles/tokens.css`、`src/pages/Overview.svelte`、`src/components/{Nav,ClusterPicker,StateBlock}.svelte`
- 要做的：左侧一级导航按需求第 4 章全量列出（未实现页面显示「P2/P3 待交付」占位，但**不得放无业务逻辑的装饰按钮**——占位项禁用并标注阶段）；顶部集群选择器调 `getConnections()`；`Overview` 展示 P1 已实现的真实指标（集群名、UUID、版本、健康、节点/索引/文档/存储、分片计数），数据全来自 `es/*` 真实调用；加载态 / 错误态 / 空态齐备；主题跟随 `data-dbx-theme`，颜色只用 CSS 变量；`await window.dbxPlugin.ready` 后才启动逻辑；`onContext` 在销毁时 unsubscribe。
- 验证：`npm run build` 通过且产物相对路径；`tsc --noEmit` 无错误；官方 `dbx-plugin dev --path . --port 5190` 打开后 Overview 显示的是 mock ES 返回的真实字段，无硬编码指标。

### 任务 T16：官方 dev host 端到端
- 文件路径：`scripts/mock-es.py`（轻量，仅测试用，内存常驻 <50 MB）、`scripts/smoke-test.py`
- 要做的：mock 只提供 P1 需要的真实 API 形状（`/`、`/_cluster/health`、`/_cluster/stats`、`/_cat/nodes`、`/_cat/indices`、`/_cluster/state`），明确标注「仅用于自动化测试」。用官方 dev host 驱动 Sidecar 跑通「打开工作台 → 选择连接 → 连接测试 → Overview 出数」。
- 验证：协议 smoke 全绿；`dbx-plugin dev` 页面无 JS 错误（浏览器控制台检查）；Overview 指标与 mock 返回值逐字段一致。

### 任务 T17：打包、签名、包内回归
- 文件路径：`scripts/sign-package.py`、`dist/`
- 要做的：`check-manifest.py` → `dbx-plugin package .` → `sign-package.py`（新 key id `local.es.studio.release`，写入本机 trust store）→ `--verify` → `check-manifest.py --package <signed>`；从签名包解出 Sidecar 再跑一遍 smoke-test；核对包内 `ui/index.html` 与本地构建字节一致、5 个文件 checksum 全匹配、无私钥进包。
- 验证：全部退出码 0；`SIGNATURE VALID`；Node `crypto` 独立交叉验签 `VALID`。

### 任务 T18：两段式评审与阶段验收报告
- 文件路径：`docs/P1-ACCEPTANCE.md`
- 要做的：逐条核对需求 12 章第一阶段 10 项 + 14.3 DBX 插件验收；记录测试输出、评审问题与严重度、内存/磁盘占用；登记偏差（真实 ES 集成测试待内存达标补跑）。
- 验证：报告内每一项有「通过 / 未通过 / 待补」结论与证据；`git status` 干净（产物已提交或忽略）；**停下来交用户验收，不擅自进入 P2**。

---

## 阶段闸口（Gate）

进入 P2 前必须同时满足：
1. T1–T18 全部完成且测试全绿；
2. 两段式评审无 Critical；
3. 可安装 `.signed.dbxp` 产出且验签通过；
4. 用户明确验收 P1（D1 分批验收）。
