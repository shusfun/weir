---
name: weir-kernel-driver
description: 改 Weir 的内核驱动（internal/kernels/drivers）、驱动格式（kernelconf/driver.go）、模板引擎（kernelconf/tmpl.go）或模板变量（kernelconf/view.go），或为新的 Xray / sing-box 版本加驱动时使用。
---

# 改 Weir 内核驱动

驱动决定某个内核某个版本区间怎么运行、支持什么、建节点的方案、怎么生成配置。业务规则见 [design.html 第 03 节](../../../docs/design.html#kernels)；dsh 写驱动用的说明是 `internal/center/dshskills/weir-driver/SKILL.md`，格式改了两边一起改。

## 先判断改哪里

| 变化 | 改哪里 |
|---|---|
| 新内核版本删了 / 改名了 / 新增了某个配置字段 | 新驱动 `extends` 上一版本，覆盖对应 `templates` 条目或写 `strip`；收紧上一版本的 `max_version` |
| 已知的客户端不兼容 | 驱动的 `compat` |
| 建节点方案、高级字段 | 驱动的 `presets` |
| 模板需要一个引擎还没提供的值 | `kernelconf/view.go` 加变量，并更新两份 skill 的变量说明 |
| 全新协议、新的统计方式、新的分享链接格式 | Go 代码（`view.go`、`center/presets.go` 的 `newSecret`、`internal/subout` 等）+ 驱动 |
| 安全约束 | `kernelconf/render.go` 的 `enforce`，不要只写在驱动里 |

## 规则

- 格式严格解析：加字段时同时改 `Driver` 结构、`validate`、内置驱动、两份 skill。
- 在 `{…}` 流式映射里，可省略的键要加引号：`{"host?": "${ib.ws_host}"}`；块式写法 `host?:` 不用。
- 模板里的数字键加引号（`"0":`），否则解析报错。
- 生成器只能是 `kernelconf.Generators` 里的名称（`"$gen:名称"`，可以嵌在字符串里，如 `"/$gen:hex12"`）；新生成器在 `center/presets.go` 的 `presetSettings` 实现。
- `run` / `check` 只能是选项名、子命令和一次 `{config}`；辅助程序以 root 启动内核，参数不能带路径或值。
- 统计 / API 只监听回环、只用分配的端口；入站只用分配给节点的端口；证书只在托管目录；不写日志文件。这些由 `enforce` 强制，新增模板时不要依赖「驱动会写对」。

## 验证

1. `go test ./internal/kernelconf/ ./internal/kernels/`。`golden_test.go` 把内置驱动的输出与对照文件逐字比较（对照文件来自改为驱动之前的生成器）。
2. 内置驱动的输出**有意**改变时：确认改动正确，再更新对照文件（`testdata/golden/`），并在提交说明里写清原因。不要为了让测试通过而改对照文件。
3. 加了新的协议组合或模板分支时，在 `golden_test.go` 的场景里加上它，避免以后悄悄变化。
4. 有真实内核时跑 `python deploy/e2e.py -run <相关用例>`；中心是 Linux 时，「内核 → 驱动 → 只检查」会用已下载的内核检查每个方案。
