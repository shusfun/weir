import { NodeSummary } from "./node-summary";
import { useEffect, useState } from "react";
import { api } from "../api";
import type { NodeChoice } from "../lib/core";
// 链式代理出口的填写：建节点第 ③ 步（管理员）与用户自建链式代理共用。
// 代理文本由用户点击解析；节点链接只接受单个节点，订阅地址会被拒绝。
import { CheckboxField, Field, FormGrid, Input, Notice, PasswordInput, ChoiceCard, Radio, RadioGroup, Segmented, Select, SelectContent, SelectItem, SelectTrigger, SelectValue, SkeletonCards, StatusLamp, Textarea } from "@weir/ui";
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
  const where = p.location === "center" ? "中心" : p.location === "gate" ? "在 VPS 上" : "";
  if (!p.ok) return [where, p.error ? `实测失败：${p.error}` : "实测失败：原因未知"].filter(Boolean).join(" · ");
  const parts = [where, p.mode === "proxy" ? "已连通" : "只测了端口"].filter(Boolean);
  parts.push(p.exit_ip ? `落地 ${p.exit_ip}` : "落地未知");
  parts.push(placeText(p.place));
  if (p.latency_ms !== undefined) parts.push(`${p.latency_ms} ms`);
  parts.push(`速率 ${rateText(p.rate_bps)}`);
  if (p.note && p.mode !== "proxy") parts.push(p.note);
  return parts.join(" · ");
}

export function ProbeReadouts({ result, location, historical = false }: { result: ExitProbe | null | undefined; location: string; historical?: boolean }) {
  const latencyKnown = result?.latency_ms != null;
  const udpCaption = !result ? "等待测试" : result.udp === true ? "UDP 可用" : result.udp === false ? "仅 TCP 可通" : result.mode === "proxy" ? "代理协议往返" : "TCP 端口握手";
  return (
    <div className="rounded-[var(--weir-radius-17)] border border-[var(--weir-line-soft)] bg-[var(--weir-well)] p-[var(--weir-space-4)] [corner-shape:squircle]">
      <div className="mb-[var(--weir-space-3)] flex flex-wrap items-center justify-between gap-2 border-b border-[var(--weir-line-soft)] pb-[var(--weir-space-3)]">
        <span className="flex items-center gap-2">
          <StatusLamp tone={!result ? "unknown" : result.ok ? "ok" : "bad"} />
          <strong className="text-body font-semibold">
            {!result ? "尚未实测" : result.ok ? (result.mode === "proxy" ? "代理握手已连通" : "端口可达") : "实测未连通"}
          </strong>
        </span>
        <span className="font-mono text-fine text-[var(--weir-ink-3)]">
          {!result ? `${location} · 点下方开始实测` : `${historical ? "上次记录" : "本次结果"} · ${location}`}
        </span>
      </div>
      {/* 还没实测时只留上面一行结论，不摆出三栏「未知」 */}
      {result && <div className="grid gap-[var(--weir-space-3)] sm:grid-cols-3 sm:divide-x sm:divide-[var(--weir-line-soft)]">
        <div className="grid content-start gap-1">
          <span className="text-fine text-[var(--weir-ink-3)]">延迟与协议</span>
          <span className={latencyKnown ? "font-mono text-title3 font-semibold text-[var(--weir-ink)]" : "text-headline font-semibold text-[var(--weir-ink-3)]"}>
            {latencyKnown ? <>{result!.latency_ms}<span className="ml-1 font-sans text-meta font-normal text-[var(--weir-ink-3)]">ms</span></> : "未知"}
          </span>
          <span className="text-fine text-[var(--weir-ink-3)]">{udpCaption}</span>
        </div>
        <div className="grid content-start gap-1 sm:pl-[var(--weir-space-3)]">
          <span className="text-fine text-[var(--weir-ink-3)]">落地出口</span>
          <span className={result?.exit_ip ? "font-mono text-subheadline font-semibold break-all text-[var(--weir-ink)]" : "text-headline font-semibold text-[var(--weir-ink-3)]"}>
            {result?.exit_ip ?? "未知"}
          </span>
          <span className="text-fine text-[var(--weir-ink-3)]">{result ? placeText(result.place) : ""}</span>
        </div>
        <div className="grid content-start gap-1 sm:pl-[var(--weir-space-3)]">
          <span className="text-fine text-[var(--weir-ink-3)]">下载吞吐</span>
          <span className={result?.rate_bps != null ? "font-mono text-subheadline font-semibold text-[var(--weir-ink)]" : "text-headline font-semibold text-[var(--weir-ink-3)]"}>
            {result ? rateText(result.rate_bps) : "未知"}
          </span>
          <span className="text-fine text-[var(--weir-ink-3)] break-words">{result?.download_error || (result?.rate_bps == null ? "这次没有测下载" : "测试下载")}</span>
        </div>
      </div>}
      {result && !result.ok && result.error && (
        <div className="mt-[var(--weir-space-3)] rounded-[var(--weir-radius-9)] bg-[var(--weir-bad-soft)] px-3 py-2 text-meta text-[var(--weir-bad-ink)] break-words">
          {result.error}
        </div>
      )}
    </div>
  );
}

