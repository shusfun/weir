# MiGate

开源仓库：https://github.com/shusfun/MiSub

中心和 Gate 使用同一份 GitHub Release。目标机器不编译源码。公开仓库安装不需要令牌。

## 安装

和 Sub2API 一样，直接把脚本交给 Bash。有终端时会先选语言，再问中心的监听地址和端口；直接回车使用 `127.0.0.1:19810`。

安装只配置监听地址，不需要填写公共 URL。推荐保持 `127.0.0.1`，由 1Panel 等反代提供 HTTPS；中心按每次请求识别访问域名，订阅 / 分享链接、Cookie 的 `Secure` 与通行密钥都随之生成，因此反代需转发 `Host` 和 `X-Forwarded-Proto`（只信任本机反代的转发头）。跨站提交按浏览器 `Sec-Fetch-Site` / `Origin` 拒绝。监听 `0.0.0.0` 时是明文 HTTP 直连，安装会给出警告。

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

安装包先直连 GitHub 下载；低于 100 KiB/s 持续 15 秒或失败时，依次改用 `gh-proxy.com`、`ghfast.top`、`ghproxy.net`、`gh.llkk.cc`，都不行再慢速直连。`checksums.txt` 始终直连 GitHub 获取，镜像返回的包校验不一致即丢弃；令牌不会发给镜像。`MIGATE_GITHUB_MIRRORS=off` 关闭镜像，也可写逗号分隔的主机名替换默认列表。中途按 Ctrl+C 会清理临时下载目录。

安装完成后可以直接使用二进制：

```sh
migate-control --version
migate-control --setup
systemctl status migate-control
journalctl -u migate-control -f
systemctl restart migate-control
```

`--setup` 在终端里完成数据库、Redis 和管理员创建。不带参数启动时，如果还没安装，只打开安装向导。Gate 使用 `migate-gate version` 和 `migate-gate pair --center <HTTPS 地址> --name <名称>`。

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
