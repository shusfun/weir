// VPS 与节点：VPS 连接中心后只在这里操作；节点用四步向导创建，高级参数可在详情中修改。
import { useEffect, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { Badge, Button, Card, CardContent, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Field, Form, Input, Notice, PageHeader, SkeletonCards, StatusLamp, Switch, Table, TableBody, TableCell, TableHead, TableHeader, TableRow, useToast, useConfirm } from "@weir/ui";
import { FileBadge, Plus, Server, Sparkles, Trash2 } from "lucide-react";
import { openDsh, useDsh } from "../components/dsh";
import { GateRouting } from "../components/gate-routing";
import { GateDetail } from "../components/gate-detail";
import { api } from "../api";
import { useFormAction, useRowAction } from "../lib/action";
import { State } from "../components/feedback";
import { InviteGate } from "../components/invite-gate";
import { CertManager } from "../components/cert-manager";
import { NodeWizard } from "../components/node-wizard";
import { date } from "../lib/models";
import { useConsoleContext } from "../lib/console-context";
import { protocolNames, type Gate, type NodeItem } from "../lib/core";

const stateText: Record<Gate["state"], [string, "ok" | "warn" | "bad" | "off"]> = {
  applied: ["已生效", "ok"],
  pending: ["待生效", "warn"],
  failed: ["应用失败", "bad"],
  revoked: ["已撤销", "off"],
};

export function GatesPage() {
  const confirm = useConfirm();
  const toast = useToast();
  const gates = useQuery({ queryKey: ["gates"], queryFn: async () => (await api("/admin/gates")).items as Gate[], refetchInterval: 10_000 });
  const nodes = useQuery({ queryKey: ["nodes"], queryFn: async () => (await api("/admin/nodes")).items as NodeItem[], refetchInterval: 10_000 });
  const [invite, setInvite] = useState(false);
  const [wizard, setWizard] = useState(false);
  const [certs, setCerts] = useState(false);
  const [edit, setEdit] = useState<Gate | null>(null);
  const dsh = useDsh();
  const [routing, setRouting] = useState<Gate | null>(null);
  const [detail, setDetail] = useState<Gate | null>(null);
  const refresh = () => Promise.all([gates.refetch(), nodes.refetch()]);
  // 界面导航（dsh 与关系图）：?gate=编号 打开这台 VPS 的详情；?node=编号 关掉详情，滚到并高亮这个节点所在行。
  const [params, setParams] = useSearchParams();
  const [focusNode, setFocusNode] = useState<number | null>(null);
  useEffect(() => {
    const id = Number(params.get("gate"));
    const g = id ? gates.data?.find((x) => x.id === id) : undefined;
    if (g) {
      setDetail(g);
      params.delete("gate");
      setParams(params, { replace: true });
    }
    const nid = Number(params.get("node"));
    if (nid && nodes.data?.some((n) => n.id === nid)) {
      setDetail(null);
      setRouting(null);
      setFocusNode(nid);
      params.delete("node");
      setParams(params, { replace: true });
    }
  }, [params, gates.data, nodes.data, setParams]);
  useEffect(() => {
    if (!focusNode) return;
    const row = document.querySelector<HTMLElement>(`[data-node-row="${focusNode}"]`);
    row?.scrollIntoView({ block: "center" });
    row?.focus({ preventScroll: true });
    const t = setTimeout(() => setFocusNode(null), 2400);
    return () => clearTimeout(t);
  }, [focusNode]);
  const offline = (gates.data ?? []).filter((g) => g.status === "active" && !g.online).map((g) => `VPS「${g.name}」离线`);
  const pending = (gates.data ?? []).filter((g) => g.state === "pending").map((g) => `VPS「${g.name}」配置待生效`);
  const failed = (gates.data ?? []).filter((g) => g.state === "failed").map((g) => `VPS「${g.name}」配置应用失败：${g.last_error}`);
  useConsoleContext({
    page: "VPS 与节点",
    list: { selected: gates.data?.length ?? 0, items: (gates.data ?? []).slice(0, 20).map((g) => ({ type: "gate", name: g.name, id: g.id })) },
    notices: [...offline, ...pending, ...failed],
  });
  useConsoleContext(detail ? { object: { type: "gate", name: detail.name, id: detail.id }, dialog: { title: "VPS 详情" } } : null);
  const { run: act, isBusy } = useRowAction<number>({ refetch: refresh });
  const header = (
    <PageHeader
      eyebrow="工作区"
      title="VPS 与节点"
      description="VPS 执行一次安装命令后自动连接；之后建节点、改配置都在这里完成。"
      actions={
        <>
          <Button variant="outline" onClick={() => setCerts(true)}><FileBadge />证书</Button>
          <Button variant="outline" onClick={() => setInvite(true)}><Server />添加 VPS</Button>
          <Button disabled={!gates.data?.some((g) => g.status === "active")} onClick={() => setWizard(true)}><Plus />新建节点</Button>
        </>
      }
    />
  );
  const dialogs = (
    <>
      <InviteGate open={invite} close={() => setInvite(false)} done={() => void gates.refetch()} />
      <CertManager open={certs} close={() => setCerts(false)} />
      {detail && <GateDetail gate={gates.data?.find((x) => x.id === detail.id) ?? detail} nodes={nodes.data ?? []} close={() => setDetail(null)} />}
      {routing && <GateRouting gate={routing} close={() => setRouting(null)} />}
      {edit && <GateEdit gate={edit} close={() => setEdit(null)} saved={() => { setEdit(null); toast("已保存", { tone: "ok" }); void refresh(); }} />}
      <NodeWizard open={wizard} close={() => setWizard(false)} done={() => { toast("节点已创建", { tone: "ok" }); void refresh(); }} gates={gates.data ?? []} nodes={nodes.data ?? []} />
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
      {gates.data.length === 0 && <State empty="还没有 VPS" detail="点「添加 VPS」生成安装命令，在 VPS 上执行后即可建节点。" />}
      {gates.data.map((g) => {
        const [label, tone] = stateText[g.state];
        const list = nodes.data.filter((n) => n.gate_id === g.id);
        return (
          <Card key={g.id}>
            <CardContent className="stack">
              <div className="flex flex-wrap items-center justify-between gap-3">
                <div className="min-w-0">
                  <h2 className="flex items-center gap-2 text-base font-semibold">
                    <StatusLamp tone={g.online ? "ok" : "off"} label={g.online ? "在线" : "离线"} />
                    {g.name}
                    <Badge variant="soft" tone={tone === "off" ? "neutral" : tone}>{label}</Badge>
                  </h2>
                  <small className="text-[var(--mg-ink-3)]">
                    {g.public_host || "等待上报地址"} · {g.arch} · 程序 {g.version || "未知"} · 最近心跳 {date(g.last_seen_at)}
                    {Object.entries(g.kernels_running ?? {}).map(([k, v]) => ` · ${k} ${v}`)}
                  </small>
                  {g.state === "failed" && g.last_error && <p className="text-[var(--mg-bad-ink)] text-sm">{g.last_error}</p>}
                </div>
                <span className="flex flex-nowrap gap-1.5">
                  <Button size="sm" variant="outline" onClick={() => setDetail(g)}>详情</Button>
                  <Button size="sm" variant="outline" onClick={() => setRouting(g)}>出口与分流</Button>
                  <Button size="sm" variant="outline" onClick={() => setEdit(g)}>编辑</Button>
                </span>
              </div>
              {list.length === 0 ? (
                <State empty="这台 VPS 还没有节点" />
              ) : (
                <div className="overflow-x-auto">
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead>节点</TableHead>
                        <TableHead>协议</TableHead>
                        <TableHead>端口</TableHead>
                        <TableHead>出口</TableHead>
                        <TableHead>凭据</TableHead>
                        <TableHead>启用</TableHead>
                        <TableHead><span className="sr-only">操作</span></TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {list.map((n) => (
                        <TableRow key={n.id} data-node-row={n.id} tabIndex={focusNode === n.id ? -1 : undefined} className={focusNode === n.id ? "bg-[var(--mg-accent-soft)] transition-colors" : undefined}>
                          <TableCell>{n.name}</TableCell>
                          <TableCell>{protocolNames[n.protocol] ?? n.protocol}{n.security === "reality" ? " + REALITY" : n.network === "ws" ? " + WS" : ""}</TableCell>
                          <TableCell className="font-mono">{n.port}</TableCell>
                          <TableCell>{n.chain.kind === "none" ? "直连" : n.chain.kind === "socks5" ? `SOCKS5 ${n.chain.address}:${n.chain.port}` : n.chain.kind === "source" ? "订阅源节点" : `经 ${nodes.data.find((x) => x.id === n.chain.node_id)?.name ?? "节点"}`}</TableCell>
                          <TableCell>{n.credentials}</TableCell>
                          <TableCell>
                            <Switch aria-label={"启用 " + n.name} checked={n.enabled} disabled={isBusy(n.id)} onCheckedChange={(v) => act(n.id, () => api("/admin/nodes/" + n.id, "PATCH", { enabled: v }), v ? "节点已启用" : "节点已停用")} />
                          </TableCell>
                          <TableCell>
                            <span className="flex flex-nowrap gap-1.5">
                            <Button size="sm" variant="outline" disabled={isBusy(n.id)} onClick={() => act(n.id, async () => {
                              const r = await api(`/admin/nodes/${n.id}/test`, "POST", {});
                              toast(`实测通过：${r.latency_ms} ms`, { tone: "ok" });
                            }, "实测完成")}>实测</Button>
                            <Button size="sm" variant="outline" title={dsh.data?.ready ? undefined : "请先在系统设置中启用 dsh"} disabled={!dsh.data?.ready} onClick={() => openDsh({ notice: `dsh 已知道你在看节点「${n.name}」，直接说要怎么调整即可。`, nodeId: n.id, nodeName: n.name })}><Sparkles />AI 调整</Button>
                            <Button size="icon-sm" variant="destructive" aria-label={"删除 " + n.name} disabled={isBusy(n.id)}
                              onClick={async () => (await confirm({ title: `删除节点「${n.name}」？`, description: "使用它的订阅会立即失效。", confirmText: "删除", tone: "danger" })) && act(n.id, () => api("/admin/nodes/" + n.id, "DELETE"), "节点已删除")}>
                              <Trash2 />
                            </Button>
                            </span>
                          </TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                </div>
              )}
            </CardContent>
          </Card>
        );
      })}
      {dialogs}
    </div>
  );
}

// GateEdit：名称与公网地址。地址留空时按 Gate 上报的出口 IP；有域名时填域名（订阅与证书使用）。
function GateEdit({ gate, close, saved }: { gate: Gate; close: () => void; saved: () => void }) {
  const [name, setName] = useState(gate.name);
  const [host, setHost] = useState(gate.public_host);
  const save = useFormAction(() => api("/admin/gates/" + gate.id, "PATCH", { name: name.trim(), public_host: host.trim() }), { onSuccess: saved });
  return (
    <Dialog open onOpenChange={(o) => !o && !save.busy && close()}>
      <DialogContent size="md">
        <DialogTitle>编辑 VPS</DialogTitle>
        <DialogDescription>公网地址会写进订阅；改动后用户更新订阅即可生效。</DialogDescription>
        <Form busy={save.busy} errors={save.errors} onSubmit={() => void save.submit()}>
          <Field label="名称"><Input required maxLength={100} value={name} onChange={(e) => setName(e.target.value)} /></Field>
          <Field label="公网地址" hint="IP 或解析到这台 VPS 的域名。"><Input value={host} onChange={(e) => setHost(e.target.value)} /></Field>
          {save.error && <Notice tone="bad">{save.error}</Notice>}
          <DialogFooter>
            <Button type="button" variant="outline" disabled={save.busy} onClick={close}>取消</Button>
            <Button type="submit" disabled={save.busy}>{save.busy ? "正在保存…" : "保存"}</Button>
          </DialogFooter>
        </Form>
      </DialogContent>
    </Dialog>
  );
}
