import { NodeSummary } from "./node-summary";
import { useEffect, useState } from "react";
import { api } from "../api";
import type { NodeChoice } from "../lib/core";
// 链式代理出口的填写：建节点第 ③ 步（管理员）与用户自建链式代理共用。
// 代理文本由用户点击解析；节点链接只接受单个节点，订阅地址会被拒绝。
import { CheckboxField, Field, FormGrid, Input, Notice, PasswordInput, ChoiceCard, Radio, RadioGroup, Readout, Segmented, Select, SelectContent, SelectItem, SelectTrigger, SelectValue, Textarea } from "@weir/ui";
import type { ExitSpec, ExitKind, ExitProbe } from "../lib/core";
import { placeText, rateText } from "../lib/linkmap";
import { parseExitText, type ProxyKind } from "../lib/exit-parse";
import { useExitCatalog } from "../lib/exits";
import { State } from "./feedback";
import { ProxyPaste } from "./proxy-paste";
import { SourcePicker } from "./source-picker";

type Top = "freedom" | "blackhole" | "source" | "proxy" | "link" | "node";
const topOf = (k: ExitKind): Top => (k === "socks5" || k === "http" || k === "https" ? "proxy" : k);


export { exitKindName } from "../lib/exits";

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

// probeText 说明一次实测：位置、连通、落地、地区和速率都只属于这次结果。
export function probeText(p?: ExitProbe | null) {
  if (!p) return "还没有实测";
  const where = p.location === "center" ? "中心" : p.location === "gate" ? `VPS ${p.gate_id ?? ""}`.trim() : "";
  if (!p.ok) return [where, p.error ? `实测失败：${p.error}` : "实测失败：原因未知"].filter(Boolean).join(" · ");
  const parts = [where, p.mode === "proxy" ? "已连通" : "只测了端口"].filter(Boolean);
  parts.push(p.exit_ip ? `落地 ${p.exit_ip}` : "落地未知");
  parts.push(placeText(p.place));
  if (p.latency_ms !== undefined) parts.push(`${p.latency_ms} ms`);
  parts.push(`速率 ${rateText(p.rate_bps)}`);
  if (p.note && p.mode !== "proxy") parts.push(p.note);
  return parts.join(" · ");
}

export function ProbeReadouts({ result, location }: { result: ExitProbe | null | undefined; location: string }) {
  return (
    <div className="stack weir-probe-readouts">
      <Readout label="测试位置" value={location} />
      <Readout label="连通" value={result ? (result.ok ? "已连通" : "未连通") : null} caption={result?.latency_ms != null ? `${result.latency_ms} ms` : "延迟未知"} />
      <Readout label="落地" value={result?.exit_ip ?? null} caption={result ? placeText(result.place) : "位置未知"} />
      <Readout label="速率" value={result ? rateText(result.rate_bps) : null} caption={result?.download_error || (result?.rate_bps == null ? "速率未知" : "本次下载")} />
    </div>
  );
}

