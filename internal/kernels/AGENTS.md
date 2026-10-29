# AGENTS.md：内核与驱动

补充根目录 [AGENTS.md](../../AGENTS.md)。适用于 `internal/kernels`（拉取、缓存、内置驱动 `drivers/`）、`internal/kernelconf`（驱动格式、模板引擎、生成后的检查）、`internal/fetch`、`internal/ghrelease`。业务规则见 [design.html 第 03 节](../../docs/design.html#kernels)。

- **Xray、sing-box、mihomo 只作为独立二进制调用**，不引入它们的 Go 包。配置的最终校验交给二进制，命令写在驱动的 `check` 里。
- **中心从上游拉取内核**：经 `internal/fetch` 下载（可续传、镜像回退），按 GitHub 发布的官方摘要校验，不一致即丢弃；再推给 Gate。Gate 不访问 GitHub。sing-box 用本项目 CI 加 `with_v2ray_api` 编译的版本（`.github/workflows/sing-box.yml`）。
- **版本之间的差异写进驱动，不写进 Go**。引擎只负责变量（`kernelconf/view.go`）与生成后的强制检查（`kernelconf/render.go` 的 `enforce`）；改驱动或格式时按 [weir-kernel-driver](../../.agents/skills/weir-kernel-driver/SKILL.md)。
- **驱动严格解析**，未知字段直接拒绝。格式加字段时，同一次改动里同时改解析代码（`kernelconf/driver.go`）、内置驱动、dsh 的 `weir-driver` skill。
- **不支持就报错**：驱动不允许的协议、传输、安全层或特性，生成时明确报错，不静默降级。
- **内核用本项目自己的目录和服务**（`/opt/weir/cores`、`/etc/weir/gate/cores`），不碰 x-ui 等其他服务。端口按 Gate 上报的实际占用分配；统计和 API 端口只监听回环（`enforce` 强制）。
- **验证**：`go test ./internal/kernelconf/ ./internal/kernels/`（含新旧对照 `golden_test.go`）；改了驱动或模板时再跑 `python deploy/e2e.py -run <相关用例>`，用真实内核验证。
