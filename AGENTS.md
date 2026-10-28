# Weir 开发约定

## 先读什么

- 文档只记录与用户讨论确认过的业务和开发规则，不记过程、证据和运行快照（这些看 Git 历史）。不写「当前线上版本」：线上状态以动手前现场核实为准。
- 用户最新的明确要求优先于这些文档。

## 项目是什么

- 合并两个上游：**MiGate**（Gate 设备、Xray / sing-box 内核管理）和 **MiSub**（订阅管理）。**Sub2API** 只作为框架参考（后台布局、认证、安装方式），不作为功能来源。
- 个人使用项目，不讨论许可证和对外宣传。

## 技术栈与结构

- Monorepo：`apps/console`（React + TypeScript + Vite，TanStack Query）、`apps/weir`（`migate` 程序入口，内嵌控制台）、`internal`（中心、Gate、订阅等 Go 包）、`packages/ui`（UIAble 组件源码）、`packages/contracts`（中心与 Gate 的协议）、`deploy`、`docs`。
- 服务端全部用 Go，Node.js 只用于本地构建和上游对照测试。唯一例外是托管的 dsh（DeepSeek Harness）：它和内核一样是中心下载的独立程序，连同 Node.js 运行环境一起托管，见下文「dsh」。
- 单个 `migate` 二进制，角色分为单机 / 管理端 / Gate；存储可选 PostgreSQL + Redis 或只用 SQLite。PG / Redis 只使用 `migate` schema 和 `migate:` 前缀，不得清空共享实例。
- 基础控件优先使用 `packages/ui` 中的 UIAble 组件，避免另造一套。页面要处理加载、空数据、失败和权限状态，支持键盘操作和移动端。
- 表单统一用 `Form` / `Field`：校验参照 Element Plus（必填标 *、错误显示在字段下方、错误行预留高度不跳动、失焦校验、提交时聚焦第一个错误），服务端字段错误按错误码标到对应字段。确认与输入用 `useConfirm` / `usePrompt`，不用浏览器原生弹窗；密码类输入用 `PasswordInput`，可切换查看；开关 / 勾选用 `SwitchField` / `CheckboxField`，单选用 `RadioGroup`，折叠用 `Collapsible`。

## 内核

- Xray、sing-box、mihomo 一律以**独立二进制**调用，不引入它们的 Go 包，内核更新不需要 Weir 发版。
- 中心从上游拉取二进制（镜像回退 + 官方摘要校验），再推送给 Gate；Gate 不访问 GitHub。sing-box 由 CI 跟随上游标签编译，加上 `with_v2ray_api`。
- 配置按内核版本生成，依据是版本档案 YAML。校验交给二进制：Gate 上用 `xray run -test` / `sing-box check`，中心用 `mihomo -t` / `sing-box check` 检查生成的订阅。
- 内核由本项目自己管理：使用独立目录和 systemd 单元，不碰 x-ui 等其他服务。端口不得与其他服务冲突：Gate 上报本机实际占用的端口，中心分配时避开；内核统计 / API 端口只监听回环地址。

## dsh（AI）

- AI 由托管的 dsh 提供，只支持 DeepSeek；DeepSeek 的 Key 只在 dsh 自己的设置里填写，Weir 不保存。不再接 Claude / GPT。
- 中心下载 Node.js（nodejs.org 官方摘要）与 `@deepseek-ai/dsh`（npm 官方源 sha512），`npm install --ignore-scripts`，不在服务器上编译；版本可切换、回退，切换前备份 dsh 的会话。
- dsh 由独立的管理进程运行（系统用户 `migate-dsh`，systemd 单元 `migate-dsh`），读不到中心与 Gate 的配置、数据库和密钥；中心与它只通过 `/var/lib/migate/dsh/control`（中心写）和 `status`（dsh 写）交换文件。
- dsh 只能通过 Weir 的 MCP 工具读取脱敏状态、提交草稿；确认弹窗由 Weir 自己弹出，dsh 无法确认。MCP 只监听回环，令牌每次启动重新生成。
- 分层：MCP 是底层（中心生成的 `migate-basic` 预设，插件不可用时也在）；Weir 维护的 dsh 插件（`internal/center/dshplugin/`，只用声明式组合，不含脚本）提供「Weir 助手」预设与控制台上下文；dsh 升级后插件不兼容时，用 dsh 的创造模式改工作区里的副本，Weir 校验、试启动后作为草稿请管理员确认。
- 补丁不能按 id 改写 dsh 自带的预设：那会替换整行 `config`。只新增行、改默认预设。

