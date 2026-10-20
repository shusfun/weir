require('./production-guard.cjs');
// Real API/UI workflows, synthetic inventory; cleanup only IDs created in this run.
const { chromium } = require("playwright");
const fs = require("fs");
const path = require("path");
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
  const page = await context.newPage();
  const errors = [];
  page.on("pageerror", (e) => errors.push(e.message));
  const owned = [];
  const checks = [];
  const tag = "r2-" + Date.now();
  const dialog = page.getByRole("dialog");
  async function json(method, endpoint, data) {
    const r = await page.evaluate(
      async ({ method, endpoint, data }) => {
        const r = await fetch("/api/v1" + endpoint, {
          method,
          headers: { "Content-Type": "application/json" },
          body: data === undefined ? undefined : JSON.stringify(data),
        });
        return { status: r.status, body: await r.json() };
      },
      { method, endpoint, data },
    );
    if (r.status >= 400) throw Error("Inventory acceptance API " + r.status);
    return r.body;
  }
  async function tab(label) {
    await page.getByRole("tab", { name: label, exact: true }).click();
    await page.waitForFunction(
      () =>
        ![...document.querySelectorAll(".page-state")].some((e) =>
          e.textContent.includes("正在加载数据"),
        ),
    );
  }
  async function save(kind) {
    const response = page.waitForResponse(
      (r) =>
        r.url().endsWith("/api/v1/admin/resources/" + kind) &&
        r.request().method() === "POST",
    );
    await dialog.getByRole("button", { name: "保存", exact: true }).click();
    const r = await response;
    if (r.status() !== 200) throw Error("Form save failed " + kind);
    const result = await r.json();
    owned.push([kind, result.id]);
    await dialog.waitFor({ state: "hidden" });
    return result;
  }
  try {
    await page.goto("https://sub.caowo.baby/subscriptions?kind=nodes");
    await page.getByRole("button", { name: "导入节点", exact: true }).click();
    await dialog
      .getByLabel("节点清单内容", { exact: true })
      .fill(
        `socks5://u:synthetic@example.com:1080#${tag}-A\nsocks5://u:synthetic@example.com:1080#${tag}-duplicate\nhttp://u:synthetic@example.org:8080#${tag}-B`,
      );
    const before = (await json("GET", "/admin/resources/nodes")).total;
    await dialog.getByRole("button", { name: "解析预览", exact: true }).click();
    await dialog
      .getByText("准备导入 2 个 · 检测到 1 个重复项", { exact: true })
      .waitFor();
    if ((await json("GET", "/admin/resources/nodes")).total !== before)
      throw Error("Preview wrote inventory");
    const importedResponse = page.waitForResponse(
      (r) =>
        r.url().endsWith("/api/v1/admin/subscriptions/import") &&
        r.request().method() === "POST",
    );
    await dialog
      .getByRole("button", { name: "确认导入 2 个节点", exact: true })
      .click();
    const imported = await (await importedResponse).json();
    for (const id of imported.ids) owned.push(["nodes", id]);
    await dialog.getByText("已导入 2 个节点", { exact: true }).waitFor();
    await page.keyboard.press("Escape");
    checks.push(
      "URI import preview does not write; identity dedup commits two nodes",
    );
    await tab("处理链");
    await page.getByRole("button", { name: "新增处理链", exact: true }).click();
    await dialog.getByLabel("名称", { exact: true }).fill(tag + "-chain");
    await dialog.getByLabel("名称模板", { exact: true }).fill("R2-{name}");
    await dialog.getByLabel("添加处理步骤", { exact: true }).click();
    await page.getByRole("option", { name: "筛选节点", exact: true }).click();
    await dialog.getByRole("button", { name: "添加步骤", exact: true }).click();
    await dialog
      .getByLabel("包含名称规则 1 匹配模式", { exact: true })
      .fill(tag + "-A");
    await dialog
      .getByRole("button", { name: "上移步骤 2", exact: true })
      .click();
    await dialog.getByText("处理前后对照", { exact: true }).click();
    await dialog
      .getByLabel("测试节点链接（每行一条）", { exact: true })
      .fill(
        `socks5://u:synthetic@example.com:1080#${tag}-A\nhttp://u:synthetic@example.org:8080#${tag}-B`,
      );
    await dialog
      .getByRole("button", { name: "预览处理结果", exact: true })
      .click();
    await dialog.getByLabel("处理后结果", { exact: true }).waitFor();
    const output = await dialog
      .getByLabel("处理后结果", { exact: true })
      .inputValue();
    if (!output.includes("R2-") || output.includes(tag + "-B"))
      throw Error("Ordered pipeline not applied");
    const chain = await save("operators");
    checks.push(
      "filter/rename step editing, reordering and real before-after output",
    );
    await tab("输出模板");
    await page
      .getByRole("button", { name: "新增输出模板", exact: true })
      .click();
    await dialog.getByLabel("名称", { exact: true }).fill(tag + "-template");
    await dialog
      .getByLabel("DNS 服务器（逗号分隔；IP、https:// 或 tls://）", {
        exact: true,
      })
      .fill("1.1.1.1");
    const template = await save("templates");
    await tab("订阅组");
    await page.getByRole("button", { name: "新增订阅组", exact: true }).click();
    await dialog.getByLabel("名称", { exact: true }).fill(tag + "-group");
    const nodes = dialog
      .locator("fieldset")
      .filter({ has: page.locator("legend").filter({ hasText: "独立节点" }) });
    await nodes.getByLabel("搜索独立节点", { exact: true }).fill(tag);
    await nodes.getByText(tag + "-A", { exact: true }).click();
    await nodes.getByText(tag + "-B", { exact: true }).click();
    await nodes
      .getByRole("button", { name: "上移 " + tag + "-B", exact: true })
      .click();
    const operators = dialog
      .locator("fieldset")
      .filter({ has: page.locator("legend").filter({ hasText: "处理链" }) });
    await operators.getByLabel("搜索处理链", { exact: true }).fill(tag);
    await operators.getByText(tag + "-chain", { exact: true }).click();
    const templates = dialog
      .locator("fieldset")
      .filter({ has: page.locator("legend").filter({ hasText: "输出模板" }) });
    await templates.getByLabel("搜索输出模板", { exact: true }).fill(tag);
    await templates.getByText(tag + "-template", { exact: true }).click();
    const users = dialog
      .locator("fieldset")
      .filter({
        has: page.locator("legend").filter({ hasText: "应用处理链的用户" }),
      });
    await users
      .getByLabel("搜索应用处理链的用户", { exact: true })
      .fill(s.test_users[0].email);
    await users.getByText(s.test_users[0].email, { exact: true }).click();
    await dialog
      .getByRole("button", { name: "高级 JSON", exact: true })
      .click();
    const advanced = JSON.parse(
      await dialog.getByLabel("profiles JSON").inputValue(),
    );
    if (
      advanced.node_ids[0] !== imported.ids[1] ||
      advanced.operator_ids[0] !== chain.id ||
      advanced.template_ids[0] !== template.id
    )
      throw Error("Ordered selector lost IDs");
    await dialog.getByRole("button", { name: "返回表单", exact: true }).click();
    const group = await save("profiles");
    const config = await json("POST", "/admin/profiles/preview", {
      id: group.id,
      target: "clash",
    });
    if (
      config.node_count !== 1 ||
      !config.output.includes("R2-") ||
      !config.output.includes("1.1.1.1")
    )
      throw Error("Group/template output mismatch");
    checks.push(
      "searchable node/chain/template/user selectors and order preserve actual arrays; assigned template converts profile",
    );
    await tab("处理链");
    const chainCard = page
      .locator(".inventory-item")
      .filter({
        has: page.getByRole("heading", { name: tag + "-chain", exact: true }),
      });
    await chainCard.getByRole("button", { name: "编辑", exact: true }).click();
    await dialog.getByLabel("启用步骤 1", { exact: true }).click();
    await save("operators");
    checks.push(
      "UI disables first filter before assigning transform to Gate subscription",
    );
    // User's actual subscription must exclude external inventory; only rename valid Gate grants.
    const userContext = await browser.newContext();
    const up = await userContext.newPage();
    await up.goto("https://sub.caowo.baby/");
    const user = await up.evaluate(async (creds) => {
      const login = await fetch("/api/v1/auth/login", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(creds),
      });
      if (login.status !== 200) throw Error("user login failed");
      const info = await (await fetch("/api/v1/subscription")).json();
      const text = await (
        await fetch(new URL(info.url).pathname + "?target=clash")
      ).text();
      return { text, node_count: info.node_count };
    }, s.test_users[0]);
    if (
      !user.text.includes("154.9.26.146") ||
      user.text.includes("example.com") ||
      user.text.includes("example.org")
    )
      throw Error("External inventory leaked into user subscription");
    await userContext.close();
    require("child_process").execFileSync(
      "python",
      ["deploy/verify-live-client.py"],
      { stdio: "pipe" },
    );
    checks.push(
      "real Xray clients connect with assigned rename/template and independent credentials",
    );
    checks.push(
      "assigned profile never adds private inventory to ordinary user Gate subscription",
    );
    await tab("手动节点");
    await page.getByLabel("搜索手动节点", { exact: true }).fill(tag);
    await page
      .getByRole("heading", { name: tag + "-A", exact: true })
      .waitFor();
    for (const width of [1440, 1280, 768, 390]) {
      await page.setViewportSize({ width, height: 1000 });
      if (
        await page.evaluate(
          () => document.documentElement.scrollWidth > innerWidth,
        )
      )
        throw Error("Inventory overflow " + width);
      await page.screenshot({
        path: `docs/evidence/inventory-live-${width}.png`,
        fullPage: true,
        mask: [page.locator(".sensitive-output")],
      });
    }
    await page.setViewportSize({ width: 1440, height: 1000 });
    await page.getByLabel("选择当前页", { exact: true }).click();
    const failed = imported.ids[1];
    await context.route("**/api/v1/admin/resources/nodes/" + failed, (r) =>
      r.request().method() === "DELETE"
        ? r.fulfill({
            status: 409,
            contentType: "application/json",
            body: JSON.stringify({ error: { code: "injected_failure" } }),
          })
        : r.continue(),
    );
    await page.getByRole("button", { name: "批量删除", exact: true }).click();
    await dialog.getByRole("button", { name: "确认删除", exact: true }).click();
    await page.getByText(/成功 1 项；失败 1 项/).waitFor();
    await context.unroute("**/api/v1/admin/resources/nodes/" + failed);
    checks.push(
      "batch partial failure shows exact counts, retains failed selection; injected failure labelled",
    );
    if (errors.length) throw Error("page runtime errors");
    fs.writeFileSync(
      "docs/evidence/inventory-live-flow.json",
      JSON.stringify(
        {
          version: (await json("GET", "/admin/overview")).version,
          date: new Date().toISOString(),
          checks,
          errors,
        },
        null,
        2,
      ),
    );
    console.log(
      "Live inventory, pipeline, profile, template and partial-failure browser acceptance passed",
    );
  } finally {
    for (const [kind, id] of owned.reverse()) {
      const status = await page.evaluate(
        async ({ kind, id }) =>
          (
            await fetch("/api/v1/admin/resources/" + kind + "/" + id, {
              method: "DELETE",
            })
          ).status,
        { kind, id },
      );
      if (status !== 200 && status !== 404)
        throw Error("Owned acceptance resource cleanup failed");
    }
    await browser.close();
  }
})().catch(async (e) => {
  if (browser) await browser.close();
  console.error(e.message.replace(/cookie: [^\n]+/gi, "cookie: [REDACTED]"));
  process.exitCode = 1;
});
