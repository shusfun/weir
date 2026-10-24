# MiGate dsh 插件

MiGate 随程序发布、托管的 dsh 自动加载的插件：

- 新增「MiGate 助手」预设并设为默认：只有 MiGate 的工具、待办与说明，没有 Shell、写文件和联网工具；
- 每段对话开始读取控制台上下文（管理员正在看哪个节点或页面）；
- 加载 `$DSH_HOME/AGENTS.md` 里的 MiGate 说明。

插件只在 `package.json` 的 `migate.dsh` 范围内的 dsh 版本上加载；不兼容时 MiGate 只启用基础预设，MiGate 的工具照常可用。
