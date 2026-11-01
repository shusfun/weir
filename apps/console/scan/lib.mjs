// 扫描脚本的公共部分：在普通 Node 里解析参数并转交给 ego-browser 运行；在 ego-browser 里提供登录、接口调用与页面脚本注入。
// ego-browser 的脚本进程拿不到命令行参数和环境变量，所以参数写进 out/.run-*.json，再用 ASCII 的一行入口导入本模块。
import { readFile, writeFile, mkdir } from "node:fs/promises";
import { fileURLToPath, pathToFileURL } from "node:url";
import { dirname, join } from "node:path";
import { spawn } from "node:child_process";
import { existsSync } from "node:fs";

export const scanDir = dirname(fileURLToPath(import.meta.url));
export const outRoot = join(scanDir, "out");
export const inEgo = typeof globalThis.taskSpace === "function";
// 每次运行用独立的空间名：出错中断的空间会被 ego 记为用户所有，不能接管，不能复用同名
export const spaceName = (kind) => `weir-ui-${kind}-${stamp()}`;

// 解析 --key value / --key=value / --flag
export function parseArgs(argv) {
  const out = {};
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i];
    if (!a.startsWith("--")) continue;
    let [k, v] = a.slice(2).split(/=(.*)/s);
    if (v === undefined) v = argv[i + 1] && !argv[i + 1].startsWith("--") ? argv[++i] : "true";
    out[k] = v;
  }
  return out;
}

function egoExe() {
  const home = process.env.USERPROFILE || process.env.HOME || "";
  const candidates = [process.env.EGO_BROWSER, join(home, ".local/share/ego/active_version_dir/ego-browser.exe"), join(home, ".local/share/ego/active_version_dir/ego-browser")];
  return candidates.find((p) => p && existsSync(p)) || "ego-browser";
}

// 在普通 Node 里调用：把参数写进文件，由 ego-browser 导入入口模块运行，输出直接透传。
export async function relay(entryFile, args) {
  await mkdir(outRoot, { recursive: true });
  const cfgPath = join(outRoot, `.run-${Date.now()}.json`);
  await writeFile(cfgPath, JSON.stringify(args), "utf8");
  const entry = pathToFileURL(entryFile).href;
  const cfgUrl = pathToFileURL(cfgPath).href;
  const code = `globalThis.__weirScanConfig = '${cfgUrl}'; await import('${entry}?t=${Date.now()}');`;
  return await new Promise((resolve) => {
    const child = spawn(egoExe(), ["nodejs", "-e", code], { stdio: "inherit", windowsHide: true });
    child.on("exit", (c) => resolve(c ?? 1));
    child.on("error", (e) => {
      console.error("无法启动 ego-browser：", e.message);
      resolve(1);
    });
  });
}

export async function loadConfig() {
  const u = globalThis.__weirScanConfig;
  if (!u) return {};
  return JSON.parse(await readFile(new URL(u), "utf8"));
}

// 复用同名 TaskSpace，拿到它的第一个页面；空间是新建的就是 p1
export async function openSpace(kind = "scan") {
  const task = await taskSpace(spaceName(kind));
  const pages = await task.pages();
  const page = pages.length ? pages[0] : await task.newPage();
  console.log(`[扫描] TaskSpace ${task.spaceId}，页面 ${page.label}`);
  return { task, page };
}

// 走登录表单登录；已登录就直接返回。直接请求登录接口会被拒（origin_rejected）。
export async function ensureLogin(page, base, email, password) {
  await page.goto(base + "/");
  await page.waitForFunction(() => !!document.querySelector('input[autocomplete="username"], .shell-main, [aria-label="主导航"], [aria-label="打开导航"]'), undefined, { timeout: 20_000 });
  const needLogin = await page.evaluate(() => !!document.querySelector('input[autocomplete="username"]'));
  if (!needLogin) return false;
  await page.fill('input[autocomplete="username"]', email);
  await page.fill('input[autocomplete="current-password"]', password);
  await page.click('form button[type="submit"]', { label: "提交登录表单" });
  await page.waitForFunction(() => !!document.querySelector(".shell-main"), undefined, { timeout: 20_000 });
  return true;
}

// 在页面里同源调用 /api/v1；返回 { status, data }
// 别的代理改代码时 Vite 可能整页刷新，页面目标短暂失效；这类错误等一下重试
export const transient = (e) => /navigated or closed|Execution context was destroyed|Cannot find context|EGO_PAGE_UNRESPONSIVE|Target closed/i.test(String(e?.message ?? e));
export async function retry(fn, times = 3, page) {
  for (let i = 0; ; i++) {
    try {
      return await fn();
    } catch (e) {
      if (i >= times - 1 || !transient(e)) throw e;
      if (page) await page.waitForTimeout(1500).catch(() => {});
      else await new Promise((r) => setTimeout(r, 1500));
    }
  }
}

export async function api(page, path, method = "GET", body) {
  const res = await retry(() => page.fetch("/api/v1" + path, {
    method,
    headers: { "content-type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
    timeout: 20_000,
  }), 3, page);
  let data = null;
  try {
    data = res.body ? JSON.parse(res.body) : null;
  } catch {
    data = res.body;
  }
  return { status: res.status, data };
}

// 页面侧检查脚本：几个文件按顺序拼起来注入，定义 window.__weirScan
const probeFiles = ["probe-core.js", "probe-static.js", "probe-a11y.js", "probe-interact.js"];
let probeSource = null;
export async function probeCode() {
  if (probeSource) return probeSource;
  const parts = [];
  for (const f of probeFiles) parts.push(await readFile(join(scanDir, f), "utf8"));
  probeSource = `(() => { if (window.__weirScan) return true;\n${parts.join("\n")}\nreturn true; })()`;
  return probeSource;
}

// 页面整页刷新或热更新后 window.__weirScan 会丢，调用前都确认一次
export async function ensureProbe(page) {
  const ok = await page.evaluate(() => !!window.__weirScan).catch(() => false);
  if (!ok) await page.evaluate(await probeCode());
}

export function stamp(d = new Date()) {
  const p = (n) => String(n).padStart(2, "0");
  return `${d.getFullYear()}${p(d.getMonth() + 1)}${p(d.getDate())}-${p(d.getHours())}${p(d.getMinutes())}${p(d.getSeconds())}`;
}
