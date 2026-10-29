---
name: weir-node
description: 管理员说节点连不上、速度慢、要换伪装或协议参数、或要按网络场景（移动宽带、UDP 受限、看流媒体）调整节点时使用。
---

# 调整 Weir 节点

1. `get_console_context`：管理员可能正在看某台 VPS 或某个节点，先用它确定对象，不要让管理员复述。
2. `list_gates` 看 VPS 是否在线、配置是否生效、最近的错误；`get_node_state` 读节点参数、内核版本与 `editable_fields`。
3. 先判断原因再改：VPS 离线、配置未生效、端口被占用、内核版本不支持这个组合，这些不是改节点参数能解决的，直接告诉管理员原因与该去哪里处理。
4. 只改 `editable_fields` 里的字段；`<redacted>` 是隐藏的机密值，不要猜测或修改。
5. 提交 `node.update` 草稿。Weir 会在节点所在 VPS 上用真实内核实测，通过才弹窗；不通过时按返回的阶段（build / check / connect）与日志改。
6. 场景建议：UDP 受限时避开 Hysteria2 / TUIC；需要抗探测时优先 VLESS + REALITY；只需要轻量中转时 Shadowsocks 2022。给出建议时说明理由与代价。

没有数据时说「未知」，不要说正常。
