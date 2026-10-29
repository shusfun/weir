---
name: weir-ops
description: 管理员要开用户、分节点、建订阅链接，配置出口与分流（如流媒体走家宽），管理订阅源、订阅组、规则模板、手动节点，下载或重启内核，续签证书，或一句话要做好几件事时使用。
---

# 用草稿完成 Weir 的日常操作

## 拼一份草稿

- 一件事拆成几步放进同一份草稿：新建对象的步骤用 `as` 起名，后面的步骤用 `"$名字"` 引用。确认时按顺序执行，任一步失败，能撤回的全部撤回。
- 先用只读工具查清楚现状和编号，再拼参数；`list_actions` 里有每项能力的参数说明。
- 标了「机密」的参数（订阅地址、代理密码、分享链接、节点链接）不要填，管理员在对话里贴了也不要填：留空提交，告诉管理员在确认窗里粘贴。
- 删除类能力是危险操作，只能是最后一步，确认时要当前密码与二次验证码；先说明影响。
- 草稿被拒时，按返回的原因（错误码与说明）修改后重新提交。

## 常见任务

- **开用户并分节点**：`user.create`（node_ids、额度、有效期）→ 需要更多链接时再加 `link.create`。改已有用户用 `user.update`，改链接用 `link.update`。
- **流媒体走家宽**：`list_outbounds` 看已有出口；没有就 `outbound.create`（SOCKS5 / HTTP 代理，密码留给管理员填）→ `route.create`（domain 如 `geosite:netflix`，outbound_tag 填出口标签）。先 `probe_outbound` 实测出口能不能连上。
- **订阅管理**：`list_sources` / `list_profiles` / `list_templates` 看现状；`source.create`（订阅地址留给管理员填）→ `source.refresh` → `profile.create`（source_ids 含 "$名字"）→ `profile.grant`（完整的用户名单）。
- **内核**：`list_kernels` 看已下载的版本，`list_kernel_releases` 看可下载的版本，`kernel.fetch` 下载；内核卡住时 `kernel.restart`（节点会断开几秒）。新版本需要新驱动时用 weir-driver。
- **证书**：`list_certs` 看到期与续签错误，`cert.renew` 立即续签中心签发的证书。

没有数据时说「未知」，不要说正常。
