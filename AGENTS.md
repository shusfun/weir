# AGENTS.md

Weir：个人使用的代理管理面板，合并 **MiGate**（Gate 设备、Xray / sing-box 内核管理）与 **MiSub**（订阅管理）。**Sub2API** 只作后台布局、认证、安装方式的参考，不作功能来源。

## 先看什么

- `docs/design.html`：业务基线，做什么、怎么交互、验收清单都以它为准。标「目标」的还没实现；上游功能吸收哪些、不要哪些见第 07 节，表里没有的先问用户。
- `docs/implementation.html`：阶段、状态和下一步。
- 用户最新的明确要求优先于文档。文档没写到的业务问题先问用户，不要自己定。

## 结构

- `apps/weir`：`migate` 程序入口，内嵌控制台（`apps/weir/static` 是 `pnpm build` 的产物）
- `apps/console`：控制台（React + TypeScript + Vite，TanStack Query）
- `internal`：中心、Gate、订阅等 Go 包；`internal/e2e` 是容器里跑真实内核的端到端测试
- `packages/ui`：UIAble 组件源码；`packages/contracts`：中心与 Gate 的协议
- `internal/kernels/profiles`：内核版本档案 YAML
- `deploy`：安装脚本、测试用数据服务、e2e 与发布脚本

## 常用命令

```bash
pnpm install --frozen-lockfile          # 安装前端依赖
pnpm dev                                # 控制台开发服务器 127.0.0.1:5173
pnpm check                              # 控制台类型检查
pnpm build                              # 构建控制台到 apps/weir/static
go build ./... && go vet ./...
go test ./...                           # 中心测试默认用 SQLite
docker compose -f deploy/test.compose.yml up -d
MIGATE_CENTER_STORE=postgres go test ./internal/center/   # 用 PostgreSQL + Redis 跑中心测试
python deploy/e2e.py [-run 正则]        # 容器里跑真实内核的 e2e，需要 Docker 与 dist/cores
CGO_ENABLED=0 GOOS=linux go build -o dist/migate ./apps/weir
```

## 本地开发环境

- 只跑一套：控制台 `http://127.0.0.1:5173`（`/api`、`/sub/` 代理到中心）+ 中心 `http://127.0.0.1:19810`（`migate center --serve --config <目录>/config.json`）+ `deploy/test.compose.yml`（PostgreSQL `127.0.0.1:25432`、Redis `127.0.0.1:26379`）。启动前先看端口上有没有在跑，有就复用。
- 开发管理员：`admin@dev.test` / `WeirDev2026`（只用于本机演示数据，不得用于生产）。脚本要走登录表单，直接请求 `/api/v1/auth/login` 会因缺少 `Sec-Fetch-Site` 被拒为 `origin_rejected`。
- 跑容器验收前先停掉开发环境，验收完立即停掉容器；自己起的进程和容器用完即停。

## 代码约定

- 先读代码再改，沿用现有架构、命名和风格。服务端只用 Go；Node.js 只用于构建、上游对照测试和托管的 dsh。
- 上游页面按原有交互移植，繁琐的地方优化，不改写成通用增删改查表单后丢掉原有流程。
- 存储可选 PostgreSQL + Redis 或只用 SQLite。PG / Redis 只用 `migate` schema 和 `migate:` 前缀，不得清空共享实例。
- Xray、sing-box、mihomo 只作为独立二进制调用，不引入它们的 Go 包。中心从上游拉取（镜像回退 + 官方摘要校验）后推给 Gate，Gate 不访问 GitHub；sing-box 用本项目 CI 加 `with_v2ray_api` 编译的版本。配置按版本档案生成，校验交给二进制（`xray run -test`、`sing-box check`、`mihomo -t`）。版本档案严格解析，加字段时同时改解析代码和内置档案。
- 内核使用本项目自己的目录和服务，不碰 x-ui 等其他服务；端口按 Gate 上报的实际占用分配，统计 / API 端口只监听回环。
- 控制台：基础控件用 `packages/ui`；页面处理加载、空数据、失败、无权限四种状态，支持键盘和手机宽度。
- 表单统一用 `Form` / `Field`，校验参照 Element Plus（必填标 *、错误显示在字段下方且预留高度、失焦校验、提交时聚焦第一个错误），服务端字段错误按错误码标到字段。确认与输入用 `useConfirm` / `usePrompt`，不用浏览器原生弹窗；密码输入用 `PasswordInput`，可切换查看；开关 / 勾选 / 单选 / 折叠用 `SwitchField` / `CheckboxField` / `RadioGroup` / `Collapsible`。
- 观测数据缺失时显示「未知」，不能显示为正常或 0。

## 安全底线

- 后端强制校验角色和资源归属；隐藏菜单不能代替鉴权。普通用户只能看到和管理自己的链接、节点、链式代理；管理员可以查看所有用户的配置原文（用户明确要求）。
- Gate 主动连接中心，不开放公网管理接口；配对凭证短时一次性，设备身份可撤销；Agent 只执行允许的业务操作，不提供任意 Shell。
- 用户自建链式代理只允许公网地址，保存前由 Gate 实际连接测试。
- 中心只接受 HTTPS（本机回环直连除外），只信任本机回环反代的转发头。中心地址的记录与切换按 `docs/design.html#center-origin`。
- AI 由托管的 dsh 提供，只支持 DeepSeek，Key 只在 dsh 里填，Weir 不保存。dsh 只能读脱敏状态、提交草稿，确认由 Weir 弹窗、管理员点；dsh 子域名、能力目录、页面感知的规则见 `docs/design.html` 第 06 节。节点密码、UUID、订阅令牌、SOCKS5 密码、私钥不发给模型；安全事件发给 Laya 与 dsh 时不含来源地址、账号和令牌，封禁同样要管理员确认。模型故障不能影响基础功能。
- dsh 以独立系统用户 `migate-dsh` 运行，读不到中心与 Gate 的配置、数据库和密钥，与中心只经 `/var/lib/migate/dsh` 下的 `control`（中心写）和 `status`（dsh 写）交换文件；MCP 只监听回环，令牌每次启动重新生成；安装用 `npm install --ignore-scripts`，切换版本前备份 dsh 会话；补丁不能按 id 改写 dsh 自带的预设，只能新增行、改默认预设。
- 私钥、令牌、真实订阅凭据和服务器登录信息不得写入仓库或文档。

## 文档、提交与部署

- 文档用 HTML，只记与用户确认过的业务和规则，不记过程、证据和运行快照。只改现有文档，不新建重复用途的文档；行为变了同步改文档，错的直接改正。
- 提交只在用户要求时做，不推送；不改写已推送的历史。
- 部署：本机 `CGO_ENABLED=0` 交叉编译 Linux 产物，服务器不编译。动手前先核实机器实际状态；只改本项目的资源，保留现有 1Panel、反代、数据库实例和 x-ui；生产迁移保留现有凭据和订阅令牌，并且可以回退。
