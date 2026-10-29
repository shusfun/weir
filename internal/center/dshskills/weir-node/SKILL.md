---
name: weir-node
description: 管理员要新建节点、推荐协议、节点连不上或速度慢、要换伪装或协议参数、要删节点，或要按网络场景（移动宽带、UDP 受限、看流媒体）调整节点时使用。
---

# 新建与调整 Weir 节点

## 先查清楚

1. `get_console_context`：管理员可能正停在「新建节点」弹窗里，已经选了方案、VPS 或填了端口；接着他填的往下做，不要让他复述。
2. `list_presets` 读可选的协议方案。推荐协议只在这些方案里选，说出方案名，不要提列表里没有的协议。
3. `list_gates` / `get_gate` 看 VPS 是否在线、配置是否生效、公网地址、正在监听的端口；`list_nodes` 看已有节点。

## 新建节点

1. 按场景选方案：需要抗探测、不确定网络时用 REALITY 类方案；UDP 受限时避开 Hysteria2 / TUIC；只需要轻量中转时用 Shadowsocks 2022。说明理由与代价。
2. 端口：不指定时系统自动分配（REALITY 优先 443）。管理员想要某个端口时先 `check_port`。
3. REALITY 伪装目标：不填时由 VPS 实测内置候选站点、取最快的；管理员想先看候选站点时调用 `probe_reality`。
4. 提交 `node.create` 草稿。可以把后续步骤拼进同一份草稿：`node.create`（as: n）→ `user.create` 或 `user.update`（node_ids 含 "$n"）→ `link.create`。
5. 试运行会在 VPS 上用真实内核实测；不通过时按返回的原因改（换伪装目标、换端口、换方案）后重新提交。

## 调整与排查

1. `get_node_state` 读节点参数与 `editable_fields`；`test_node` 实测现在的参数；连不上、断流时读 `get_kernel_diagnostics`。
2. 先判断原因再改：VPS 离线、配置未生效、端口被占用、内核版本不支持这个组合，这些不是改节点参数能解决的，直接告诉管理员原因与该去哪里处理。
3. 只改 `editable_fields` 里的字段；`<redacted>` 是隐藏的机密值，不要猜测或修改。提交 `node.update` 草稿，Weir 实测通过才弹窗。
4. 删节点用 `node.delete`（危险操作，只能是最后一步）；先说明会影响哪些用户。

没有数据时说「未知」，不要说正常。
