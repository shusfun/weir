// VPS 的出口与分流规则：例如把流媒体域名经家宽 SOCKS5 出去。被规则引用的出口不能停用、改名或删除。
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Badge, Button, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Field, Input, Notice, SectionHeader, Segmented, Select, SelectContent, SelectItem, SelectTrigger, SelectValue, Switch, Textarea, useToast } from "@migate/ui";
import { Plus, Trash2 } from "lucide-react";
import { api } from "../api";
import type { Gate } from "../lib/core";
import { State } from "./feedback";

interface Outbound {
  id: number;
  tag: string;
  name: string;
  protocol: string;
  address: string;
  port: number;
  username: string;
  password?: string;
  enabled: boolean;
}
interface Route {
  id: number;
  outbound_tag: string;
  domain: string;
  ip: string;
  enabled: boolean;
  priority: number;
  node_id?: number | null;
  user_id?: number | null;
}

const protoNames: Record<string, string> = { socks: "SOCKS5", http: "HTTP 代理", freedom: "直连", blackhole: "拦截" };

export function GateRouting({ gate, close }: { gate: Gate; close: () => void }) {
  const toast = useToast();
  const base = `/admin/gates/${gate.id}`;
  const outs = useQuery({ queryKey: ["outbounds", gate.id], queryFn: async () => (await api(base + "/outbounds")).items as Outbound[] });
  const routes = useQuery({ queryKey: ["routes", gate.id], queryFn: async () => (await api(base + "/routes")).items as Route[] });
  const [editOut, setEditOut] = useState<Outbound | "new" | null>(null);
  const [editRoute, setEditRoute] = useState<Route | "new" | null>(null);
  const [busy, setBusy] = useState("");
  async function run(key: string, fn: () => Promise<unknown>, ok: string) {
    setBusy(key);
    try {
      const r = await fn();
      toast(ok, { tone: "ok", description: r && typeof r === "object" && "latency_ms" in r ? `延迟 ${(r as { latency_ms: number }).latency_ms} ms` : undefined });
      await Promise.all([outs.refetch(), routes.refetch()]);
    } catch (e) {
      toast("操作未完成", { description: String(e), tone: "bad" });
    } finally {
      setBusy("");
    }
  }
  return (
    <Dialog open onOpenChange={(o) => !o && close()}>
      <DialogContent size="xl">
        <DialogTitle>出口与分流 · {gate.name}</DialogTitle>
        <DialogDescription>规则按优先级从小到大匹配，排在用户自建链式代理之后、节点链式代理之前；不匹配的流量按节点设置出去。</DialogDescription>
        <SectionHeader title="出口" actions={<Button size="sm" onClick={() => setEditOut("new")}><Plus />添加出口</Button>} />
        {outs.isPending || outs.error ? (
          <State loading={outs.isPending} error={outs.error} onRetry={() => outs.refetch()} />
        ) : outs.data.length === 0 ? (
          <State empty="还没有自定义出口" detail="内置直连出口 direct 始终可用。" />
        ) : (
          <ul className="stack">
            {outs.data.map((o) => (
              <li key={o.id} className="flex flex-wrap items-center justify-between gap-2">
                <span><code>{o.tag}</code> {o.name} <Badge variant="soft" tone="neutral">{protoNames[o.protocol] ?? o.protocol}</Badge>{o.address && <small className="ml-1">{o.address}:{o.port}</small>}{!o.enabled && <Badge variant="soft" tone="warn">停用</Badge>}</span>
                <span className="flex flex-nowrap gap-1.5">
                  {o.address && <Button size="sm" variant="outline" disabled={busy === "p" + o.id} onClick={() => run("p" + o.id, () => api(`${base}/outbounds/${o.id}/probe`, "POST", {}), "实测可用")}>实测</Button>}
                  <Button size="sm" variant="outline" onClick={() => setEditOut(o)}>编辑</Button>
                  <Button size="icon-sm" variant="destructive" aria-label={"删除出口 " + o.tag} disabled={busy === "o" + o.id} onClick={() => window.confirm(`删除出口 ${o.tag}？`) && run("o" + o.id, () => api(`${base}/outbounds/${o.id}`, "DELETE"), "已删除")}><Trash2 /></Button>
                </span>
              </li>
            ))}
          </ul>
        )}
        <SectionHeader title="分流规则" actions={<Button size="sm" onClick={() => setEditRoute("new")}><Plus />添加规则</Button>} />
        {routes.isPending || routes.error ? (
          <State loading={routes.isPending} error={routes.error} onRetry={() => routes.refetch()} />
        ) : routes.data.length === 0 ? (
          <State empty="还没有分流规则" />
        ) : (
          <ul className="stack">
            {routes.data.map((r) => (
              <li key={r.id} className="flex flex-wrap items-center justify-between gap-2">
                <span className="min-w-0">
                  <Badge variant="soft" tone="neutral">#{r.priority}</Badge> → <code>{r.outbound_tag}</code>
                  <small className="block max-w-xl truncate text-[var(--mg-ink-3)]">{[r.domain && "域名 " + r.domain, r.ip && "IP " + r.ip].filter(Boolean).join(" · ") || "全部流量"}</small>
                </span>
                <span className="flex flex-nowrap gap-1.5">
                  <Switch aria-label="启用规则" checked={r.enabled} disabled={busy === "r" + r.id} onCheckedChange={(v) => run("r" + r.id, () => api(`${base}/routes/${r.id}`, "PATCH", { ...r, enabled: v }), v ? "已启用" : "已停用")} />
                  <Button size="sm" variant="outline" onClick={() => setEditRoute(r)}>编辑</Button>
                  <Button size="icon-sm" variant="destructive" aria-label="删除规则" disabled={busy === "r" + r.id} onClick={() => window.confirm("删除这条规则？") && run("r" + r.id, () => api(`${base}/routes/${r.id}`, "DELETE"), "已删除")}><Trash2 /></Button>
                </span>
              </li>
            ))}
          </ul>
        )}
        {editOut && <OutboundForm base={base} value={editOut} close={() => setEditOut(null)} saved={() => { setEditOut(null); toast("已保存", { tone: "ok" }); void outs.refetch(); }} />}
        {editRoute && <RouteForm base={base} value={editRoute} outbounds={outs.data ?? []} close={() => setEditRoute(null)} saved={() => { setEditRoute(null); toast("已保存", { tone: "ok" }); void routes.refetch(); }} />}
      </DialogContent>
    </Dialog>
  );
}

