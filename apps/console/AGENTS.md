# AGENTS.md：控制台

补充根目录 [AGENTS.md](../../AGENTS.md)。

- **基础控件用 `packages/ui`**，不在页面里另写按钮、输入框、弹窗。
- **每个页面处理加载、空数据、失败、无权限四种状态**；支持键盘操作，390 宽度下不出现横向滚动。
- **表单统一用 `Form` / `Field`**，校验参照 Element Plus：必填标 *，错误显示在字段下方并预留高度，失焦时校验，提交时聚焦第一个错误；服务端返回的字段错误按错误码标到对应字段。
- **确认与输入用 `useConfirm` / `usePrompt`**，不用浏览器原生弹窗。密码输入用 `PasswordInput`，可切换查看；开关、勾选、单选、折叠用 `SwitchField` / `CheckboxField` / `RadioGroup` / `Collapsible`。
- **页面、弹窗、向导用 `useConsoleContext` 声明自己的上下文**，供 dsh 读取；表单字段按能力目录脱敏，规则见 [design.html#dsh-context](../../docs/design.html#dsh-context)。hook 要写在所有提前 `return` 之前。
- **不跳动**（[design.html#console-ux](../../docs/design.html#console-ux)）：按钮忙碌用 `busy` / `busyText`，不在文字里写三元；弹窗的提交错误给 `DialogFooter error`，其他表单的错误放在按钮之后；任务用 `JobLine` / `JobView`，不按状态增减行；检查结果放在按钮下方或固定高度区里。
- **对象可跳转**：显示对象名用 `ObjectLink`，列表行加 `dataObject(...)`，详情窗用 `useDialogParam` 记在地址栏；新对象类型同时加进 `lib/objects.ts` 与中心 `objects.go`。提到别的页面给链接，不写纯文字路径。
- **构建产物要提交**：`pnpm build` 输出到 `apps/weir/static`，中心程序嵌入的是编译时那一份；在 19810 上看改动要先重新编译中心。
- **验证**：`pnpm check`、`pnpm build`，再用 ego-browser 打开改过的页面实际看一遍，步骤见 [weir-checks](../../.agents/skills/weir-checks/SKILL.md)。
