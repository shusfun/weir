# AGENTS.md

Weir 是个人使用的代理管理面板：合并 [MiGate](https://github.com/imzyb/MiGate)（Gate 设备、Xray / sing-box 内核管理）与原来的订阅管理（JS 版，已逐文件迁移到 Go）。[Sub2API](https://github.com/Wei-Shaw/sub2api) 只作后台布局、认证、安装方式的参考，不作功能来源。

- 做什么、怎么交互、验收清单以 [docs/design.html](docs/design.html) 为准；标「目标」的还没实现，上游功能吸收哪些见第 07 节，表里没有的先问用户。
- 阶段、状态和下一步见 [docs/implementation.html](docs/implementation.html)。
- 用户最新的明确要求优先于文档；文档没写到的业务问题先问用户，不要自己定。

## 术语

| 说法 | 含义 |
|---|---|
| Weir | 产品名；程序、系统用户、服务、目录、PG schema 与 Redis 前缀都叫 `weir`（上游项目 MiGate 保留原名） |
| 中心 | `weir center`：用户、节点、订阅、审计与内嵌控制台 |
| Gate | `weir gate`：VPS 上的执行端，主动连中心、托管内核；**控制台里叫「VPS」** |
| 单机 | 同一台机器装中心与本机 Gate |
| 节点 | Gate 上的一个入站；授权单位是「用户 → 节点」 |
| 内核 | Xray、sing-box（Gate 上运行）、mihomo（中心检查订阅） |
| dsh | 托管的 DeepSeek Harness，提供 AI 助手 |

## 目录

```
apps/weir/            weir 程序入口；static/ 是 pnpm build 的产物，嵌入程序
apps/console/         控制台（React + TypeScript + Vite，TanStack Query）
packages/ui/          UIAble 组件源码
packages/contracts/   中心与 Gate 的协议
internal/center/      中心：HTTP 接口、能力目录、Gate 同步、dsh 网关、订阅管理附加功能
internal/gate/        Gate Agent：领取配置、应用与回滚、白名单动作
internal/kernels/     内核拉取、校验、缓存与内置驱动（drivers/）
internal/kernelconf/  驱动格式、模板引擎：按驱动把 GateConfig 生成为内核配置，生成后强制检查
internal/xray/        REALITY 密钥与统计接口客户端（Xray / sing-box 共用）
internal/fetch/       可续传、可取消的下载；internal/ghrelease/ 读 GitHub Release
internal/selfupdate/  自更新的 root 一侧（weir self-update）
internal/dsh/         dsh 管理进程（以 weir-dsh 运行）
internal/subgroup/    订阅源编排：订阅源 + 手动节点 → 订阅组
internal/sub/         订阅格式生成器（Clash、Loon 等，从原 JS 版迁移）
internal/subout/      订阅输出格式
internal/store/       ent 数据访问、键值与锁
internal/e2e/         容器里跑真实内核的端到端测试
deploy/               安装与升级脚本、发版打包、测试用数据服务、e2e 驱动
docs/                 design.html、implementation.html
```

## 命令

```bash
pnpm install --frozen-lockfile   # 前端依赖
pnpm dev                         # 控制台开发服务器 127.0.0.1:5173（/api、/sub/ 代理到 19810）
pnpm check                       # 控制台类型检查
pnpm build                       # 构建控制台到 apps/weir/static
go build ./... && go vet ./...
go test ./internal/<包>/          # 中心测试默认用 SQLite
docker compose -f deploy/test.compose.yml up -d
WEIR_CENTER_STORE=postgres go test ./internal/center/   # PG + Redis 跑中心测试
python deploy/e2e.py [-run 正则]  # 容器里跑真实内核，需要 Docker 与 dist/cores
CGO_ENABLED=0 GOOS=linux go build -o dist/weir ./apps/weir
```

## 改完怎么验证

按改动选最小的、能发现回归的检查，不默认跑全集；只报告实际跑过的命令和结果。改了什么跑什么、本地开发环境与浏览器验收见 [weir-checks](.agents/skills/weir-checks/SKILL.md)。

## 排查问题

所有错误先查 trace：找到出错那次操作的完整记录与编号（请求编号、MCP 排查编号、dsh 会话里的工具调用、草稿编号、配置版本号）；再按泳道（浏览器、中心、dsh、Weir MCP、草稿、Gate、内核）把这次操作经过的每一段对齐，找出断在哪一段。trace 或某段泳道没有记录，就先补记录、重跑一次再查，不靠猜，也不先改代码。泳道与在哪里看见 [weir-debug](.agents/skills/weir-debug/SKILL.md)。

## 规则

- **先读代码再改**，沿用现有架构、命名和风格。服务端只用 Go；Node.js 只用于构建、上游对照测试和托管的 dsh。
- **改这些目录前先读它们的 `AGENTS.md`**：`apps/console/`、`internal/kernels/`（也管 `kernelconf`、`fetch`、`ghrelease`）、`internal/dsh/`、`deploy/`。
- **上游页面按原有交互移植**，只优化繁琐的步骤，不改写成通用增删改查表单而丢掉原有流程。
- **存储**可选 PostgreSQL + Redis 或只用 SQLite。PG / Redis 只用 `weir` schema 和 `weir:` 前缀，不得清空共享实例。
- **观测数据缺失显示「未知」**，不能显示为正常或 0。失败要暴露真实原因，不静默降级，不用占位结果冒充成功。
- **注释写代码表达不了的约定与原因**，不复述代码，不写修改过程。

## 安全底线

完整规则见 [design.html 第 08 节](docs/design.html#security)，以下不得违反：

- 后端强制校验角色和资源归属，隐藏菜单不能代替鉴权。普通用户只能看到和管理自己的链接、节点、链式代理；管理员可以查看所有用户的配置原文。
- Gate 主动连接中心，不开放公网管理接口；配对凭证短时一次性，设备身份可撤销；Agent 只执行白名单动作，不提供任意 Shell。
- 中心只接受 HTTPS（本机回环直连除外），只信任本机回环反代的转发头；中心地址的记录与切换按 [design.html#center-origin](docs/design.html#center-origin)。
- 用户自建链式代理只允许公网地址，保存前由 Gate 实际连接测试。
- AI 只提交草稿，由 Weir 弹窗、管理员确认；节点密码、UUID、订阅令牌、SOCKS5 密码、私钥不发给模型；模型故障不能影响基础功能。dsh 的隔离规则见 [internal/dsh/AGENTS.md](internal/dsh/AGENTS.md)。
- 私钥、令牌、真实订阅凭据和服务器登录信息不得写入仓库或文档。

## 文档与提交

- 文档用 HTML，只记与用户确认过的业务和规则，写当前状态，不记过程、证据和运行快照。只改现有文档，不新建重复用途的文档；行为变了同步改，错的直接改正。
- design.html 的「已有 / 目标」以代码为准，发现标错就改正。
- 改了控制台要连同 `apps/weir/static` 的构建产物一起提交。
- 提交、推送、发版只在用户要求时做；不改写已推送的历史。发版与服务器升级按 [weir-release](.agents/skills/weir-release/SKILL.md)。

## 修改本文件

本文件只放每次会话都要遵守的规则，每条一到三行。只在某个目录适用的写进该目录的 `AGENTS.md`，流程写成 `.agents/skills/<名称>/SKILL.md`，业务规则写进 design.html，这里只链接。每个 skill 在 `.claude/skills/` 下建同名软链接；只在本机使用、含凭据的 skill 只放 `.claude/skills/`，并写进 `.git/info/exclude`。