## 权限与安全底线

- 后端强制校验角色和资源归属。普通用户只能看到和管理自己的链接、节点、链式代理；隐藏菜单不能代替鉴权。
- 管理员可以查看所有用户的配置原文（链接、SOCKS5 密码等），这是用户的明确要求。
- Gate 主动连接中心，不开放公网管理接口。配对凭证短时、一次性；设备身份独立、可撤销。Agent 只执行允许的业务操作，不提供任意 Shell。
- 用户自建链式代理只允许公网地址，拒绝本机、内网和云元数据地址；保存前由 Gate 实际连接测试。
- AI 只出草稿，测试通过、管理员看过差异并确认后才生效。节点密码、UUID、订阅令牌、SOCKS5 密码、私钥不发给模型；用户名、邮箱等账号信息可以出现在管理员发起的对话里。模型故障不能影响基础功能。
- 安全防御：规则（限流、失败登录计数）照常运行；Laya 只判断事件是否可疑；可疑事件交给 dsh 分析，dsh 只能提交防御草稿，管理员确认后才封禁。发给 Laya 与 dsh 的事件不含来源地址、账号和令牌。
- 观测数据缺失时显示「未知」，不能显示为正常或 0。
- 私钥、令牌、真实订阅凭据和服务器登录信息不得写入仓库或文档。

## 开发与验证

- 先读代码再改，沿用现有架构、命名和风格。
- 按改动范围运行构建和相关测试；前端改动要在本地实际页面中看一眼。测试用数据服务为 `deploy/test.compose.yml`。
- 本地开发环境：控制台 `http://127.0.0.1:5173`（Vite，`/api`、`/sub/` 代理到中心）；中心 `http://127.0.0.1:19810`（`migate center --serve --config <目录>/config.json`）；数据服务 `deploy/test.compose.yml`（PostgreSQL `127.0.0.1:25432`、Redis `127.0.0.1:26379`）。数据都是演示值，可随时清空重建。
- 开发管理员（用户 2026-10-22 指定，只用于本机一次性数据栈，不得用于生产或共享实例）：`admin@dev.test` / `WeirDev2026`，未开启二次验证。重建数据栈后重新初始化并改回这个密码。自动化脚本要走登录表单：直接请求 `/api/v1/auth/login` 缺少浏览器的 `Sec-Fetch-Site`，会被拒绝为 `origin_rejected`。
- 本地只跑一套环境：开发统一用 5173（Vite）+ 19810（中心）+ `deploy/test.compose.yml`。启动前先看端口上已经在跑什么，有就复用，不要另起临时中心、验收容器或第二个 Vite。Gate / 真实内核必须进容器验收时，先停掉其他环境，验收完立即停掉容器。自己启动的进程和容器用完即停。
- 已取消证据门禁、四种宽度逐图截图、每批双机发版等流程，`deploy/` 下与之相关的脚本已清理，不要再加回来。
- 文档使用 HTML。只修改现有文档，不新建用途重复的文档。行为变化要同步更新相关文档；有错误要直接改正，不保留矛盾的表述。
- 历史代码归档于 `C:/develop/MiGate-backup-20261018-c8250b23/`，不要删除备份或重写 Git 历史。

## 部署

- 本机构建，`CGO_ENABLED=0` 交叉编译 Linux 产物，上传前校验；服务器不编译源码。
- 动手部署前先核实机器的实际状态。只修改本项目的资源，保留现有 1Panel、反代、数据库实例和 x-ui。生产迁移要保留现有凭据和订阅令牌，并且可以回退。
