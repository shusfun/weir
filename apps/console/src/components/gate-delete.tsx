import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { ShieldOff, Trash2 } from "lucide-react";
import { Button, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Notice, SectionHeader, useConfirm } from "@weir/ui";
import { api } from "../api";
import { useQuery } from "../lib/query";
import { useFormAction, useRowAction } from "../lib/action";
import type { Gate } from "../lib/core";
import { useConsoleContext } from "../lib/console-context";
import { ObjectLink } from "./object-link";
import { State } from "./feedback";

interface DeleteObject { id: number; name: string }
interface DeletePreview {
  preview_token: string;
  impact: { nodes: DeleteObject[]; chains: DeleteObject[]; exits: DeleteObject[]; blocked_exits: DeleteObject[]; routes: number[]; users: number[]; links: number[]; credentials: number[]; certificates: DeleteObject[]; laya: boolean };
}

export function GateLifecycle({ gate }: { gate: Gate }) {
  const [deleting, setDeleting] = useState(false);
  const confirm = useConfirm();
  const action = useRowAction<number>({ invalidate: [["gates"], ["nodes"]] });
  return <>
    {gate.status === "active" && <Button size="icon" variant="ghost" title="撤销 VPS 身份" aria-label={`撤销 ${gate.name} 身份`} busy={action.isBusy()} onClick={async () => {
      if (await confirm({ title: `撤销「${gate.name}」？`, description: "VPS 得知撤销后会停止托管内核，重启后也不会恢复。当前失联时无法确认服务已经停止。", confirmText: "撤销身份", tone: "danger" })) {
        await action.run(gate.id, () => api(`/admin/gates/${gate.id}/revoke`, "POST", {}), "身份已撤销");
      }
    }}><ShieldOff /></Button>}
    {gate.object_state?.next.includes("delete") && <Button size="icon" variant="ghost" title="删除 VPS 记录" aria-label={`删除 ${gate.name} 记录`} onClick={() => setDeleting(true)}><Trash2 /></Button>}
    {deleting && <GateDelete gate={gate} close={() => setDeleting(false)} />}
  </>;
}

function GateDelete({ gate, close }: { gate: Gate; close: () => void }) {
  const client = useQueryClient();
  const [result, setResult] = useState<{ warnings: string[] } | null>(null);
  const q = useQuery({ queryKey: ["gate-delete-preview", gate.id], queryFn: () => api(`/admin/gates/${gate.id}/delete-preview`, "POST", {}) as Promise<DeletePreview>, staleTime: 0, refetchOnWindowFocus: false });
  const action = useFormAction(() => api(`/admin/gates/${gate.id}`, "DELETE", { preview_token: q.data?.preview_token }), {
    onSuccess: async (value) => {
      setResult(value);
      if (!value.warnings?.length) close();
      await Promise.all(["gates", "nodes", "exits", "routes", "users", "links", "node-catalog", "certs", "laya"].map(key => client.invalidateQueries({ queryKey: [key] })));
    },
  });
  useConsoleContext({ object: { type: "gate", id: gate.id, name: gate.name }, dialog: { title: "删除 VPS 记录" } });
  const p = q.data?.impact;
  return <Dialog open onOpenChange={open => !open && !action.busy && close()}><DialogContent size="lg">
    <DialogTitle>删除「{gate.name}」的记录</DialogTitle>
    <DialogDescription>先核对影响，再确认删除。已有订阅令牌保留，已删除节点的凭据失效；此操作不能确认失联机器上的服务已经停止。</DialogDescription>
    {result ? <Notice tone="warn">记录已删除。{result.warnings.join("；")}</Notice> : !p ? <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} /> : <div className="stack">
      <Notice tone="warn">删除 {p.nodes.length} 个节点、{p.chains.length} 条链式、{p.exits.length} 个出口、{p.routes.length} 条分流规则及 {p.credentials.length} 份凭据。更新 {p.users.length} 个用户授权与 {p.links.length} 条订阅的选用列表。</Notice>
      {p.nodes.length > 0 && <div><SectionHeader title="删除节点" />{p.nodes.map(n => <p key={n.id}><ObjectLink to={{ type: "node", id: n.id }}>{n.name}</ObjectLink></p>)}</div>}
      {p.chains.length > 0 && <div><SectionHeader title="删除链式" />{p.chains.map(n => <p key={n.id}>{n.name} · #{n.id}</p>)}</div>}
      {p.certificates.length > 0 && <div><SectionHeader title="保留证书并解除 VPS 绑定" /><Notice tone="warn">保留证书编号、材料与其他节点的引用。原 VPS 的申请、续签和本机私钥访问停止。</Notice>{p.certificates.map(cert => <p key={cert.id}><ObjectLink to={{ type: "cert", id: cert.id }}>{cert.name}</ObjectLink></p>)}</div>}
      {p.laya && <Notice tone="warn">Laya 当前使用这台 VPS，删除时将停用 Laya 并解除设备绑定。重新选择设备并验证后才能启用。</Notice>}
      {p.blocked_exits.length > 0 && <div><SectionHeader title="其他 VPS 上的落地引用" /><Notice tone="warn">以下出口保留编号，改为拦截全部匹配流量。请另行选择可用落地。</Notice>{p.blocked_exits.map(e => <p key={e.id}><ObjectLink to={{ type: "exit", id: e.id }}>{e.name}</ObjectLink></p>)}</div>}
      {p.users.length > 0 && <p>受影响用户：{p.users.map(id => <ObjectLink key={id} to={{ type: "user", id }}>#{id} </ObjectLink>)}</p>}
      {p.links.length > 0 && <p>受影响订阅：{p.links.map(id => <ObjectLink key={id} to={{ type: "link", id }}>#{id} </ObjectLink>)}</p>}
    </div>}
    <DialogFooter error={action.error}>
      <Button variant="outline" disabled={action.busy} onClick={close}>关闭</Button>
      {!result && <><Button variant="outline" disabled={action.busy} busy={q.isFetching} onClick={() => void q.refetch()}>重新预览</Button><Button variant="destructive" disabled={!p || q.isFetching} busy={action.busy} onClick={() => void action.submit()}>确认删除记录</Button></>}
    </DialogFooter>
  </DialogContent></Dialog>;
}
