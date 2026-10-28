# Weir

个人使用的代理管理面板，合并 [MiGate](https://github.com/imzyb/MiGate)（Gate 设备、Xray / sing-box 内核管理）与 [MiSub](https://github.com/imzyb/MiSub)（订阅管理）。中心管理用户、节点和订阅；Gate 在 VPS 上运行内核，代理流量直达 Gate，不经过中心。业务基线见 `docs/design.html`，阶段与进度见 `docs/implementation.html`。

只发布一个程序 `migate`，按安装角色运行：

| 角色 | 用途 |
| --- | --- |
| `standalone` 单机 | 中心 + 本机 Gate，一台 VPS 就能用；中心初始化后本机 Gate 自动配对 |
| `center` 中心 | 只装中心，Gate 装在其他 VPS 上 |
| `gate` | 只装 Gate，由中心配对和下发配置 |

## 安装

```sh
curl -sSL https://raw.githubusercontent.com/shusfun/weir/main/deploy/install-online.sh | bash
```

脚本依次询问语言、角色和中心监听地址（默认 `127.0.0.1:19810`），从 GitHub Release 下载安装包，目标机器不编译。直连 GitHub 低于 100 KiB/s 持续 15 秒时依次改用 `gh-proxy.com`、`ghfast.top`、`ghproxy.net`、`gh.llkk.cc`；校验和始终直连 GitHub 获取，镜像返回的包校验不一致就丢弃。`MIGATE_GITHUB_MIRRORS=off` 关闭镜像，也可以写逗号分隔的主机名替换默认列表。

中心只接受 HTTPS（本机回环直连除外）。保持监听 `127.0.0.1`，在 1Panel 等本机反代里把域名指向这个地址并配好证书，反代需要转发 `Host` 和 `X-Forwarded-Proto`。安装时不问域名，管理员第一次用域名打开控制台时自动记下。

## 初始化

用 `https://域名` 打开控制台进入安装向导：验证初始化凭证 → 选择存储 → 创建管理员。

- 安装结束时打印初始化凭证，30 分钟有效。找不到或过期时执行 `migate` 选 `6`：没过期就照旧显示，过期了自动换新；`7` 强制换新。
- 存储默认只用 SQLite。选 PostgreSQL + Redis 时，中心会探测本机实例；认出 1Panel 装的就填好地址、账号和推荐的空库，密码框留空即可，密码由中心直接读取，不经过浏览器。中心只使用 `migate` schema 和 `migate:` 前缀。
- 已有 Weir 账户的库不能重新初始化：要继续用原来的数据请恢复原配置文件，否则换一个空库。
- 也可以在终端完成：`migate` 菜单 `5`，或 `migate center --setup`。

## 添加 Gate

在控制台的 VPS 页面点「添加 VPS」，把生成的一行命令粘贴到目标 VPS 上执行：它从中心下载同版本程序、安装 Gate 角色并配对。命令里的配对凭证 1 小时有效，只能用一次。Gate 主动连接中心，不开放公网管理接口，也不访问 GitHub；内核和后续版本由中心校验后推送。

## 日常维护

root 在终端执行 `migate` 打开中文菜单：服务状态、启动、停止、重启、重设管理员密码（不需要旧密码，并撤销该管理员的全部登录）、卸载。

```sh
bash install-online.sh upgrade             # 升级到最新正式版
bash install-online.sh install -v v0.1.10   # 安装或切换到指定版本
bash install-online.sh rollback v0.1.9      # 回退
bash install-online.sh list-versions        # 列出 GitHub Release

journalctl -u migate-center -f              # 中心日志；Gate 为 migate-gate，内核辅助程序为 migate-helper
migate version
```

每次安装保留上一个版本，启动失败时自动切回。

## 卸载

`migate` 菜单 `8`，或：

```sh
curl -sSL https://raw.githubusercontent.com/shusfun/weir/main/deploy/install-online.sh | bash -s -- uninstall
```

两种方式都先确认，再问是否同时删除配置和数据，默认保留。没有终端时要写 `-y`，删除数据要写 `--purge`。PostgreSQL 的 `migate` schema、Redis 的 `migate:` 键、数据库实例、1Panel、反代和 x-ui 都不会动。

## 目录

| 路径 | 内容 |
| --- | --- |
| `/opt/migate` | 程序（`releases/<版本>`，`current` 指向当前版本）与 Gate 托管的内核 |
| `/etc/migate` | 配置；`center/setup-token` 是初始化凭证，仅 root 可读 |
| `/var/lib/migate` | 数据：SQLite 数据库、Gate 状态、dsh |
| `/usr/local/lib/migate/install.sh` | 本机安装脚本，升级、回退和卸载都由它执行 |

## 开发

先读 `AGENTS.md`（结构、约定、本地开发环境）。

```sh
pnpm install --frozen-lockfile && pnpm dev   # 控制台 127.0.0.1:5173
pnpm check && pnpm build                     # 类型检查；构建到 apps/weir/static
go build ./... && go vet ./... && go test ./...
CGO_ENABLED=0 GOOS=linux go build -o dist/migate ./apps/weir
```

发布：推送与 `VERSION` 一致的 `v*` 标签，GitHub Actions 构建 linux/amd64 包并创建 Release，随后把 `VERSION` 推进到下一个修订号。
