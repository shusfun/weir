// VPS 与节点：每台 VPS 一张卡片，最上面是在不在线，直接显示主机观测；它上面的每个节点一行，点开看细节；
// 「＋ 协议」直接在这台 VPS 上建节点。VPS 用 SSH 一次装好，或复制安装命令自己执行。
import { useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import {
  Badge, Button, Card, CardContent, Chip, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Field, Form, Input,
  Notice, PageHeader, Readout, SkeletonCards, StatusLamp, SwitchField, useToast, useConfirm, type LampTone,
} from "@weir/ui";
import { Pencil, Plus, Route, Server, Sparkles, Trash2 } from "lucide-react";
import { openDsh } from "../components/dsh";
import { ObjectLink, dataObject, useDialogParam } from "../components/object-link";
import { exitKindName, probeText } from "../components/chain-exit";
import { NodeAccess } from "../components/node-access";
import { GateRouting } from "../components/gate-routing";
import { GateDetail, pct, size } from "../components/gate-detail";
import { api } from "../api";
import { useFormAction, useRowAction } from "../lib/action";
import { WaitText } from "../components/wait";
import { GateUpdateLine, GateVersion } from "../components/gate-version";
import { State } from "../components/feedback";
import { InviteGate } from "../components/invite-gate";
import { CertManager } from "../components/cert-manager";
import { NodeWizard } from "../components/node-wizard";
import { date } from "../lib/models";
import { rateText } from "../lib/linkmap";
import { field, useConsoleContext } from "../lib/console-context";
import { nodeStatus, protocolNames, relativeTime, type Gate, type NodeItem } from "../lib/core";
import { GettingStarted } from "../components/getting-started";

const stateText: Record<Gate["state"], [string, "ok" | "warn" | "bad" | "off"]> = {
  applied: ["已生效", "ok"],
  pending: ["待生效", "warn"],
  failed: ["应用失败", "bad"],
  revoked: ["已撤销", "off"],
};

// gateStatus：在线 = 有心跳且配置已生效；离线、待生效、应用失败都写明。
function gateStatus(g: Gate): [string, LampTone] {
  if (g.status === "revoked") return ["已撤销", "off"];
  if (!g.online) return ["离线", "bad"];
  if (g.state === "failed") return ["在线 · 配置应用失败", "bad"];
  if (g.state === "pending") return ["在线 · 配置待生效", "warn"];
  return ["在线", "ok"];
}

function protocolLabel(n: NodeItem) {
  return (protocolNames[n.protocol] ?? n.protocol) + (n.security === "reality" ? " + REALITY" : n.network === "ws" ? " + WS" : "");
}

function exitLabel(n: NodeItem, nodes: NodeItem[]) {
  if (n.chain.kind === "none") return "直连";
  if (n.chain.kind === "source") return "订阅源节点";
  if (n.chain.kind === "node") return "经 " + (nodes.find((x) => x.id === n.chain.node_id)?.name ?? "节点");
  return exitKindName[n.chain.kind];
}

export function GatesPage() {
  const toast = useToast();
  const gates = useQuery({ queryKey: ["gates"], queryFn: async () => (await api("/admin/gates")).items as Gate[],
    // 有 VPS 在自更新时刷新快一些，进度才连得上
    refetchInterval: (q) => ((q.state.data ?? []).some((g) => g.self_update?.state === "downloading" || g.self_update?.state === "replacing") ? 2000 : 10_000) });
  const nodes = useQuery({ queryKey: ["nodes"], queryFn: async () => (await api("/admin/nodes")).items as NodeItem[], refetchInterval: 10_000 });
  const [invite, setInvite] = useState(false);
  // wizard：建节点向导；数字是从某台 VPS 的「＋ 协议」打开的（这台 VPS 已选好）
  const [wizard, setWizard] = useState<number | "any" | null>(null);
  const [edit, setEdit] = useState<Gate | null>(null);
  const [routing, setRouting] = useState<Gate | null>(null);
  const refresh = () => Promise.all([gates.refetch(), nodes.refetch()]);
  // 地址栏（对象链接、dsh 的界面导航、关系图）：?gate=编号 是这台 VPS 的详情；?node=编号 是节点详情；?certs=1 是证书窗口。
  const [params, setParams] = useSearchParams();
  const detailParam = useDialogParam("gate", gates.data, !gates.isFetching);
  const nodeParam = useDialogParam("node", nodes.data, !nodes.isFetching);
  const detail = detailParam.current;
  const certs = params.get("certs") === "1";
  const setCerts = (on: boolean) => setParams((p) => { if (on) p.set("certs", "1"); else p.delete("certs"); return p; }, { replace: !on });
  const offline = (gates.data ?? []).filter((g) => g.status === "active" && !g.online).map((g) => `VPS「${g.name}」离线`);
  const pending = (gates.data ?? []).filter((g) => g.state === "pending").map((g) => `VPS「${g.name}」配置待生效`);
  const failed = (gates.data ?? []).filter((g) => g.state === "failed").map((g) => `VPS「${g.name}」配置应用失败：${g.last_error}`);
  useConsoleContext({
    page: "VPS 与节点",
    list: { total: gates.data?.length ?? 0, items: (gates.data ?? []).slice(0, 20).map((g) => ({ type: "gate", name: g.name, id: g.id })) },
    notices: [...offline, ...pending, ...failed],
  });
  useConsoleContext(detail ? { object: { type: "gate", name: detail.name, id: detail.id }, dialog: { title: "VPS 详情" } } : null);
  const header = (
    <PageHeader
      title="VPS 与节点"
      description="每个节点都会进入用户的订阅。VPS 用 SSH 一次装好，之后建节点、改配置都在这里完成。"
      actions={
        <>
          <Button variant="outline" disabled={!gates.data?.some((g) => g.status === "active")} onClick={() => setWizard("any")}><Plus />新建节点</Button>
          <Button onClick={() => setInvite(true)}><Server />添加 VPS</Button>
        </>
      }
    />
  );
  const gateOf = (n: NodeItem) => gates.data?.find((g) => g.id === n.gate_id);
  const dialogs = (
    <>
      <InviteGate open={invite} close={() => setInvite(false)} done={() => void gates.refetch()} />
      <CertManager open={certs} close={() => setCerts(false)} />
      {detail && <GateDetail gate={detail} nodes={nodes.data ?? []} close={detailParam.close} />}
      {nodeParam.current && <NodeDetail node={nodeParam.current} gate={gateOf(nodeParam.current)} nodes={nodes.data ?? []} close={nodeParam.close} changed={() => void refresh()} />}
      {routing && <GateRouting gate={routing} close={() => setRouting(null)} />}
      {edit && <GateEdit gate={edit} close={() => setEdit(null)} saved={() => { setEdit(null); toast("已保存", { tone: "ok" }); void refresh(); }} />}
      <NodeWizard
        open={wizard !== null}
        gateId={typeof wizard === "number" ? wizard : undefined}
        close={() => setWizard(null)}
        done={() => { toast("节点已创建", { tone: "ok" }); void refresh(); }}
        gates={gates.data ?? []}
        nodes={nodes.data ?? []}
      />
    </>
  );
  if (gates.isPending || nodes.isPending || gates.error || nodes.error)
    return (
      <div className="stack">
        {header}
        <State loading={gates.isPending || nodes.isPending} error={gates.error ?? nodes.error} onRetry={refresh} skeleton={<SkeletonCards count={2} lines={2} />} />
        {dialogs}
      </div>
    );
  return (
    <div className="stack">
      {header}
      <GettingStarted gates={gates.data} nodes={nodes.data} addGate={() => setInvite(true)} addNode={() => setWizard("any")} />
      {gates.data.map((g) => {
        const [label, tone] = gateStatus(g);
        const [stateLabel, stateTone] = stateText[g.state];
        const list = nodes.data.filter((n) => n.gate_id === g.id);
        const usable = list.filter((n) => nodeStatus(n, g)[1] === "ok").length;
        // 离线时不把上一次的主机数据当成现在的
        const host = g.online ? g.host ?? {} : {};
        return (
          <Card key={g.id} {...dataObject({ type: "gate", id: g.id })}>
            <CardContent className="stack">
              <div className="flex items-start justify-between gap-3 max-sm:flex-wrap">
                <div className="min-w-0 max-sm:basis-full">
                  <h2 className="flex flex-wrap items-center gap-2 text-base font-semibold">
                    <StatusLamp size="lg" tone={tone} label={label} />
                    {g.name}
                    <span className={"text-body font-medium " + (tone === "ok" ? "text-[var(--weir-ok-ink)]" : tone === "warn" ? "text-[var(--weir-warn-ink)]" : "text-[var(--weir-bad-ink)]")}>{label}</span>
                  </h2>
                  <small className="flex flex-wrap items-center gap-x-1.5 text-[var(--weir-ink-3)]">
                    {g.public_host && g.public_host !== g.name ? g.public_host + " · " : !g.public_host ? "等待上报地址 · " : ""}{list.length} 个节点{list.length ? `，${usable} 个可用` : ""} · <span title={date(g.last_seen_at)}>{relativeTime(g.last_seen_at)}心跳</span> <GateVersion gate={g} />
                  </small>
                  {g.state === "failed" && g.last_error && <p className="text-body text-[var(--weir-bad-ink)]">{g.last_error}</p>}
                  <GateUpdateLine gate={g} />
                  {!g.public_host && (
                    <p className="text-body text-[var(--weir-warn-ink)]">
                      还没有公网地址，这台 VPS 上的节点生成不了节点链接。{g.colocated ? "中心与它在同一台机器上，记录不到来源地址；" : ""}请点「编辑」填写{g.public_host_hint ? `（本机网卡上的公网 IP 是 ${g.public_host_hint}）` : ""}。
                    </p>
                  )}
                </div>
                <span className="flex flex-wrap gap-1.5">
                  <Button size="sm" variant="outline" onClick={() => detailParam.open(g)}>详情</Button>
                  <Button size="sm" variant="outline" onClick={() => setRouting(g)}><Route />出口与分流</Button>
                  <Button size="sm" variant="outline" onClick={() => setEdit(g)}><Pencil />编辑</Button>
                </span>
              </div>
              <dl className="m-0 grid grid-cols-[auto_1fr_1fr] gap-x-4 gap-y-0.5 text-sm tabular-nums sm:flex sm:flex-wrap sm:gap-x-6">
                {[["CPU", pct(host.cpu_percent)], ["内存", `${size(host.mem_used)} / ${size(host.mem_total)}`], ["磁盘", `${size(host.disk_used)} / ${size(host.disk_total)}`]].map(([k, v]) => (
                  <div key={k} className="min-w-0">
                    <dt className="text-meta text-[var(--weir-ink-3)]">{k}</dt>
                    <dd className="m-0 font-medium">{v}</dd>
                  </div>
                ))}
              </dl>
              {list.length > 0 && (
                <ul className="group-list" aria-label={`${g.name} 上的节点`}>
                  {list.map((n) => {
                    const [s, t, why] = nodeStatus(n, g);
                    return (
                      <li key={n.id} {...dataObject({ type: "node", id: n.id })}>
                        <button type="button" onClick={() => nodeParam.open(n)} className="weir-press flex min-h-[var(--weir-tap-touch)] w-full flex-wrap items-center gap-x-3 gap-y-0.5 py-1.5 text-left text-sm">
                          <span className="flex min-w-0 items-center gap-2 font-medium"><StatusLamp tone={t} label={s} />{n.name}</span>
                          <small className="tabular-nums text-[var(--weir-ink-3)]">{n.name.includes(protocolLabel(n)) ? "" : protocolLabel(n) + " "}:{n.port} · {s} · 出口 {exitLabel(n, nodes.data)} · {n.rate?.minutes ? rateText(n.rate.p90_bps) : "速率未知"}</small>
                          {t !== "ok" && <small className={"basis-full " + (t === "bad" ? "text-[var(--weir-bad-ink)]" : t === "warn" ? "text-[var(--weir-warn-ink)]" : "text-[var(--weir-ink-3)]")}>{why}</small>}
                        </button>
                      </li>
                    );
                  })}
                </ul>
              )}
              <div className="flex flex-wrap items-center gap-2">
                <Chip add disabled={g.status !== "active"} onClick={() => setWizard(g.id)}><Plus />节点</Chip>
              </div>
              {g.status === "active" && g.state !== "applied" && <small className="text-[var(--weir-ink-3)]">配置：<Badge variant="soft" tone={stateTone === "off" ? "neutral" : stateTone}>{stateLabel}</Badge></small>}
            </CardContent>
          </Card>
        );
      })}
      {dialogs}
    </div>
  );
}

// NodeDetail：一个节点的全部细节与操作：能不能用、要放行的端口、出口、启用、实测、AI 调整、删除。
function NodeDetail({ node: n, gate: g, nodes, close, changed }: { node: NodeItem; gate?: Gate; nodes: NodeItem[]; close: () => void; changed: () => void }) {
  const confirm = useConfirm();
  const toast = useToast();
  const { run, isBusy, waitFor } = useRowAction<string>({ refetch: async () => changed() });
  const [s, tone, why] = nodeStatus(n, g);
  useConsoleContext({ object: { type: "node", name: n.name, id: n.id }, dialog: { title: "节点详情" } });
  const exit =
    n.chain.kind === "none" ? (n.route_exit ? `经 VPS 出口 ${n.route_exit.name || n.route_exit.tag}${n.route_exit.address ? `（${n.route_exit.address}:${n.route_exit.port}）` : ""}` : "直接从 VPS 出去")
    : n.chain.kind === "node" ? <>经 <ObjectLink to={{ type: "node", id: n.chain.node_id ?? undefined, name: nodes.find((x) => x.id === n.chain.node_id)?.name ?? "节点" }} /></>
    : n.chain.kind === "source" ? "订阅源节点"
    : `${exitKindName[n.chain.kind]} ${n.chain.address ?? ""}${n.chain.port ? ":" + n.chain.port : ""}`;
  return (
    <Dialog open onOpenChange={(o) => !o && close()}>
      <DialogContent size="md">
        <DialogTitle>{n.name}</DialogTitle>
        <DialogDescription>{protocolLabel(n)} · {g ? <ObjectLink to={{ type: "gate", id: g.id, name: g.name }} /> : "VPS 已不存在"}</DialogDescription>
        <div className="stack">
          <p className="flex items-center gap-2 text-body"><StatusLamp tone={tone} label={s} /><strong>{s}</strong><span className="text-[var(--weir-ink-3)]">{why}</span></p>
          {n.readiness?.state === "credentials" && <Notice tone="warn">请到 <Link to="/users" onClick={close}>用户</Link> 分配这个节点并创建可用订阅；节点会在凭据生效后启动。</Notice>}
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
            <Readout label="端口" value={String(n.port)} />
            <Readout label="内核" value={n.kernel || "未知"} />
            <Readout label="凭据" value={String(n.credentials)} caption="每条链接一份" />
            <Readout label="下载速率" value={n.rate?.minutes ? rateText(n.rate.p90_bps) : "未知"} caption={n.rate?.minutes ? `近 24 小时 ${n.rate.minutes} 分钟有流量` : "近 24 小时没有流量"} />
          </div>
          <NodeAccess ports={n.ports} reach={n.reach} />
          <div className="text-body">
            <span className="text-[var(--weir-ink-3)]">出口：</span>{exit}
            {n.chain.kind !== "none" && <small className="block text-[var(--weir-ink-3)]">{probeText(n.chain.probe)}</small>}
          </div>
          <SwitchField label="启用" description="停用后这个节点不在任何订阅里，VPS 上也不再监听。" checked={n.enabled} disabled={isBusy("enable")}
            onCheckedChange={(v) => void run("enable", () => api("/admin/nodes/" + n.id, "PATCH", { enabled: v }), v ? "节点已启用" : "节点已停用")} />
          {g && !g.online && <Notice tone="warn">这台 VPS 离线，实测要等它重新连上。</Notice>}
        </div>
        <DialogFooter status={(waitFor("test") || waitFor("delete")) && <WaitText wait={waitFor("test") || waitFor("delete")} />}>
          <Button variant="destructive" disabled={isBusy()} busy={isBusy("delete")} busyText="正在删除…" onClick={async () => (await confirm({ title: `删除节点「${n.name}」？`, description: "使用它的订阅会立即失效。", confirmText: "删除", tone: "danger" })) && (await run("delete", () => api("/admin/nodes/" + n.id, "DELETE"), "节点已删除")) && close()}>
            <Trash2 />删除
          </Button>
          <Button variant="outline" onClick={() => { close(); openDsh({ notice: `dsh 已知道你在看节点「${n.name}」，直接说要怎么调整即可。`, nodeId: n.id, nodeName: n.name }); }}><Sparkles />AI 调整</Button>
          <Button disabled={isBusy() || !g?.online} busy={isBusy("test")} busyText="正在实测…"
            onClick={() => void run("test", async () => { const r = await api(`/admin/nodes/${n.id}/test`, "POST", {}); toast(`实测通过：${r.latency_ms} ms`, { tone: "ok" }); }, undefined, { wait: { label: `正在让 ${g?.name ?? "VPS"} 用真实内核实测「${n.name}」`, max: 50 } })}>
            实测
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// GateEdit：名称与公网地址。地址留空时按 Gate 上报的出口 IP；有域名时填域名（订阅与证书使用）。
function GateEdit({ gate, close, saved }: { gate: Gate; close: () => void; saved: () => void }) {
  const [name, setName] = useState(gate.name);
  const [host, setHost] = useState(gate.public_host);
  const save = useFormAction(() => api("/admin/gates/" + gate.id, "PATCH", { name: name.trim(), public_host: host.trim() }), {
    fields: { invalid_public_host: "public_host", public_host_not_public: "public_host", public_host_unresolved: "public_host", public_host_lookup_failed: "public_host" },
    onSuccess: saved,
  });
  useConsoleContext({ object: { type: "gate", name: gate.name, id: gate.id }, dialog: { title: "编辑 VPS" }, form: [field("name", "名称", name), field("public_host", "公网地址", host)] });
  return (
    <Dialog open onOpenChange={(o) => !o && !save.busy && close()}>
      <DialogContent size="md">
        <DialogTitle>编辑 VPS</DialogTitle>
        <DialogDescription>公网地址会写进订阅；改动后用户更新订阅即可生效。</DialogDescription>
        <Form busy={save.busy} errors={save.errors} onSubmit={() => void save.submit()}>
          <Field label="名称"><Input required maxLength={100} value={name} onChange={(e) => setName(e.target.value)} /></Field>
          <Field label="公网地址" name="public_host" hint={gate.public_host_hint && !host ? `IP 或解析到这台 VPS 的域名，粘贴网址也可以；本机网卡上的公网 IP 是 ${gate.public_host_hint}。域名开了 CDN 代理时节点连不上，请填 IP。` : "IP 或解析到这台 VPS 的域名，粘贴网址也可以；域名开了 CDN 代理时节点连不上，请填 IP。"}>
            <Input value={host} placeholder={gate.public_host_hint} onChange={(e) => setHost(e.target.value)} />
          </Field>
          {gate.public_host_hint && !host && <Button type="button" size="sm" variant="outline" onClick={() => setHost(gate.public_host_hint ?? "")}>填入 {gate.public_host_hint}</Button>}
          <DialogFooter error={save.error}>
            <Button type="button" variant="outline" disabled={save.busy} onClick={close}>取消</Button>
            <Button type="submit" disabled={save.busy} busy={save.busy} busyText="正在保存…">保存</Button>
          </DialogFooter>
        </Form>
      </DialogContent>
    </Dialog>
  );
}
