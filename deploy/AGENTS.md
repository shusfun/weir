# AGENTS.md：安装、升级与发版

补充根目录 [AGENTS.md](../AGENTS.md)。具体步骤见 [weir-release](../.agents/skills/weir-release/SKILL.md)。

- **正式发布包由 CI 构建**：推送与 `VERSION` 一致的 `v*` 标签后，`release.yml` 构建并发布 GitHub Release，成功后由机器人提交「advance VERSION」。服务器不编译；本机交叉编译（`CGO_ENABLED=0 GOOS=linux`）只用于测试。
- **安装脚本只动本项目的资源**：`/usr/local/bin/migate`、`/usr/local/lib/migate`、`/opt/migate`、`/etc/migate`、`/var/lib/migate` 与 `migate-*` 服务。保留现有的 1Panel、反代、数据库实例和 x-ui。每次安装保留上一版本，启动失败时自动切回。
- **脚本在没有终端时不能提问**。现在 `install-online.sh` 要设 `MIGATE_NONINTERACTIVE=1` 才不会卡在选语言。
- **动手前先核实机器实际状态**。生产迁移要保留现有凭据和订阅令牌，并且可以回退。
- **验证**：`sh -n deploy/install.sh deploy/github-update.sh && bash -n deploy/install-online.sh`；在 `deploy/` 下运行 `python version_test.py`、`python github_update_test.py`、`python install_online_test.py`。改了安装流程时，再在带 systemd 的容器里实际装一次。
