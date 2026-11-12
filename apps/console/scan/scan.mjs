// 控制台界面扫描：逐个路由、标签页、弹层，在 390×844 / 1280×800、浅色 / 深色下检查排版、反馈与动效，输出到 out/<时间>/。
// 用法：node apps/console/scan/scan.mjs [--routes /system,/gates] [--checks tap,contrast] [--widths 390] [--themes light]
//       [--popups 0] [--tabs 0] [--base http://127.0.0.1:22340]
// 检查项：tokens type tap contrast press motion cls frames overflow scroll keyboard（页面错误总是记录）
import { inEgo, relay, parseArgs, loadConfig, openSpace, ensureLogin, ensureProbe, transient, outRoot, scanDir, stamp, api } from "./lib.mjs";
import { fileURLToPath } from "node:url";
import { join } from "node:path";
import { readFile, writeFile, mkdir, appendFile } from "node:fs/promises";

if (!inEgo) process.exit(await relay(fileURLToPath(import.meta.url), parseArgs(process.argv.slice(2))));

const { writeSummary } = await import(new URL("./summary.mjs?t=" + Date.now(), import.meta.url).href);
const cfg = await loadConfig();
const allChecks = ["tokens", "type", "tap", "contrast", "press", "motion", "cls", "frames", "overflow", "scroll", "keyboard"];
const checks = cfg.checks ? String(cfg.checks).split(",").map((s) => s.trim()).filter((c) => allChecks.includes(c)) : allChecks;
const has = (c) => checks.includes(c);
const base = (cfg.base || "http://127.0.0.1:22340").replace(/\/$/, "");
const widths = (cfg.widths ? String(cfg.widths).split(",") : ["390", "1280"]).map(Number).filter(Boolean);
// 按压与键盘检查会移动焦点、可能关掉非模态弹层，放在最后一个主题跑；默认最后一个是浅色
const themes = cfg.themes ? String(cfg.themes).split(",") : ["dark", "light"];
const doPopups = cfg.popups !== "0" && cfg.popups !== "false";
const doTabs = cfg.tabs !== "0" && cfg.tabs !== "false";
const viewports = { 390: [390, 844], 1280: [1280, 800] };
const outDir = join(outRoot, cfg.out || stamp());
await mkdir(outDir, { recursive: true });
const jsonl = join(outDir, "violations.jsonl");
await writeFile(jsonl, "", "utf8");

// 路由表取自 app.tsx：去掉通配、重定向和带参数的路径
async function routesFromApp() {
  const src = await readFile(join(scanDir, "../src/app.tsx"), "utf8");
  const out = [];
  for (const m of src.matchAll(/<Route\s+path="([^"]+)"\s+element=\{([^\n]*)\}\s*\/>/g)) {
    const [, path, el] = m;
    if (path === "*" || path === "/" || path.includes(":") || /^<Navigate/.test(el.trim())) continue;
    if (!out.includes(path)) out.push(path);
  }
  return out;
}
// Git Bash 会把 /users 改写成 C:/Program Files/Git/users，这里还原；也接受不带斜杠的写法
const normRoute = (r) => {
  r = r.trim().replace(/\\/g, "/").replace(/^[A-Za-z]:\/.*?\/Git(?=\/)/, "");
  return r && !r.startsWith("/") ? "/" + r : r;
};
const routes = cfg.routes ? String(cfg.routes).split(",").map(normRoute).filter(Boolean) : [...(await routesFromApp()), "/p/demo"];