export function ExitFields({ value, onChange, admin, sourcePath, nodes, nodeHint, savedSecrets }: {
  value: ExitSpec;
  onChange: (v: ExitSpec) => void;
  admin: boolean;
  sourcePath: string;
  nodes: NodeChoice[];
  nodeHint: string;
  savedSecrets?: { password?: boolean; link?: boolean };
}) {
  const top = topOf(value.kind);
  const set = (patch: Partial<ExitSpec>) => onChange({ ...value, ...patch });
  const catalog = useExitCatalog(admin);
  const options = (catalog.data?.kinds ?? []).filter((v,i,a) => v.kind !== "freedom" && a.findIndex(x=>x.group===v.group)===i);

  const pick = (t: Top) => onChange(t === "proxy" ? { kind: "socks5",remark:value.remark } : t === "source" ? { kind: "source", mode: "filter",remark:value.remark } : { kind: t,remark:value.remark });
  return (
    <>
      <RadioGroup aria-label="出口类型" value={top} onValueChange={v=>pick(String(v) as Top)} className="grid gap-2 sm:grid-cols-2">
        {options.map(k=><ChoiceCard key={k.group} selected={top===k.group} className="min-w-0"><Radio value={k.group}/><span className="min-w-0"><strong className="block">{k.group==="proxy"?"代理":k.label}</strong><small>{k.description}</small></span></ChoiceCard>)}
      </RadioGroup>
      {!catalog.data && <State loading={catalog.isPending} error={catalog.error} onRetry={()=>catalog.refetch()}/>}
      {top !== "freedom" && <Field label="备注（可选）" name="remark" hint="会显示在订阅的节点名里"><Input maxLength={100} value={value.remark ?? ""} onChange={e=>set({remark:e.target.value})}/></Field>}
      {top === "freedom" && <p className="text-[var(--weir-ink-3)]">流量从这台 VPS 直接访问目标网站。</p>}
      {top === "source" && <SourcePicker path={sourcePath} value={value} onChange={(v) => onChange({ ...value, ...v, kind: "source" })} />}
      {top === "proxy" && <ProxyFields value={value} set={set} savedPassword={savedSecrets?.password} />}
      {top === "link" && <LinkField value={value.link ?? ""} onChange={(link) => set({ link, insecure_identities: [] })} confirmations={value.insecure_identities ?? []} onConfirm={(insecure_identities) => set({insecure_identities})} saved={savedSecrets?.link && value.link === undefined} />}
      {top === "node" &&
        (nodes.length === 0 ? (
          <State empty="没有可作出口的节点" detail={nodeHint} />
        ) : (
          <Field label="出口节点" name="exitNode" required>
            <Select required value={value.node_id ? String(value.node_id) : ""} onValueChange={(v) => set({ node_id: Number(v) })}>
              <SelectTrigger><SelectValue>{nodes.find(n => n.id === value.node_id) ? <NodeSummary node={nodes.find(n => n.id === value.node_id)!} compact /> : "请选择"}</SelectValue></SelectTrigger>
              <SelectContent>
                {nodes.map((n) => (
                  <SelectItem key={n.id} value={String(n.id)}><NodeSummary node={n} /></SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
        ))}
      {top !== "freedom" && top !== "blackhole" && <Notice role="status">这里只保存出口资料。在某台 VPS 上使用时，才由那台 VPS 检查是否支持、是否连通。流量只在入口计量一次。</Notice>}
    </>
  );
}

function ProxyFields({ value, set, savedPassword }: { value: ExitSpec; set: (p: Partial<ExitSpec>) => void; savedPassword?: boolean }) {
  return (
    <>
      <ProxyPaste onApply={p => set({ kind: p.kind, address: p.address, port: p.port, username: p.username ?? "", password: p.password ?? "" })} />
      <Segmented aria-label="代理协议" size="sm" value={value.kind as ProxyKind} onValueChange={(kind) => set({ kind })} options={(["socks5","http","https"] as const).map(k=>[k,k.toUpperCase()] as const)} />
      <FormGrid>
        <Field label="地址" name="address" required hint={value.kind === "https" ? "按这个地址校验证书" : "只能是公网地址"}>
          <Input required autoComplete="off" value={value.address ?? ""} onChange={(e) => set({ address: e.target.value.trim() })} />
        </Field>
        <Field label="端口" name="port" required customError={value.port && value.port > 65535 ? "端口须在 1–65535 之间" : ""}>
          <Input inputMode="numeric" required value={value.port ? String(value.port) : ""} onChange={(e) => set({ port: Number(e.target.value.replace(/\D/g, "")) || undefined })} />
        </Field>
        <Field label="用户名（可选）" name="username">
          <Input autoComplete="off" value={value.username ?? ""} onChange={(e) => set({ username: e.target.value })} />
        </Field>
        <Field label="密码（可选）" name="password" hint={savedPassword ? "已保存密码；不修改则保留，输入后清空则删除" : undefined}>
          <PasswordInput autoComplete="new-password" placeholder={savedPassword ? "已保存" : undefined} value={value.password ?? ""} onChange={(e) => set({ password: e.target.value })} />
        </Field>
      </FormGrid>
    </>
  );
}

function LinkField({ value, onChange, saved, confirmations, onConfirm }: { value: string; onChange: (v: string) => void; saved?: boolean; confirmations: string[]; onConfirm: (ids: string[]) => void }) {
  const [preview, setPreview] = useState<{link: string; identity: string; requires_confirmation: boolean} | null>(null);
  const [failure, setFailure] = useState<{link: string; message: string} | null>(null);
  useEffect(() => {
    if (!value.trim()) return;
    const controller = new AbortController();
    const timer = window.setTimeout(() => {
      api("/me/exit-link-preview", "POST", {link: value}, {signal: controller.signal}).then(result => {
        if (!controller.signal.aborted) { setPreview({...result, link: value}); setFailure(null); }
      }).catch(error => {
        if (!controller.signal.aborted) setFailure({link: value, message: error instanceof Error ? error.message : "节点参数暂时无法检查"});
      });
    }, 350);
    return () => { window.clearTimeout(timer); controller.abort(); };
  }, [value]);
  const r = value.trim() ? parseExitText(value) : null;
  const hint = r?.type === "subscription" ? "这是订阅地址，不能作出口；请粘贴其中一个节点的链接" : r?.type === "proxy" ? "这是代理地址，请改选「代理」" : r?.type === "error" && r.message !== "内容为空" ? r.message : "";
  return (
    <>
    <Field label="节点链接" name="link" required={!saved} customError={hint || (failure?.link === value ? failure.message : "")} hint={saved ? "已保存节点链接；不填写则保留原链接" : "粘贴一个节点的链接。完整协议与安全参数由中心检查，再由入口 VPS 实测。"}>
      <Textarea required={!saved} rows={3} spellCheck={false} className="font-mono text-meta" value={value} placeholder={saved ? "已保存" : undefined} onChange={(e) => onChange(e.target.value.trim())} />
    </Field>
    {preview?.link === value && preview.requires_confirmation && <CheckboxField label="我确认这个节点跳过证书校验" description="无法验证远端身份。确认仅适用于这条节点；连接凭据或安全参数变化后需要重新确认。" checked={confirmations.includes(preview.identity)} onCheckedChange={checked => onConfirm(checked ? [preview.identity] : [])} />}
    </>
  );
}
