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
5. 等 Release 工作流跑完：`gh run list --workflow release.yml --limit 1` 拿到运行 id，再 `gh run watch <id> --exit-status`。确认 `gh release view v<版本>` 里有 `migate-<版本>-linux-amd64.tar.gz` 和 `checksums.txt`。
6. `git pull --ff-only`，取回机器人推进后的 `VERSION`。

工作流失败时，先看日志找原因，不要删掉标签重新打。删除已推送的标签是对外的破坏性操作，必须先问用户。

## 升级服务器

1. 先核实机器的实际状态：`migate version`、`cat /etc/migate/role`、`systemctl list-units 'migate*'`。
2. 用 SSH 执行。SSH 不带终端时必须加 `MIGATE_NONINTERACTIVE=1`，否则脚本会卡在选语言：

   ```sh
   curl -fsSL https://raw.githubusercontent.com/shusfun/weir/main/deploy/install-online.sh | MIGATE_NONINTERACTIVE=1 bash -s -- upgrade -y
   ```

   指定版本时把 `upgrade` 换成 `install -v v<版本>`，回退用 `rollback v<旧版本>`。服务器连 GitHub 慢时多等一会儿，不要中途断开。
3. 核对：`migate version` 显示新版本；对应角色的 `migate-center` / `migate-gate` / `migate-dsh` / `migate-helper` 都是 `active`。
4. 用浏览器打开控制台，确认页面正常、VPS 上显示的程序版本已更新。
5. 只动本项目的资源；1Panel、反代、数据库实例和 x-ui 保持原样。
