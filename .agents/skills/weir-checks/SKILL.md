---
name: weir-checks
description: 改完 Weir 的代码或文档后，选出该跑的检查、启动或复用本地开发环境、用 ego-browser 验收页面、在容器里跑真实内核 e2e 时使用。
---

# Weir 改完怎么验证

每处改动都要有一项检查：改坏了这项检查会失败。只在改动确实涉及的范围上加更大的检查，不默认跑全集。只报告实际跑过的命令和结果；没跑的就说没跑，并说明原因。

## 改了什么跑什么

| 改动 | 检查 |
|---|---|
| 控制台页面或组件 | `pnpm check`、`pnpm build`；再按下方「浏览器验收」实际看改过的页面 |
| 某个 Go 包 | `go build ./... && go vet ./...`，再跑这个包的 `go test ./internal/<包>/` |
| 中心存储、迁移、锁或键值 | 再用 PG + Redis 跑一次：`docker compose -f deploy/test.compose.yml up -d` 后 `WEIR_CENTER_STORE=postgres go test ./internal/center/` |
| 内核配置生成、版本档案、Gate 应用流程 | 包测试，再跑 `python deploy/e2e.py -run <相关用例>` |
| 安装、升级、发版脚本 | 见 [deploy/AGENTS.md](../../../deploy/AGENTS.md) 的「验证」 |
| MiSub 迁移的订阅逻辑 | 包测试；与上游原代码的对照测试用 Node v24 跑（会话默认的 Node 可能是 v22，要显式切换 PATH） |
| 只改文档 | 浏览器打开改过的 HTML，确认排版正常；design.html 里验收清单的脚本要能运行 |

## 本地开发环境

只跑一套。启动前先查端口有没有进程，有就复用：

- 控制台：`pnpm dev`，地址 `http://127.0.0.1:5173`，`/api` 和 `/sub/` 代理到中心。
- 中心：`http://127.0.0.1:19810`，启动命令 `weir center --serve --config <目录>/config.json`。中心嵌入的是编译时的 `apps/weir/static`，改了前端要在 19810 上看，就得重新编译中心再重启。
- 数据服务：`deploy/test.compose.yml`，PostgreSQL 在 `127.0.0.1:25432`，Redis 在 `127.0.0.1:26379`。
- 开发管理员：`admin@dev.test` / `WeirDev2026`，只用于本机演示数据，不得用于生产。脚本要走登录表单；直接请求 `/api/v1/auth/login` 会因为缺少 `Sec-Fetch-Site` 被拒，返回 `origin_rejected`。
- 自己起的进程和容器，用完即停。

## 浏览器验收

浏览器一律用 ego-browser，先运行 `ego-browser skill`，照它输出的用法写脚本。

1. 打开改过的页面，检查四种状态：加载、空数据、失败（停掉中心或屏蔽接口）、无权限（用普通用户登录）。
2. 把视口设到 390 宽度，确认没有横向滚动；用 Tab 走一遍，确认焦点顺序合理、焦点环可见。
3. 改了表单时：空表单提交，确认聚焦到第一个错误；再制造一个服务端字段错误，确认错误标在对应字段下方。
4. 改了弹窗、任务进度或会异步出现的内容时，查跳动：在页面里挂 `PerformanceObserver`（`type: "layout-shift"`，只记 `hadRecentInput` 为 false 的条目），再每 250 ms 记一次弹窗与目标按钮的位置；没有人操作期间偏移应为 0、位置不变。
5. 看到问题先修，修完重新验收；最后用 `task.finish({ keep: [] })` 关闭任务空间。

## 容器 e2e

需要 Docker，以及 `dist/cores` 里的内核。先停掉开发环境，再跑 `python deploy/e2e.py [-run 正则]`。跑完立即停掉容器，恢复开发环境。
