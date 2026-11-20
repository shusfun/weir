import { useEffect, useRef, useState } from "react";
import { Button, Collapsible, Field, Form, FormGrid, Input, Notice, Select, SelectContent, SelectItem, SelectTrigger, SelectValue, useConfirm } from "@weir/ui";
import { useFormAction } from "../lib/action";
import { api } from "../api";
import { useQuery } from "../lib/query";
import { browserSession, measureBrowserPath, type BrowserResult, type BrowserSession, type TestPurpose } from "../lib/browser-test";
import { field, useConsoleContext } from "../lib/console-context";
import { State } from "./feedback";

function resultText(result: BrowserResult | undefined) {
  if (!result) return "尚未测试";
  const timing = result.latency_ms == null ? "" : ` · ${Math.round(result.latency_ms)} ms`;
  const download = result.download_ms == null ? "" : ` · 下载 ${result.bytes} 字节 / ${Math.round(result.download_ms)} ms`;
  return result.reason + timing + download;
}

const labels: Record<TestPurpose, string> = { https: "HTTPS 延迟与下载", webrtc: "UDP 往返", webtransport: "QUIC 往返" };
// BrowserPathTest：blocked 是不能测的原因（由 GateBrowserTest 传入）；按节点测试时自己向中心查就绪状态。
export function BrowserPathTest({ gateId, nodeId, blocked }: { gateId?: number; nodeId?: number; blocked?: string }) {
  const readiness = useQuery({ queryKey: ["browser-test-ready", nodeId], enabled: !!nodeId && !gateId, queryFn: () => api(`/me/nodes/${nodeId}/browser-test`) as Promise<{ ready: boolean; reason: string }> });
  const reason = gateId ? blocked : readiness.isPending ? "正在确认测试服务" : readiness.error ? "测试服务状态未知" : readiness.data?.reason;
  const [busy, setBusy] = useState<TestPurpose | null>(null);
  const [results, setResults] = useState<Partial<Record<TestPurpose, BrowserResult>>>({});
  const controller = useRef<AbortController | null>(null);
  useEffect(() => () => controller.current?.abort(), []);
  useConsoleContext(busy ? { notices: ["正在进行浏览器到 VPS 的独立网络实测"] } : null);
  async function run(purpose: TestPurpose) {
    const control = new AbortController(); controller.current = control; setBusy(purpose);
    const deadline = setTimeout(() => control.abort(), 100000);
    let session: BrowserSession | undefined;
    try {
      session = browserSession(await api(gateId ? `/admin/gates/${gateId}/browser-tests` : `/me/nodes/${nodeId}/browser-tests`, "POST", { purpose }));
      for (;;) {
        control.signal.throwIfAborted();
        const status = await api(`/browser-tests/${session.id}`) as { status: string };
        if (status.status === "ready") break;
        if (status.status !== "pending") throw new Error("会话已失效，请重新开始");
        await new Promise<void>((resolve, reject) => {
          const abort = () => { clearTimeout(timer); reject(new Error("测试已取消")); };
          const timer = setTimeout(() => { control.signal.removeEventListener("abort", abort); resolve(); }, 1000);
          control.signal.addEventListener("abort", abort, { once: true });
        });
      }
      const measure = AbortSignal.any([control.signal, AbortSignal.timeout(20000)]);
      const result = await measureBrowserPath(session, measure);
      await api(`/browser-tests/${session.id}`, "POST", result);
      setResults(previous => ({ ...previous, [purpose]: result }));
    } catch (error) {
      const reason = control.signal.aborted ? "测试已取消或超时" : error instanceof Error ? error.message : "测试未完成";
      setResults(previous => ({ ...previous, [purpose]: { status: "failed", reason, bytes: 0 } }));
    } finally {
      clearTimeout(deadline);
      if (session) {
        try { await api(`/browser-tests/${session.id}`, "DELETE"); }
        catch { setResults(previous => ({ ...previous, [purpose]: { status: "failed", reason: "中心未确认会话结束；本地测试已停止，服务端令牌最多两分钟后失效", bytes: 0 } })); }
      }
      if (controller.current === control) { controller.current = null; setBusy(null); }
    }
  }
  const button = (purpose: TestPurpose, variant: "default" | "outline") => <div className="stack-tight" key={purpose}>
    <Button variant={variant} disabled={!!busy} busy={busy === purpose} busyText="正在测试" onClick={() => void run(purpose)}>{labels[purpose]}</Button>
    {results[purpose] && <small role="status">{resultText(results[purpose])}</small>}
  </div>;
  if (reason) return <p className="m-0 text-meta text-[var(--weir-ink-3)]">浏览器测试：{reason}</p>;
  return <div className="stack-tight">
    {button("https", "outline")}
    <Collapsible title="更多：UDP 与 QUIC 往返">
      <div className="stack-tight">{button("webrtc", "outline")}{button("webtransport", "outline")}</div>
    </Collapsible>
    {busy && <Button variant="ghost" onClick={() => controller.current?.abort()}>取消测试</Button>}
    <small className="text-[var(--weir-ink-3)]">只测这台浏览器到 VPS 的连接，下载 4 MiB 并限速，不代表线路带宽上限。</small>
  </div>;
}