const report = {
  meta: { started: new Date().toISOString(), base, routes, checks, widths, themes, popups: doPopups, tabs: doTabs },
  violations: [],
  typeCombos: {},
  states: [],
  frames: [],
  coverage: { contrastChecked: 0, contrastUnknown: 0, occluded: 0, inlineLinks: 0, tapTargets: 0, gradients: 0, popupsNotOpened: [], popupsNotClosed: [], stateLost: [], keyboardCapped: [], framesHidden: 0 },
  tokens: null,
};
const seenPress = [];
let ctx = { route: "", state: "", width: 0, theme: "" };
async function record(items) {
  if (!items.length) return;
  const rows = items.map((i) => ({ ...ctx, ...i }));
  report.violations.push(...rows);
  await appendFile(jsonl, rows.map((r) => JSON.stringify(r)).join("\n") + "\n", "utf8");
}
const log = (...a) => console.log("[扫描]", ...a);
// ---------- 浏览器控制 ----------
const { task, page } = await openSpace("scan");
const ev = async (fn, arg) => {
  const run = () => (arg === undefined ? page.evaluate(fn) : page.evaluate(fn, arg));
  try {
    await ensureProbe(page);
    return await run();
  } catch (e) {
    if (!transient(e) && !/__weirScan/.test(e.message)) throw e;
    await page.waitForTimeout(1500);
    await ensureProbe(page);
    return await run();
  }
};
const wait = (ms) => page.waitForTimeout(ms);
const mouse = (type, x, y, extra = {}) => page.cdp("Input.dispatchMouseEvent", { type, x, y, button: "left", buttons: type === "mousePressed" ? 1 : 0, clickCount: 1, ...extra });
const tab = async () => {
  await page.cdp("Input.dispatchKeyEvent", { type: "keyDown", key: "Tab", code: "Tab", windowsVirtualKeyCode: 9 });
  await page.cdp("Input.dispatchKeyEvent", { type: "keyUp", key: "Tab", code: "Tab", windowsVirtualKeyCode: 9 });
};
const escape = () => page.keyboard.press("Escape");
async function setWidth(w) {
  const [width, height] = viewports[w] || [w, 800];
  await page.cdp("Emulation.setDeviceMetricsOverride", { width, height, deviceScaleFactor: 1, mobile: false });
}
// 主题：控制台在「跟随系统」时监听 prefers-color-scheme；先把存储的主题设为 system，再模拟系统明暗
// 外观跟着账号保存，登录后会用账号上的主题校正本机；扫描期间账号上也临时设为「跟随系统」，结束后恢复
let savedTheme = null;
let savedAccount = null;
async function prepareTheme() {
  const r = await api(page, "/me/appearance").catch(() => null);
  if (r?.status === 200 && r.data?.saved) {
    savedAccount = r.data.appearance;
    await api(page, "/me/appearance", "PUT", { ...savedAccount, theme: "system" }).catch(() => {});
  }
  savedTheme = await page.evaluate(() => {
    try {
      const v = localStorage.getItem("weir-theme");
      localStorage.setItem("weir-theme", "system");
      return v;
    } catch {
      return null;
    }
  });
}
async function setTheme(t) {
  await page.cdp("Emulation.setEmulatedMedia", { features: [{ name: "prefers-color-scheme", value: t }] });
  const ok = await page.waitForFunction((dark) => document.documentElement.classList.contains("dark") === dark, t === "dark", { timeout: 3000 }).then(() => true, () => false);
  // 没跟上（例如页面不是控制台）就直接切 class
  if (!ok) await page.evaluate((dark) => document.documentElement.classList.toggle("dark", dark), t === "dark");
  await wait(350); // 等主题过渡结束
}
async function restoreBrowser() {
  if (savedAccount) await api(page, "/me/appearance", "PUT", savedAccount).catch(() => {});
  await page.cdp("Emulation.setEmulatedMedia", { features: [] }).catch(() => {});
  await page.cdp("Emulation.clearDeviceMetricsOverride").catch(() => {});
  await page.cdp("Emulation.setFocusEmulationEnabled", { enabled: false }).catch(() => {});
  await page
    .evaluate((v) => {
      try {
        if (v === null) localStorage.removeItem("weir-theme");
        else localStorage.setItem("weir-theme", v);
      } catch {}
    }, savedTheme)
    .catch(() => {});
}
const neutralMouse = () => mouse("mouseMoved", 1, 1).catch(() => {});
const settle = () => ev((t) => window.__weirScan.settle(t), 6000);
const currentUrl = async () => {
  const u = new URL(await page.url());
  return u.pathname + u.search;
};
async function pageErrors() {
  const errs = await ev(() => window.__weirScan.pageErrors()).catch((e) => [{ kind: "script", message: e.message }]);
  if (errs.length) await record(errs.map((e) => ({ check: "error", prop: e.kind, actual: e.message.slice(0, 300), expected: "无页面错误", el: { tag: "", role: "", text: "", selector: "(页面)", component: "(页面)" }, count: 1 })));
}
// 空闲帧率基线：窗口被遮挡或在后台时浏览器限流 requestAnimationFrame，慢帧就不判定
let idleFps = 60;
async function measureIdleFps() {
  idleFps = await page.evaluate(() => new Promise((res) => {
    let n = 0;
    const t0 = performance.now();
    const f = () => (++n, performance.now() - t0 < 1000 ? requestAnimationFrame(f) : res(n));
    requestAnimationFrame(f);
  }));
  report.meta.idleFps = [...(report.meta.idleFps || []), idleFps];
  if (idleFps < 50) log(`空闲帧率只有 ${idleFps} fps（窗口被遮挡或在后台），慢帧不判定，只记长任务`);
}
// 记录一段操作期间的长任务与慢帧
async function framed(phase, fn) {
  if (!has("frames")) return await fn();
  await ev(() => window.__weirScan.framesStart()).catch(() => {});
  const r = await fn();
  await wait(400);
  const f = await ev(() => window.__weirScan.framesStop()).catch(() => null);
  if (f) {
    report.frames.push({ ...ctx, phase, ...f });
    // 帧间隔中位数 > 30ms 说明浏览器在限流（窗口被遮挡、不在前台），慢帧不能算页面的问题
    const throttled = f.hidden || idleFps < 50 || (f.median || 0) > 30;
    if (throttled) report.coverage.framesHidden++;
    const items = [];
    if (f.longtasks.length) items.push({ check: "frames", prop: "长任务 > 50ms", actual: `${f.longtasks.length} 个，最长 ${Math.max(...f.longtasks)}ms（${phase}）`, expected: "无 > 50ms 长任务", el: { tag: "", role: "", text: phase, selector: "(页面)", component: "(页面)" }, count: f.longtasks.length });
    if (f.slow.length && !throttled) items.push({ check: "frames", prop: "帧间隔 > 34ms", actual: `${f.slow.length} 帧，最长 ${Math.max(...f.slow)}ms / 共 ${f.frames} 帧（${phase}）`, expected: "帧间隔 ≤ 34ms", el: { tag: "", role: "", text: phase, selector: "(页面)", component: "(页面)" }, count: f.slow.length });
    await record(items);
  }
  return r;
}
// ---------- 单个状态的检查 ----------
async function checkCls(phase) {
  await ev(() => window.__weirScan.clsStart());
  await wait(1500);
  const c = await ev(() => window.__weirScan.clsStop());
  if (c.value > 0) await record([{ check: "cls", prop: "布局偏移", actual: `${c.value.toFixed(4)}（${phase}）`, expected: "0", el: c.sources[0] || { tag: "", role: "", text: "", selector: "(页面)", component: "(页面)" }, count: 1, sources: c.sources }]);
}

