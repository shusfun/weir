// Dedicated local UI + real control/PG/Redis; signed controlled device receipts.
// Kernel/network verification runs separately against the actual Go Agent.
require("./production-guard.cjs");
const { chromium } = require("playwright"),
  fs = require("node:fs"),
  cp = require("node:child_process"),
  crypto = require("node:crypto");
const f = require("./browser-fixture.cjs").fixture();
if (f.target !== "disposable-local")
  throw Error("Controlled device fixture is local only");
let browser,
  stage = "start";
(async () => {
  browser = await chromium.launch({ channel: "chrome", args: f.args });
  const context = await browser.newContext({
    viewport: { width: 1440, height: 1000 },
  });
  await context.addCookies([
    {
      name: "migate_session",
      value: f.s.admin_cookie.slice("migate_session=".length),
      ...f.cookie,
    },
  ]);
  context.setDefaultTimeout(15000);
  const page = await context.newPage(),
    prefix = "local-pool-" + Date.now(),
    checks = [];
  async function req(method, url, data, status = 200, bearer) {
    return page.evaluate(
      async ({ method, url, data, status, bearer }) => {
        const r = await fetch("/api/v1" + url, {
          method,
          headers: {
            "Content-Type": "application/json",
            ...(bearer ? { Authorization: "Bearer " + bearer } : {}),
          },
          body: data === undefined ? undefined : JSON.stringify(data),
        });
        if (r.status !== status)
          throw Error(
            "Fixture HTTP " + r.status + " " + url + " expected " + status,
          );
        return r.json();
      },
      { method, url, data, status, bearer },
    );
  }
  async function shots(label, locator) {
    for (const width of [1440, 1280, 768, 390]) {
      await page.setViewportSize({ width, height: 1000 });
      await locator.scrollIntoViewIfNeeded();
      const box = await locator.boundingBox();
      if (
        !box ||
        box.x < 0 ||
        box.x + box.width > width + 1 ||
        (await page.evaluate(
          () => document.documentElement.scrollWidth > innerWidth,
        ))
      )
        throw Error("Pool layout clipped");
      await locator.screenshot({
        path: `${f.evidence}pool-${label}-${width}.png`,
      });
      if ((await locator.getAttribute("role")) === "dialog") {
        const close = locator.getByRole("button", {
          name: /^(关闭|取消)$/,
          exact: true,
        });
        if (await close.count()) {
          await close.last().scrollIntoViewIfNeeded();
          const footer = await close.last().boundingBox();
          const bounds = await locator.boundingBox();
          if (
            !footer ||
            footer.y < bounds.y ||
            footer.y + footer.height > bounds.y + bounds.height + 1
          )
            throw Error("Pool footer clipped");
          await locator.screenshot({
            path: `${f.evidence}pool-${label}-actions-${width}.png`,
          });
        }
      }
    }
  }
  const { publicKey, privateKey } = crypto.generateKeyPairSync("ed25519");
  await page.goto(f.origin + "/devices");
  const paired = await req(
    "POST",
    "/devices/pair",
    {
      name: prefix,
      public_key: publicKey
        .export({ format: "der", type: "spki" })
        .subarray(-32)
        .toString("base64"),
    },
    201,
  );
  const device = paired.device_id,
    base = `/admin/devices/${device}/pools`;
  await req("POST", `/admin/devices/${device}/approve`, {
    code: paired.pair_code,
    fingerprint: paired.fingerprint,
  });
  const challenge = await req("POST", "/devices/challenge", {
    device_id: device,
  });
  const session = await req("POST", "/devices/auth", {
    device_id: device,
    challenge: challenge.challenge,
    signature: crypto
      .sign(null, Buffer.from(challenge.challenge), privateKey)
      .toString("base64"),
  });
  await req(
    "POST",
    "/agent/heartbeat",
    {
      version: "isolated-signed-protocol-fixture",
      capabilities: [
        "outbound.pool.v1",
        "outbound.tools.v1",
        "outbound.transport.v1",
        "resource.enabled.v1",
      ],
      traffic_status: "unknown",
    },
    200,
    session.token,
  );
  const source = await req("POST", "/admin/resources/sources", {
    data: {
      name: prefix + " source",
      url: "https://example.com/local-pool",
      refresh_seconds: 86400,
      enabled: true,
    },
  });
  // Seed only server-owned cache in the dedicated disposable PG container.
  const seed = {
    links: [
      "http://u:synthetic@example.com:8080#香港出口 · 长名称验收",
      "http://u:synthetic@example.com:8080#duplicate",
      "trojan://synthetic@example.org:443?sni=example.org#备用出口",
      "snell://synthetic@example.com:443?version=4#Snell 尚不支持托管",
    ],
    node_count: 4,
    last_update: new Date().toISOString().replace(/\.\d{3}Z$/, "Z"),
    next_refresh: new Date(Date.now() + 86400000).toISOString(),
  };
  const encoded = Buffer.from(JSON.stringify(seed)).toString("base64");
  const sql = `UPDATE migate.resources SET data=data || convert_from(decode('${encoded}','base64'),'UTF8')::jsonb WHERE id=${source.id} AND kind='sources' AND data->>'name'='${prefix} source';`;
  const seeded = cp.spawnSync(
    "docker",
    [
      "compose",
      "-f",
      "deploy/test.compose.yml",
      "exec",
      "-T",
      "postgres",
      "psql",
      "-U",
      "gate",
      "-d",
      "gate",
      "-v",
      "ON_ERROR_STOP=1",
    ],
    { input: sql, encoding: "utf8", windowsHide: true },
  );
  if (seeded.status !== 0 || !seeded.stdout.includes("UPDATE 1"))
    throw Error("Dedicated source cache seed failed");
  await page.goto(`${f.origin}/devices/${device}?tab=outbounds`);

  await page.getByRole("button", { name: "创建出站池", exact: true }).waitFor();
  await shots("empty", page.locator("main"));
  await page.getByRole("button", { name: "创建出站池", exact: true }).click();
  let dialog = page.getByRole("dialog");
  await dialog
    .getByLabel("名称", { exact: true })
    .fill(prefix + " · 华东出口池");
  await dialog.getByRole("combobox", { name: "订阅源", exact: true }).click();
  await page
    .getByRole("option", { name: prefix + " source", exact: true })
    .click();
  if (
    !(
      await dialog
        .getByRole("combobox", { name: "订阅源", exact: true })
        .innerText()
    ).includes("source")
  )
    throw Error("Source selector label missing");
  await shots("create", dialog);
  await dialog.getByRole("button", { name: "取消", exact: true }).click();
  if ((await req("GET", base)).items.length) throw Error("Cancel saved pool");
  await page.getByRole("button", { name: "创建出站池", exact: true }).click();
  dialog = page.getByRole("dialog");
  await dialog
    .getByLabel("名称", { exact: true })
    .fill(prefix + " · 华东出口池");
  await dialog.getByRole("combobox", { name: "订阅源", exact: true }).click();
  await page
    .getByRole("option", { name: prefix + " source", exact: true })
    .click();
  await dialog.getByRole("button", { name: "保存", exact: true }).click();
  await dialog.waitFor({ state: "hidden" });
  await page.getByRole("button", { name: "节点与应用", exact: true }).click();
  dialog = page.getByRole("dialog");
  await dialog.getByRole("button", { name: "预览缓存", exact: true }).click();
  await dialog.getByText("去重 1 项", { exact: false }).waitFor();
  await shots("preview", dialog);
  await dialog
    .getByRole("textbox", { name: "筛选出口节点", exact: true })
    .fill("香港");
  await dialog
    .getByRole("button", { name: "选择筛选结果", exact: true })
    .click();
  await dialog
    .getByRole("textbox", { name: "筛选出口节点", exact: true })
    .fill("");
  if ((await dialog.locator("input[type=checkbox]:checked").count()) !== 1)
    throw Error("Filtered selection incorrect");
  if (
    !(await dialog
      .getByRole("checkbox", { name: "选择 Snell 尚不支持托管", exact: true })
      .isDisabled())
  )
    throw Error("Unsupported node selectable");
  await dialog
    .getByRole("button", { name: "应用所选节点", exact: true })
    .click();
  await shots("confirm", dialog);
  await dialog.getByRole("button", { name: "返回选择", exact: true }).click();
  if ((await req("GET", base)).items[0].status !== "unapplied")
    throw Error("Confirmation cancel queued task");
  await dialog
    .getByRole("button", { name: "应用所选节点", exact: true })
    .click();
  const response = page.waitForResponse(
    (r) => r.url().endsWith("/apply") && r.request().method() === "POST",
  );
  await dialog.getByRole("button", { name: "确认提交", exact: true }).click();
  if ((await response).status() !== 202) throw Error("Apply form rejected");
  await dialog.waitFor({ state: "hidden" });
  let pool = (await req("GET", base)).items[0];
  if (pool.member_count !== 0 || pool.status !== "pending")
    throw Error("Pending represented as applied");
  stage = "signed receipt";
  const tasks = await req("GET", "/agent/tasks", undefined, 200, session.token);
  if (
    tasks.items.length !== 1 ||
    tasks.items[0].operation !== "outbound.pool.apply"
  )
    throw Error("Unexpected pool task");
  const task = tasks.items[0];
  const receipt = {
    task_id: task.id,
    revision: task.revision,
    status: "applied",
    data: { applied: true, source: "controlled-local-device-receipt" },
  };
  await req("POST", "/agent/results", receipt, 200, session.token);
  await req("POST", "/agent/results", receipt, 200, session.token);
  await page.reload();
  await page.getByText("已生效 1 个", { exact: false }).waitFor();
  await shots("applied", page.locator("main"));
  checks.push(
    "Actual UI create/cancel, source name selection, cache dedup, unsupported refusal, search selection, confirmation/cancel, queued state, authenticated idempotent receipt, reload",
  );
  stage = "failure and restore";
  await page.getByRole("button", { name: "节点与应用", exact: true }).click();
  dialog = page.getByRole("dialog");
  await page.route("**/pools/*/preview", (r) =>
    r.fulfill({
      status: 502,
      contentType: "application/json",
      body: '{"error":{"code":"pool_refresh_failed_previous_preserved"}}',
    }),
  );
  await dialog.getByRole("button", { name: "刷新并预览", exact: true }).click();
  await dialog
    .getByText("上游刷新失败，已保留原缓存和已生效出口。", { exact: false })
    .waitFor();
  await shots("error", dialog);
  await page.unroute("**/pools/*/preview");
  await dialog.getByRole("button", { name: "预览缓存", exact: true }).click();
  await dialog
    .getByRole("textbox", { name: "筛选出口节点", exact: true })
    .fill("no-match");
  await dialog.getByText("没有匹配的节点", { exact: true }).waitFor();
  await shots("no-match", dialog);
  await dialog.getByRole("button", { name: "关闭", exact: true }).click();
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
      async (url) => (await fetch("/api/v1" + url)).status,
      base,
    )) !== 403
  )
    throw Error("Ordinary pool access");
  await ordinary.close();
  checks.push(
    "502 failure preserves applied state, retry, empty search, close button, ordinary pool 403; actual core behavior tested separately",
  );
  stage = "failed receipt and retry";
  await page.getByRole("button", { name: "节点与应用", exact: true }).click();
  dialog = page.getByRole("dialog");
  await dialog.getByRole("button", { name: "预览缓存", exact: true }).click();
  await dialog
    .getByRole("button", { name: "应用所选节点", exact: true })
    .click();
  await dialog.getByRole("button", { name: "确认提交", exact: true }).click();
  await dialog.waitFor({ state: "hidden" });
  async function complete(status) {
    const queued = (
      await req("GET", "/agent/tasks", undefined, 200, session.token)
    ).items[0];
    if (!queued) throw Error("No queued fixture task");
    await req(
      "POST",
      "/agent/results",
      {
        task_id: queued.id,
        revision: queued.revision,
        status,
        data: {
          applied: status === "applied",
          source: "controlled-local-device-receipt",
        },
      },
      200,
      session.token,
    );
    await page.reload();
  }
  await complete("failed");
  await page.getByText("应用失败，保留上次生效配置", { exact: true }).waitFor();
  await shots("failed-receipt", page.locator("main"));
  await page.getByRole("button", { name: "节点与应用", exact: true }).click();
  dialog = page.getByRole("dialog");
  await dialog.getByRole("button", { name: "预览缓存", exact: true }).click();
  await dialog
    .getByRole("button", { name: "应用所选节点", exact: true })
    .click();
  await dialog.getByRole("button", { name: "确认提交", exact: true }).click();
  await dialog.waitFor({ state: "hidden" });
  await complete("applied");
  stage = "disable and delete";
  await page.getByRole("button", { name: "节点与应用", exact: true }).click();
  dialog = page.getByRole("dialog");
  await dialog.getByRole("button", { name: "停用", exact: true }).click();
  await shots("disable-confirm", dialog);
  await dialog.getByRole("button", { name: "确认提交", exact: true }).click();
  await dialog.waitFor({ state: "hidden" });
  await complete("applied");
  if (
    (await req("GET", base)).items[0].enabled ||
    (await req("GET", base)).items[0].member_count !== 0
  )
    throw Error("Disable not applied");
  await page.getByRole("button", { name: "节点与应用", exact: true }).click();
  dialog = page.getByRole("dialog");
  await dialog.getByRole("button", { name: "删除池", exact: true }).click();
  await dialog.getByRole("button", { name: "确认提交", exact: true }).click();
  await dialog.waitFor({ state: "hidden" });
  await complete("applied");
  if ((await req("GET", base)).items.length) throw Error("Delete not applied");
  checks.push(
    "Failed signed receipt preserves members, UI retry applies, UI disable and confirmed delete use successful receipts",
  );
  stage = "auto mode, full pool ordering and partial refresh";
  const a=await req("POST",base,{name:prefix+" Pool A",source_id:source.id,auto_refresh:true,prepend:true});
  const b=await req("POST",base,{name:prefix+" Pool B",source_id:source.id});
  await page.reload();
  await page.getByRole("button",{name:"调整池顺序",exact:true}).click();
  dialog=page.getByRole("dialog");
  await dialog.getByRole("button",{name:"下移 "+prefix+" Pool A",exact:true}).click();
  await shots("order",dialog);
  await dialog.getByRole("button",{name:"取消",exact:true}).click();
  if((await req("GET",base)).items.some(p=>p.priority))throw Error("Cancelled order saved");
  await page.getByRole("button",{name:"调整池顺序",exact:true}).click();
  dialog=page.getByRole("dialog");
  await dialog.getByRole("button",{name:"下移 "+prefix+" Pool A",exact:true}).click();
  await dialog.getByRole("button",{name:"保存池顺序",exact:true}).click();
  await dialog.waitFor({state:"hidden"});
  const ordered=(await req("GET",base)).items;
  if(ordered.find(p=>p.id===a.id).priority!==20||ordered.find(p=>p.id===b.id).priority!==10)throw Error("Pool order not persisted");
  const row=page.locator(".list-row").filter({hasText:prefix+" Pool A"});
  await row.getByRole("button",{name:"编辑",exact:true}).click();dialog=page.getByRole("dialog");
  if(!(await dialog.getByRole("switch",{name:"自动同步全部可托管节点",exact:true}).isChecked()))throw Error("Auto switch state lost");
  await shots("auto-settings",dialog);await dialog.getByRole("button",{name:"取消",exact:true}).click();
  await row.getByRole("button",{name:"节点与应用",exact:true}).click();dialog=page.getByRole("dialog");
  await dialog.getByRole("button",{name:"预览缓存",exact:true}).click();
  await dialog.getByText("已选 2 个",{exact:false}).waitFor();
  if(!(await dialog.getByRole("checkbox",{name:"选择 香港出口 · 长名称验收",exact:true}).isDisabled()))throw Error("Automatic pool permits partial selection");
  await shots("auto-preview",dialog);
  await dialog.getByRole("button",{name:"应用所选节点",exact:true}).click();await dialog.getByRole("button",{name:"确认提交",exact:true}).click();await dialog.waitFor({state:"hidden"});
  await complete("applied");await page.getByText("已生效 2 个",{exact:false}).waitFor();
  // The isolated upstream is deliberately unavailable. Per-item failure must be
  // visible and must retain the two acknowledged members.
  await page.route("**/pools/refresh",r=>r.fulfill({status:200,contentType:"application/json",body:JSON.stringify({items:[{pool_id:a.id,result:"source_refresh_failed_previous_preserved"},{pool_id:b.id,result:"manual_preview_required"}]})}));
  await page.getByRole("button",{name:"刷新全部来源",exact:true}).click();
  await page.getByRole("status").filter({hasText:"来源刷新失败，保留原出口"}).waitFor();
  await shots("refresh-report",page.locator("main"));await page.unroute("**/pools/refresh");
  checks.push("Pool full snapshot order/cancel/reload; explicit auto settings/all-supported selection/first acknowledgement; partial refresh report does not claim all success; automatic network behavior has separate integration/Agent evidence");
  stage="manual order, TCP observations and public directory";
  async function settleTool(data){
    const q=(await req("GET","/agent/tasks",undefined,200,session.token)).items[0];
    if(!q)throw Error("No tool task");
    await req("POST","/agent/results",{task_id:q.id,revision:q.revision,status:"applied",data:typeof data==="function"?data(q):data??{applied:true}},200,session.token);
    return q;
  }
  for(const [tag,address] of [["manual-one","8.8.8.8"],["manual-two","1.1.1.1"]]){
    await req("POST",`/admin/devices/${device}/tasks`,{operation:"outbound.upsert",confirm:true,payload:{tag:prefix+tag,protocol:"http",address,port:8080,remark:tag,transport_options:{}}},202);
    await settleTool();
  }
  await page.reload();await page.getByRole("button",{name:"手动出站排序",exact:true}).click();dialog=page.getByRole("dialog");
  await dialog.getByRole("button",{name:"下移 manual-one",exact:true}).click();await shots("manual-order",dialog);
  await dialog.getByRole("button",{name:"取消",exact:true}).click();
  await page.getByRole("button",{name:"手动出站排序",exact:true}).click();dialog=page.getByRole("dialog");
  await dialog.getByRole("button",{name:"下移 manual-one",exact:true}).click();await dialog.getByRole("button",{name:"确认提交排序任务",exact:true}).click();await dialog.waitFor({state:"hidden"});
  await settleTool();await page.reload();
  let inventory=(await req("GET",`/admin/resources/device-outbound?owner_id=${device}`)).items;
  if(inventory.find(r=>r.data.remark==="manual-one").data.sort!==20)throw Error("Manual sort not acknowledged");
  await page.getByRole("button",{name:"TCP 连通性探测",exact:true}).click();dialog=page.getByRole("dialog");
  await shots("probe-selection",dialog);await dialog.getByRole("button",{name:"开始 TCP 探测",exact:true}).click();await dialog.waitFor({state:"hidden"});
  await settleTool(q=>({source:"gate_tcp_connect",observed_at:new Date().toISOString(),items:q.payload.items.map((item,i)=>({id:item.id,status:i===0?"reachable":"blocked",...(i===0?{latency_ms:7}:{})}))}));
  await page.getByText(/TCP 可达/).waitFor();await shots("probe-result",page.locator("main"));
  const key=Buffer.from(JSON.parse(fs.readFileSync(f.configPath,"utf8")).encryption_key,"hex");
  const publicCandidates=Array.from({length:25},(_,i)=>({key:crypto.createHash("sha256").update("local-catalog-"+i).digest("hex"),protocol:"http",address:`8.8.8.${i+1}`,port:8080,country_code:i<20?"US":"JP",country:i<20?"美国":"日本",city:"专用测试城市",organization:"受控公共目录元数据",username:"synthetic-user",password:"synthetic-secret"}));
  for(const proto of ["socks","http","https"]){
    const items=publicCandidates.map(p=>({...p,protocol:proto})),iv=crypto.randomBytes(12),cipher=crypto.createCipheriv("aes-256-gcm",key,iv);
    const body=Buffer.from(`proxy-catalog:${proto}:`+JSON.stringify({observed_at:new Date().toISOString(),items,rejected:2}));
    const sealed=Buffer.concat([iv,cipher.update(body),cipher.final(),cipher.getAuthTag()]).toString("base64");
    const seeded=cp.spawnSync("docker",["compose","-f","deploy/test.compose.yml","exec","-T","redis","redis-cli","--user","migate","--pass","local-test-only","-x","SET",`migate:proxy-catalog:${proto}`],{input:sealed,encoding:"utf8",windowsHide:true});
    if(seeded.status!==0||!seeded.stdout.includes("OK"))throw Error("Dedicated encrypted catalog seed failed");
  }
  await page.getByRole("button",{name:"公共代理目录",exact:true}).click();dialog=page.getByRole("dialog");
  await dialog.getByText("8.8.8.1:8080",{exact:true}).waitFor();await shots("public-catalog",dialog);
  await dialog.getByRole("button",{name:"下一页",exact:true}).click();await dialog.getByText("8.8.8.21:8080",{exact:true}).waitFor();
  await dialog.getByRole("combobox",{name:"公共代理地区",exact:true}).click();await page.getByRole("option",{name:"日本 (5)",exact:true}).click();
  await dialog.getByText("5 项",{exact:false}).waitFor();
  await dialog.getByRole("textbox",{name:"搜索公共代理",exact:true}).fill("8.8.8.21");
  const candidate=dialog.locator(".inventory-item").filter({hasText:"8.8.8.21:8080"});await candidate.getByRole("button",{name:"TCP 探测",exact:true}).click();
  await settleTool({source:"gate_tcp_connect",observed_at:new Date().toISOString(),items:[{id:1,status:"reachable",latency_ms:9}]});
  await dialog.getByText("TCP 可达 · 9 ms",{exact:true}).waitFor();
  await candidate.getByRole("button",{name:"导入出站",exact:true}).click();await shots("public-import-confirm",dialog);
  await dialog.getByRole("button",{name:"返回目录",exact:true}).click();
  await candidate.getByRole("button",{name:"导入出站",exact:true}).click();await dialog.getByRole("button",{name:"确认导入此代理",exact:true}).click();
  await settleTool();await dialog.getByText(/导入任务/).waitFor();
  await dialog.getByRole("button",{name:"关闭",exact:true}).click();await page.reload();
  inventory=(await req("GET",`/admin/resources/device-outbound?owner_id=${device}`)).items;
  if(!inventory.some(r=>r.data.tag==="public-socks-"+publicCandidates[20].key.slice(0,16)))throw Error("Catalog import not acknowledged");
  await page.getByRole("button",{name:"公共代理目录",exact:true}).click();dialog=page.getByRole("dialog");
  await dialog.getByRole("combobox",{name:"公共代理类型",exact:true}).click();await page.getByRole("option",{name:"HTTPS",exact:true}).click();
  await dialog.getByText("8.8.8.1:8080",{exact:true}).waitFor();
  await page.route("**/proxy-catalog/https?*",r=>r.fulfill({status:503,contentType:"application/json",body:JSON.stringify({error:"proxy_catalog_unavailable"})}));
  await dialog.getByRole("textbox",{name:"搜索公共代理",exact:true}).fill("retry");
  await dialog.getByText("暂时无法加载",{exact:true}).waitFor();await shots("public-error",dialog);
  await page.unroute("**/proxy-catalog/https?*");await dialog.getByRole("button",{name:"重新加载",exact:true}).click();
  await dialog.getByText("没有匹配的公共代理",{exact:true}).waitFor();await dialog.getByRole("button",{name:"关闭",exact:true}).click();
  checks.push("Manual full-snapshot order cancel/submit/receipt/reload; selected TCP task observations; encrypted dedicated public catalog region/search/page, fixed types, single probe/import confirm/cancel/actual receipt and 503 retry. Network samples in this local UI flow are controlled, not real proxy claims.");
  fs.writeFileSync(
    f.evidence + "live-outbound-pools.json",
    JSON.stringify(
      {
        date: new Date().toISOString(),
        result: "passed",
        target:
          "dedicated local app; controlled signed device protocol fixture; no network acceptance claim",
        checks,
        visual: "manual review pending",
      },
      null,
      2,
    ),
  );
  await browser.close();
  browser = null;
  console.log("Local outbound pool complete UI workflow passed");
})().catch(async (e) => {
  fs.writeFileSync(
    f.evidence + "pool-browser-failure.json",
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
  console.error(
    "Pool UI failed at " + stage + ": " + String(e.message).split("\n")[0],
  );
  if (browser) await browser.close();
  process.exitCode = 1;
});
