# 适配 MiGate 插件

这是 MiGate 插件的副本，在这里修改不会直接生效。改好后回到 MiGate 控制台「内核 → dsh」，点「提交适配草稿」：
MiGate 会校验文件、用当前 dsh 版本试启动一次，再弹窗请管理员确认差异，确认后才启用。

适配规则：

- 只修改 `package.json`、`cordis.patch.yml`、`README.md`、`ADAPT.md` 这四个文件。
- dsh 升级后插件不兼容时，对照当前 dsh 的组合包与预设写法修改 `cordis.patch.yml`，
  并在 `package.json` 里提高 `version`、更新 `migate.dsh` 兼容范围。
- `cordis.patch.yml` 只能新增行（`insert`）、改写 `agent-preset-registry` 的默认预设；
  不要按 id 改写 dsh 自带的预设，那会替换它的整个配置。
- 不要使用 `!!js`、插件管理、Shell、终端、联网工具。
- MiGate 工具的地址与令牌保持占位符 `{{MIGATE_MCP_URL}}`、`{{MIGATE_MCP_TOKEN}}`。
- 不要用 dsh 的插件管理直接安装本插件。
