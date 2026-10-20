require('./production-guard.cjs');
// Run while the owned real source fixture is active; never save source URLs.
const { chromium } = require("playwright");
const fs = require("node:fs");
const path = require("node:path");
let browser;
(async () => {
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
  const context = await browser.newContext({
    viewport: { width: 390, height: 900 },
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
  await page.goto("https://sub.caowo.baby/subscriptions?kind=sources");
  const row = page
    .locator(".inventory-item")
    .filter({
      has: page
        .locator("small")
        .filter({ hasText: new RegExp("^#" + process.argv[2] + "$") }),
    });
  if (process.argv[3] === "available") {
    await row
      .getByText(/上传 128 B \/ 下载 256 B \/ 总量 1024 B \/ 到期/)
      .waitFor();
    await page.screenshot({
      path: "docs/evidence/live-source-metadata-mobile.png",
      fullPage: true,
    });
  } else await row.getByText("未知", { exact: true }).waitFor();
  if (
    errors.length ||
    (await page.evaluate(
      () => document.documentElement.scrollWidth > innerWidth,
    ))
  )
    throw Error("source observation mobile rendering failed");
  await browser.close();
  console.log("Source metadata browser check passed: " + process.argv[3]);
})().catch(async (e) => {
  if (browser) await browser.close();
  console.error(e.message);
  process.exitCode = 1;
});
