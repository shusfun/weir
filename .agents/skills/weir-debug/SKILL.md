---
name: weir-debug
description: 排查 Weir 的任何错误时使用：页面报错、接口失败、dsh 工具调用失败或草稿没生效、VPS 配置不生效或被拒绝、内核起不来或反复重启、周期任务（订阅刷新、证书续签、自动备份）没执行、自更新失败。先查 trace，再按泳道对齐，缺记录先补，最后才复现和修改。
---

# Weir 排查问题

顺序固定：**先查 trace → 按泳道对齐 → 缺记录先补 → 最后复现和修改**。不看记录就读代码猜原因、改代码试，都不允许。

## 1. 先查 trace

trace 是出错那次操作的完整记录。先找到它，拿到编号，读原始参数和原始返回，再下判断：

| 出错的地方 | trace 与编号 |
|---|---|
| 页面报错、接口失败 | 失败请求的响应头 `X-Request-Id`（开发者工具的网络面板；ego-browser 脚本里监听响应读取） |
| dsh 说工具失败、做不了 | dsh 会话文件里那次 `tool/call`（实际传的参数）与 `tool/result`（实际返回）；返回里的「排查编号」 |
| dsh 的草稿没生效、确认失败 | 草稿编号（工具返回与确认窗里都有）；确认请求的 `X-Request-Id` |
| VPS 配置不生效 | 配置版本号（`GET /api/v1/admin/gates` 的 `config_revision`、`applied_revision`） |
| 周期任务没执行 | 任务名（见下方「各泳道的日志」） |
| 自更新失败 | `/var/lib/weir/update/status.json` |

dsh 或别的 agent 转述的现象只是线索：先看 trace 里它实际传了什么、拿到了什么（例如编号传成了字符串 `"1"`），不要照转述去改。

## 2. 按泳道对齐

一次操作会经过几段泳道。用编号把每一段的记录对齐，找出断在哪一段：

| 泳道 | 记什么 | 在哪里看 | 和相邻泳道怎么对上 |
|---|---|---|---|
| 浏览器 | 页面发出的请求与响应 | 开发者工具；ego-browser 脚本 | `X-Request-Id` |
| 中心 HTTP | 出问题的请求 `msg=request id=` | 服务器 `journalctl -u weir-center -o cat`；本地是 `weir center --serve` 的输出（后台运行时 `… 2>&1 \| tee /tmp/weir-center.log`） | `id=` 就是 `X-Request-Id` |
| dsh | 模型每一步的思考、工具调用参数与返回 | `/var/lib/weir/dsh/state/home/sessions/<工作区>/session-*/session.v4.jsonl.zstd`（`zstdcat` 解开，看 `tool/call`、`tool/result`；最新的会话按修改时间取）；dsh 进程本身在 `journalctl -u weir-dsh` | 返回里的「排查编号」 |
| Weir MCP | 失败的工具调用 `msg="mcp tool failed" trace= tool= args= err=`（`args` 只记每个参数的类型） | 中心日志 | `trace=` 就是排查编号 |
| 草稿 | 提交（审计 `ai.draft`，目标含草稿编号）、确认时被拒 `draft rejected at confirm`、执行失败 `draft failed draft= step= action=`、生效（审计 `ai.apply`）；试运行与执行里处理函数的 5xx `draft step handler failed trace=` | 「操作审计」与中心日志 | 草稿编号；试运行的 `trace=` 是那次 MCP 调用的排查编号，确认时的 `trace=` 是确认请求的编号 |
| Gate | 配置下发：中心 `gate rejected revision gate= revision=`；Gate `revision N applied` / `rejected, keeping M` | `journalctl -u weir-gate`（单机的本机 Gate 在 `weir-center`） | 配置版本号 |
| 内核 | 退出与重启 `<内核> exited (<状态>), restarting in <秒>; last output:` | `journalctl -u weir-helper`；VPS 详情的「诊断」（最近 50 行，IP 已遮盖） | 配置版本号与时间 |
| 周期任务 | `msg="task failed" task= err=`（同一原因只记第一次），恢复时 `task recovered` | 中心日志 | 任务名与时间 |
| 自更新 | 进度与失败原因 | `status.json`、`journalctl -u weir-update` | 版本号 |

