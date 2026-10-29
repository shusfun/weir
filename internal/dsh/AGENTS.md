# AGENTS.md：dsh 托管

补充根目录 [AGENTS.md](../../AGENTS.md)。适用于 `internal/dsh`，以及中心里与 dsh 有关的代码（`internal/center/dsh*.go`、`dshplugin/`、`dshskills/`、`capabilities.go`、`capability_ops.go`、`capability_list.go`、`driver_caps.go`）。业务规则见 [design.html 第 06 节](../../docs/design.html#ai)。

- **dsh 以独立系统用户 `weir-dsh` 运行**，读不到中心与 Gate 的配置、数据库和密钥。它与中心只通过 `/var/lib/weir/dsh` 下的 `control`（中心写）和 `status`（dsh 写）交换文件。
- **MCP 只监听回环**，令牌每次启动重新生成，不写进日志。
- **DeepSeek Key 只在 dsh 里填**，只支持 DeepSeek；Weir 不保存、不转发 Key。
- **安装与切换版本**：按官方摘要校验，用 `npm install --ignore-scripts` 安装，切换版本前备份 dsh 会话。
- **dsh 补丁**只能新增行、改默认预设；不能按 id 改写 dsh 自带的预设，那样会替换它的整份配置。MCP 地址、令牌与 skill 目录是占位符，`validatePlugin` 锁定它们。
- **插件随 Weir 发版适配**：dsh 出新版本后测试，在 `dshplugin/package.json` 的 `weir.dsh` 里放宽已验证范围。超出范围时只启用基础预设，后台安全分析暂停（它按工具 id 禁用工具）。
- **写给 dsh 的文字**：`dshInstructions`（`$DSH_HOME/AGENTS.md`）只写每次会话都要遵守的规则；流程写成 `dshskills/<名称>/SKILL.md`；工具说明只写在能力目录里。同一件事不写两处。
- **dsh 只能读脱敏状态、提交草稿**，确认窗由 Weir 弹出、管理员点。新能力登记在 `capability_list.go`（声明式：处理函数 + 参数说明 + 怎么读改前的值），执行时调用与手动操作相同的处理函数；处理函数要在第一次写入前调用 `dryRun`，否则不能作为草稿能力。订阅地址、代理密码、分享链接这类参数标 `Secret`，由管理员在确认窗里填。能力目录、页面感知、子域名隐藏的规则见 design.html 第 06 节对应小节。
- **安全事件**发给 Laya 与 dsh 时不含来源地址、账号和令牌；封禁同样要管理员确认。
