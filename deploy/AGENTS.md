# AGENTS.md：安装、升级与发版

补充根目录 [AGENTS.md](../AGENTS.md)。具体步骤见 [weir-release](../.agents/skills/weir-release/SKILL.md)。

- **正式发布包由 CI 构建**：推送与 `VERSION` 一致的 `v*` 标签后，`release.yml` 构建并发布 GitHub Release，成功后由机器人提交「advance VERSION」。服务器不编译；本机交叉编译（`CGO_ENABLED=0 GOOS=linux`）只用于测试。
- **安装脚本只动本项目的资源**：`/usr/local/bin/weir`、`/usr/local/lib/weir`、`/opt/weir`、`/etc/weir`、`/var/lib/weir` 与 `weir-*` 服务。保留现有的 1Panel、反代、数据库实例和 x-ui。每次安装保留上一版本，启动失败时自动切回。
- **脚本在没有终端时不能提问**：`install-online.sh` 要能真正打开 `/dev/tty` 才算可交互（SSH 不带终端时会自动不提问）；`WEIR_NONINTERACTIVE=1` 强制不提问。
- **两个架构**：发布包 `weir-<版本>-linux-{amd64,arm64}.tar.gz`，`checksums.txt` 列出两者；`install-online.sh` 按 `uname -m` 选包（`WEIR_ARCH` 只用于测试）。
- **自更新**：`install.sh` 为中心装 `weir-update.path` / `weir-update.service`，`/var/lib/weir/update`（2770 root:weir-center）给中心写请求、读进度；`weir-update.service` 不在 `stop_units` 里，否则自更新时会结束安装本身。发布目录保留整个包（含 `install.sh`），切回上一版本时用它重新安装。
- **动手前先核实机器实际状态**。生产迁移要保留现有凭据和订阅令牌，并且可以回退。
- **验证**：`sh -n deploy/install.sh && bash -n deploy/install-online.sh`；在 `deploy/` 下运行 `python version_test.py`、`python install_online_test.py`。改了安装流程时，再在带 systemd 的容器里实际装一次。
