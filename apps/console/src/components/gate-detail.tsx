// VPS 详情：主机观测（缺失显示「未知」）、本机其他程序占用的端口、内核版本锁定、内核诊断 / 重启、节点出口关系。
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Badge, Button, Field, Notice, SectionHeader, Select, SelectContent, SelectItem, SelectTrigger, SelectValue, Sheet, SheetContent, useToast } from "@migate/ui";
import { api } from "../api";
import { bytes, date } from "../lib/models";
import type { Gate, NodeItem } from "../lib/core";
import { KernelDiagnostics } from "../pages/kernels";

type Host = Record<string, number | string | undefined>;
interface Port {
  port: number;
  proto: string;
  process?: string;
}

const unknown = "未知";
const pct = (v: unknown) => (typeof v === "number" ? v.toFixed(1) + "%" : unknown);
const size = (v: unknown) => (typeof v === "number" ? bytes(v) : unknown);
const uptime = (v: unknown) => (typeof v === "number" ? `${Math.floor(v / 86400)} 天 ${Math.floor((v % 86400) / 3600)} 小时` : unknown);

export function GateDetail({ gate, nodes, close }: { gate: Gate & { host?: Host; listening_ports?: Port[]; kernel_pins?: Record<string, string> }; nodes: NodeItem[]; close: () => void }) {
  const toast = useToast();
  const kernels = useQuery({ queryKey: ["kernels"], queryFn: async () => (await api("/admin/kernels")).kernels as Record<string, { default: string; cached: Record<string, string[] | null> }> });
  const [pins, setPins] = useState<Record<string, string>>(gate.kernel_pins ?? {});
  const [diag, setDiag] = useState<{ kernel: string; result: Record<string, unknown> } | null>(null);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const h = gate.host ?? {};
  const own = new Set(nodes.filter((n) => n.gate_id === gate.id).map((n) => n.port));
  const foreign = (gate.listening_ports ?? []).filter((p) => !own.has(p.port));
  async function run(key: string, fn: () => Promise<unknown>, ok: string) {
    setBusy(key);
    setError("");
    try {
      await fn();
      toast(ok, { tone: "ok" });
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy("");
    }
  }
  const mine = nodes.filter((n) => n.gate_id === gate.id);
  const exitName = (n: NodeItem) => n.chain.kind === "none" ? "直连" : n.chain.kind === "socks5" ? `家宽 SOCKS5 ${n.chain.address}:${n.chain.port}` : `经 ${nodes.find((x) => x.id === n.chain.node_id)?.name ?? "节点"}（${nodes.find((x) => x.id === n.chain.node_id)?.gate_name ?? ""}）`;
  return (
    <Sheet open onOpenChange={(o) => !o && close()}>
      <SheetContent side="right" title={gate.name} description={`${gate.public_host || "等待上报地址"} · ${gate.arch} · 最近心跳 ${date(gate.last_seen_at)}`} className="w-full sm:max-w-[640px]">
        <div className="stack overflow-y-auto p-4">
          <SectionHeader title="主机" description="由 VPS 读取本机 /proc 与磁盘信息上报；网卡总量可能包含虚拟网卡，不作为用户计费依据。" />
          <dl className="grid grid-cols-2 gap-2 text-sm">
            <dt>CPU</dt><dd>{pct(h.cpu_percent)}</dd>
            <dt>负载</dt><dd>{typeof h.load1 === "number" ? h.load1.toFixed(2) : unknown}</dd>
            <dt>内存</dt><dd>{size(h.mem_used)} / {size(h.mem_total)}</dd>
            <dt>磁盘</dt><dd>{size(h.disk_used)} / {size(h.disk_total)}</dd>
            <dt>网卡累计</dt><dd>↓ {size(h.net_rx_bytes)} · ↑ {size(h.net_tx_bytes)}</dd>
            <dt>运行时间</dt><dd>{uptime(h.uptime_secs)}</dd>
          </dl>
          <SectionHeader title="其他程序占用的端口" description="建节点时自动避开这些端口。" />
          {foreign.length === 0 ? <small>{gate.listening_ports ? "没有" : unknown}</small> : (
            <div className="flex flex-wrap gap-1.5">{foreign.map((p) => <Badge key={p.proto + p.port} variant="soft" tone="neutral">{p.port}/{p.proto}{p.process ? ` ${p.process}` : ""}</Badge>)}</div>
          )}
          <SectionHeader title="内核" description="可为这台 VPS 锁定版本；不锁定时使用默认版本。" />
          {kernels.isPending || kernels.error ? <small>{kernels.error ? String(kernels.error) : "正在读取…"}</small> : (["xray", "sing-box"] as const).map((k) => {
            const cached = kernels.data[k]?.cached[gate.arch] ?? [];
            return (
              <div key={k} className="stack rounded-[var(--mg-radius)] border border-[var(--mg-line)] p-3">
                <strong>{k} <small className="font-normal text-[var(--mg-ink-3)]">运行中 {gate.kernels_running?.[k] ?? "无"}</small></strong>
                <Field label="版本">
                  <Select value={pins[k] ?? ""} onValueChange={(v) => setPins({ ...pins, [k]: String(v) })}>
                    <SelectTrigger aria-label={k + " 版本"}><SelectValue>{pins[k] || `跟随默认（${kernels.data[k]?.default || "最新已下载"}）`}</SelectValue></SelectTrigger>
                    <SelectContent>
                      <SelectItem value="">跟随默认</SelectItem>
                      {cached.map((v) => <SelectItem key={v} value={v}>{v}</SelectItem>)}
                    </SelectContent>
                  </Select>
                </Field>
                <div className="actions">
                  <Button size="sm" variant="outline" disabled={!!busy || !gate.online} onClick={() => void run("d" + k, async () => setDiag({ kernel: k, result: (await api(`/admin/gates/${gate.id}/kernels/${k}/diagnostics`, "POST", {})).result }), "已读取诊断")}>诊断</Button>
                  <Button size="sm" variant="outline" disabled={!!busy || !gate.online} onClick={() => window.confirm(`重启 ${gate.name} 上的 ${k}？连接会短暂中断。`) && run("r" + k, () => api(`/admin/gates/${gate.id}/kernels/${k}/restart`, "POST", {}), "已重启")}>重启</Button>
                </div>
                {diag?.kernel === k && <KernelDiagnostics result={diag.result} />}
              </div>
            );
          })}
          <div className="actions"><Button disabled={!!busy} onClick={() => void run("pins", () => api("/admin/gates/" + gate.id, "PATCH", { kernel_pins: Object.fromEntries(Object.entries(pins).filter(([, v]) => v)) }), "版本设置已保存")}>保存版本设置</Button></div>
          {!gate.online && <Notice tone="warn">VPS 离线：诊断与重启不可用。</Notice>}
          <SectionHeader title="节点与出口" description="入口节点 → 出口；分流规则在「出口与分流」中设置。" />
          {mine.length === 0 ? <small>这台 VPS 还没有节点。</small> : (
            <ul className="stack">{mine.map((n) => <li key={n.id} className="text-sm"><strong>{n.name}</strong> :{n.port} → {exitName(n)}{!n.enabled && <Badge variant="soft" tone="warn">停用</Badge>}</li>)}</ul>
          )}
          {error && <Notice tone="bad">{error}</Notice>}
        </div>
      </SheetContent>
    </Sheet>
  );
}
