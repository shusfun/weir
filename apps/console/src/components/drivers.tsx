// 内核驱动管理：每个内核按版本区间一份驱动（怎么运行、支持什么、建节点的方案、怎么生成配置）。
// 可新增、编辑、停用、恢复内置；每次保存留历史版本，可看差异、可退回。保存前检查：严格解析与展开 →
// 每个方案的样例配置用对应版本内核检查 → 改的是在用驱动时在一台 VPS 上实测。
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Badge, Button, Card, CardContent, Collapsible, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Field, Form, Input, Notice, SectionHeader, SkeletonList, Textarea, useConfirm } from "@weir/ui";
import { Bot, Plus, RotateCcw } from "lucide-react";
import { api, ApiError } from "../api";
import { useRowAction } from "../lib/action";
import { date } from "../lib/models";
import { State } from "./feedback";
import { DiffView, type DiffLine } from "./diff";
import { openDsh } from "./dsh";
import { ObjectLink, dataObject, useStringDialogParam } from "./object-link";
import { field, useConsoleContext } from "../lib/console-context";

interface DriverItem {
  name: string;
  kernel: string;
  min_version: string;
  max_version?: string;
  extends?: string;
  source: "builtin" | "custom" | "modified";
  disabled: boolean;
  error?: string;
  versions: string[];
  gates: string[];
  gate_refs?: { id: number; name: string }[];
}
interface DriverCheck {
  name: string;
  kernel: string;
  state: "passed" | "failed" | "unknown";
  error?: string;
  version?: string;
  samples: { preset: string; state: string; log?: string }[];
  live?: { state: string; gate?: string; node?: string; error?: string };
  diff?: DiffLine[];
}
interface History {
  revision: number;
  disabled: boolean;
  note: string;
  source: string;
  at: string;
  restored: boolean;
  diff: DiffLine[];
}

const sourceLabel = { builtin: ["内置", "neutral"], custom: ["自定义", "ok"], modified: ["改过的内置", "warn"] } as const;

export function DriversPanel() {
  const q = useQuery({ queryKey: ["drivers"], queryFn: async () => (await api("/admin/drivers")).drivers as DriverItem[] });
  // ?driver=名称 打开这个驱动；?driver=new 打开「新增驱动」（对象链接、dsh 的界面导航都走这里）
  const param = useStringDialogParam("driver");
  const open = param.value && param.value !== "new" ? param.value : null;
  const creating = param.value === "new";
  useConsoleContext({ list: { total: q.data?.length ?? 0, items: (q.data ?? []).slice(0, 20).map((d) => ({ type: "driver", name: d.name, key: d.name })) } });
  return (
    <Card>
      <CardContent className="stack">
        <SectionHeader
          title="内核驱动"
          description="每个内核版本怎么运行、支持哪些协议、建节点的方案、怎么生成配置，都写在驱动里；新版本用 extends 继承上一版本、只写变化。引擎强制检查统计端口只监听回环、证书只用托管目录，驱动写错也绕不过。"
          actions={
            <span className="flex gap-2">
              <Button size="sm" variant="outline" onClick={() => openDsh({ notice: "dsh 会用 weir-driver skill：读内核更新说明，为新版本起草驱动；草稿检查通过后会弹窗给你看差异。直接说要为哪个内核的哪个版本写驱动即可。" })}><Bot />让 dsh 起草</Button>
              <Button size="sm" variant="outline" onClick={() => param.open("new")}><Plus />新增驱动</Button>
            </span>
          }
        />
        {q.isPending || q.error ? <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} skeleton={<SkeletonList rows={4} lines={1} />} /> : (
          <ul className="m-0 list-none space-y-2 p-0">
            {q.data.map((d) => (
              <li key={d.name} {...dataObject({ type: "driver", key: d.name })}>
                <button type="button" className="flex w-full flex-wrap items-center gap-2 rounded-[var(--weir-radius)] border border-[var(--weir-line)] px-3 py-2 text-left hover:bg-[var(--weir-well)]" onClick={() => param.open(d.name)}>
                  <code className="text-xs">{d.name}</code>
                  <Badge variant="soft" tone={sourceLabel[d.source][1]}>{sourceLabel[d.source][0]}</Badge>
                  {d.disabled && <Badge variant="soft" tone="bad">已停用</Badge>}
                  {d.error && <Badge variant="soft" tone="bad">无法展开</Badge>}
                  <small className="text-[var(--weir-ink-3)]">{d.kernel} {d.min_version} – {d.max_version || "最新"}{d.extends ? ` · 继承 ${d.extends}` : ""}</small>
                  <small className="ml-auto text-[var(--weir-ink-3)]">{d.versions.length > 0 ? `已下载 ${d.versions.join("、")}` : "没有已下载的版本"}{d.gates.length > 0 ? ` · ${d.gates.length} 台 VPS 在用` : ""}</small>
                </button>
              </li>
            ))}
          </ul>
        )}
      </CardContent>
      {open && <DriverDialog name={open} close={param.close} changed={() => void q.refetch()} />}
      {creating && <NewDriverDialog close={param.close} created={(n) => { param.open(n); void q.refetch(); }} />}
    </Card>
  );
}

