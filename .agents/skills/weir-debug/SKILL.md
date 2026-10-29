---
name: weir-debug
description: 排查 Weir 的问题时使用：页面报错、接口失败、VPS 配置不生效或被拒绝、内核起不来或反复重启、周期任务（订阅刷新、证书续签、自动备份）没执行、自更新失败。先看日志，再按对象追踪，最后才复现和修改。
---

# Weir 排查问题

顺序固定：**先看日志 → 再追踪 → 最后复现和修改**。不看日志就读代码猜原因、改代码试，都不允许。

## 1. 先看日志

### 在哪里看

| 环境 | 中心 | Gate |
|---|---|---|
| 本地开发 | `weir center --serve` 进程的终端输出。进程在后台、输出没留下时，重启并把输出写到文件：`… 2>&1 \| tee /tmp/weir-center.log` | 本地一般没有 Gate；容器 e2e 的日志在 `deploy/e2e.py` 的输出里 |
| 服务器 | `journalctl -u weir-center -o cat --since "30 min ago"` | `journalctl -u weir-gate`（Agent）、`journalctl -u weir-helper`（root 辅助程序：内核进程、证书、自更新的执行） |
| 单机 | 同上 | 本机 Gate 跑在 `weir-center` 进程里，Agent 的日志在 `weir-center`；内核退出仍在 `weir-helper` |

dsh 的日志在 `journalctl -u weir-dsh`。自更新的进度与失败原因在 `/var/lib/weir/update/status.json`，安装过程在 `journalctl -u weir-update`。线上测试面板的登录方式见本机 skill `weir-live-test`。

### 日志里有什么

- **出问题的请求**：`msg=request id=<请求编号> method= route= status= code= user= err=`。5xx 记 ERROR，带真实原因 `err`；400、403、409、422、429 记 WARN，只带错误码。401、404、`setup_required`、`source_blocked` 不记。
- **周期任务**：`msg="task failed" task=<名称> err=`，同一原因只记第一次；恢复时记 `msg="task recovered"`。任务名有 `reset-usage`、`refresh-sources`、`auto-backup`、`check-alerts`、`observe-security`、`reclaim-budgets`、`renew-certs`、`prune-samples`、`maintenance-lock`。
- **配置下发**：中心记 `msg="gate rejected revision" gate= revision= applied= err=`；Gate 记 `revision N applied` 或 `revision N rejected, keeping M: <原因>`。
- **内核**：helper 记 `<内核> exited (<退出状态>), restarting in <秒>; last output: <最后一行>`。

### 怎么找到那一行

页面上报错时，打开浏览器开发者工具，找到失败请求，复制响应头 `X-Request-Id`。用 ego-browser 时，在脚本里监听响应并读取这个头。然后：

```bash
journalctl -u weir-center -o cat --since "1 hour ago" | grep 'id=<请求编号>'
journalctl -u weir-center -o cat --since today | grep 'level=ERROR'
journalctl -u weir-center -o cat --since today | grep 'task failed'
```

## 2. 再追踪

日志给出起点后，顺着对象往下查。每一步都用页面或接口实际看，不凭代码推断。

| 现象 | 追踪顺序 |
|---|---|
| 页面操作失败 | 请求日志的 `err` → 「操作审计」里同一时间、同一操作者的记录 → 改动涉及的对象（用户、节点、VPS） |
| VPS 显示「失败」或「待生效」 | 「VPS 与节点」卡片上的失败原因 → `GET /api/v1/admin/gates` 里的 `config_revision`、`applied_revision`（页面不显示版本号） → 中心日志 `gate rejected revision` → Gate 日志里同一版本号的 `rejected` → VPS 详情的内核「诊断」（最近 50 行输出，IP 已遮盖） |
| VPS 显示「离线」 | Gate 日志里的 `config:`、`report state:` 连接错误 → 中心地址是否切换过（`center switched to`） → 中心请求日志里 `/agent/` 路由的 403、5xx |
| 内核反复重启 | helper 日志的 `exited … last output` → VPS 详情的「诊断」 → 这次下发的驱动与配置版本 |
| 订阅刷新、证书续签、备份没发生 | 中心日志 `task failed task=<名称>` → 对应设置页的配置 → 外部服务（订阅源、ACME、WebDAV）是否可达 |
| 自更新失败 | `status.json` 的日志 → `journalctl -u weir-update` → 回退后的版本 |

中心和 Gate 的日志按配置版本号对上：中心的 `revision=` 就是 Gate 的 `revision N`。

## 3. 最后才复现和修改

- 日志与追踪找到原因后，再写能复现它的检查：后端逻辑用 Go 测试，页面流程用 ego-browser 脚本，内核与 Gate 应用用 `python deploy/e2e.py -run <用例>`。先确认这项检查在修复前失败，修复后通过。怎么选检查见 [weir-checks](../weir-checks/SKILL.md)。
- **日志里找不到原因，就先补日志**：在出问题的位置补一行能说明原因的日志，部署或重跑后再看，不靠猜。补的日志和修复一起提交。
- 补日志的规则：
  - 中心 handler 返回 5xx 用 `failInternal(c, status, code, err)`，不要用 `failure` 把 `err` 丢掉。
  - 周期任务的错误交给 `taskLog.note`。
  - 写进日志的错误先过 `redact`（网址只留协议与主机）；内核输出先过 `maskIPs`。
  - 不得写入节点密码、UUID、订阅令牌、SOCKS5 密码、私钥、原始订阅地址。
- 改完只报告实际跑过的检查和结果。
