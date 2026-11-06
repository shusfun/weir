// VPS 与节点：每台 VPS 一张卡片，最上面是在不在线，直接显示主机观测；它上面的每个节点一行，点开看细节；
// 「＋ 协议」直接在这台 VPS 上建节点。VPS 用 SSH 一次装好，或复制安装命令自己执行。
import { useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { useQuery } from "../lib/query";
import {
  Badge, Button, Card, CardContent, Chip, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Field, Form, Input,
  Notice, PageHeader, Readout, SkeletonCards, StatusLamp, SwitchField, useToast, useConfirm, type LampTone,
} from "@weir/ui";
import { Pencil, Plus, Route, Server, Sparkles, Trash2 } from "lucide-react";
import { openDsh } from "./dsh";
import { ObjectLink, dataObject, useDialogParam } from "./object-link";
import { exitKindName, probeText } from "./chain-exit";
import { NodeAccess } from "./node-access";
import { NodeTestControl } from "./node-test";

import { ProcessUsage, freshHost } from "./resource-usage";
import { NodeSummary } from "./node-summary";
import { GateDetail, pct, size } from "./gate-detail";
import { api } from "../api";
import { useFormAction, useRowAction } from "../lib/action";
import { WaitText } from "./wait";
import { GateUpdateLine, GateVersion } from "./gate-version";
import { State } from "./feedback";
import { InviteGate } from "./invite-gate";
import { CertManager } from "./cert-manager";
import { NodeWizard } from "./node-wizard";
import { date } from "../lib/models";
import { rateText } from "../lib/linkmap";
import { field, useConsoleContext } from "../lib/console-context";
import { nodeStatus, protocolNames, relativeTime, type Gate, type NodeItem } from "../lib/core";
import { GettingStarted } from "./getting-started";


import { memo } from "react";
import { useNavigate } from "react-router-dom";
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

export function protocolLabel(n: NodeItem) {
  return (protocolNames[n.protocol] ?? n.protocol) + (n.security === "reality" ? " + REALITY" : n.network === "ws" ? " + WS" : "");
}

function exitLabel(n: NodeItem, nodes: NodeItem[]) {
  if (n.exit.kind === "freedom") return "直连";
  if (n.exit.kind === "source") return "订阅源节点";
  if (n.exit.kind === "node") return "经 " + (nodes.find((x) => x.id === n.exit.node_id)?.name ?? "节点");
  return exitKindName[n.exit.kind];
}


export const GateCard = memo(function GateCard({gate:g,nodes,onDetail,onEdit,onNode,onAdd}:{gate:Gate;nodes:NodeItem[];onDetail:(g:Gate)=>void;onEdit:(g:Gate)=>void;onNode:(n:NodeItem)=>void;onAdd:(id:number)=>void}) {
const navigate=useNavigate();

        const [label, tone] = gateStatus(g);
        const [stateLabel, stateTone] = stateText[g.state];
        const list = nodes;
        const usable = list.filter((n) => nodeStatus(n, g)[1] === "ok").length;
        // 离线时不把上一次的主机数据当成现在的
        const host = freshHost(g.host,g.online) ?? {};
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
                  <Button size="sm" variant="outline" onClick={() => onDetail(g)}>详情</Button>
                  <Button size="sm" variant="outline" onClick={() => navigate(`/exits?gate=${g.id}`)}><Route />出口与分流</Button>
                  <Button size="sm" variant="outline" onClick={() => onEdit(g)}><Pencil />编辑</Button>
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
              <ProcessUsage host={g.host} online={g.online} names={["xray", "sing-box"]} />
              {list.length > 0 && (
                <ul className="group-list" aria-label={`${g.name} 上的节点`}>
                  {list.map((n) => {
                    const [s, t, why] = nodeStatus(n, g);
                    return (
                      <li key={n.id} {...dataObject({ type: "node", id: n.id })}>
                        <div className="grid min-w-0 grid-cols-[minmax(0,1fr)_minmax(0,1fr)] items-center gap-2 max-sm:grid-cols-1">
                        <button type="button" onClick={() => onNode(n)} className="weir-press flex min-h-[var(--weir-tap-touch)] w-full min-w-0 flex-wrap items-center gap-x-3 gap-y-0.5 py-1.5 text-left text-sm">
                          <span className="flex min-w-0 items-start gap-2"><StatusLamp tone={t} label={s} /><NodeSummary node={n} /></span>
                          <small className="text-[var(--weir-ink-3)]">{s} · {n.rate?.minutes ? rateText(n.rate.p90_bps) : "速率未知"}</small>
                          {t !== "ok" && <small className={"basis-full " + (t === "bad" ? "text-[var(--weir-bad-ink)]" : t === "warn" ? "text-[var(--weir-warn-ink)]" : "text-[var(--weir-ink-3)]")}>{why}</small>}
                        </button>
                        <NodeTestControl node={n} gate={g} />
                        </div>
                      </li>
                    );
                  })}
                </ul>
              )}
              <div className="flex flex-wrap items-center gap-2">
                <Chip add disabled={g.status !== "active"} onClick={() => onAdd(g.id)}><Plus />节点</Chip>
              </div>
              {g.status === "active" && g.state !== "applied" && <small className="text-[var(--weir-ink-3)]">配置：<Badge variant="soft" tone={stateTone === "off" ? "neutral" : stateTone}>{stateLabel}</Badge></small>}
            </CardContent>
          </Card>
        );

});