export function ExitFields({ value, onChange, admin, sourcePath, nodes, nodeHint, savedSecrets, hideNodeOption }: {
  value: ExitSpec;
  onChange: (v: ExitSpec) => void;
  admin: boolean;
  sourcePath: string;
  nodes: NodeChoice[];
  nodeHint: string;
  savedSecrets?: { password?: boolean; link?: boolean };
  hideNodeOption?: boolean;
}) {
  const top = topOf(value.kind);
  const set = (patch: Partial<ExitSpec>) => onChange({ ...value, ...patch });
  const catalog = useExitCatalog(admin);
  const options = (catalog.data?.kinds ?? []).filter((v, i, a) =>
    v.kind !== "freedom"
    && (!hideNodeOption || (v.group !== "node" && v.group !== "blackhole"))
    && a.findIndex(x => x.group === v.group) === i,
  );

  const pick = (t: Top) => onChange(t === "proxy" ? { kind: "socks5", remark: value.remark } : t === "source" ? { kind: "source", mode: "filter", remark: value.remark } : { kind: t, remark: value.remark });
  if (!catalog.data) return <State loading={catalog.isPending} error={catalog.error} onRetry={() => catalog.refetch()} skeleton={<SkeletonCards count={3} lines={1} />} />;
  return (
    <>
      <RadioGroup aria-label="出口类型" value={top} onValueChange={v => pick(String(v) as Top)} className="grid gap-2 sm:grid-cols-3">
        {options.map(k => (
          <ChoiceCard key={k.group} selected={top === k.group} render={<label />} className="min-w-0 py-3">
            <Radio value={k.group} className="sr-only" />
            <strong className="truncate text-body font-semibold">{k.group === "proxy" ? "SOCKS5 / HTTP" : k.label}</strong>
            <small className="text-fine text-[var(--weir-ink-3)]">{k.description}</small>
          </ChoiceCard>
        ))}
      </RadioGroup>
      {top !== "freedom" && <Field label="备注（可选）" name="remark" hint="显示在管理列表与订阅节点名里"><Input maxLength={100} placeholder="例如：美东住宅 SOCKS5" value={value.remark ?? ""} onChange={e => set({ remark: e.target.value })} /></Field>}
      {top === "freedom" && <p className="m-0 text-meta text-[var(--weir-ink-3)]">流量从这台 VPS 直接访问目标网站。</p>}
      {top === "source" && <SourcePicker path={sourcePath} value={value} onChange={(v) => onChange({ ...value, ...v, kind: "source" })} />}
      {top === "proxy" && <ProxyFields value={value} set={set} savedPassword={savedSecrets?.password} />}
      {top === "link" && <LinkField value={value.link ?? ""} onChange={(link) => set({ link, insecure_identities: [] })} confirmations={value.insecure_identities ?? []} onConfirm={(insecure_identities) => set({ insecure_identities })} saved={savedSecrets?.link && value.link === undefined} />}
      {top === "node" &&
        (nodes.length === 0 ? (
          <State empty="没有可作出口的节点" detail={nodeHint} />
        ) : (
          <Field label="落地节点" name="exitNode" required>
            <Select required value={value.node_id ? String(value.node_id) : ""} onValueChange={(v) => set({ node_id: Number(v) })}>
              <SelectTrigger><SelectValue>{nodes.find(n => n.id === value.node_id) ? <NodeSummary node={nodes.find(n => n.id === value.node_id)!} compact /> : "请选择落地节点"}</SelectValue></SelectTrigger>
              <SelectContent>
                {nodes.map((n) => (
                  <SelectItem key={n.id} value={String(n.id)}><NodeSummary node={n} /></SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
        ))}
      {top !== "freedom" && top !== "blackhole" && <small className="block text-fine text-[var(--weir-ink-3)]">在节点上启用时由入口 VPS 实测连通后生效；用户流量只在入口计量一次。</small>}
    </>
  );
}

function ProxyFields({ value, set, savedPassword }: { value: ExitSpec; set: (p: Partial<ExitSpec>) => void; savedPassword?: boolean }) {
  return (
    <>
      <div className="flex flex-wrap items-center justify-between gap-2">
        <Segmented aria-label="代理协议" size="sm" value={value.kind as ProxyKind} onValueChange={(kind) => set({ kind })} options={(["socks5","http","https"] as const).map(k=>[k,k.toUpperCase()] as const)} />
      </div>
      <ProxyPaste onApply={p => set({ kind: p.kind, address: p.address, port: p.port, username: p.username ?? "", password: p.password ?? "" })} />
      <div className="grid min-w-0 gap-x-[var(--weir-space-3)] gap-y-[var(--weir-gap-9)] sm:grid-cols-[minmax(0,1fr)_131px]">
        <Field label="服务器地址" name="address" required hint={value.kind === "https" ? "按这个地址校验证书" : "填写公网 IP 或域名"}>
          <Input required autoComplete="off" placeholder="203.0.113.10" value={value.address ?? ""} onChange={(e) => set({ address: e.target.value.trim() })} />
        </Field>
        <Field label="端口" name="port" required customError={value.port && value.port > 65535 ? "端口须在 1–65535 之间" : ""}>
          <Input inputMode="numeric" required placeholder="1080" value={value.port ? String(value.port) : ""} onChange={(e) => set({ port: Number(e.target.value.replace(/\D/g, "")) || undefined })} />
        </Field>
      </div>
      <FormGrid>
        <Field label="用户名（可选）" name="username">
          <Input autoComplete="off" value={value.username ?? ""} onChange={(e) => set({ username: e.target.value })} />
        </Field>
        <Field label="密码（可选）" name="password" hint={savedPassword ? "已保存；留空保留原密码" : undefined}>
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
