// 经接口给本地开发中心造演示数据：用户、订阅源、手动节点、订阅组、公开页面、全局公告。
// 可重复运行：按邮箱 / 名称 / 地址 / slug 查重，已存在的不再创建。VPS 要真实 Gate 配对，这里不造。
// 用法：node apps/console/scan/seed.mjs [--base http://127.0.0.1:5173]
import { inEgo, relay, parseArgs, loadConfig, openSpace, ensureLogin, api } from "./lib.mjs";
import { fileURLToPath } from "node:url";

if (!inEgo) {
  process.exit(await relay(fileURLToPath(import.meta.url), parseArgs(process.argv.slice(2))));
}

const cfg = await loadConfig();
const base = cfg.base || "http://127.0.0.1:5173";
const { task, page } = await openSpace("seed");
const items = (d) => (Array.isArray(d) ? d : d?.items ?? []);
const log = [];
const must = (r, what) => {
  if (r.status >= 300) throw new Error(`${what} 失败：HTTP ${r.status} ${JSON.stringify(r.data)}`);
  return r.data;
};

try {
  await ensureLogin(page, base, cfg.email || "admin@dev.test", cfg.password || "WeirDev2026");

  // 用户：只用于本机演示，密码满足「大写 + 小写 + 数字、至少 6 位」
  const users = items((await api(page, "/admin/users")).data);
  const wantUsers = [
    { email: "alice@dev.test", name: "Alice 演示", quota: 100 * 2 ** 30, monthly_reset: true },
    { email: "bob@dev.test", name: "Bob 演示", expires_at: new Date(Date.now() + 20 * 86400e3).toISOString() },
    { email: "carol@dev.test", name: "Carol（已停用）", disabled: true },
    { email: "ops@dev.test", name: "运维管理员", role: "admin" },
    { email: "dave.long-name-for-layout@dev.test", name: "一个很长很长的用户名用来检查窄屏换行与截断效果" },
  ];
  for (const u of wantUsers) {
    let row = users.find((x) => x.email === u.email);
    if (!row) {
      const { disabled, ...body } = u;
      row = must(await api(page, "/admin/users", "POST", { ...body, password: "WeirDemo2026" }), "创建用户 " + u.email);
      row = row.user ?? row;
      log.push("新建用户 " + u.email);
      if (disabled && row?.id) must(await api(page, `/admin/users/${row.id}`, "PATCH", { enabled: false }), "停用用户");
    }
    const uid = row?.id;
    if (uid && u.email === "alice@dev.test") {
      const links = items((await api(page, `/admin/users/${uid}/links`)).data);
      if (!links.some((l) => l.name === "备用")) {
        must(await api(page, `/admin/users/${uid}/links`, "POST", { name: "备用" }), "新建订阅链接");
        log.push("给 alice 新建订阅链接「备用」");
      }
    }
  }

  // 订阅源：.invalid 域名不会被解析，刷新时显示真实的拉取失败
  const sources = items((await api(page, "/admin/subscriptions/sources")).data);
  const wantSources = [
    { name: "演示机场 A", url: "https://airport-a.weir-demo.invalid/sub?target=clash", website: "https://airport-a.weir-demo.invalid", remark: "演示数据，地址不可达" },
    { name: "演示机场 B（停用）", url: "https://airport-b.weir-demo.invalid/api/v1/client/subscribe", enabled: false },
  ];
  for (const s of wantSources) {
    if (sources.some((x) => x.url === s.url || x.name === s.name)) continue;
    must(await api(page, "/admin/subscriptions/sources", "POST", s), "创建订阅源 " + s.name);
    log.push("新建订阅源 " + s.name);
  }

  // 手动节点：地址用 TEST-NET（192.0.2.0/24、198.51.100.0/24），凭据是假的
  const nodesRes = items((await api(page, "/admin/subscriptions/nodes")).data);
  const wantNodes = [
    { name: "香港 01", group: "演示", url: "trojan://demo-password@192.0.2.10:443?sni=hk.weir-demo.invalid#HK01" },
    { name: "日本 01", group: "演示", url: "vless://00000000-0000-4000-8000-000000000001@192.0.2.20:443?security=tls&type=ws&path=%2Fws#JP01" },
    { name: "新加坡 01", group: "演示", url: "ss://YWVzLTEyOC1nY206ZGVtby1wYXNz@198.51.100.30:8388#SG01" },
    { name: "美国 01（停用）", group: "备用", url: "trojan://demo-password@198.51.100.40:443#US01", enabled: false },
  ];
  for (const n of wantNodes) {
    if (nodesRes.some((x) => x.url === n.url || x.name === n.name)) continue;
    must(await api(page, "/admin/subscriptions/nodes", "POST", n), "创建手动节点 " + n.name);
    log.push("新建手动节点 " + n.name);
  }

  // 订阅组
  const profiles = items((await api(page, "/admin/subscriptions/profiles")).data);
  const allNodes = items((await api(page, "/admin/subscriptions/nodes")).data);
  const wantProfiles = [
    { name: "演示订阅组", all_sources: true, node_ids: allNodes.map((n) => n.id) },
    { name: "移动端精简", all_sources: false, source_ids: [], node_ids: allNodes.slice(0, 2).map((n) => n.id) },
  ];
  for (const p of wantProfiles) {
    if (profiles.some((x) => x.name === p.name)) continue;
    must(await api(page, "/admin/subscriptions/profiles", "POST", p), "创建订阅组 " + p.name);
    log.push("新建订阅组 " + p.name);
  }
  const profile = items((await api(page, "/admin/subscriptions/profiles")).data).find((x) => x.name === "演示订阅组");

  // 公开页面
  const pages = items((await api(page, "/admin/public-pages")).data);
  if (!pages.some((x) => (x.slug ?? x.key) === "demo")) {
    must(
      await api(page, "/admin/public-pages", "POST", {
        slug: "demo",
        data: {
          enabled: true,
          title: "演示订阅页",
          description: "本机演示数据：用于检查公开页面的排版。",
          notice: "这是演示公告，内容不代表真实服务。",
          notice_title: "提示",
          notice_type: "info",
          notice_dismissible: true,
          show_login: true,
          theme: "",
          show_share: true,
          cards: profile ? [{ profile_id: profile.id, target: "clash", title: "Clash 订阅" }, { profile_id: profile.id, target: "singbox", title: "sing-box 订阅" }] : [],
          guestbook: { enabled: false, allow_anonymous: false, require_audit: true },
        },
      }),
      "创建公开页面",
    );
    log.push("新建公开页面 /p/demo");
  }

  // 全局公告：只在未启用时写入，避免覆盖手动改过的公告
  const ann = (await api(page, "/admin/announcement")).data;
  if (ann?.announcement && !ann.announcement.enabled) {
    const body = {
      revision: ann.revision,
      announcement: { ...ann.announcement, enabled: true, authenticated: true, public: true, title: "演示公告", content: "本机演示：今晚 23:00 维护 10 分钟，期间订阅可能拉取失败。", type: "info", dismissible: true },
    };
    let r = await api(page, "/admin/announcement", "PUT", body);
    // 旧版中心保存公告要重新验证密码；新版不再要求且拒绝多余字段，所以只在被要求时才带上
    if (r.status === 401 && r.data?.error?.code === "password_required") r = await api(page, "/admin/announcement", "PUT", { ...body, password: cfg.password || "WeirDev2026", code: "" });
    must(r, "写入全局公告");
    log.push("启用全局公告");
  }

  console.log(log.length ? "[造数据] " + log.join("；") : "[造数据] 演示数据已齐全，没有新建");
  console.log("[造数据] 未覆盖：VPS 与节点需要真实 Gate 配对，没有造");
} catch (e) {
  console.error("[造数据] 出错：", e.message);
  process.exitCode = 1;
}
await task.finish({ keep: [] });