// CheckView 显示保存前检查的结果：每个方案的样例配置、内核检查、VPS 实测。
function CheckView({ c }: { c: DriverCheck }) {
  const tone = c.state === "passed" ? "ok" : c.state === "failed" ? "bad" : "warn";
  return (
    <div className="stack">
      <Notice tone={tone}>
        {c.state === "passed" ? `检查通过：每个方案的样例配置都通过了 ${c.kernel} ${c.version} 的检查。` : c.error + "。"}
        {c.live && `VPS 实测：${c.live.state === "passed" ? `通过（${c.live.gate} / ${c.live.node}）` : c.live.state === "failed" ? `未通过（${c.live.gate} / ${c.live.node}）：${c.live.error}` : c.live.error}`}
      </Notice>
      {c.samples.length > 0 && (
        <div className="flex flex-wrap gap-1.5">
          {c.samples.map((s) => <Badge key={s.preset} variant="soft" tone={s.state === "passed" ? "ok" : s.state === "failed" ? "bad" : "neutral"} title={s.log}>{s.preset}：{s.state === "passed" ? "通过" : s.state === "failed" ? "未通过" : "未知"}</Badge>)}
        </div>
      )}
      {c.samples.filter((s) => s.log).map((s) => <pre key={s.preset} className="max-h-40 overflow-auto rounded-[var(--weir-radius)] bg-[var(--weir-well)] p-2 text-xs">{s.preset}：{s.log}</pre>)}
    </div>
  );
}