async function checkPress(popupOnly, stateUrl) {
  const cands = await ev((a) => window.__weirScan.pressCandidates(a), { popupOnly, seen: seenPress, max: 20 });
  const items = [];
  const popupsBefore = await ev(() => window.__weirScan.popups().length);
  await ev(() => window.__weirScan.blockClicks(true));
  try {
    for (const c of cands) {
      seenPress.push(c.sig);
      const p = await ev((id) => window.__weirScan.pressPrep(id), c.id);
      if (!p || !p.hit) continue;
      await mouse("mouseMoved", p.x, p.y);
      await wait(260); // 悬停过渡结束后再取按下前的样子
      await ev((id) => window.__weirScan.pressBefore(id), c.id);
      await mouse("mousePressed", p.x, p.y);
      const r = await ev((id) => window.__weirScan.pressSample(id), c.id);
      await mouse("mouseReleased", p.x, p.y);
      await neutralMouse();
      if (r.changed === false) items.push({ check: "press", prop: `按压反馈（${c.kind}）`, actual: "按下前后 transform / opacity / 背景 / 阴影均无变化", expected: "按下时有可见变化", el: p.desc, count: 1 });
      // 按下也可能打开弹层或切换地址（部分控件在 pointerdown 时响应），复原
      for (let i = 0; i < 3 && (await ev(() => window.__weirScan.popups().length)) > popupsBefore; i++) {
        await escape();
        await wait(300);
      }
      if ((await currentUrl()) !== stateUrl) {
        await ev((u) => window.__weirScan.navigate(u), stateUrl);
        await settle();
        if (popupOnly) break; // 弹层已随地址关闭
      }
    }
  } finally {
    await ev(() => window.__weirScan.blockClicks(false)).catch(() => {});
  }
  await record(items);
}

async function checkKeyboard(popupOnly) {
  const cap = popupOnly ? 100 : 150;
  await ev((a) => window.__weirScan.keyStart(a), { popupOnly });
  let prog = { steps: 0, cycled: false, left: false };
  let lefts = 0;
  for (let batch = 0; batch < 20; batch++) {
    for (let i = 0; i < 10; i++) await tab();
    prog = await ev(() => window.__weirScan.keyProgress());
    if (prog.left) lefts++;
    if (prog.cycled || prog.gone || lefts >= 2 || prog.steps >= cap) break;
  }
  const capped = !prog.cycled && !prog.gone && lefts < 2;
  if (capped) report.coverage.keyboardCapped.push(`${ctx.route} ${ctx.state} ${ctx.width}`);
  const r = await ev((a) => window.__weirScan.keyFinish(a), { capped, gone: !!prog.gone });
  await record(r.items);
}