function OutboundForm({ base, value, close, saved }: { base: string; value: Outbound | "new"; close: () => void; saved: () => void }) {
  const o = value === "new" ? undefined : value;
  const [f, setF] = useState({ tag: o?.tag ?? "", name: o?.name ?? "", protocol: (o?.protocol ?? "socks") as "socks" | "http" | "freedom" | "blackhole", address: o?.address ?? "", port: o?.port ? String(o.port) : "", username: o?.username ?? "", password: o?.password ?? "", enabled: o?.enabled ?? true });
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const set = (p: Partial<typeof f>) => setF({ ...f, ...p });
  const remote = f.protocol === "socks" || f.protocol === "http";
  async function submit() {
    setBusy(true);
    setError("");
    try {
      const body = { ...f, port: Number(f.port) || 0 };
      await (o ? api(`${base}/outbounds/${o.id}`, "PATCH", body) : api(base + "/outbounds", "POST", body));
      saved();
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <Dialog open onOpenChange={(v) => !v && !busy && close()}>
      <DialogContent size="md">
        <DialogTitle>{o ? "编辑出口" : "添加出口"}</DialogTitle>
        <DialogDescription>标签只能用小写字母、数字、- 和 _；不能使用 direct、chain-、node- 等系统保留名称。</DialogDescription>
        <form className="stack" onSubmit={(e) => { e.preventDefault(); void submit(); }}>
          <Segmented aria-label="出口类型" value={f.protocol} onValueChange={(protocol) => set({ protocol })} options={[["socks", "SOCKS5"], ["http", "HTTP 代理"], ["freedom", "直连"], ["blackhole", "拦截"]] as const} />
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label="标签" htmlFor="ob-tag"><Input id="ob-tag" required value={f.tag} onChange={(e) => set({ tag: e.target.value.trim() })} /></Field>
            <Field label="名称（可选）" htmlFor="ob-name"><Input id="ob-name" value={f.name} onChange={(e) => set({ name: e.target.value })} /></Field>
            {remote && <>
              <Field label="地址" htmlFor="ob-addr"><Input id="ob-addr" required value={f.address} onChange={(e) => set({ address: e.target.value.trim() })} /></Field>
              <Field label="端口" htmlFor="ob-port"><Input id="ob-port" inputMode="numeric" required value={f.port} onChange={(e) => set({ port: e.target.value.replace(/\D/g, "") })} /></Field>
              <Field label="用户名（可选）" htmlFor="ob-user"><Input id="ob-user" autoComplete="off" value={f.username} onChange={(e) => set({ username: e.target.value })} /></Field>
              <Field label="密码（可选）" htmlFor="ob-pass"><Input id="ob-pass" type="password" autoComplete="new-password" value={f.password} onChange={(e) => set({ password: e.target.value })} /></Field>
            </>}
          </div>
          {o && <label className="actions"><Switch aria-label="启用" checked={f.enabled} onCheckedChange={(v) => set({ enabled: v })} />启用</label>}
          {error && <Notice tone="bad">{error}</Notice>}
          <DialogFooter>
            <Button type="button" variant="outline" disabled={busy} onClick={close}>取消</Button>
            <Button type="submit" disabled={busy}>{busy ? "正在保存…" : "保存"}</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function RouteForm({ base, value, outbounds, close, saved }: { base: string; value: Route | "new"; outbounds: Outbound[]; close: () => void; saved: () => void }) {
  const r = value === "new" ? undefined : value;
  const [tag, setTag] = useState(r?.outbound_tag ?? outbounds.find((o) => o.enabled)?.tag ?? "direct");
  const [domain, setDomain] = useState((r?.domain ?? "").split(",").join("\n"));
  const [ip, setIp] = useState((r?.ip ?? "").split(",").join("\n"));
  const [priority, setPriority] = useState(String(r?.priority ?? 10));
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const choices = [{ tag: "direct", name: "直连" }, ...outbounds.filter((o) => o.enabled)];
  async function submit() {
    setBusy(true);
    setError("");
    try {
      const body = { outbound_tag: tag, domain, ip, priority: Number(priority) || 0, enabled: r?.enabled ?? true, node_id: r?.node_id ?? null, user_id: r?.user_id ?? null };
      await (r ? api(`${base}/routes/${r.id}`, "PATCH", body) : api(base + "/routes", "POST", body));
      saved();
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <Dialog open onOpenChange={(v) => !v && !busy && close()}>
      <DialogContent size="md">
        <DialogTitle>{r ? "编辑规则" : "添加规则"}</DialogTitle>
        <DialogDescription>每行一条：domain:netflix.com、geosite:disney、full:api.example.com；IP 如 1.1.1.1/32、geoip:jp。</DialogDescription>
        <form className="stack" onSubmit={(e) => { e.preventDefault(); void submit(); }}>
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label="出口">
              <Select value={tag} onValueChange={(v) => setTag(String(v))}>
                <SelectTrigger aria-label="出口"><SelectValue>{choices.find((c) => c.tag === tag)?.tag ?? tag}</SelectValue></SelectTrigger>
                <SelectContent>{choices.map((c) => <SelectItem key={c.tag} value={c.tag}>{c.tag}{c.name ? `（${c.name}）` : ""}</SelectItem>)}</SelectContent>
              </Select>
            </Field>
            <Field label="优先级" htmlFor="rt-pri" hint="数字越小越先匹配。"><Input id="rt-pri" inputMode="numeric" value={priority} onChange={(e) => setPriority(e.target.value.replace(/\D/g, ""))} /></Field>
          </div>
          <Field label="域名" htmlFor="rt-domain"><Textarea id="rt-domain" rows={4} className="font-mono text-xs" value={domain} onChange={(e) => setDomain(e.target.value)} /></Field>
          <Field label="IP" htmlFor="rt-ip"><Textarea id="rt-ip" rows={2} className="font-mono text-xs" value={ip} onChange={(e) => setIp(e.target.value)} /></Field>
          {error && <Notice tone="bad">{error}</Notice>}
          <DialogFooter>
            <Button type="button" variant="outline" disabled={busy} onClick={close}>取消</Button>
            <Button type="submit" disabled={busy}>{busy ? "正在保存…" : "保存"}</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
