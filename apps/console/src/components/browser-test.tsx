import { useEffect, useRef, useState } from "react";
import { Button, Collapsible, Field, Form, FormGrid, Input, Notice, Select, SelectContent, SelectItem, SelectTrigger, SelectValue, useConfirm } from "@weir/ui";
import { useFormAction } from "../lib/action";
import { api } from "../api";
import { useQuery } from "../lib/query";
import { browserSession, measureBrowserPath, type BrowserResult, type BrowserSession, type TestPurpose } from "../lib/browser-test";
import { useConsoleContext } from "../lib/console-context";
import { State } from "./feedback";

function resultText(result: BrowserResult | undefined) {
  if (!result) return "尚未测试";
  const timing = result.latency_ms == null ? "" : ` · ${Math.round(result.latency_ms)} ms`;
  const download = result.download_ms == null ? "" : ` · 下载 ${result.bytes} 字节 / ${Math.round(result.download_ms)} ms`;
  return result.reason + timing + download;
}

const labels: Record<TestPurpose, string> = { https: "HTTPS 延迟与下载", webrtc: "UDP 往返", webtransport: "QUIC 往返" };
export function BrowserPathTest({ gateId, nodeId }: { gateId?: number; nodeId?: number }) {
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
  return <Collapsible variant="section" title="浏览器 → VPS" summary={busy ? "实测中" : "按需实测"}>
    <p className="text-meta">只测本次浏览器到 VPS 的路径。下载固定为 4 MiB，服务端限速，结果不代表线路带宽上限。代理协议及 VPS 到落地的可用性见节点实测；不能据此断言运营商限速。</p>
    {(Object.keys(labels) as TestPurpose[]).map(purpose => <div className="stack-tight" key={purpose}>
      <Button variant="outline" disabled={!!busy} busy={busy === purpose} busyText="等待与测试中" onClick={() => void run(purpose)}>{labels[purpose]}</Button>
      <small role="status">{resultText(results[purpose])}</small>
    </div>)}
    {busy && <Button variant="ghost" onClick={() => controller.current?.abort()}>取消测试</Button>}
    <Notice>证书不受信任、浏览器能力不足或端口无法访问时，相关路径无法完成。请核对证书与厂商安全组中的测试端口。</Notice>
  </Collapsible>;
}

type TestConfig = { config: { host: string; cert_id: number; port: number; rtc_port: number } | null; observation: { observed_at: string; healthy: boolean; reason?: string } | null };
export function GateBrowserTest({ gateId }: { gateId: number }) {
  const q = useQuery({ queryKey: ["browser-test-config", gateId], queryFn: () => api(`/admin/gates/${gateId}/browser-test`) as Promise<TestConfig> });
  const certs = useQuery({ queryKey: ["browser-test-certs"], queryFn: () => api("/admin/certs") as Promise<{ items: { id: number; name: string; domain: string; gate_id?: number }[] }> });
  const [host, setHost] = useState<string | null>(null), [cert, setCert] = useState<string | null>(null);
  const confirm = useConfirm();
  const requested = useRef(true);
  const saveAction = useFormAction(() => api(`/admin/gates/${gateId}/browser-test`, "PUT", { enabled: requested.current, host: host ?? q.data?.config?.host ?? "", cert_id: Number(cert ?? q.data?.config?.cert_id ?? 0) }), { onSuccess: () => q.refetch(), fields: { browser_test_host_required: "host", browser_test_cert_required: "cert_id" } });
  const busy = saveAction.busy;
  async function save(enabled: boolean) {
    if (!await confirm({ title: enabled ? "启用独立浏览器测试端点？" : "关闭测试端点？", description: enabled ? "只提供限流测试数据，使用选定证书并放行分配的本机测试端口。云安全组需按显示的端口手动设置。" : "关闭监听并取消当前测试会话。", confirmText: "确认" })) return;
    requested.current = enabled;
    await saveAction.submit();
  }
  return <Collapsible variant="section" title="浏览器测试服务" summary={q.data?.config ? "已配置" : "未启用"}>
    {q.isPending || q.error ? <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} /> : <>
      <Form busy={busy} errors={saveAction.errors} onSubmit={() => void save(true)}><FormGrid>
        <Field name="host" label="证书对应的 VPS 域名或 IP" required><Input value={host ?? q.data.config?.host ?? ""} onChange={e => setHost(e.target.value)} required /></Field>
        <Field name="cert_id" label="测试证书" required><Select name="cert_id" required value={cert ?? String(q.data.config?.cert_id ?? "")} onValueChange={v => setCert(v ?? "")}><SelectTrigger><SelectValue placeholder="选择已有证书" /></SelectTrigger><SelectContent>{certs.data?.items.filter(c => !c.gate_id || c.gate_id === gateId).map(c => <SelectItem key={c.id} value={String(c.id)}>{c.name} · {c.domain}</SelectItem>)}</SelectContent></Select></Field>
      </FormGrid><div className="actions"><Button type="submit" busy={busy}>保存并启用</Button>{q.data.config && <Button variant="outline" disabled={busy} onClick={() => void save(false)}>关闭服务</Button>}</div></Form>
      {certs.error && <State error={certs.error} onRetry={() => certs.refetch()} />}
      {saveAction.error && <Notice tone="bad">{saveAction.error}</Notice>}
      {q.data.config && <Notice>HTTPS / QUIC：{q.data.config.port}（TCP / UDP）；WebRTC：{q.data.config.rtc_port}（UDP）。{q.data.observation ? `${q.data.observation.healthy ? "监听已就绪" : q.data.observation.reason || "监听已停止"} · ${new Date(q.data.observation.observed_at).toLocaleString()}` : "服务运行状态未知，等待 VPS 回执。"}</Notice>}
      <BrowserPathTest gateId={gateId} />
    </>}
  </Collapsible>;
}