// 一个状态在一个宽度下：每个主题跑静态检查；与颜色无关的交互检查只在第一个主题跑
async function scanCombos(stateUrl, popupOnly, keyboard = true) {
  for (const [ti, theme] of themes.entries()) {
    ctx.theme = theme;
    await setTheme(theme);
    await neutralMouse();
    if (has("cls")) await checkCls(popupOnly ? "弹层打开后静置" : "页面打开后静置");
    const r = await ev((a) => window.__weirScan.runStatic(a), { checks, popupOnly, narrow: ctx.width < 600 });
    if (r.missing) {
      report.coverage.stateLost.push(`${ctx.route} ${ctx.state} ${ctx.width} ${theme}`);
      return false;
    }
    if (!report.tokens) report.tokens = { count: r.tokenCount, typeTokens: r.typeTokens, durations: r.durations, radii: r.radii };
    for (const [k, v] of Object.entries(r.combos)) report.typeCombos[k] = (report.typeCombos[k] || 0) + v;
    for (const k of ["contrastChecked", "contrastUnknown", "occluded", "inlineLinks", "tapTargets", "gradients"]) report.coverage[k] += r.stats[k] || 0;
    await record(r.items);
    if (ti === themes.length - 1) {
      if (has("press")) await checkPress(popupOnly, stateUrl);
      if (has("keyboard") && keyboard && (await ev((p) => !!window.__weirScan.scopeRoot(p), popupOnly))) await checkKeyboard(popupOnly);
    }
    await ev(() => window.__weirScan.resetScroll()).catch(() => {});
    await pageErrors();
    report.states.push({ ...ctx });
  }
  return true;
}
// ---------- 路由、标签页、弹层 ----------
async function openUrl(url, full) {
  await framed("切换页面", async () => {
    if (full) {
      await page.goto(base + url);
      await ensureProbe(page);
    } else await ev((u) => window.__weirScan.navigate(u), url);
    await settle();
  });
  // 中心重启后会话失效会回到登录表单：重新登录再打开
  if (!url.startsWith("/p/") && (await page.evaluate(() => !!document.querySelector('input[autocomplete~="username"]')))) {
    log("会话失效，重新登录");
    await ensureLogin(page, base, cfg.email || "admin@dev.test", cfg.password || "WeirDev2026");
    await page.goto(base + url);
    await ensureProbe(page);
    await settle();
  }
}
// 点击打了标记的元素：先滚到视口中间（滚动区里的按钮 page.click 可能判为不在视口），失败再在页面里直接点击
async function clickMarked(sel, label) {
  await ev((sel) => document.querySelector(sel)?.scrollIntoView({ block: "center", inline: "center" }), sel);
  try {
    await page.click(sel, { label });
  } catch (e) {
    if (!/not visible|timed out/.test(e.message)) throw e;
    await ev((sel) => document.querySelector(sel)?.click(), sel);
  }
}
// 主内容里第一个标签栏的各个标签（第 0 个就是默认状态）
const listTabs = () =>
  ev(() => {
    const main = document.querySelector(".shell-main") || document.body;
    const list = main.querySelector("[role=tablist]");
    if (!list) return [];
    return [...list.querySelectorAll("[role=tab]")].map((t, i) => ({ i, text: (t.innerText || t.getAttribute("aria-label") || "").trim().slice(0, 20) }));
  });
async function clickTab(i) {
  await ev((i) => {
    const main = document.querySelector(".shell-main") || document.body;
    main.querySelector("[role=tablist]").querySelectorAll("[role=tab]")[i].setAttribute("data-weir-tab", "");
  }, i);
  await framed("切换标签", async () => {
    await clickMarked("[data-weir-tab]", "切换标签页");
    await settle();
  });
  await ev(() => document.querySelectorAll("[data-weir-tab]").forEach((n) => n.removeAttribute("data-weir-tab")));
}

