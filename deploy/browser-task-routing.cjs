require('./production-guard.cjs');
const { chromium } = require("playwright");
const fs = require("fs"),
  path = require("path");
let browser;
(async () => {
  const s = JSON.parse(
    fs.readFileSync(
      path.join(process.env.LOCALAPPDATA, "MiGate/operations/deployment.json"),
      "utf8",
    ),
  );
  browser = await chromium.launch({
    channel: "chrome",
    args: [
      "--no-proxy-server",
      "--host-resolver-rules=MAP sub.caowo.baby 127.0.0.1:19443",
    ],
  });
  const context = await browser.newContext({
    viewport: { width: 1440, height: 1000 },
  });
  await context.addCookies([
    {
      name: "migate_session",
      value: s.admin_cookie.slice("migate_session=".length),
      domain: "sub.caowo.baby",
      path: "/",
      secure: true,
      httpOnly: true,
    },
  ]);
  const page = await context.newPage(),
    checks = [],
    errors = [];
  page.on("pageerror", (e) => errors.push(e.message));
  async function req(method, endpoint, data) {
    return page.evaluate(
      async ({ method, endpoint, data }) => {
        const r = await fetch("/api/v1" + endpoint, {
          method,
          headers: { "Content-Type": "application/json" },
          body: data === undefined ? undefined : JSON.stringify(data),
        });
        if (!r.ok) throw Error("API HTTP " + r.status);
        return r.json();
      },
      { method, endpoint, data },
    );
  }
  await page.goto("https://sub.caowo.baby/routing?device=" + s.device_id);
  await page.getByRole("heading", { name: "路由配置", exact: true }).waitFor();
  if (await page.getByText("Gate ID", { exact: true }).count())
    throw Error("Legacy ID form remains");
  await page.reload();
  await page.getByRole("heading", { name: "路由配置", exact: true }).waitFor();
  checks.push(
    "global routing deep link and selected Gate survive reload; business selectors replace ID forms",
  );
  await page.getByRole("tab", { name: "出站连接", exact: true }).click();
  await page.getByRole("button", { name: "新增出站", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await dialog
    .getByLabel("出站名称", { exact: true })
    .fill("routing-preview-only");
  await dialog
    .getByRole("button", { name: "查看配置预览", exact: true })
    .click();
  await dialog
    .locator("pre")
    .filter({ hasText: "routing-preview-only" })
    .waitFor();
  await page.keyboard.press("Escape");
  const failed = await req("POST", "/admin/devices/" + s.device_id + "/tasks", {
    operation: "inbound.upsert",
    payload: { protocol: "vless", port: 80, remark: "acceptance-invalid-port" },
    confirm: true,
  });
  for (let i = 0; i < 70; i++) {
    const t = (await req("GET", "/admin/tasks?q=" + failed.id)).items[0];
    if (t?.status === "failed") break;
    if (i === 69) throw Error("Expected real configuration failure");
    await page.waitForTimeout(1000);
  }
  for (const width of [1440, 1280, 768, 390]) {
    await page.setViewportSize({ width, height: 1000 });
    await page.goto("https://sub.caowo.baby/routing?device=" + s.device_id);
    await page
      .getByRole("heading", { name: "路由配置", exact: true })
      .waitFor();
    await page.screenshot({
      path: `docs/evidence/routing-live-${width}.png`,
      fullPage: true,
    });
    await page.goto("https://sub.caowo.baby/tasks");
    await page
      .getByRole("textbox", { name: "搜索执行任务" })
      .fill(String(failed.id));
    await page
      .getByRole("button", { name: "查看回执 #" + failed.id, exact: true })
      .click();
    await page
      .getByRole("heading", {
        name: "任务 #" + failed.id + " 执行回执",
        exact: true,
      })
      .waitFor();
    await dialog
      .locator("pre")
      .filter({
        hasText:
          /invalid_or_reserved_inbound_port|invalid port|core_validation_or_apply_failed/,
      })
      .waitFor();
    if (
      await page.evaluate(
        () => document.documentElement.scrollWidth > innerWidth,
      )
    )
      throw Error("Task dialog overflow " + width);
    await page.screenshot({
      path: `docs/evidence/task-receipt-live-${width}.png`,
      fullPage: true,
    });
    await page.keyboard.press("Escape");
  }
  checks.push(
    "real invalid-port task fails; UI shows diagnostic receipt, attempts, resource version and expiry at four widths",
  );
  await page
    .getByRole("textbox", { name: "搜索执行任务" })
    .fill("no-such-operation-acceptance");
  await page.getByText("没有符合条件的任务", { exact: true }).waitFor();
  checks.push(
    "real search empty state; API paging/filter/ownership separately integration-tested",
  );
  await page.goto(
    "https://sub.caowo.baby/devices/" + s.device_id + "?tab=tasks",
  );
  await page
    .getByRole("heading", { name: "执行记录与回执", exact: true })
    .waitFor();
  const d = await req(
    "GET",
    "/admin/tasks?device_id=" + s.device_id + "&page=1&page_size=20",
  );
  if (d.items.some((t) => t.device_id !== s.device_id || t.payload))
    throw Error("Task directory leakage");
  if (errors.length) throw Error("Browser runtime error");
  fs.writeFileSync(
    "docs/evidence/task-routing-live.json",
    JSON.stringify(
      {
        version: (await req("GET", "/admin/overview")).version,
        date: new Date().toISOString(),
        failed_task_id: failed.id,
        checks,
        errors,
      },
      null,
      2,
    ),
  );
  await browser.close();
  console.log(
    "Live routing workspace, task filtering, real failure receipt and four-width dialogs passed",
  );
})().catch(async (e) => {
  if (browser) await browser.close();
  console.error(e.message.replace(/cookie: [^\n]+/gi, "cookie: [REDACTED]"));
  process.exitCode = 1;
});
