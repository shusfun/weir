// @weir/ui：基础控件来源 codedthemes/uiable（MIT，见 LICENSE），已移植到 Vite，外观与反馈向 iOS 对齐（规范见 docs/design.html#console-visual）。
// 本地新增：lamp / readout / faceplate / segmented / tabs / overlay / page / toast / form / choice-field / collapsible / password-input / confirm / radio-group，基于 @base-ui/react 1.9。
// 动效部件：check-mark（描绘对勾）/ skeleton / menu-toggle / drawer（底部抽屉）/ burst /
// slider / flip-card / card-stack / carousel；动效变量 --weir-spring / --weir-bounce / --weir-ease / --weir-decel 与各档时长。
// 点击处展开：press-origin 记下按压点，弹窗从那里展开、收回；chip（标签键）、split-button（分体按钮）、
// file-picker（选择 / 拖入文件）；copy-field（只读地址框，末尾复制图标）。按压反馈统一为缩小 + 填充色（theme.css）。
// 任务与对象：job（任务位 JobLine / 任务详情 JobView）、object-link（对象链接、详情窗记在地址栏），只管显示，数据由调用方传入。
// 样式入口：src/styles/tokens.css（设计变量）、theme.css（Tailwind 映射、按压反馈、触屏命中区）、instruments.css（仪表部件）、
// controls.css（开关、分段控件、滚动区）、motion.css（动效部件）。
export * from "./button";
export * from "./input";
export * from "./input-group";
export * from "./card";
export * from "./inset-group";
export * from "./table";
export * from "./badge";
export * from "./label";
export * from "./dialog";
export * from "./select";
export * from "./separator";
export * from "./switch";
export * from "./textarea";
export * from "./progress";
export * from "./checkbox";
export * from "./lamp";
export * from "./readout";
export * from "./faceplate";
export * from "./segmented";
export * from "./tabs";
export * from "./overlay";
export * from "./page";
export * from "./toast";
export * from "./counter";
export * from "./module";
export * from "./notice";
export * from "./form";
export * from "./field";
export * from "./choice-field";
export * from "./collapsible";
export * from "./password-input";
export * from "./confirm";
export * from "./radio-group";
export * from "./copy-button";
export * from "./copy-field";
export * from "./reorder-buttons";
export * from "./scroll-area";
export * from "./sortable-list";
export * from "./display";
export * from "./motion";
export * from "./check-mark";
export * from "./skeleton";
export * from "./menu-toggle";
export * from "./drawer";

export * from "./burst";
export * from "./slider";
export * from "./flip-card";
export * from "./card-stack";
export * from "./carousel";
export * from "./chip";
export * from "./split-button";
export * from "./file-picker";
export * from "./press-origin";
export * from "./job";
export * from "./object-link";
