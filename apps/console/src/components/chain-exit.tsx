// 链式代理出口的填写：建节点第 ③ 步（管理员）与用户自建链式代理共用。
// 代理支持 SOCKS5 / HTTP / HTTPS，可以直接粘贴商家给的文本自动拆开；分享链接只接受单个节点，订阅地址会被拒绝。
import { useState } from "react";
import { Field, FormGrid, Input, Notice, PasswordInput, Segmented, Select, SelectContent, SelectItem, SelectTrigger, SelectValue, Textarea } from "@weir/ui";
import type { ChainSpec, ExitKind, ExitProbe } from "../lib/core";
import { parseExitText, type ProxyKind } from "../lib/exit-parse";
import { State } from "./feedback";
import { SourcePicker } from "./source-picker";

type Top = "none" | "source" | "proxy" | "link" | "node";
const topOf = (k: ExitKind): Top => (k === "socks5" || k === "http" || k === "https" ? "proxy" : k);
const proxyKinds = [["socks5", "SOCKS5"], ["http", "HTTP"], ["https", "HTTPS"]] as const;

export const exitKindName: Record<string, string> = {
  none: "直接出去", socks5: "SOCKS5 代理", http: "HTTP 代理", https: "HTTPS 代理", link: "分享链接节点", node: "另一台 VPS", source: "订阅源节点",
};

// exitErrorFields 把出口相关的错误码标到实际显示的字段上（按当前出口类型）。
export function exitErrorFields(kind: ExitKind): Record<string, string> {
  switch (topOf(kind)) {
    case "proxy":
      return { exit_not_public: "address", invalid_chain: "address", exit_unsupported: "address", chain_probe_failed: "address", invalid_outbound_credentials: "username" };
    case "link":
      return { invalid_link: "link", exit_not_public: "link", exit_unsupported: "link", chain_probe_failed: "link" };
    case "node":
      return { exit_not_allowed: "exitNode", chain_self: "exitNode", chain_cycle: "exitNode", chain_exit_unavailable: "exitNode", chain_exit_unsupported: "exitNode", chain_exit_no_address: "exitNode", exit_unsupported: "exitNode", chain_probe_failed: "exitNode" };
    case "source":
      return { exit_not_allowed: "source_id", source_not_found: "source_id", invalid_chain: "match", chain_exit_unavailable: "match", group_unsupported: "match", exit_unsupported: "match", chain_probe_failed: "match" };
  }
  return {};
}

// probeText 说明实测结果：按协议真连时给出实际出口 IP 与延迟；只测了端口时如实写明。
export function probeText(p?: ExitProbe | null) {
  if (!p) return "还没有实测";
  const parts = [p.mode === "proxy" ? "已按协议实测" : "只测了端口"];
  if (p.exit_ip) parts.push("出口 IP " + p.exit_ip);
  if (p.latency_ms !== undefined) parts.push(p.latency_ms + " ms");
  if (p.note && p.mode !== "proxy") parts.push(p.note);
  return parts.join(" · ");
}

