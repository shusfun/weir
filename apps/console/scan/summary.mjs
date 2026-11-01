// 由 report.json 生成 summary.md；也可单独运行：
//   node apps/console/scan/summary.mjs <输出目录>                 重新生成摘要
//   node apps/console/scan/summary.mjs --compare <目录A> <目录B>   比较两次扫描（复测、稳定性）
import { readFile, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

export const checkNames = {
  tokens: "设计变量", type: "字号层级", tap: "点击区域", contrast: "对比度", press: "按压反馈", motion: "动效",
  cls: "布局跳动", frames: "掉帧", overflow: "横向溢出", scroll: "滚动条", keyboard: "键盘", error: "页面错误",
};
// 同一问题：路由 + 状态 + 检查 + 属性 + 实测值 + 元素（不区分宽度与主题）
export const issueKey = (v) => [v.route, v.state, v.check, v.prop, v.actual, v.el?.selector, v.el?.text].join("|");
const comboKey = (v) => [issueKey(v), v.width, v.theme].join("|");
const md = (s) => String(s ?? "").replace(/\|/g, "\\|").replace(/\n/g, " ");
const countBy = (rows, f) => {
  const m = new Map();
  for (const r of rows) m.set(f(r), (m.get(f(r)) || 0) + 1);
  return [...m.entries()].sort((a, b) => b[1] - a[1]);
};

// 没有遍历到或无法判定的部分
export function uncovered(report) {
  const c = report.coverage;
  const out = [
    "需要 VPS 的页面状态：本地中心没有配对的 Gate，VPS 卡片、节点、内核推送、链路图里的 VPS 等状态未覆盖",
    "未登录页面（登录、初始化）、普通用户视角与「无权限」状态未扫描；加载中、接口失败状态未模拟",
    "二级弹层（弹层里再打开的弹层）、删除 / 停用 / 保存等会改数据的按钮及其确认框没有点开",
    "按压反馈与键盘遍历只在最后一个主题（默认浅色）下检查；按压反馈按组件特征去重，同类元素只按第一个",
  ];
  if (c.contrastUnknown) out.push(`对比度：${c.contrastUnknown} 处文字的背景是渐变或图片，无法判定`);
  if (c.occluded) out.push(`点击区域：${c.occluded} 个元素中心被其他元素遮住，未判定`);
  if (c.inlineLinks) out.push(`点击区域：${c.inlineLinks} 个正文行内链接按 WCAG 2.5.8 例外不判`);
  if (c.framesHidden) out.push(`掉帧：${c.framesHidden} 段记录期间浏览器在限流 requestAnimationFrame（窗口被遮挡或不在前台，空闲帧率 ${(report.meta.idleFps || []).join(" / ")} fps），慢帧未判定，只记长任务`);
  if (c.popupsNotOpened.length) out.push(`点了没有打开弹层：${c.popupsNotOpened.join("；")}`);
  if (c.popupsNotClosed.length) out.push(`Esc 关不掉、改为重新加载：${c.popupsNotClosed.join("；")}`);
  if (c.stateLost.length) out.push(`扫描时状态丢失（弹层被关闭或页面刷新）：${c.stateLost.join("；")}`);
  if (c.keyboardCapped.length) out.push(`键盘遍历达到步数上限、未走完一圈（只判 tabindex<0 的元素）：${c.keyboardCapped.join("；")}`);
  return out;
}

export async function writeSummary(dir, report) {
  const v = report.violations;
  const issues = new Map();
  for (const r of v) {
    const k = issueKey(r);
    const hit = issues.get(k);
    if (hit) hit.combos.add(`${r.width}/${r.theme}`);
    else issues.set(k, { ...r, combos: new Set([`${r.width}/${r.theme}`]) });
  }
  const iss = [...issues.values()];
  const L = [];
  const m = report.meta;
  L.push(`# 控制台界面扫描摘要`, "");
  L.push(`- 开始 ${m.started}，结束 ${m.finished || "未结束"}${m.aborted ? `，**中途中断：${md(m.aborted)}**` : ""}`);
  L.push(`- 地址 ${m.base}；宽度 ${m.widths.join(" / ")}；主题 ${m.themes.join(" / ")}；弹层 ${m.popups ? "开" : "关"}；标签页 ${m.tabs ? "开" : "关"}`);
  L.push(`- 检查项 ${m.checks.map((c) => checkNames[c] || c).join("、")}`);
  L.push(`- 扫描状态 ${new Set(report.states.map((s) => s.route + "|" + s.state)).size} 个（含宽度与主题组合 ${report.states.length} 个）；路由 ${m.routes.join(" ")}`);
  if (report.tokens) L.push(`- 设计变量 ${report.tokens.count} 个；字号变量 ${report.tokens.typeTokens} 个；时长 ${report.tokens.durations.filter(Boolean).join("/")} ms；圆角 ${report.tokens.radii.join(" / ")}`);
  L.push("", "## 按检查项", "", "| 检查项 | 违规记录（含宽度 × 主题） | 去重后的问题 | 涉及元素出现次数 |", "|---|---:|---:|---:|");
  for (const [c] of Object.entries(checkNames)) {
    const rows = v.filter((x) => x.check === c);
    L.push(`| ${checkNames[c]}（${c}） | ${rows.length} | ${iss.filter((x) => x.check === c).length} | ${rows.reduce((a, b) => a + (b.count || 1), 0)} |`);
  }
  L.push("", "## 违规最多的组件（去重后的问题数）", "", "| 组件 | 问题数 | 主要检查项 |", "|---|---:|---|");
  for (const [comp, n] of countBy(iss, (x) => x.el?.component || "?").slice(0, 20)) {
    const top = countBy(iss.filter((x) => (x.el?.component || "?") === comp), (x) => checkNames[x.check] || x.check).slice(0, 3).map(([k, c]) => `${k} ${c}`).join("、");
    L.push(`| ${md(comp)} | ${n} | ${top} |`);
  }
  L.push("", "## 违规最多的页面（去重后的问题数）", "", "| 路由 | 问题数 | 主要检查项 |", "|---|---:|---|");
  for (const [route, n] of countBy(iss, (x) => x.route).slice(0, 20)) {
    const top = countBy(iss.filter((x) => x.route === route), (x) => checkNames[x.check] || x.check).slice(0, 4).map(([k, c]) => `${k} ${c}`).join("、");
    L.push(`| ${md(route)} | ${n} | ${top} |`);
  }
  L.push("", "## 字号层级：实际出现的 font-size / line-height 组合（px）", "", "| 组合 | 出现次数 |", "|---|---:|");
  for (const [k, n] of Object.entries(report.typeCombos).sort((a, b) => b[1] - a[1])) L.push(`| ${k} | ${n} |`);
  L.push("", "## 各检查项示例（每项前 8 条，去重后）", "");
  for (const c of Object.keys(checkNames)) {
    const rows = iss.filter((x) => x.check === c);
    if (!rows.length) continue;
    L.push(`### ${checkNames[c]}`, "", "| 路由 | 状态 | 宽度/主题 | 元素 | 属性 | 实测 | 标准 |", "|---|---|---|---|---|---|---|");
    for (const r of rows.slice(0, 8)) L.push(`| ${md(r.route)} | ${md(r.state)} | ${[...r.combos].join(" ")} | ${md(`${r.el?.component} · ${r.el?.role || r.el?.tag} · ${r.el?.text || ""} · \`${r.el?.selector}\``)} | ${md(r.prop)} | ${md(r.actual)} | ${md(r.expected)} |`);
    L.push("");
  }
  L.push("## 未覆盖", "");
  for (const u of uncovered(report)) L.push(`- ${u}`);
  L.push("");
  await writeFile(join(dir, "summary.md"), L.join("\n"), "utf8");
}

// 比较两次扫描：只比较两边都扫过的路由；掉帧、布局跳动受机器负载影响，单独列出
async function compare(a, b) {
  const ra = JSON.parse(await readFile(join(a, "report.json"), "utf8"));
  const rb = JSON.parse(await readFile(join(b, "report.json"), "utf8"));
  const routes = new Set(ra.meta.routes.filter((r) => rb.meta.routes.includes(r)));
  const pick = (r, timing) => new Set(r.violations.filter((x) => routes.has(x.route) && ["frames", "cls"].includes(x.check) === timing).map(comboKey));
  for (const timing of [false, true]) {
    const A = pick(ra, timing), B = pick(rb, timing);
    const onlyA = [...A].filter((k) => !B.has(k)), onlyB = [...B].filter((k) => !A.has(k));
    console.log(`${timing ? "掉帧 / 布局跳动" : "其他检查"}：A ${A.size} 条，B ${B.size} 条，只在 A ${onlyA.length}，只在 B ${onlyB.length}`);
    for (const k of onlyA.slice(0, 15)) console.log("  - 只在 A：" + k);
    for (const k of onlyB.slice(0, 15)) console.log("  + 只在 B：" + k);
  }
}

if (process.argv[1] && fileURLToPath(import.meta.url) === process.argv[1] && typeof globalThis.taskSpace !== "function") {
  const args = process.argv.slice(2);
  if (args[0] === "--compare") await compare(args[1], args[2]);
  else if (args[0]) {
    const report = JSON.parse(await readFile(join(args[0], "report.json"), "utf8"));
    await writeSummary(args[0], report);
    console.log("已生成 " + join(args[0], "summary.md"));
  } else console.log("用法：node summary.mjs <输出目录> | --compare <目录A> <目录B>");
}
