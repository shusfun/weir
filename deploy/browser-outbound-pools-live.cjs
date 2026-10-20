require("./production-guard.cjs");
const { chromium } = require("playwright"),
  fs = require("node:fs"),
  path = require("node:path");
const f = require("./browser-fixture.cjs").fixture();
const fixture = JSON.parse(
  fs.readFileSync(
    path.join(process.env.LOCALAPPDATA, "MiGate/operations/pool-fixture.json"),
    "utf8",
  ),
);
if (fixture.prefix !== process.env.MIGATE_FIXTURE_PREFIX)
  throw Error("Fixture does not belong to current lease");
let browser,
  stage = "start";
(async () => {
  browser = await chromium.launch({ channel: "chrome", args: f.args });
  const context = await browser.newContext({
    viewport: { width: 1440, height: 1000 },
  });
  context.setDefaultTimeout(15000);
  await context.addCookies([
    {
      name: "migate_session",
      value: f.s.admin_cookie.slice("migate_session=".length),
      ...f.cookie,
    },
  ]);
  const page = await context.newPage(),
    base = `/admin/devices/${fixture.device}/pools`,
    checks = [];
  async function req(method, url, data, status = 200) {
    return page.evaluate(
      async ({ method, url, data, status }) => {
        const r = await fetch("/api/v1" + url, {
          method,
          headers: { "Content-Type": "application/json" },
          body: data === undefined ? undefined : JSON.stringify(data),
        });
        if (r.status !== status)
          throw Error("Live UI fixture HTTP " + r.status);
        return r.json();
      },
      { method, url, data, status },
    );
  }
  async function shots(name, locator) {
    for (const width of [1440, 1280, 768, 390]) {
      await page.setViewportSize({ width, height: 1000 });
      await locator.scrollIntoViewIfNeeded();
      const box = await locator.boundingBox();
      if (
        !box ||
        box.x < 0 ||
        box.x + box.width > width + 1 ||
        (await locator.evaluate((e) => e.scrollWidth > e.clientWidth + 1))
      )
        throw Error("Live pool layout clipped");
      await locator.screenshot({
        path: `docs/evidence/live-pool-${name}-${width}.png`,
      });
      const close = locator.getByRole("button", {
        name: /^(关闭|取消)$/,
        exact: true,
      });
      if (await close.count()) {
        await close.last().scrollIntoViewIfNeeded();
        const footer = await close.last().boundingBox(),
          bounds = await locator.boundingBox();
        if (
          !footer ||
          footer.y < bounds.y ||
          footer.y + footer.height > bounds.y + bounds.height + 1
        )
          throw Error("Live pool actions clipped");
        await locator.screenshot({
          path: `docs/evidence/live-pool-${name}-actions-${width}.png`,
        });
      }
    }
  }
  const poolRow = () =>
    page.locator(".list-row").filter({ hasText: fixture.prefix + "UI 出口池" });
  async function waitApplied() {
    const deadline = Date.now() + 90000;
    while (Date.now() < deadline) {
      const pool = (await req("GET", base)).items.find(
        (p) => p.name === fixture.prefix + "UI 出口池",
      );
      if (!pool || pool.status === "applied") return pool;
      if (pool.status === "failed")
        throw Error("Actual Gate pool apply failed");
      await new Promise((r) => setTimeout(r, 500));
    }
    throw Error("Gate pool receipt timeout");
  }
  await page.goto(f.origin + `/devices/${fixture.device}?tab=outbounds`);
  await page.getByRole("button", { name: "创建出站池", exact: true }).click();
  let dialog = page.getByRole("dialog");
  async function fields() {
    await dialog
      .getByLabel("名称", { exact: true })
      .fill(fixture.prefix + "UI 出口池");
    await dialog.getByRole("combobox", { name: "订阅源", exact: true }).click();
    await page
      .getByRole("option", { name: fixture.source_name, exact: true })
      .click();
  }
  await fields();
  await shots("create", dialog);
  await dialog.getByRole("button", { name: "取消", exact: true }).click();
  if (
    (await req("GET", base)).items.some(
      (p) => p.name === fixture.prefix + "UI 出口池",
    )
  )
    throw Error("Cancel saved pool");
  await page.getByRole("button", { name: "创建出站池", exact: true }).click();
  dialog = page.getByRole("dialog");
  await fields();
  await dialog.getByRole("button", { name: "保存", exact: true }).click();
  await dialog.waitFor({ state: "hidden" });
  stage = "preview";
  await poolRow()
    .getByRole("button", { name: "节点与应用", exact: true })
    .click();
  dialog = page.getByRole("dialog");
  await dialog.getByRole("button", { name: "刷新并预览", exact: true }).click();
  await dialog.getByText("去重 1 项", { exact: false }).waitFor();
  await shots("preview", dialog);
  await dialog
    .getByRole("textbox", { name: "筛选出口节点", exact: true })
    .fill("Loopback");
  await dialog
    .getByRole("button", { name: "选择筛选结果", exact: true })
    .click();
  await dialog
    .getByRole("textbox", { name: "筛选出口节点", exact: true })
    .fill("");
  if ((await dialog.locator("input[type=checkbox]:checked").count()) !== 1)
    throw Error("Live filtered selection failed");
  if (
    !(await dialog
      .getByRole("checkbox", { name: "选择 Snell 尚不支持托管", exact: true })
      .isDisabled())
  )
    throw Error("Unsupported managed node selectable");
  await dialog
    .getByRole("button", { name: "应用所选节点", exact: true })
    .click();
  await shots("confirm", dialog);
  await dialog.getByRole("button", { name: "返回选择", exact: true }).click();
  await dialog
    .getByRole("button", { name: "应用所选节点", exact: true })
    .click();
  await dialog.getByRole("button", { name: "确认提交", exact: true }).click();
  await dialog.waitFor({ state: "hidden" });
  const applied = await waitApplied();
  if (applied.member_count !== 1 || !applied.enabled)
    throw Error("Actual pool membership differs");
  await page.reload();
  await poolRow().getByText("已生效", { exact: true }).waitFor();
  await shots("applied", page.locator("main"));
  stage = "auto settings and full pool order";
  await poolRow().getByRole("button", {name:"编辑",exact:true}).click();
  dialog = page.getByRole("dialog");
  await dialog.getByRole("switch", {name:"自动同步全部可托管节点",exact:true}).check();
  await dialog.getByRole("switch", {name:"在手动出站前展示",exact:true}).check();
  await shots("auto-settings", dialog);
  await dialog.getByRole("button", {name:"取消",exact:true}).click();
  if ((await req("GET",base)).items.find(p=>p.id===applied.id).auto_refresh)
    throw Error("Cancelled auto settings persisted");
  await poolRow().getByRole("button", {name:"编辑",exact:true}).click();
  dialog = page.getByRole("dialog");
  await dialog.getByRole("switch", {name:"自动同步全部可托管节点",exact:true}).check();
  await dialog.getByRole("switch", {name:"在手动出站前展示",exact:true}).check();
  await dialog.getByRole("button", {name:"保存",exact:true}).click();
  await dialog.waitFor({state:"hidden"});
  const second = await req("POST",base,{name:fixture.prefix+"UI order pool",source_id:applied.source_id});
  await page.reload();
  await page.getByRole("button",{name:"调整池顺序",exact:true}).click();
  dialog=page.getByRole("dialog");
  await dialog.getByRole("button",{name:"下移 "+fixture.prefix+"UI 出口池",exact:true}).click();
  await shots("pool-order",dialog);
  await dialog.getByRole("button",{name:"取消",exact:true}).click();
  if ((await req("GET",base)).items.some(p=>p.priority)) throw Error("Cancelled pool order persisted");
  await page.getByRole("button",{name:"调整池顺序",exact:true}).click();
  dialog=page.getByRole("dialog");
  await dialog.getByRole("button",{name:"下移 "+fixture.prefix+"UI 出口池",exact:true}).click();
  await dialog.getByRole("button",{name:"保存池顺序",exact:true}).click();
  await dialog.waitFor({state:"hidden"});
  const ordered=(await req("GET",base)).items;
  if(ordered.find(p=>p.id===applied.id).priority!==20 || ordered.find(p=>p.id===second.id).priority!==10)
    throw Error("Live complete pool order lost");
  await poolRow().getByRole("button",{name:"节点与应用",exact:true}).click();
  dialog=page.getByRole("dialog");
  await dialog.getByRole("button",{name:"预览缓存",exact:true}).click();
  if(!(await dialog.getByRole("checkbox",{name:"选择 Loopback acceptance",exact:true}).isDisabled()))
    throw Error("Automatic pool permits partial selection");
  await shots("auto-preview",dialog);
  await dialog.getByRole("button",{name:"应用所选节点",exact:true}).click();
  await dialog.getByRole("button",{name:"确认提交",exact:true}).click();
  await dialog.waitFor({state:"hidden"});
  await waitApplied();
  await page.getByRole("button",{name:"刷新全部来源",exact:true}).click();
  await page.getByRole("status").filter({hasText:"来源已刷新，请预览应用"}).waitFor();
  await shots("refresh-report",page.locator("main"));
  checks.push("Live auto/prepend cancel/save, all-supported selection, complete pool order cancel/submit/reload, real per-pool full refresh");
  checks.push(
    "Actual domain UI create/cancel/source label/fresh fetch/dedup/filter/select/confirm/cancel; real Agent receipt and inventory refresh",
  );
  stage = "retry";
  await poolRow()
    .getByRole("button", { name: "节点与应用", exact: true })
    .click();
  dialog = page.getByRole("dialog");
  await page.route("**/pools/*/preview", (r) =>
    r.fulfill({
      status: 502,
      contentType: "application/json",
      body: '{"error":{"code":"pool_refresh_failed_previous_preserved"}}',
    }),
  );
  await dialog.getByRole("button", { name: "刷新并预览", exact: true }).click();
  await dialog.getByRole("alert").filter({ hasText: "上游刷新失败" }).waitFor();
  await shots("error", dialog);
  await page.unroute("**/pools/*/preview");
  await dialog.getByRole("button", { name: "预览缓存", exact: true }).click();
  await dialog.getByText("去重 1 项", { exact: false }).waitFor();
  await dialog.getByRole("button", { name: "关闭", exact: true }).click();
  stage = "disable/delete";
  await poolRow()
    .getByRole("button", { name: "节点与应用", exact: true })
    .click();
  dialog = page.getByRole("dialog");
  await dialog.getByRole("button", { name: "停用", exact: true }).click();
  await shots("disable", dialog);
  await dialog.getByRole("button", { name: "确认提交", exact: true }).click();
  await dialog.waitFor({ state: "hidden" });
  const disabled = await waitApplied();
  if (disabled.member_count !== 0 || disabled.enabled)
    throw Error("Actual disabled pool retained members");
  const currentSecond=(await req("GET",base)).items.find(p=>p.id===second.id);
  const removal=await req("POST",base+`/${second.id}/apply`,{version:currentSecond.version,enabled:false,delete:true,confirm:true},202);
  const removalDeadline=Date.now()+90000;
  while(Date.now()<removalDeadline){
    if(!(await req("GET",base)).items.some(p=>p.id===second.id))break;
    await new Promise(r=>setTimeout(r,500));
  }
  if((await req("GET",base)).items.some(p=>p.id===second.id))throw Error("Ordering pool removal failed");
  await page.reload();
  await poolRow()
    .getByRole("button", { name: "节点与应用", exact: true })
    .click();
  dialog = page.getByRole("dialog");
  await dialog.getByRole("button", { name: "删除池", exact: true }).click();
  await dialog.getByRole("button", { name: "确认提交", exact: true }).click();
  await dialog.waitFor({ state: "hidden" });
  if (await waitApplied()) throw Error("Actual pool deletion failed");
  checks.push(
    "Injected browser 502 retry is UI evidence only; real Agent disable/delete and confirmed membership removal",
  );
  const ordinary = await browser.newContext();
  await ordinary.addCookies([
    {
      name: "migate_session",
      value: f.sessions[f.s.test_users[0].email].slice(
        "migate_session=".length,
      ),
      ...f.cookie,
    },
  ]);
  const user = await ordinary.newPage();
  await user.goto(f.origin + "/subscription");
  if (
    (await user.evaluate(
      async (base) => (await fetch("/api/v1" + base)).status,
      base,
    )) !== 403
  )
    throw Error("Ordinary pool access");
  await ordinary.close();
  const version = (await req("GET", "/admin/overview")).version;
  fs.writeFileSync(
    "docs/evidence/live-outbound-pools-browser.json",
    JSON.stringify(
      {
        date: new Date().toISOString(),
        version,
        result: "passed",
        checks,
        visual: "manual screenshot review pending",
      },
      null,
      2,
    ),
  );
  await browser.close();
  browser = null;
})().catch(async (e) => {
  fs.writeFileSync(
    "docs/evidence/live-pool-browser-failure.json",
    JSON.stringify(
      {
        date: new Date().toISOString(),
        stage,
        detail: String(e.message).split("\n")[0],
      },
      null,
      2,
    ),
  );
  console.error(stage + ": " + String(e.message).split("\n")[0]);
  if (browser) await browser.close();
  process.exitCode = 1;
});
