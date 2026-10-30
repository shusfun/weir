---
name: weir-ops
description: 管理员要开用户、分节点、建订阅链接与设生效时间、看订阅拉取情况，调整代理页（按类型开关、类型出口、分流到哪一组、自动选择与按速率排序），配置 VPS 上的出口与分流（如流媒体走家宽），管理订阅源、订阅组、规则模板、手动节点，下载或重启内核，续签证书，或一句话要做好几件事时使用。
---

# 用草稿完成 Weir 的日常操作

## 拼一份草稿

- 一件事拆成几步放进同一份草稿：新建对象的步骤用 `as` 起名，后面的步骤用 `"$名字"` 引用。确认时按顺序执行，任一步失败，能撤回的全部撤回。
- 先用只读工具查清楚现状和编号，再拼参数；`list_actions` 里有每项能力的参数说明。
- 标了「机密」的参数（订阅地址、代理密码、分享链接、节点链接）不要填，管理员在对话里贴了也不要填：留空提交，告诉管理员在确认窗里粘贴。
- 删除类和不能撤回的能力只能是最后一步；删除类确认时要当前密码与二次验证码。先说明影响。
- 草稿被拒时，按返回的原因（错误码与说明）修改后重新提交。

## 常见任务

- **开用户并分节点**：`user.create`（node_ids、额度、有效期）→ 需要更多链接时再加 `link.create`。改已有用户用 `user.update`，改链接用 `link.update`。
- **链接的生效时间**：`link.create` / `link.update` 的 `starts_at` / `ends_at`（RFC 3339，都可不填；结束不能晚于用户到期）。`get_user` 里每条链接的 `state` 是 active / not_started / expired。不在时段里拉不到节点，VPS 也停用这条链接的凭据。
- **订阅有没有人在用**：`get_link_stats`（可按用户）看近 24 小时 / 30 天拉取次数、被拒次数、用的什么客户端、最近一次拉取。它不给来源 IP，管理员要看来源时让他打开「订阅管理」里那条链接的「拉取记录」。
- **代理页**（按用户，对他的全部链接生效，客户端下次更新订阅时生效）：先 `get_proxy` 看类型、分流、自动选择与此刻的排序。
  - 某类节点不放进订阅：`proxy.update` 的 `disabled`（类型 key，逗号分隔；给的是完整名单）。
  - 网站走哪一组：`proxy.update` 的 `rule_ai` / `rule_streaming` / `rule_google` / `rule_telegram` / `rule_cn` / `rule_final`，值为 auto / select / direct / reject 或一个类型 key（`rule_final` 不能 reject）。这是写进订阅、由客户端执行的分流；要在 VPS 上把某些网站从别的出口送出去，用下面的「流媒体走家宽」。
  - 自动选择：`proxy.update` 的 `auto_mode`（latency 客户端测延迟 / ranked 按速率排序）、`auto_top`、`refresh_minutes`（最短 30）。
  - 速率来自各节点自己统计的下载流量（近 24 小时有流量的分钟的第 90 百分位）；`rate.minutes` 为 0 是没有数据，说「未知」，不要说成 0 或慢。
  - 给一类节点统一加出口：`proxy.type_exit`（代理密码、分享链接留给管理员填；会逐个节点实测，有一个不通就不保存）；去掉用 `proxy.type_exit_delete`。两者都不能撤回，只能是最后一步。
- **流媒体走家宽（VPS 上分流）**：`list_outbounds` 看已有出口；没有就 `outbound.create`（SOCKS5 / HTTP 代理，密码留给管理员填）→ `route.create`（domain 如 `geosite:netflix`，outbound_tag 填出口标签）。先 `probe_outbound` 实测出口能不能连上。
- **订阅源**：`list_sources` / `list_profiles` / `list_templates` 看现状；`source.create`（订阅地址留给管理员填）→ `source.refresh` → `profile.create`（source_ids 含 "$名字"）→ `profile.grant`（完整的用户名单）。
- **内核**：`list_kernels` 看已下载的版本，`list_kernel_releases` 看可下载的版本，`kernel.fetch` 下载；内核卡住时 `kernel.restart`（节点会断开几秒）。新版本需要新驱动时用 weir-driver。
- **证书**：`list_certs` 看到期与续签错误，`cert.renew` 立即续签中心签发的证书。
- **加一台 VPS**：没有对应的能力。SSH 登录密码与私钥不能经过你，主机指纹要管理员自己核对：请他到「VPS 与节点 → 添加 VPS」用「SSH 一次装好」或「复制安装命令」。

没有数据时说「未知」，不要说正常。
