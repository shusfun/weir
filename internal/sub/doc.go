// Package sub 是 MiSub 订阅引擎（functions/ 下的纯逻辑部分）的 Go 移植，
// 对应上游提交 4952c16fef2423b2881c8aeecc63647526f42bda（MIT）。
//
// 每个文件对应一个上游模块，导出函数与上游导出名一一对应（如 parseNodeList → ParseNodeList），
// 行为以 testdata/upstream 下原样保存的上游 JS 为准，由 Node 对照测试逐字节比较。
// 网络拉取、KV / D1 存储、HTTP 处理与 Cloudflare 运行时不在本包范围内，由中心调用本包。
//
// 数据模型沿用 JS 语义（见 jscompat 包）：代理、策略组、规则等开放结构使用保持属性顺序的
// *jscompat.Object 与 *jscompat.Array，未知字段原样保留，不做白名单过滤；nil 表示 undefined。
//
// 常用入口：
//
//   - 解析订阅内容：ExtractValidNodes、ParseNodeList（sing-box JSON / Clash YAML / Base64 / 链接 / Surge 与 QX 行）
//   - 链接 ↔ Clash 代理：URLToClashProxy、URLsToClashProxies、ConvertClashProxyToURL、ExtractSingboxNodes
//   - 节点处理：ApplyNodeTransformPipeline（过滤 / 重命名 / 去重 / 排序）、RunOperatorChain（算子链）、
//     ApplyFilterRules、ApplyManualNodeName、AddFlagEmoji、PrependNodeName、FixNodeURLEncoding
//   - 内置生成器：TransformBuiltinSubscription（clash / surge / loon / quanx / singbox / egern）
//   - ini 模板：GetBuiltinTemplate（6 个内置模板）、RenderXxxFromIniTemplate、ParseIniTemplate、
//     ApplySmartModelOptimizations、RenderXxxFromTemplateModel、ApplyMihomoChainProxy
//   - 客户端识别：DetermineTargetFormat、IsMetaCore、IsBrowserAgent、GetProcessedUserAgent
//   - DNS：ResolveSafeDNSConfig、BuildSingboxDNSConfig
//
// 与上游有意保留的差异：
//
//   - 正则使用 regexp2 的 ECMAScript 模式实现 JS 语义，额外设置 2 秒匹配超时（防止恶意规则拖垮服务），超时按未匹配处理；
//     .NET 独有的分组语法（如 (?i)、(?>...)）按语法错误处理。
//   - Go 字符串无法保存孤立的 UTF-16 代理项，JS 中会产生孤立代理的极端结果在 Go 中输出为 U+FFFD。
//   - localeCompare 固定使用 en-US 排序规则（与 Cloudflare Workers 默认区域一致），基于 golang.org/x/text/collate，
//     与 V8 内置 ICU 在罕见字符上可能有细微差别。
//   - YAML 解析基于 yaml.v3 语法树并按 js-yaml 4 的类型规则还原标量，不支持 !!binary / !!set / !!omap 等少见标签；
//     YAML 输出只实现上游实际使用的选项（indent=2、lineWidth=-1、noRefs）。
//   - atob 的错误信息取 Node 的文案（出现在 SS2022 校验的提示中），Workers 的文案略有不同。
//   - 上游在少数畸形输入（例如名称或字段不是字符串）上会抛出 TypeError，Go 版本通常按 String() 转换后继续；
//     上游通过原型链读到的内置属性（如 REGION_EMOJI['constructor']）不做模拟。
//   - isExplicitDnsBlock 在上游某些情况下返回 undefined，Go 的 IsExplicitDNSBlock 统一返回布尔值。
//   - new URL / URLSearchParams 为自实现的 WHATWG URL（IDNA 使用 golang.org/x/net/idna），不支持 file: 协议。
//     Node 22 的 URLSearchParams 在“非法百分号转义 + 非 ASCII 字符”时会把字符截断成单字节，Go 端按规范实现，
//     对照测试对这类输入做了排除。
//   - 格式错误的 YAML 在 yaml.v3 与 js-yaml 之间的容错程度不完全相同（随机变异的 Clash 配置中约 1% 结果不同），
//     解析失败时上游与本包都会退回按 Base64 / 链接解析。
package sub
