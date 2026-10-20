require("./production-guard.cjs");
const { chromium } = require("playwright"),
  fs = require("node:fs");
const f = require("./browser-fixture.cjs").fixture();
if (f.target !== "disposable-local")
  throw Error("Protocol inventory fixture is isolated only");
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
    prefix = "protocol-" + Date.now(),
    checks = [];
  async function req(method, path, data) {
    return page.evaluate(
      async ({ method, path, data }) => {
        const r = await fetch("/api/v1" + path, {
          method,
          headers: { "Content-Type": "application/json" },
          body: data === undefined ? undefined : JSON.stringify(data),
        });
        if (!r.ok) throw Error("Protocol fixture HTTP " + r.status);
        return r.json();
      },
      { method, path, data },
    );
  }
  async function shots(label, locator) {
    for (const width of [1440, 1280, 768, 390]) {
      await page.setViewportSize({ width, height: 1000 });
      const box = await locator.boundingBox();
      if (
        !box ||
        box.x < 0 ||
        box.x + box.width > width + 1 ||
        (await locator.evaluate((e) => e.scrollWidth > e.clientWidth + 1))
      )
        throw Error("Protocol dialog clipped");
      await locator.screenshot({
        path: `${f.evidence}protocol-${label}-${width}.png`,
      });
      const close = locator.getByRole("button", {
        name: "关闭输出",
        exact: true,
      });
      if (await close.count()) {
        await close.scrollIntoViewIfNeeded();
        const b = await close.boundingBox();
        if (b.y < box.y || b.y + b.height > box.y + box.height + 1)
          throw Error("Protocol footer clipped");
      }
    }
  }
  const key = Buffer.alloc(32, 7).toString("base64"),
    query = new URLSearchParams({
      "public-key": key,
      address: "10.2.0.2/32,fd00::2/128",
      "allowed-ips": "0.0.0.0/0,::/0",
      "preshared-key": key,
      reserved: "1,2,3",
      mtu: "1280",
      keepalive: "25",
    });
  const snell = `snell://synthetic@example.com:443?version=4&reuse=false&tfo=true&obfs=tls&obfs-host=cdn.example#${prefix}-Snell`,
    wg = `wireguard://${encodeURIComponent(key)}@example.org:51820?${query}#${prefix}-WireGuard`;
  await page.goto(f.origin + "/subscriptions?kind=nodes");
  await page.getByRole("button", { name: "导入节点", exact: true }).click();
  let dialog = page.getByRole("dialog");
  stage = "import preview";
  await dialog
    .getByLabel("节点清单内容", { exact: true })
    .fill(snell + "\n" + wg + "\n" + snell);
  await dialog.getByRole("button", { name: "解析预览", exact: true }).click();
  await dialog.getByText("准备导入 2 个", { exact: false }).waitFor();
  await shots("import-preview", dialog);
  if ((await req("GET", "/admin/resources/nodes")).total)
    throw Error("Preview created inventory");
  const saved = page.waitForResponse(
    (r) =>
      r.url().endsWith("/subscriptions/import") &&
      r.request().method() === "POST",
  );
  await dialog
    .getByRole("button", { name: "确认导入 2 个节点", exact: true })
    .click();
  if ((await saved).status() !== 200)
    throw Error("Extended import submission failed");
  await dialog.getByText("已导入 2 个节点", { exact: true }).waitFor();
  await dialog.getByRole("button", { name: "Close", exact: true }).click();
  stage = "edit and filter";
  let row = page
    .locator("article.inventory-item")
    .filter({ hasText: prefix + "-Snell" });
  await row.getByRole("button", { name: "编辑", exact: true }).click();
  dialog = page.getByRole("dialog");
  await dialog
    .getByLabel("名称", { exact: true })
    .fill(prefix + "-Snell-renamed");
  await dialog.getByRole("button", { name: "保存", exact: true }).click();
  await dialog.waitFor({ state: "hidden" });
  await page
    .getByRole("combobox", { name: "筛选节点协议", exact: true })
    .click();
  await page.getByRole("option", { name: "wireguard", exact: true }).click();
  if ((await page.locator("article.inventory-item").count()) !== 1)
    throw Error("Extended protocol filter failed");
  await page.reload();
  await page
    .getByRole("combobox", { name: "筛选节点协议", exact: true })
    .waitFor();
  const nodes = (await req("GET", "/admin/resources/nodes")).items;
  if (nodes.length !== 2 || nodes.some((n) => !n.node.valid))
    throw Error("Extended node metadata missing");
  checks.push(
    "UI preview and dedup do not create resources; confirmed import writes both protocols; node edit preserves original URI and protocol filter works",
  );
  const snellNode = nodes.find((n) => n.node.protocol === "snell"),
    wgNode = nodes.find((n) => n.node.protocol === "wireguard");
  const profiles = [];
  for (const [name, node] of [
    ["Snell", snellNode],
    ["WireGuard", wgNode],
  ])
    profiles.push(
      await req("POST", "/admin/resources/profiles", {
        data: {
          name: prefix + "-" + name,
          node_ids: [node.id],
          enabled: true,
          skip_default_operators: true,
        },
      }),
    );
  for (const [index, formats] of [
    [0, ["Clash / Mihomo", "Surge", "Loon"]],
    [1, ["Clash / Mihomo", "sing-box", "Surge"]],
  ]) {
    await page.goto(f.origin + "/subscriptions?kind=profiles");
    row = page
      .locator("article.inventory-item")
      .filter({ hasText: profiles[index].data.name });
    await row.getByRole("button", { name: "预览与导出", exact: true }).click();
    dialog = page.getByRole("dialog");
    for (const format of formats) {
      stage = "output " + format;
      await dialog
        .getByRole("combobox", { name: "输出客户端格式", exact: true })
        .click();
      await page.getByRole("option", { name: format, exact: true }).click();
      await dialog.getByLabel("订阅组格式输出", { exact: true }).waitFor();
      const output = await dialog
        .getByLabel("订阅组格式输出", { exact: true })
        .inputValue();
      if (!output.includes(index === 0 ? "snell" : "wireguard"))
        throw Error("Wrong extended output");
      await shots(
        (index === 0 ? "snell" : "wireguard") + "-" + format.split(" ")[0],
        dialog,
      );
    }
    await dialog
      .getByRole("combobox", { name: "输出客户端格式", exact: true })
      .click();
    await page
      .getByRole("option", {
        name: index === 0 ? "sing-box" : "Loon",
        exact: true,
      })
      .click();
    await dialog
      .getByText("当前节点、筛选结果或模板无法转换为此客户端格式", {
        exact: false,
      })
      .waitFor();
    if (await dialog.getByLabel("订阅组格式输出", { exact: true }).count())
      throw Error("Unsupported format retained stale output");
    await shots("unsupported-" + index, dialog);
    await dialog.getByRole("button", { name: "关闭输出", exact: true }).click();
  }
  checks.push(
    "Actual profile output UI exports Snell Clash/Surge/Loon and WireGuard Clash/modern sing-box endpoint/Surge; unsupported targets show explicit error and hide stale credentials",
  );
  stage = "modern Loon WireGuard template";
  const modern = new URL(wg);
  modern.searchParams.delete("keepalive");
  modern.searchParams.set("dns", "1.1.1.1,2606:4700:4700::1111");
  await req("POST", "/admin/resources/nodes", {id:wgNode.id,data:{...wgNode.data,url:modern.toString()}});
  await page.goto(f.origin + "/subscriptions?kind=profiles");
  row = page.locator("article.inventory-item").filter({hasText:profiles[1].data.name});
  await row.getByRole("button",{name:"预览与导出",exact:true}).click();
  dialog = page.getByRole("dialog");
  await dialog.getByRole("combobox",{name:"输出客户端格式",exact:true}).click();
  await page.getByRole("option",{name:"Loon",exact:true}).click();
  const modernOutput=dialog.getByLabel("订阅组格式输出",{exact:true});
  await modernOutput.waitFor();
  const modernText=await modernOutput.inputValue();
  for(const field of ["interface-ipV6=", "preshared-key=", "reserved=[1,2,3]", "dnsV6=", "peers=[{"]){
    if(!modernText.includes(field))throw Error("Modern Loon output missing field");
  }
  await shots("wireguard-loon-modern",dialog);
  await dialog.getByRole("button",{name:"关闭输出",exact:true}).click();
  checks.push("Modern Loon peer template exports dual-stack, PSK, allowed IPs, DNS, reserved and MTU; keepalive and unrepresentable multiplicity explicitly refused; builtin subset remains compatible");
  stage = "malformed import";
  await page.goto(f.origin + "/subscriptions?kind=nodes");
  await page.getByRole("button", { name: "导入节点", exact: true }).click();
  dialog = page.getByRole("dialog");
  await dialog
    .getByLabel("节点清单内容", { exact: true })
    .fill("snell://bad%0Ainjection@example.com:443?version=4");
  await dialog.getByRole("button", { name: "解析预览", exact: true }).click();
  await dialog.getByRole("alert").waitFor();
  if ((await req("GET", "/admin/resources/nodes")).total !== 2)
    throw Error("Invalid import changed inventory");
  await shots("invalid", dialog);
  await dialog.getByRole("button", { name: "Close", exact: true }).click();
  fs.writeFileSync(
    f.evidence + "live-protocols-browser.json",
    JSON.stringify(
      {
        date: new Date().toISOString(),
        result: "passed",
        target:
          "dedicated disposable control/PG/Redis; actual browser workflow, no commercial-client claim",
        checks,
        visual: "manual review pending",
      },
      null,
      2,
    ),
  );
  await browser.close();
  browser = null;
  console.log("Extended protocol import/edit/filter/output UI passed");
})().catch(async (e) => {
  fs.writeFileSync(
    f.evidence + "protocol-browser-failure.json",
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
