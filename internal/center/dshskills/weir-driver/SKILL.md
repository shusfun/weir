---
name: weir-driver
description: 管理员要支持新的 Xray / sing-box 版本、内核更新后配置报错、或要为内核新增或修改驱动时使用。
---

# 写 Weir 内核驱动

驱动是一份 YAML：某个内核某个版本区间怎么运行、支持什么、建节点的方案，以及怎么生成配置。你只能提交草稿；Weir 检查通过后弹窗给管理员看差异，确认才生效，旧版本保留可退回。

## 步骤

1. `get_console_context` 看管理员在看什么；`list_drivers` 列出现有驱动与它们覆盖的版本、谁在用。
2. `get_kernel_release_notes` 读目标版本与上一版本之间的更新说明。只关心会影响服务端配置的变化：字段改名、删除、新增必填项、默认值变化、协议或传输被移除。
3. `get_driver` 读当前覆盖它的驱动全文与展开结果。
4. 新版本只写变化：新建一个驱动，`extends` 上一版本的驱动，写自己的 `name`、`min_version`（需要时 `max_version`），只覆盖有变化的 `templates` 条目或用 `strip` 删除被移除的字段；同时把上一版本驱动的 `max_version` 收紧（另一步草稿）。没有变化就回答「不需要新驱动」。
5. `submit_draft` 提交 `driver.save`。被拒时按返回的原因改：解析错误、样例配置生成失败、内核检查失败的输出、VPS 实测失败。
6. 以下情况回答「需要 Weir 发版」，不要硬写驱动：全新的协议；需要新的统计方式；需要新的节点链接或订阅格式；引擎没有提供需要的变量。

## 格式

- 顶层字段：`kernel`、`name`、`min_version`、`max_version`、`extends`、`notes`、`protocols`、`networks`、`securities`、`outbound_networks`、`features`、`compat`、`run`、`check`、`presets`、`templates`、`strip`。未知字段直接拒绝。
- `features` 没写或为空表示一项都不支持；`protocols` / `networks` / `securities` 为空表示不限制。
- `outbound_networks` 是作客户端连出去（链式代理、分流出口）时支持的传输，没写时只有 tcp；能连哪些出口协议看模板里有没有 `outbound.<协议>`。保存出口时按入口内核的这两项检查，连不了就报错。
- `run` / `check` 只能写选项名、子命令和一次 `{config}`。
- `compat`：`{kernel, range, protocol, security, reason}`，写已知的客户端不兼容（如某版本 REALITY 与 sing-box 1.14.3 不通）。
- 继承：子驱动的 `templates` 按名称覆盖、`presets` 按 id 覆盖，其他写了的字段整体替换；版本区间只取子驱动自己的。

## 模板

模板是结构化 YAML，入口是 `root`，输出一定是合法 JSON：

- `"${表达式}"` 整个字符串就是一个表达式时保留原类型；否则按文本拼接。
- `key?:` 值为空（null、""、0、false、空列表、空对象）时省略这个键。在 `{…}` 里要写成 `"key?":`。
- `{$if: 条件, $then: 值, $else: 值}`；没有 `$else` 且条件不成立时省略。
- `{$each: 列表, $as: 名称, $do: 值}` 得到列表，放在列表里时展开进外层列表。
- `{$use: 模板名, $with: {…}, $spread: true}` 渲染另一个模板；名称可以带 `${}`。
- `{$merge: [对象, …]}` 合并对象。
- 表达式：路径、字面量、`== != < <= > >=`、`in`、`not`、`and`、`or`、括号；过滤器 `default(x)`、`trim`、`lower`、`int`、`split`（逗号）、`rules`（逗号 / 换行）、`lines`、`cidr`、`join(sep)`、`list`、`map(key)`、`where(key, v)`、`len`、`first(a, b)`、`flat`。

变量：`inbounds`（ib：节点参数 + tag、stats_tag、clients）、`outbounds`（ob：tag、protocol、address、port、username、password、group、s.出口设置：security、network、sni、fingerprint、alpn、public_key、short_id、path、host、service_name、mode（XHTTP 模式）、flow、method 等）、`routes`（r：outbound、balancer、inbound、user、domain、ip、protocol）、`groups`、`stats`（统计端口）、`probe`。凭据只能引用，不能生成。

## 引擎强制检查（写错也绕不过）

统计 / API 只监听回环且用分配的端口；入站只能监听分配给节点的端口；证书只能在托管目录；不能写日志文件；sing-box 的 experimental 只允许 v2ray_api。