请求日志只记出问题的请求：5xx 记 ERROR，带真实原因 `err`；400、403、409、422、429 记 WARN，只带错误码；401、404、`setup_required`、`source_blocked` 不记。周期任务名有 `reset-usage`、`refresh-sources`、`auto-backup`、`check-alerts`、`observe-security`、`reclaim-budgets`、`renew-certs`、`prune-samples`、`maintenance-lock`。线上测试面板的登录方式见本机 skill `weir-live-test`。

```bash
journalctl -u weir-center -o cat --since "1 hour ago" | grep -E 'id=<编号>|trace=<编号>'
journalctl -u weir-center -o cat --since today | grep 'level=ERROR'
journalctl -u weir-center -o cat --since today | grep -E 'mcp tool failed|draft (failed|rejected)'
```

### 断点所在的泳道里，按对象往下查

每一步都用页面或接口实际看，不凭代码推断。

| 现象 | 追踪顺序 |
|---|---|
| 页面操作失败 | 请求日志的 `err` → 「操作审计」里同一时间、同一操作者的记录 → 改动涉及的对象（用户、节点、VPS） |
| dsh 工具失败 | 会话里的实际参数 → `mcp tool failed` 的 `args` 与 `err` → 同一 `trace=` 的处理函数 5xx → 工具对应的能力（`capabilities.go`、`capability_list.go`） |
| VPS 显示「失败」或「待生效」 | 卡片上的失败原因 → `config_revision`、`applied_revision` → 中心 `gate rejected revision` → Gate 同一版本号的 `rejected` → 内核「诊断」 |
| VPS 显示「离线」 | Gate 日志里的 `config:`、`report state:` 连接错误 → 中心地址是否切换过（`center switched to`） → 中心请求日志里 `/agent/` 路由的 403、5xx |
| 内核反复重启 | helper 的 `exited … last output` → 「诊断」 → 这次下发的驱动与配置版本 |
| 订阅刷新、证书续签、备份没发生 | `task failed task=<名称>` → 对应设置页的配置 → 外部服务（订阅源、ACME、WebDAV）是否可达 |
| 自更新失败 | `status.json` 的日志 → `journalctl -u weir-update` → 回退后的版本 |

## 3. 缺记录就先补

trace 拿不到编号、某段泳道没有记录、或记录里没有原因，先在那一段补上记录，部署或重跑一次再查，不靠猜，也不先改代码。补的记录和修复一起提交。

- 新的入口（新的协议、新的进程内调用）要有编号，并能和相邻泳道对上：沿用 `requestID()`，进程内调用用 `withTrace` 把编号带下去。
- 中心 handler 返回 5xx 用 `failInternal(c, status, code, err)`，不要用 `failure` 把 `err` 丢掉。
- 周期任务的错误交给 `taskLog.note`。
- 写进日志的错误先过 `redact`（网址只留协议与主机）；内核输出先过 `maskIPs`。参数只记类型或能力名，不记值。
- 不得写入节点密码、UUID、订阅令牌、SOCKS5 密码、私钥、原始订阅地址。

## 4. 最后才复现和修改

- 找到断点和原因后，再写能复现它的检查：后端逻辑用 Go 测试（按 trace 里的原始参数构造输入，例如 dsh 实际传的字符串编号），页面流程用 ego-browser 脚本，内核与 Gate 应用用 `python deploy/e2e.py -run <用例>`。先确认这项检查在修复前失败，修复后通过。怎么选检查见 [weir-checks](../weir-checks/SKILL.md)。
- 改完只报告实际跑过的检查和结果。
