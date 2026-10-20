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
  let resource;
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
  const dialog = page.getByRole("dialog");
  async function step(name) {
    await dialog.getByLabel("添加处理步骤", { exact: true }).click();
    await page.getByRole("option", { name, exact: true }).click();
    await dialog.getByRole("button", { name: "添加步骤", exact: true }).click();
  }
  async function preview() {
    const response = page.waitForResponse(
      (r) =>
        r.url().endsWith("/subscriptions/preview") &&
        r.request().method() === "POST",
    );
    await dialog
      .getByRole("button", { name: "预览处理结果", exact: true })
      .click();
    if (!(await response).ok()) throw Error("Preview failed");
    await dialog.getByLabel("处理后结果", { exact: true }).waitFor();
    return decodeURIComponent(
      await dialog.getByLabel("处理后结果", { exact: true }).inputValue(),
    );
  }
  try {
    await page.goto("https://sub.caowo.baby/subscriptions?kind=operators");
    await page.getByRole("button", { name: "新增处理链", exact: true }).click();
    const name = "operators-extended-" + Date.now();
    await dialog.getByLabel("名称", { exact: true }).fill(name);
    await dialog
      .getByRole("switch", { name: "启用名称模板", exact: true })
      .click();
    await dialog
      .getByRole("switch", { name: "启用正则替换", exact: true })
      .click();
    await dialog
      .getByRole("button", { name: "添加重命名规则", exact: true })
      .click();
    await dialog
      .getByLabel("重命名规则 1 匹配模式", { exact: true })
      .fill("HK-(\\d{2})");
    await dialog
      .getByLabel("重命名规则 1 替换文本", { exact: true })
      .fill("香港-$1");
    await dialog.getByLabel("重命名规则 1 标志", { exact: true }).fill("g");
    await step("筛选节点");
    await dialog
      .getByRole("switch", { name: "启用协议筛选", exact: true })
      .click();
    await dialog
      .getByRole("checkbox", { name: "Shadowsocks", exact: true })
      .click();
    await dialog
      .getByRole("switch", { name: "启用地区筛选", exact: true })
      .click();
    await dialog.getByLabel("搜索地区筛选", { exact: true }).fill("香港");
    await dialog.getByRole("checkbox", { name: "香港", exact: true }).click();
    await step("排序");
    await dialog.getByLabel("排序字段 1", { exact: true }).click();
    await page.getByRole("option", { name: "地区", exact: true }).click();
    await dialog.getByLabel("自定义排序 1", { exact: true }).fill("香港\n日本");
    await dialog
      .getByRole("button", { name: "添加排序字段", exact: true })
      .click();
    await dialog.getByLabel("排序字段 2", { exact: true }).click();
    await page.getByRole("option", { name: "端口", exact: true }).click();
    await dialog.getByLabel("排序方向 2", { exact: true }).click();
    await page.getByRole("option", { name: "降序", exact: true }).click();
    await dialog.getByText("处理前后对照", { exact: true }).click();
    await dialog
      .getByLabel("测试节点链接（每行一条）", { exact: true })
      .fill(
        "ss://YWVzLTEyOC1nY206c3ludGhldGlj@example.com:8388#HK-01\nss://YWVzLTEyOC1nY206c3ludGhldGlj@example.org:8389#HK-02\nss://YWVzLTEyOC1nY206c3ludGhldGlj@example.net:8390#US-03\nhttp://u:synthetic@example.net:8080#HK-04",
      );
    const output = await preview();
    if (
      output.split("\n").length !== 2 ||
      !output.includes("香港-02") ||
      output.includes("US-03") ||
      output.includes("HK-04") ||
      output.indexOf(":8389") > output.indexOf(":8388")
    )
      throw Error("Extended filter/rename/sort mismatch");
    checks.push(
      "UI protocol and pinned-region filter, comma-bearing RE2 expression, explicit flags/capture replacement and multi-key custom sort produce actual backend output",
    );
    await dialog.getByLabel("重命名规则 1 匹配模式", { exact: true }).fill("[");
    await dialog
      .getByRole("button", { name: "预览处理结果", exact: true })
      .click();
    await dialog.getByRole("alert").waitFor();
    await dialog
      .getByLabel("重命名规则 1 匹配模式", { exact: true })
      .fill("HK-(\\d{2})");
    await preview();
    checks.push(
      "invalid RE2 preview fails explicitly; corrected form recovers",
    );
    for (const width of [1440, 1280, 768, 390]) {
      await page.setViewportSize({ width, height: 1000 });
      await dialog
        .getByLabel("排序字段 1", { exact: true })
        .scrollIntoViewIfNeeded();
      const bounds = await dialog.locator("form").boundingBox(),
        popup = await dialog.boundingBox();
      if (width >= 768 && bounds.width < popup.width * 0.75)
        throw Error("Operator form squeezed into dialog column");
      if (
        await page.evaluate(
          () => document.documentElement.scrollWidth > innerWidth,
        )
      )
        throw Error("Operator dialog overflow " + width);
      await page.screenshot({
        path: `docs/evidence/operator-fields-live-${width}.png`,
        fullPage: true,
        mask: [page.locator(".sensitive-output")],
      });
    }
    const response = page.waitForResponse(
      (r) =>
        r.url().endsWith("/resources/operators") &&
        r.request().method() === "POST",
    );
    await dialog.getByRole("button", { name: "保存", exact: true }).click();
    const saved = await (await response).json();
    resource = saved.id;
    await dialog.waitFor({ state: "hidden" });
    const stored = (await req("GET", "/admin/resources/operators")).items.find(
      (r) => r.id === resource,
    );
    if (
      !stored ||
      stored.data.operators[2].params.keys.length !== 2 ||
      stored.data.operators[0].params.regex.rules[0].flags !== "g"
    )
      throw Error("Extended fields not persisted");
    checks.push(
      "saved operator resource retains flags, selected protocols/regions and two sort priorities",
    );
    if (errors.length) throw Error("Browser runtime errors");
    fs.writeFileSync(
      "docs/evidence/operator-fields-live.json",
      JSON.stringify(
        {
          version: (await req("GET", "/admin/overview")).version,
          date: new Date().toISOString(),
          checks,
          errors,
        },
        null,
        2,
      ),
    );
    console.log(
      "Real extended operator form, backend output, invalid-pattern recovery and persistence passed",
    );
  } finally {
    if (resource) await req("DELETE", "/admin/resources/operators/" + resource);
    await browser.close();
  }
})().catch(async (e) => {
  if (browser) await browser.close();
  console.error(e.message.replace(/cookie: [^\n]+/gi, "cookie: [REDACTED]"));
  process.exitCode = 1;
});
