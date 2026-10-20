# MiGate

中心和 Gate 使用同一份 GitHub Release。目标机器不编译源码。

## 安装

私有仓库先在当前终端设置 `MIGATE_GITHUB_TOKEN`。不要把令牌写进命令历史以外的文件，也不要提交到仓库。

交互安装会询问角色。中心还会询问监听地址和公共 URL，直接回车使用 `127.0.0.1:19810` 与 `http://127.0.0.1:19810`。

```sh
curl -fsSL https://raw.githubusercontent.com/shusfun/MiSub/main/deploy/install-online.sh | sh
```

也可以直接指定：

```sh
curl -fsSL https://raw.githubusercontent.com/shusfun/MiSub/main/deploy/install-online.sh | sh -s -- control 127.0.0.1:19810 https://你的域名
curl -fsSL https://raw.githubusercontent.com/shusfun/MiSub/main/deploy/install-online.sh | sh -s -- gate
curl -fsSL https://raw.githubusercontent.com/shusfun/MiSub/main/deploy/install-online.sh | sh -s -- --version v0.2.0 control
```

没有终端时使用上面的参数，不再提问。Gate 装好以后，后续版本由中心校验并推送，不需要再到 Gate 的机器上更新。

## 卸载

交互卸载会先确认，再选择范围。直接回车保留配置、初始化锁、数据和托管内核。

```sh
curl -fsSL https://raw.githubusercontent.com/shusfun/MiSub/main/deploy/install-online.sh | sh -s -- uninstall
```

没有终端时必须写出 `-y`。清除范围也必须显式写出：

```sh
curl -fsSL https://raw.githubusercontent.com/shusfun/MiSub/main/deploy/install-online.sh | sh -s -- uninstall control -y
curl -fsSL https://raw.githubusercontent.com/shusfun/MiSub/main/deploy/install-online.sh | sh -s -- uninstall gate --with-cores -y
curl -fsSL https://raw.githubusercontent.com/shusfun/MiSub/main/deploy/install-online.sh | sh -s -- uninstall control --purge -y
```

`--with-cores` 只移除清单中的托管内核。`--purge` 先备份本项目数据，再移除这些数据。数据库实例、数据库账号、1Panel、反代和 x-ui 会保留。

管理员也可以在中心的「版本发布」里卸载指定 Gate。旧 Gate 没有对应能力时保持待执行。移除内核或清除数据需要再次确认。