export function ChainExitFields({ value, onChange, admin, sourcePath, nodes, nodeHint }: {
  value: ChainSpec;
  onChange: (v: ChainSpec) => void;
  admin: boolean;
  sourcePath: string;
  nodes: { id: number; name: string; gate_name?: string }[];
  nodeHint: string;
}) {
  const top = topOf(value.kind);
  const set = (patch: Partial<ChainSpec>) => onChange({ ...value, ...patch });
  const options: (readonly [Top, string])[] = admin
    ? [["none", "直接出去"], ["source", "订阅源节点"], ["proxy", "代理"], ["node", "另一台 VPS"], ["link", "分享链接"]]
    : [["source", "订阅组里的节点"], ["proxy", "代理"], ["node", "另一节点"], ["link", "分享链接"]];
  const pick = (t: Top) => onChange(t === "proxy" ? { kind: "socks5" } : t === "source" ? { kind: "source", mode: "filter" } : { kind: t });
  return (
    <>
      <Segmented aria-label="出口类型" value={top} onValueChange={pick} options={options} />
      {top === "none" && <p className="text-[var(--mg-ink-3)]">流量从这台 VPS 直接访问目标网站。</p>}
      {top === "source" && <SourcePicker path={sourcePath} value={value} onChange={(v) => onChange({ ...value, ...v, kind: "source" })} />}
      {top === "proxy" && <ProxyFields value={value} set={set} />}
      {top === "link" && <LinkField value={value.link ?? ""} onChange={(link) => set({ link })} />}
      {top === "node" &&
        (nodes.length === 0 ? (
          <State empty="没有可作出口的节点" detail={nodeHint} />
        ) : (
          <Field label="出口节点" name="exitNode" required>
            <Select required value={value.node_id ? String(value.node_id) : ""} onValueChange={(v) => set({ node_id: Number(v) })}>
              <SelectTrigger><SelectValue>{nodes.find((n) => n.id === value.node_id)?.name ?? "请选择"}</SelectValue></SelectTrigger>
              <SelectContent>
                {nodes.map((n) => (
                  <SelectItem key={n.id} value={String(n.id)}>{n.gate_name ? `${n.name}（${n.gate_name}）` : n.name}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
        ))}
      {top !== "none" && <Notice role="status">保存前由入口 VPS 实测：代理、分享链接与订阅节点按协议真连一次并记下实际出口 IP；流量只在入口计量一次。</Notice>}
    </>
  );
}

function ProxyFields({ value, set }: { value: ChainSpec; set: (p: Partial<ChainSpec>) => void }) {
  const [paste, setPaste] = useState("");
  const [pasteError, setPasteError] = useState("");
  const apply = (text: string) => {
    setPaste(text);
    if (!text.trim()) return setPasteError("");
    const r = parseExitText(text);
    if (r.type === "proxy") {
      setPasteError("");
      set({ kind: r.proxy.kind ?? (value.kind as ProxyKind), address: r.proxy.address, port: r.proxy.port, username: r.proxy.username ?? "", password: r.proxy.password ?? "" });
    } else if (r.type === "link") {
      setPasteError("这是一条分享链接，请改选「分享链接」");
    } else if (r.type === "subscription") {
      setPasteError("这是订阅地址，不能作出口");
    } else {
      setPasteError(r.message);
    }
  };
  return (
    <>
      <Field label="粘贴识别（可选）" customError={pasteError} hint="支持 host:port、host:port:用户名:密码、用户名:密码@host:port、socks5:// 与 http(s):// 地址；识别后填到下面。">
        <Input autoComplete="off" spellCheck={false} value={paste} placeholder="1.2.3.4:1080:user:pass" onChange={(e) => apply(e.target.value)} />
      </Field>
      <Segmented aria-label="代理协议" size="sm" value={value.kind as ProxyKind} onValueChange={(kind) => set({ kind })} options={proxyKinds} />
      <FormGrid>
        <Field label="地址" name="address" required hint={value.kind === "https" ? "按这个地址校验证书" : "只能是公网地址"}>
          <Input required autoComplete="off" value={value.address ?? ""} onChange={(e) => set({ address: e.target.value.trim() })} />
        </Field>
        <Field label="端口" required customError={value.port && value.port > 65535 ? "端口须在 1–65535 之间" : ""}>
          <Input inputMode="numeric" required value={value.port ? String(value.port) : ""} onChange={(e) => set({ port: Number(e.target.value.replace(/\D/g, "")) || undefined })} />
        </Field>
        <Field label="用户名（可选）" name="username">
          <Input autoComplete="off" value={value.username ?? ""} onChange={(e) => set({ username: e.target.value })} />
        </Field>
        <Field label="密码（可选）">
          <PasswordInput autoComplete="new-password" value={value.password ?? ""} onChange={(e) => set({ password: e.target.value })} />
        </Field>
      </FormGrid>
    </>
  );
}

function LinkField({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  const r = value.trim() ? parseExitText(value) : null;
  const hint = r?.type === "subscription" ? "这是订阅地址，不能作出口；请粘贴其中一个节点的分享链接" : r?.type === "proxy" ? "这是代理地址，请改选「代理」" : r?.type === "error" && r.message !== "内容为空" ? r.message : "";
  return (
    <Field label="分享链接" name="link" required customError={hint} hint="一个节点的链接：vless:// vmess:// trojan:// ss:// hysteria2:// tuic:// anytls://。跳过证书校验或只用证书指纹的节点不能作出口。">
      <Textarea required rows={3} spellCheck={false} className="font-mono text-xs" value={value} onChange={(e) => onChange(e.target.value.trim())} />
    </Field>
  );
}
