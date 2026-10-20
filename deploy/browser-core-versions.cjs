require('./production-guard.cjs');
const { chromium } = require("playwright");
const fs = require("fs"),
  path = require("path"),
  { execFileSync } = require("child_process");
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
  async function wait(id) {
    for (let i = 0; i < 100; i++) {
      const t = (await req("GET", "/admin/tasks?q=" + id)).items[0];
      if (t?.status === "applied") return t;
      if (t?.status === "failed")
        throw Error("Version task failed: " + t.result?.message);
      await page.waitForTimeout(1000);
    }
    throw Error("Version task timed out");
  }
  async function send(operation, payload) {
    return wait(
      (
        await req("POST", "/admin/devices/" + s.device_id + "/tasks", {
          operation,
          payload,
          confirm: true,
        })
      ).id,
    );
  }
  async function ui(card, button) {
    await card
      .getByRole("button", { name: button, exact: true })
      .click({ timeout: 45000 });
    const response = page.waitForResponse(
      (r) => r.url().endsWith("/tasks") && r.request().method() === "POST",
    );
    await page
      .getByRole("dialog")
      .getByRole("button", { name: "确认执行", exact: true })
      .click();
    return wait((await (await response).json()).id);
  }
  function clients() {
    execFileSync("python", ["deploy/verify-live-client.py"], {
      stdio: ["ignore", "pipe", "pipe"],
    });
    checks.push(
      "two actual independent client credentials reach Gate after version switch",
    );
  }
  let success = false;
  try {
    await page.goto(
      "https://sub.caowo.baby/devices/" + s.device_id + "?tab=cores",
    );
    const xray = page
      .locator(".core-version-card")
      .filter({
        has: page.getByRole("heading", { name: "Xray", exact: true }),
      });
    await xray.getByRole("combobox", { name: "xray 安装版本" }).waitFor();
    await xray.getByRole("combobox", { name: "xray 安装版本" }).click();
    await page.getByRole("option", { name: /26\.10\.17/ }).click();
    const upgraded = await ui(xray, "安装 / 升级");
    if (upgraded.result.version !== "26.10.17")
      throw Error("Unexpected selected version");
    clients();
    checks.push(
      "UI installs reviewed Xray 26.10.17 package, verifies config and stats readiness",
    );
    await xray
      .getByRole("button", { name: "回滚上一版本", exact: true })
      .click({ timeout: 45000 });
    const response = page.waitForResponse(
      (r) => r.url().endsWith("/tasks") && r.request().method() === "POST",
    );
    await page
      .getByRole("dialog")
      .getByRole("button", { name: "确认执行", exact: true })
      .click();
    const rollback = await wait((await (await response).json()).id);
    clients();
    if (rollback.result.current === upgraded.result.current)
      throw Error("Rollback did not change binary");
    checks.push("UI rollback restores prior binary digest and real clients");
    // Re-deliver the exact task to the local helper: persisted receipt must keep
    // the rollback result rather than toggling current and previous a second time.
    const script =
      "import sys,json;sys.path.insert(0,'deploy');from remote import connect,run;body=json.dumps({'task_id':" +
      rollback.id +
      ",'revision':" +
      rollback.revision +
      ",'core':'xray','operation':'core.rollback','version':''});\nwith connect('gate') as c:\n code,out,err=run(c,\"curl --silent --fail --unix-socket /run/migate/helper.sock -X POST -H 'Content-Type: application/json' --data '\"+body+\"' http://helper/core-versions\");\n if code:raise RuntimeError('Helper replay failed')\n r=json.loads(out);assert r.get('replayed') is True;assert r['current']==\"" +
      rollback.result.current +
      "\"\nprint('Persistent helper receipt replay passed')";
    execFileSync("python", ["-c", script], {
      stdio: ["ignore", "pipe", "pipe"],
    });
    checks.push(
      "actual helper repeated rollback task returns persisted receipt without switching twice",
    );
    await ui(xray, "安装 / 升级");
    clients();
    await ui(xray, "清理历史版本");
    await xray.getByText(/Xray 26\.10\.17/).waitFor({ timeout: 45000 });
    await xray.locator("summary").click();
    for (const width of [1440, 1280, 768, 390]) {
      await page.setViewportSize({ width, height: 1000 });
      await page.waitForTimeout(200);
      if (
        await page.evaluate(
          () => document.documentElement.scrollWidth > innerWidth,
        )
      )
        throw Error("Core UI overflow " + width);
      await page.screenshot({
        path: `docs/evidence/core-versions-live-${width}.png`,
        fullPage: true,
        mask: [page.locator(".fingerprint")],
      });
    }
    const status = await send("core.status", {});
    const inventory = status.result.version_management;
    if (
      inventory.engines.xray.current !== upgraded.result.current ||
      !inventory.engines.xray.previous
    )
      throw Error("Cleanup lost current/rollback");
    const sing = page
      .locator(".core-version-card")
      .filter({
        has: page.getByRole("heading", { name: "sing-box", exact: true }),
      });
    await ui(sing, "安装 / 升级");
    clients();
    checks.push(
      "sing-box stats-capable pinned package reinstalls; Xray current/rollback retained during scoped history cleanup",
    );
    await send("core.install", { core: "xray", version: "26.3.27" });
    clients();
    execFileSync("python", ["deploy/verify-live-templates.py"], { stdio: ["ignore", "pipe", "pipe"] });
    checks.push("restored mixed-client compatible 26.3.27 and verified actual sing-box template client before exit");
    if (errors.length) throw Error("Browser runtime errors");
    fs.writeFileSync(
      "docs/evidence/core-versions-live.json",
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
    success = true;
  } finally {
    if (!success) {
      try {
        await send("core.install", { core: "xray", version: "26.3.27" });
      } catch {
        console.error(
          "Baseline restoration must be checked before further kernel tests",
        );
      }
    }
    await browser.close();
  }
  console.log(
    "Real UI core installation, binary upgrade, rollback, persistent replay, history preservation and client verification passed",
  );
})().catch(async (e) => {
  if (browser) await browser.close();
  console.error(e.message.replace(/cookie: [^\n]+/gi, "cookie: [REDACTED]"));
  process.exitCode = 1;
});
