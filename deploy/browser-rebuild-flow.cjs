require('./production-guard.cjs');
// Live browser acceptance. New acceptance account is recorded only in restricted operator storage.
const { chromium } = require("playwright");
const fs = require("fs");
const path = require("path");
const crypto = require("crypto");
let browser;
(async () => {
  const ops = path.join(process.env.LOCALAPPDATA, "MiGate/operations");
  const state = JSON.parse(
    fs.readFileSync(path.join(ops, "deployment.json"), "utf8"),
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
      value: state.admin_cookie.slice("migate_session=".length),
      domain: "sub.caowo.baby",
      path: "/",
      httpOnly: true,
      secure: true,
      sameSite: "Strict",
    },
  ]);
  const page = await context.newPage();
  const errors = [];
  page.on("pageerror", (e) => errors.push(e.message));
  const checks = [];
  const account = {
    email: "r1-" + crypto.randomBytes(5).toString("hex") + "@sub.caowo.baby",
    password: crypto.randomBytes(24).toString("base64url"),
  };
  const save = () =>
    fs.writeFileSync(
      path.join(ops, "rebuild-acceptance.json"),
      JSON.stringify(account, null, 2),
    );
  save();
  await page.goto("https://sub.caowo.baby/users");
  await page.getByRole("button", { name: "创建用户", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByLabel("邮箱", { exact: true }).fill(account.email);
  await dialog.getByLabel("初始密码").fill(account.password);
  await dialog.getByLabel("流量额度").fill("1");
  const created = page.waitForResponse(
    (r) =>
      r.url().endsWith("/api/v1/admin/users") &&
      r.request().method() === "POST",
  );
  await dialog.getByRole("button", { name: "创建用户", exact: true }).click();
  const response = await created;
  if (response.status() !== 201) throw Error("UI create failed");
  account.id = (await response.json()).id;
  save();
  await dialog.waitFor({ state: "hidden" });
  checks.push("UI create user with 1 GiB, default no grants");
  await page.getByLabel("搜索用户").fill(account.email);
  await page
    .getByRole("button", { name: account.email, exact: true })
    .waitFor();
  await page.getByRole("button", { name: account.email, exact: true }).click();
  await page
    .getByRole("heading", { name: account.email, exact: true })
    .waitFor();
  await page.getByText("尚未分配节点", { exact: true }).waitFor();
  await page.reload();
  await page
    .getByRole("heading", { name: account.email, exact: true })
    .waitFor();
  checks.push("search, detail deep link and reload");
  await page.getByRole("button", { name: "编辑用户", exact: true }).click();
  await dialog.getByLabel("流量额度").fill(String(2 * 1024 ** 3));
  await dialog.getByRole("button", { name: "保存变更", exact: true }).click();
  await dialog.getByRole("button", { name: "确认保存", exact: true }).click();
  await dialog.waitFor({ state: "hidden" });
  await page.getByText("2 GiB", { exact: true }).waitFor();
  checks.push("exact-byte quota edit with second confirmation");
  await page.getByRole("button", { name: "管理授权", exact: true }).click();
  await page.getByRole("button", { name: "分配节点", exact: true }).click();
  await dialog.getByLabel("Gate 设备", { exact: true }).click();
  await page.getByRole("option", { name: "gate-154", exact: true }).click();
  await dialog.getByLabel("已生效入站", { exact: true }).click();
  const options = page.getByRole("option");
  const primary = options.filter({ hasText: ":24445" });
  await primary.first().click();
  const grantResponse = page.waitForResponse(
    (r) =>
      r.url().endsWith("/api/v1/admin/grants") &&
      r.request().method() === "POST",
  );
  await dialog
    .getByRole("button", { name: "提交授权任务", exact: true })
    .click();
  const grant = await grantResponse;
  if (grant.status() !== 202) throw Error("UI grant failed");
  account.grant_id = (await grant.json()).id;
  save();
  await dialog.waitFor({ state: "hidden" });
  await page.getByText("已生效", { exact: true }).waitFor({ timeout: 60000 });
  checks.push("UI selector grant, real Gate receipt applied");
  await page.goto("https://sub.caowo.baby/devices/" + state.device_id);
  await page.getByRole("heading", { name: "gate-154", exact: true }).waitFor();
  await page.getByRole("tab", { name: "客户端授权", exact: true }).click();
  await page
    .getByRole("heading", { name: "独立客户端授权", exact: true })
    .waitFor();
  await page.reload();
  await page.getByRole("tab", { name: "客户端授权", exact: true }).waitFor();
  checks.push("device detail tabs retain deep-link state");
  await page.goto("https://sub.caowo.baby/users");
  await page
    .getByLabel("搜索用户")
    .fill("not-found-" + crypto.randomBytes(6).toString("hex"));
  await page.getByText("没有匹配的用户", { exact: true }).waitFor();
  checks.push("real empty search state");
  await context.route("**/api/v1/admin/users?*", (r) =>
    r.fulfill({
      status: 503,
      contentType: "application/json",
      body: JSON.stringify({ error: { code: "query_failed" } }),
    }),
  );
  await page.getByLabel("搜索用户").fill("failure-check");
  await page
    .getByRole("button", { name: "重新加载", exact: true })
    .waitFor()
    .catch(async (e) => {
      await page.screenshot({ path: "docs/evidence/rebuild-failed-flow.png" });
      console.error(await page.locator("main").innerText());
      throw e;
    });
  await page.screenshot({ path: "docs/evidence/rebuild-live-error-1440.png" });
  await context.unroute("**/api/v1/admin/users?*");
  await page.getByRole("button", { name: "重新加载", exact: true }).click();
  await page.getByText("没有匹配的用户", { exact: true }).waitFor();
  checks.push("injected 503 UI failure and retry (not a service outage test)");
  const ordinary = await browser.newContext({
    viewport: { width: 390, height: 900 },
  });
  const userPage = await ordinary.newPage();
  await userPage.goto("https://sub.caowo.baby/");
  await userPage.getByLabel("邮箱", { exact: true }).fill(account.email);
  await userPage.getByLabel("密码", { exact: true }).fill(account.password);
  await userPage.getByRole("button", { name: /^(登录|验证并登录)$/, exact: true }).click();
  await userPage
    .getByRole("heading", { name: "我的订阅", exact: true })
    .waitFor();
  const denied = await userPage.evaluate(
    async (id) => (await fetch("/api/v1/admin/users/" + id)).status,
    account.id,
  );
  if (denied !== 403) throw Error("ordinary user detail permission failure");
  await userPage.goto("https://sub.caowo.baby/users");
  await userPage
    .getByRole("heading", { name: "我的订阅", exact: true })
    .waitFor();
  await userPage.getByLabel("客户端格式").click();
  await userPage.getByRole("option", { name: "sing-box", exact: true }).click();
  const url = await userPage.getByLabel("singbox订阅地址").inputValue();
  if (new URL(url).searchParams.get("target") !== "singbox")
    throw Error("format URL mismatch");
  await userPage.screenshot({
    path: "docs/evidence/rebuild-live-ordinary-390.png",
    fullPage: true,
    mask: [userPage.locator(".subscription-url"), userPage.locator(".qr-code")],
  });
  checks.push(
    "ordinary login, admin API denied, route guard, subscription format and masked QR",
  );
  if (errors.length) throw Error("Browser runtime error");
  await browser.close();
  fs.writeFileSync(
    "docs/evidence/rebuild-live-flow.json",
    JSON.stringify(
      { version: "0.2.0-rc31", date: new Date().toISOString(), checks, errors },
      null,
      2,
    ),
  );
  console.log(
    "Live R1 browser workflows passed; restricted acceptance account available for real client test",
  );
})().catch(async (e) => {
  if (browser) await browser.close();
  console.error(
    e.message
      .replace(/cookie: [^\n]+/gi, "cookie: [REDACTED]")
      .replace(/migate_session=[a-z0-9]+/gi, "migate_session=[REDACTED]"),
  );
  process.exitCode = 1;
});