type TestConfig = { config: { host: string; cert_id: number; port: number; rtc_port: number } | null; observation: { observed_at: string; healthy: boolean; reason?: string } | null };
type Cert = { id: number; name: string; domain: string; gate_id?: number; not_after?: string };
// GateBrowserTest 按「现在能不能测」组织：未启用只给启用入口；已保存等 VPS 回执；就绪后才出现测试按钮。
export function GateBrowserTest({ gateId }: { gateId: number }) {
  const q = useQuery({ queryKey: ["browser-test-config", gateId], queryFn: () => api(`/admin/gates/${gateId}/browser-test`) as Promise<TestConfig>, refetchInterval: (query) => query.state.data?.config && !query.state.data.observation ? 5000 : false });
  const certs = useQuery({ queryKey: ["browser-test-certs"], queryFn: () => api("/admin/certs") as Promise<{ items: Cert[] }> });
  const [editing, setEditing] = useState(false);
  const [host, setHost] = useState<string | null>(null), [cert, setCert] = useState<string | null>(null);
  const requested = useRef(true);
  const confirm = useConfirm();
  const usable = certs.data?.items.filter(c => !c.gate_id || c.gate_id === gateId) ?? [];
  const certId = cert ?? (q.data?.config?.cert_id ? String(q.data.config.cert_id) : usable.length === 1 ? String(usable[0].id) : "");
  const picked = usable.find(c => String(c.id) === certId);
  const hostValue = host ?? q.data?.config?.host ?? picked?.domain ?? "";
  useConsoleContext({ form: [field("host", "测试地址", hostValue), field("cert_id", "测试证书", picked ? `${picked.name} · ${picked.domain}` : "")] });
  const saveAction = useFormAction(() => api(`/admin/gates/${gateId}/browser-test`, "PUT", { enabled: requested.current, host: hostValue, cert_id: Number(certId || 0) }), { onSuccess: () => { setEditing(false); return q.refetch(); }, fields: { browser_test_host_required: "host", browser_test_cert_required: "cert_id" } });
  const busy = saveAction.busy;
  async function save(enabled: boolean) {
    if (!enabled && !await confirm({ title: "关闭浏览器测试？", description: "关闭监听并取消当前测试会话。", confirmText: "关闭", tone: "danger" })) return;
    requested.current = enabled;
    await saveAction.submit();
  }
  const config = q.data?.config, observed = q.data?.observation;
  const status = !config ? "未启用" : !observed ? "等待 VPS 应用" : observed.healthy ? "已就绪" : "监听已停止";
  const blocked = !config ? "未启用" : !observed ? "等待 VPS 应用配置" : observed.healthy ? "" : observed.reason || "测试监听已停止，请重新保存配置";
  const certLabel = (c: Cert) => `${c.domain}${c.name && c.name !== c.domain ? `（${c.name}）` : ""}${c.not_after ? ` · 到期 ${new Date(c.not_after).toLocaleDateString()}` : ""}`;
  return <Collapsible variant="section" title="浏览器测试" summary={status}>
    {q.isPending || q.error ? <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} /> : !config && !editing ?
      <div className="actions"><Button variant="outline" onClick={() => setEditing(true)}>启用测试</Button></div> : <>
      {(editing || !config) && <Form busy={busy} errors={saveAction.errors} onSubmit={() => void save(true)}><FormGrid>
        <Field name="cert_id" label="测试证书" required>{certs.error ? <State error={certs.error} onRetry={() => certs.refetch()} /> : !usable.length && certs.isSuccess ? <small>这台 VPS 还没有可用证书，请先在证书设置里申请。</small> :
          <Select name="cert_id" required value={certId} onValueChange={v => { setCert(v ?? ""); setHost(null); }}><SelectTrigger><SelectValue placeholder="选择证书">{picked ? certLabel(picked) : undefined}</SelectValue></SelectTrigger><SelectContent>{usable.map(c => <SelectItem key={c.id} value={String(c.id)}>{certLabel(c)}</SelectItem>)}</SelectContent></Select>}</Field>
        <Field name="host" label="测试地址" required><Input value={hostValue} onChange={e => setHost(e.target.value)} placeholder="证书里的域名" required /></Field>
      </FormGrid><div className="actions"><Button type="submit" busy={busy} busyText="正在保存">{config ? "保存修改" : "启用"}</Button>{editing && config && <Button type="button" variant="outline" disabled={busy} onClick={() => setEditing(false)}>取消</Button>}</div></Form>}
      {saveAction.error && <Notice tone="bad">{saveAction.error}</Notice>}
      {config && !editing && <div className="stack-tight">
        <small className="text-[var(--weir-ink-3)]">{config.host} · 端口 {config.port}（TCP/UDP）、{config.rtc_port}（UDP），云安全组需放行。</small>
        <BrowserPathTest gateId={gateId} blocked={blocked} />
        <div className="actions"><Button variant="ghost" size="sm" onClick={() => setEditing(true)}>修改配置</Button><Button variant="ghost" size="sm" disabled={busy} onClick={() => void save(false)}>关闭测试</Button></div>
      </div>}
    </>}
  </Collapsible>;
}
