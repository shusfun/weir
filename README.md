# MiGate

开源仓库：https://github.com/shusfun/MiSub

中心和 Gate 使用同一份 GitHub Release。目标机器不编译源码。公开仓库安装不需要令牌。

## 安装

和 Sub2API 一样，直接把脚本交给 Bash。有终端时会先选语言，再问中心的监听地址和端口；直接回车使用 `127.0.0.1:19810`。

```sh
curl -sSL https://raw.githubusercontent.com/shusfun/MiSub/main/deploy/install-online.sh | bash
```

常用命令：

```sh
bash install-online.sh install -v v0.1.1
bash install-online.sh upgrade
bash install-online.sh rollback v0.1.1
bash install-online.sh list-versions
```

没有终端时使用默认监听地址，不再提问。Gate 装好以后，后续版本由中心校验并推送。

## 卸载

交互卸载会先确认。确认后询问是否清除配置和数据，直接回车保留。

```sh
curl -sSL https://raw.githubusercontent.com/shusfun/MiSub/main/deploy/install-online.sh | bash -s -- uninstall
```

没有终端时必须写出 `-y`。清除数据或托管内核必须显式写出：

```sh
bash install-online.sh uninstall control -y
bash install-online.sh uninstall gate --with-cores -y
bash install-online.sh uninstall control --purge -y
```

`--with-cores` 只移除清单中的托管内核。`--purge` 先备份本项目数据，再移除这些数据。数据库实例、数据库账号、1Panel、反代和 x-ui 会保留。默认卸载会去掉安装锁，下次安装重新进入 setup。

管理员也可以在中心的「版本发布」里卸载指定 Gate。旧 Gate 没有对应能力时保持待执行。移除内核或清除数据需要再次确认。