async function scanPopups(stateUrl, stateName, header) {
  const triggers = await ev((a) => window.__weirScan.findTriggers(a), { header, max: 7 });
  for (const t of triggers) {
    ctx.state = `${stateName} › ${t.kind}：${t.text}`;
    try {
      if ((await currentUrl()) !== stateUrl || (await ev(() => window.__weirScan.popups().length)) > 0) {
        await openUrl(stateUrl, false);
      }
      const again = await ev((a) => window.__weirScan.findTriggers(a), { header, max: 7 });
      const hit = again.find((x) => x.kind === t.kind && x.text === t.text);
      if (!hit) continue;
      const opened = await framed("打开弹层", async () => {
        await clickMarked(`[data-weir-trigger="${hit.idx}"]`, "打开弹层");
        const ok = await page.waitForFunction(() => window.__weirScan?.popups().length > 0, undefined, { timeout: 2500 }).then(() => true, () => false);
        if (ok) await settle();
        return ok;
      });
      if (!opened) {
        report.coverage.popupsNotOpened.push(`${ctx.route} ${ctx.width} ${t.kind}：${t.text}`);
        continue;
      }
      const keyOk = (await ev(() => window.__weirScan.popups().at(-1)?.matches("[role=menu],[role=listbox],[data-slot=menu-content],[data-slot=select-content]"))) === false;
      // 菜单 / 下拉用方向键移动，Tab 会关闭它，不做 Tab 遍历
      await scanCombos(stateUrl, true, keyOk);
      const closed = await framed("关闭弹层", async () => {
        for (let i = 0; i < 2; i++) {
          await escape();
          const ok = await page.waitForFunction(() => window.__weirScan?.popups().length === 0, undefined, { timeout: 1500 }).then(() => true, () => false);
          if (ok) return true;
        }
        return false;
      });
      if (!closed) {
        report.coverage.popupsNotClosed.push(`${ctx.route} ${ctx.width} ${t.kind}：${t.text}`);
        await openUrl(stateUrl, true);
      }
    } catch (e) {
      await record([{ check: "error", prop: "脚本", actual: e.message.slice(0, 300), expected: "无页面错误", el: { tag: "", role: "", text: t.text, selector: "(页面)", component: "(页面)" }, count: 1 }]);
      await openUrl(stateUrl, true).catch(() => {});
    }
  }
}

async function scanRoute(route, first) {
  ctx.route = route;
  ctx.state = "页面";
  const fixed = route.includes("?");
  await openUrl(route, first || route.startsWith("/p/"));
  const baseUrl = await currentUrl();
  if (!(await scanCombos(baseUrl, false))) return;
  if (doPopups) await scanPopups(baseUrl, "页面", first);
  if (!doTabs || fixed) return;
  const tabs = await listTabs();
  for (const t of tabs.slice(1)) {
    ctx.state = `标签：${t.text}`;
    await openUrl(baseUrl, false);
    await clickTab(t.i);
    const url = await currentUrl();
    if (!(await scanCombos(url, false))) continue;
    if (doPopups) await scanPopups(url, `标签：${t.text}`, false);
  }
}
// ---------- 主流程 ----------
let failed = null;
try {
  await ensureLogin(page, base, cfg.email || "admin@dev.test", cfg.password || "WeirDev2026");
  await prepareTheme();
  // 窗口不在前台时也保持页面聚焦，焦点事件与 :focus-visible 才正常
  await page.cdp("Emulation.setFocusEmulationEnabled", { enabled: true }).catch(() => {});
  log(`输出目录 ${outDir}`);
  log(`路由 ${routes.join(" ")}；检查 ${checks.join(",")}；宽度 ${widths.join(",")}；主题 ${themes.join(",")}`);
  for (const w of widths) {
    ctx.width = w;
    await setWidth(w);
    if (has("frames")) await measureIdleFps();
    for (const [i, route] of routes.entries()) {
      const t0 = Date.now();
      try {
        await scanRoute(route, i === 0);
      } catch (e) {
        await record([{ check: "error", prop: "脚本", actual: e.message.slice(0, 300), expected: "无页面错误", el: { tag: "", role: "", text: "", selector: "(页面)", component: "(页面)" }, count: 1 }]);
        // 页面可能被热更新整页刷新或卡住：重新打开，继续下一个路由
        await page.cdp("Page.stopLoading").catch(() => {});
        await page.goto(base + "/").catch(() => {});
      }
      log(`${w} ${route} 完成，用时 ${Math.round((Date.now() - t0) / 1000)} 秒，累计违规 ${report.violations.length}`);
    }
  }
} catch (e) {
  failed = e;
  console.error("[扫描] 中断：", e.stack || e.message);
} finally {
  await restoreBrowser();
  report.meta.finished = new Date().toISOString();
  report.meta.aborted = failed ? String(failed.message) : null;
  await writeFile(join(outDir, "report.json"), JSON.stringify(report, null, 1), "utf8");
  await writeSummary(outDir, report);
  log(`报告：${join(outDir, "report.json")}`);
  log(`摘要：${join(outDir, "summary.md")}`);
}
if (failed) process.exitCode = 1;
await task.finish({ keep: [] });
