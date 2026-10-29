---
name: weir-release
description: 用户要求发版、打标签，或把服务器升级到最新版 / 指定版本 / 回退时使用。
---

# Weir 发版与升级

发版与升级都会对外生效，只在用户明确要求时做。规则见 [deploy/AGENTS.md](../../../deploy/AGENTS.md)。

## 发版

1. 按 [weir-checks](../weir-checks/SKILL.md) 跑完相关检查。改了控制台时，确认 `apps/weir/static` 已重新构建，并且已经提交。
2. `git pull --ff-only` 拉到最新：上次发版后，机器人提交过「advance VERSION」。
3. 提交改动，`git push origin main`。
4. 打标签，标签必须等于 `VERSION` 里的版本：`git tag v$(cat VERSION) && git push origin v$(cat VERSION)`。
5. 等 Release 工作流跑完：`gh run list --workflow release.yml --limit 1` 拿到运行 id，再 `gh run watch <id> --exit-status`。确认 `gh release view v<版本>` 里有 `weir-<版本>-linux-amd64.tar.gz`、`weir-<版本>-linux-arm64.tar.gz` 和 `checksums.txt`。
6. `git pull --ff-only`，取回机器人推进后的 `VERSION`。

工作流失败时，先看日志找原因，不要删掉标签重新打。删除已推送的标签是对外的破坏性操作，必须先问用户。

## 升级服务器

已装 0.1.13 及以后的中心：优先在面板「系统设置 → 版本与更新」里更新（root 的更新服务下载、校验、安装，失败自动切回）；或在服务器上 `weir self-update --version <版本>`。下面是用安装脚本升级（首次安装、或面板不可用时）：

1. 先核实机器的实际状态：`weir version`、`cat /etc/weir/role`、`systemctl list-units 'weir*'`。
2. 用 SSH 执行（没有终端时脚本自动不提问；`WEIR_NONINTERACTIVE=1` 强制不提问）：

   ```sh
   curl -fsSL https://raw.githubusercontent.com/shusfun/weir/main/deploy/install-online.sh | WEIR_NONINTERACTIVE=1 bash -s -- upgrade -y
   ```

   指定版本时把 `upgrade` 换成 `install -v v<版本>`，回退用 `rollback v<旧版本>`。服务器连 GitHub 慢时多等一会儿，不要中途断开。
3. 核对：`weir version` 显示新版本；对应角色的 `weir-center` / `weir-gate` / `weir-dsh` / `weir-helper` 都是 `active`。
4. 用浏览器打开控制台，确认页面正常、VPS 上显示的程序版本已更新。
5. 只动本项目的资源；1Panel、反代、数据库实例和 x-ui 保持原样。
