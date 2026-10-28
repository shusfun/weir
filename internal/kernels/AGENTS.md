# AGENTS.md：内核

补充根目录 [AGENTS.md](../../AGENTS.md)。适用于 `internal/kernels`、`internal/kernelconf`、`internal/xray`、`internal/singbox`。业务规则见 [design.html 第 03 节](../../docs/design.html#kernels)。

- **Xray、sing-box、mihomo 只作为独立二进制调用**，不引入它们的 Go 包。配置的最终校验交给二进制：`xray run -test`、`sing-box check`、`mihomo -t`。
- **中心从上游拉取内核**：镜像回退，并按 GitHub 发布的官方摘要校验，不一致即丢弃；再推给 Gate。Gate 不访问 GitHub。sing-box 用本项目 CI 加 `with_v2ray_api` 编译的版本（`.github/workflows/sing-box.yml`）。
- **版本档案严格解析**，未知字段直接拒绝。格式加字段时，同一次改动里同时改解析代码（`kernelconf/profile.go`）和内置档案（`profiles/`）。
- **不支持就报错**：档案不允许的协议、传输、安全层或特性，生成时明确报错，不静默降级。
- **内核用本项目自己的目录和服务**（`/opt/migate/cores`、`/etc/migate/gate/cores`），不碰 x-ui 等其他服务。端口按 Gate 上报的实际占用分配；统计和 API 端口只监听回环。
- **验证**：跑相关包的 `go test`；改了配置生成或档案时，再跑 `python deploy/e2e.py -run <相关用例>`，用真实内核验证。