function DriverDialog({ name, close, changed }: { name: string; close: () => void; changed: () => void }) {
  const q = useQuery({ queryKey: ["driver", name], queryFn: () => api("/admin/drivers/" + encodeURIComponent(name)) as Promise<{ driver: DriverItem & { text: string }; resolved: string; resolve_error?: string; history: History[]; builtin?: string; usage: { versions?: string[]; gates?: string[]; gate_refs?: { id: number; name: string }[] } }> });
  const [text, setText] = useState<string | null>(null);
  const [note, setNote] = useState("");
  const [check, setCheck] = useState<DriverCheck | null>(null);
  const confirm = useConfirm();
  const { run, isBusy } = useRowAction<string>({ refetch: () => { void q.refetch(); changed(); } });
  const value = text ?? q.data?.driver.text ?? "";
  const dirty = text !== null && text !== q.data?.driver.text;
  // 页面感知：正在看哪个驱动、有没有改动、最近一次检查结果（驱动原文不上报，dsh 用 get_driver 读）
  useConsoleContext({
    object: { type: "driver", name, key: name },
    dialog: { title: "驱动 " + name },
    form: [field("dirty", "原文有未保存的改动", dirty ? "是" : "否"), field("note", "说明", note), ...(check ? [field("check_result", "最近一次检查", check.state === "passed" ? "通过" : check.state === "failed" ? "未通过：" + (check.error ?? "") : "未知")] : [])],
  });
  const save = () =>
    run("save", async () => {
      try {
        const r = await api("/admin/drivers/" + encodeURIComponent(name), "PUT", { text: value, note });
        setCheck(r.check);
        setText(null);
        setNote("");
        return r;
      } catch (e) {
        if (e instanceof ApiError) {
          const c = await api("/admin/drivers/check", "POST", { name, text: value, live: false }).catch(() => null);
          if (c) setCheck(c.check);
        }
        throw e;
      }
    }, (r) => `已保存为第 ${(r as { revision: number }).revision} 版`, { fail: "没有保存" });
  return (
    <Dialog open onOpenChange={(o) => !o && !isBusy() && close()}>
      <DialogContent size="xl">
        <DialogTitle>驱动 {name}</DialogTitle>
        <DialogDescription>
          {q.data ? <>{q.data.driver.kernel} {q.data.driver.min_version} – {q.data.driver.max_version || "最新"}；{(q.data.usage.versions ?? []).length > 0 ? <>已下载版本 {q.data.usage.versions!.map((v, i) => <span key={v}>{i > 0 && "、"}<ObjectLink to={{ type: "kernel", key: `${q.data!.driver.kernel}@${v}`, name: v }} /></span>)}</> : "没有已下载的版本"}{(q.data.usage.gate_refs ?? []).length > 0 && <>；在用：{q.data.usage.gate_refs!.map((g, i) => <span key={g.id}>{i > 0 && "、"}<ObjectLink to={{ type: "gate", id: g.id, name: g.name }} /></span>)}</>}。</> : "正在读取…"}
        </DialogDescription>
        {q.isPending || q.error || !q.data ? <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} /> : (
          <div className="stack max-h-[70vh] overflow-y-auto">
            {q.data.resolve_error && <Notice tone="bad">无法展开：{q.data.resolve_error}</Notice>}
            <Form onSubmit={() => void save()}>
              <Field label="原文（YAML）" hint="只写与继承的驱动不同的部分；在 {…} 里写可省略的键要加引号，如 &quot;host?&quot;:。">
                <Textarea className="min-h-[18rem] font-mono text-xs" spellCheck={false} value={value} onChange={(e) => { setText(e.target.value); setCheck(null); }} />
              </Field>
              <Field label="说明" hint="这次改了什么、依据哪段更新说明（记在历史里）。"><Input value={note} onChange={(e) => setNote(e.target.value)} /></Field>
              <div className="actions">
                <Button type="button" variant="outline" disabled={isBusy()} onClick={() => void run("check", async () => { const c = (await api("/admin/drivers/check", "POST", { name, text: value, live: true })).check as DriverCheck; setCheck(c); return c; }, (c) => (c as DriverCheck).state === "passed" ? "检查通过" : "")} busy={isBusy("check")} busyText="正在检查…">只检查</Button>
                <Button type="submit" disabled={!dirty || isBusy()} busy={isBusy("save")} busyText="正在检查并保存…">检查并保存</Button>
                {q.data.driver.disabled ? (
                  <Button type="button" variant="ghost" disabled={isBusy()} onClick={() => void run("toggle", () => api(`/admin/drivers/${encodeURIComponent(name)}/enable`, "POST"), "已启用")}>启用</Button>
                ) : (
                  <Button type="button" variant="ghost" disabled={isBusy()} onClick={async () => (await confirm({ title: `停用驱动 ${name}？`, description: "停用后这个版本区间没有可用驱动的内核版本不能设为默认，也不能给 VPS 用。", confirmText: "停用", tone: "danger" })) && void run("toggle", () => api(`/admin/drivers/${encodeURIComponent(name)}/disable`, "POST"), "已停用")}>停用</Button>
                )}
                {q.data.driver.source === "modified" && <Button type="button" variant="ghost" disabled={isBusy()} onClick={async () => (await confirm({ title: `把 ${name} 恢复为内置版本？`, description: "改动会保留在历史里，可以再退回。", confirmText: "恢复内置" })) && void run("restore", () => api(`/admin/drivers/${encodeURIComponent(name)}/restore`, "POST"), "已恢复内置")}><RotateCcw />恢复内置</Button>}
              </div>
              {check && <CheckView c={check} />}
            </Form>
            <Collapsible title="展开 extends 后的全文">
              <pre className="max-h-80 overflow-auto rounded-[var(--weir-radius)] bg-[var(--weir-well)] p-2 text-xs">{q.data.resolved || "（无法展开）"}</pre>
            </Collapsible>
            <SectionHeader title="历史" description={q.data.history.length === 0 ? "还没有保存过，当前是内置版本。" : "每次保存都是新版本；退回也会先检查。"} />
            {q.data.history.map((h) => (
              <Collapsible key={h.revision} variant="card" title={<span className="flex flex-wrap items-center gap-2"><code>#{h.revision}</code>{h.restored && <Badge variant="soft" tone="neutral">恢复内置</Badge>}{h.disabled && <Badge variant="soft" tone="bad">停用</Badge>}{h.source === "dsh" && <Badge variant="soft" tone="neutral">dsh 草稿</Badge>}<small className="text-[var(--weir-ink-3)]">{date(h.at)}{h.note ? " · " + h.note : ""}</small></span>}>
                <div className="stack">
                  <DiffView lines={h.diff} empty="与上一版本相同。" />
                  <div className="actions"><Button size="sm" variant="outline" disabled={isBusy()} onClick={() => void run("rb" + h.revision, () => api(`/admin/drivers/${encodeURIComponent(name)}/rollback`, "POST", { revision: h.revision }), `已退回到 #${h.revision}`)}>退回到这一版</Button></div>
                </div>
              </Collapsible>
            ))}
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}

// NewDriverDialog：新增一个驱动，默认继承同内核 min_version 最高的驱动。
function NewDriverDialog({ close, created }: { close: () => void; created: (name: string) => void }) {
  const list = useQuery({ queryKey: ["drivers"], queryFn: async () => (await api("/admin/drivers")).drivers as DriverItem[] });
  const [f, setF] = useState({ kernel: "xray", name: "", min: "", parent: "" });
  const [check, setCheck] = useState<DriverCheck | null>(null);
  const { run, isBusy } = useRowAction<string>();
  const parents = (list.data ?? []).filter((d) => d.kernel === f.kernel && !d.error);
  const parent = f.parent || parents.sort((a, b) => b.min_version.localeCompare(a.min_version, undefined, { numeric: true }))[0]?.name || "";
  const text = `kernel: ${f.kernel}\nname: ${f.name}\nextends: ${parent}\nmin_version: ${f.min}\nnotes: \n`;
  return (
    <Dialog open onOpenChange={(o) => !o && !isBusy() && close()}>
      <DialogContent size="lg">
        <DialogTitle>新增驱动</DialogTitle>
        <DialogDescription>新驱动继承一个已有驱动，只写变化；保存后可以继续编辑模板。记得把被继承驱动的 max_version 收紧到新版本之前。</DialogDescription>
        <Form onSubmit={() => void run("create", async () => {
          try {
            await api("/admin/drivers/" + encodeURIComponent(f.name), "PUT", { text, note: "新增" });
            created(f.name);
          } catch (e) {
            const c = await api("/admin/drivers/check", "POST", { name: f.name, text, live: false }).catch(() => null);
            if (c) setCheck(c.check);
            throw e;
          }
        }, "已新增", { fail: "没有新增" })}>
          <Field label="内核" required>
            <div className="flex gap-2">{["xray", "sing-box"].map((k) => <Button key={k} type="button" size="sm" variant={f.kernel === k ? "default" : "outline"} onClick={() => setF({ ...f, kernel: k, parent: "" })}>{k}</Button>)}</div>
          </Field>
          <Field label="名称" required hint="小写字母、数字、点与连字符，如 xray-27.1"><Input value={f.name} onChange={(e) => setF({ ...f, name: e.target.value.trim() })} /></Field>
          <Field label="起始版本（min_version）" required><Input value={f.min} onChange={(e) => setF({ ...f, min: e.target.value.trim() })} /></Field>
          <Field label="继承" hint="默认继承同内核起始版本最高的驱动。">
            <div className="flex flex-wrap gap-2">{parents.map((p) => <Button key={p.name} type="button" size="sm" variant={parent === p.name ? "default" : "outline"} onClick={() => setF({ ...f, parent: p.name })}>{p.name}</Button>)}</div>
          </Field>
          {/* 检查结果区固定高度、内部滚动：结果出现时下面的按钮不被顶走 */}
          <div className="h-32 overflow-y-auto rounded-[var(--weir-radius)] border border-dashed border-[var(--weir-line)] p-2">
            {check ? <CheckView c={check} /> : <small className="text-[var(--weir-ink-3)]">点「检查并新增」后，每个方案的样例配置检查结果显示在这里。</small>}
          </div>
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={close}>取消</Button>
            <Button type="submit" disabled={!f.name || !f.min || !parent || isBusy()} busy={isBusy()} busyText="正在检查…">检查并新增</Button>
          </DialogFooter>
        </Form>
      </DialogContent>
    </Dialog>
  );
}
