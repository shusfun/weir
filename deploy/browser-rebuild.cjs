require('./production-guard.cjs');
// Real API data, local built UI optional; no fabricated observations or screenshots of secrets.
const { chromium } = require("playwright");
const fs = require("node:fs");
const path = require("node:path");
let browser;
(async () => {
  const preview = process.argv.includes("--preview");
  const state = JSON.parse(
    fs.readFileSync(
      path.join(process.env.LOCALAPPDATA, "MiGate/operations/deployment.json"),
      "utf8",
    ),
  );
  browser = await chromium.launch({
    channel: "chrome",
    headless: true,
    args: [
      "--no-proxy-server",
      "--host-resolver-rules=MAP sub.caowo.baby 127.0.0.1:19443",
    ],
  });
  const errors = [];
  const checks = [];
  for (const width of [1440, 1280, 768, 390]) {
    const context = await browser.newContext({
      viewport: { width, height: 1000 },
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
    if (preview)
      await context.route("https://sub.caowo.baby/**", async (route) => {
        const pathname = new URL(route.request().url()).pathname;
        if (pathname.startsWith("/api/") || pathname.startsWith("/sub/"))
          return route.continue();
        const file = pathname.startsWith("/assets/")
          ? path.resolve("apps/control/static", "." + pathname)
          : path.resolve("apps/control/static/index.html");
        await route.fulfill({
          path: file,
          contentType: file.endsWith(".js")
            ? "application/javascript"
            : file.endsWith(".css")
              ? "text/css"
              : "text/html",
        });
      });
    const page = await context.newPage();
    page.on("pageerror", (e) => errors.push(e.message));
    for (const [route, label] of [
      ["overview", "概览"],
      ["users", "用户管理"],
      ["devices", "Gate 设备"],
      ["subscription", "我的订阅"],
      ["subscriptions", "订阅管理"],
      ["grants", "授权管理"],
    ]) {
      await page.goto("https://sub.caowo.baby/" + route);
      await page
        .getByRole("heading", { name: label, exact: true })
        .waitFor()
        .catch(async (e) => {
          console.error(
            JSON.stringify({
              route,
              width,
              url: new URL(page.url()).pathname,
              headings: await page.locator("h1,h2").allTextContents(),
              errors,
            }),
          );
          throw e;
        });
      await page.waitForFunction(
        () =>
          ![...document.querySelectorAll(".page-state")].some((e) =>
            e.textContent.includes("正在加载数据"),
          ),
      );
      await page.waitForTimeout(700);
      if (route === "overview")
        await page.getByText("一处掌握你的网络。", { exact: true }).waitFor();
      if (route === "users") await page.getByLabel("搜索用户").waitFor();
      if (route === "devices")
        await page.getByText("你的 Gate 网络", { exact: true }).waitFor();
      if (
        await page.evaluate(
          () => document.documentElement.scrollWidth > innerWidth,
        )
      )
        throw Error(route + " overflow " + width);
      await page.screenshot({
        path: `docs/evidence/rebuild-${preview ? "preview" : "live"}-${route}-${width}.png`,
        fullPage: true,
        mask: [
          page.locator(".subscription-url"),
          page.locator(".qr-code"),
          page.locator("canvas"),
          page.locator("svg[role=img]"),
        ],
      });
      checks.push({ route, width, realApi: true, localAssets: preview });
    }
    if (width === 390) {
      await page.getByRole("button", { name: "打开导航", exact: true }).click();
      const drawer = page.getByRole("dialog");
      await drawer.getByRole("link", { name: "用户管理", exact: true }).click();
      await page.getByLabel("搜索用户").waitFor();
      if ((await drawer.count()) && (await drawer.isVisible()))
        throw Error("drawer did not close");
      checks.push({ mobileNavigation: true });
    }
    await context.close();
  }
  if (errors.length) throw Error("page errors: " + errors.join(";"));
  await browser.close();
  fs.writeFileSync(
    `docs/evidence/rebuild-${preview ? "preview" : "live"}.json`,
    JSON.stringify({ date: new Date().toISOString(), checks, errors }, null, 2),
  );
  console.log(
    "Four-width real-data rebuild browser checks passed" +
      (preview ? " (local assets)" : ""),
  );
})().catch(async (e) => {
  if (browser) await browser.close();
  console.error(e.message);
  process.exitCode = 1;
});
