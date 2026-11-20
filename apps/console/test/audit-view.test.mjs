import { test } from "node:test";
import assert from "node:assert/strict";
import { describeResult, originOf } from "../src/lib/audit-view.ts";

test("结果串翻成中文，登记过的附加信息一并翻译", () => {
  assert.deepEqual(describeResult("ok chains=0"), { label: "成功", extras: ["链式代理 0"] });
  assert.deepEqual(describeResult("denied"), { label: "拒绝", extras: [] });
  assert.deepEqual(describeResult("queued nodes=3 links=2"), { label: "已排队", extras: ["节点 3", "链接 2"] });
});

test("未登记的结果码与附加键不在正文里猜", () => {
  assert.deepEqual(describeResult("weird_code secret=abc"), { label: "其他结果", extras: [] });
  assert.deepEqual(describeResult("all"), { label: "其他结果", extras: [] });
  assert.deepEqual(describeResult(""), { label: "未记录", extras: [] });
  assert.deepEqual(describeResult("ok note"), { label: "成功", extras: [] });
});

test("发起方区分 dsh 确认、dsh 自动、管理员与系统", () => {
  assert.equal(originOf({ action: "ai.node.update", actor: 1, actor_name: "alice" }).kind, "dsh-confirmed");
  assert.equal(originOf({ action: "ai.node.update", actor: 1, actor_name: "alice" }).label, "dsh 起草 · alice确认");
  assert.equal(originOf({ action: "ops.defense.tighten", actor: 0, actor_name: "" }).kind, "dsh-auto");
  assert.deepEqual(originOf({ action: "node.update", actor: 2, actor_name: "" }), { kind: "admin", label: "管理员" });
  assert.deepEqual(originOf({ action: "gate.heartbeat", actor: 0, actor_name: "" }), { kind: "system", label: "系统" });
});
