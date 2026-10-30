// VPS 的出口与分流规则：例如把流媒体域名经家宽代理出去。出口可以是 SOCKS5 / HTTP / HTTPS 代理、一条分享链接、
// 订阅源节点、直连或拦截；被规则引用的出口不能停用、改名或删除。
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Badge, Button, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Field, Form, FormGrid, Input, Notice, SectionHeader, Segmented, Select, SelectContent, SelectItem, SelectTrigger, SelectValue, Switch, SwitchField, Textarea, useToast, useConfirm, PasswordInput } from "@weir/ui";
import { Plus, Trash2 } from "lucide-react";
import { api } from "../api";
import { useFormAction, useRowAction } from "../lib/action";
import type { ExitProbe, Gate } from "../lib/core";
import { parseExitText } from "../lib/exit-parse";
import { probeText } from "./chain-exit";
import { State } from "./feedback";
import { useSources } from "./misub-sources";
import { SourceSummary } from "./link-panel";
import type { SourceResolved } from "../lib/core";
import { field, useConsoleContext } from "../lib/console-context";

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
  settings?: { source_id?: number; mode?: "node" | "filter"; match?: string } | null;
  source?: SourceResolved;
  link?: string;
  probe?: ExitProbe;
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

const protoNames: Record<string, string> = { socks: "SOCKS5", http: "HTTP 代理", https: "HTTPS 代理", link: "分享链接", freedom: "直连", blackhole: "拦截", source: "订阅源节点" };
const probed = (r: unknown) => probeText(r as ExitProbe);
const portError = (v: string) => (v && (Number(v) < 1 || Number(v) > 65535) ? "端口须在 1–65535 之间" : "");

export function GateRouting({ gate, close }: { gate: Gate; close: () => void }) {
  const confirm = useConfirm();
  const toast = useToast();
  const base = `/admin/gates/${gate.id}`;
  const outs = useQuery({ queryKey: ["outbounds", gate.id], queryFn: async () => (await api(base + "/outbounds")).items as Outbound[] });
  const routes = useQuery({ queryKey: ["routes", gate.id], queryFn: async () => (await api(base + "/routes")).items as Route[] });
  const [editOut, setEditOut] = useState<Outbound | "new" | null>(null);
  const [editRoute, setEditRoute] = useState<Route | "new" | null>(null);
  const { run, isBusy } = useRowAction<string>({ refetch: () => Promise.all([outs.refetch(), routes.refetch()]) });
  useConsoleContext({ object: { type: "gate", name: gate.name, id: gate.id }, dialog: { title: "出口与分流" }, notices: [`${outs.data?.length ?? "未知"} 个出口，${routes.data?.length ?? "未知"} 条分流规则`] });
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
                <span className="min-w-0">
                  <code>{o.tag}</code> {o.name} <Badge variant="soft" tone="neutral">{protoNames[o.protocol] ?? o.protocol}</Badge>{o.address && <small className="ml-1">{o.address}:{o.port}</small>}{o.protocol === "source" && <small className="ml-1"><SourceSummary choice={o.settings} resolved={o.source} /></small>}{!o.enabled && <Badge variant="soft" tone="warn">停用</Badge>}
                  {o.address && <small className="block text-[var(--weir-ink-3)]">{probeText(o.probe)}</small>}
                </span>
                <span className="flex flex-nowrap gap-1.5">
                  {o.address && <Button size="sm" variant="outline" disabled={isBusy("p" + o.id)} onClick={() => run("p" + o.id, () => api(`${base}/outbounds/${o.id}/probe`, "POST", {}), "实测可用", { detail: probed })}>实测</Button>}
                  <Button size="sm" variant="outline" onClick={() => setEditOut(o)}>编辑</Button>
                  <Button size="icon-sm" variant="destructive" aria-label={"删除出口 " + o.tag} disabled={isBusy("o" + o.id)} onClick={async () => (await confirm({ title: `删除出口 ${o.tag}？`, confirmText: "删除", tone: "danger" })) && run("o" + o.id, () => api(`${base}/outbounds/${o.id}`, "DELETE"), "已删除")}><Trash2 /></Button>
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
                  <small className="block max-w-xl truncate text-[var(--weir-ink-3)]">{[r.domain && "域名 " + r.domain, r.ip && "IP " + r.ip].filter(Boolean).join(" · ") || "全部流量"}</small>
                </span>
                <span className="flex flex-nowrap gap-1.5">
                  <Switch aria-label="启用规则" checked={r.enabled} disabled={isBusy("r" + r.id)} onCheckedChange={(v) => run("r" + r.id, () => api(`${base}/routes/${r.id}`, "PATCH", { ...r, enabled: v }), v ? "已启用" : "已停用")} />
                  <Button size="sm" variant="outline" onClick={() => setEditRoute(r)}>编辑</Button>
                  <Button size="icon-sm" variant="destructive" aria-label="删除规则" disabled={isBusy("r" + r.id)} onClick={async () => (await confirm({ title: "删除这条规则？", confirmText: "删除", tone: "danger" })) && run("r" + r.id, () => api(`${base}/routes/${r.id}`, "DELETE"), "已删除")}><Trash2 /></Button>
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
  const sources = useSources();
  const [f, setF] = useState({ tag: o?.tag ?? "", name: o?.name ?? "", protocol: (o?.protocol ?? "socks") as "socks" | "http" | "https" | "link" | "freedom" | "blackhole" | "source", address: o?.address ?? "", port: o?.port ? String(o.port) : "", username: o?.username ?? "", password: o?.password ?? "", enabled: o?.enabled ?? true,
    source_id: o?.settings?.source_id ?? 0, mode: (o?.settings?.mode ?? "filter") as "node" | "filter", match: o?.settings?.match ?? "", link: o?.link ?? "" });
  const set = (p: Partial<typeof f>) => setF({ ...f, ...p });
  const remote = f.protocol === "socks" || f.protocol === "http" || f.protocol === "https";
  const [paste, setPaste] = useState("");
  const parsed = paste.trim() ? parseExitText(paste) : null;
  const pasteError = parsed?.type === "subscription" ? "这是订阅地址，不能作出口" : parsed?.type === "link" ? "这是分享链接，请改选「分享链接」" : parsed?.type === "error" ? parsed.message : "";
  const applyPaste = (text: string) => {
    setPaste(text);
    const r = text.trim() ? parseExitText(text) : null;
    if (r?.type === "proxy") {
      const protocol = r.proxy.kind === "socks5" ? "socks" : r.proxy.kind ?? f.protocol;
      set({ protocol: protocol as typeof f.protocol, address: r.proxy.address, port: String(r.proxy.port), username: r.proxy.username ?? "", password: r.proxy.password ?? "" });
    }
  };
  const save = useFormAction(
    () => {
      const body = { ...f, port: Number(f.port) || 0 };
      return o ? api(`${base}/outbounds/${o.id}`, "PATCH", body) : api(base + "/outbounds", "POST", body);
    },
    { fields: { invalid_tag: "tag", tag_conflict: "tag", invalid_outbound_address: "address", invalid_outbound_credentials: "username", source_not_found: "source_id", invalid_source_exit: "match", group_unsupported: "match",
      invalid_link: "link", exit_unsupported: remote ? "address" : "link" }, onSuccess: saved },
  );
  useConsoleContext({ dialog: { title: o ? "编辑出口" : "添加出口" }, form: Object.entries(f).map(([k, v]) => field(k, k, typeof v === "object" ? JSON.stringify(v) : v, /pass|secret|key|link/i.test(k))) });
  return (
    <Dialog open onOpenChange={(v) => !v && !save.busy && close()}>
      <DialogContent size="md">
        <DialogTitle>{o ? "编辑出口" : "添加出口"}</DialogTitle>
        <DialogDescription>标签只能用小写字母、数字、- 和 _；不能使用 direct、chain-、node- 等系统保留名称。</DialogDescription>
        <Form busy={save.busy} errors={save.errors} onSubmit={() => void save.submit()}>
          <Segmented aria-label="出口类型" value={f.protocol} onValueChange={(protocol) => set({ protocol })} options={[["socks", "SOCKS5"], ["http", "HTTP"], ["https", "HTTPS"], ["link", "分享链接"], ["source", "订阅源节点"], ["freedom", "直连"], ["blackhole", "拦截"]] as const} />
          {remote && (
            <Field label="粘贴识别（可选）" customError={pasteError} hint="支持 host:port、host:port:用户名:密码、用户名:密码@host:port、socks5:// 与 http(s):// 地址。">
              <Input autoComplete="off" spellCheck={false} value={paste} placeholder="1.2.3.4:1080:user:pass" onChange={(e) => applyPaste(e.target.value)} />
            </Field>
          )}
          {f.protocol === "link" && (
            <Field label="分享链接" name="link" required hint="一个节点的链接（vless / vmess / trojan / ss / hysteria2 / tuic / anytls）；订阅地址不能作出口。这台 VPS 上的每个内核都要能连接它。">
              <Textarea required rows={3} spellCheck={false} className="font-mono text-xs" value={f.link} onChange={(e) => set({ link: e.target.value.trim() })} />
            </Field>
          )}
          {f.protocol === "source" && <Notice role="status">用订阅源里的节点作出口：一组的全部成员都下发，由内核按延迟自动选、出故障自动切换（最多 50 个）；订阅源刷新后随之更新，刷新失败或暂时没有匹配时保留上一次可用的；从没解析成功时流量被丢弃，不会改成直连。</Notice>}
          <FormGrid>
            <Field label="标签" name="tag" customError={/^[a-z0-9_-]*$/.test(f.tag) ? "" : "标签只能使用小写字母、数字、- 和 _"}>
              <Input required value={f.tag} onChange={(e) => set({ tag: e.target.value.trim() })} />
            </Field>
            <Field label="名称（可选）"><Input value={f.name} onChange={(e) => set({ name: e.target.value })} /></Field>
            {f.protocol === "source" && <>
              <Field label="订阅源" name="source_id">
                <Select required value={f.source_id ? String(f.source_id) : ""} onValueChange={(v) => set({ source_id: Number(v) })}>
                  <SelectTrigger><SelectValue>{(sources.data?.items ?? []).find((x) => x.id === f.source_id)?.name ?? "请选择"}</SelectValue></SelectTrigger>
                  <SelectContent>{(sources.data?.items ?? []).map((x) => <SelectItem key={x.id} value={String(x.id)}>{x.name || x.url}</SelectItem>)}</SelectContent>
                </Select>
              </Field>
              <Field label="选择方式">
                <Segmented aria-label="选择方式" value={f.mode} onValueChange={(mode) => set({ mode })} options={[["filter", "按名称筛选一组"], ["node", "单个节点"]] as const} />
              </Field>
              <Field label={f.mode === "node" ? "节点名称（完全一致）" : "名称筛选（正则）"} name="match" hint={f.mode === "filter" ? "例如 ^HK 或 香港|HK；取匹配的第一个可用节点" : undefined}>
                <Input required value={f.match} onChange={(e) => set({ match: e.target.value })} />
              </Field>
            </>}
            {remote && <>
              <Field label="地址" name="address"><Input required value={f.address} onChange={(e) => set({ address: e.target.value.trim() })} /></Field>
              <Field label="端口" customError={portError(f.port)}><Input inputMode="numeric" required value={f.port} onChange={(e) => set({ port: e.target.value.replace(/\D/g, "") })} /></Field>
              <Field label="用户名（可选）" name="username"><Input autoComplete="off" value={f.username} onChange={(e) => set({ username: e.target.value })} /></Field>
              <Field label="密码（可选）"><PasswordInput autoComplete="new-password" value={f.password} onChange={(e) => set({ password: e.target.value })} /></Field>
            </>}
          </FormGrid>
          {o && <SwitchField label="启用" checked={f.enabled} onCheckedChange={(v) => set({ enabled: v })} />}
          <DialogFooter error={save.error}>
            <Button type="button" variant="outline" disabled={save.busy} onClick={close}>取消</Button>
            <Button type="submit" disabled={save.busy} busy={save.busy} busyText="正在保存…">保存</Button>
          </DialogFooter>
        </Form>
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
  const choices = [{ tag: "direct", name: "直连" }, ...outbounds.filter((o) => o.enabled)];
  const save = useFormAction(
    () => {
      const body = { outbound_tag: tag, domain, ip, priority: Number(priority) || 0, enabled: r?.enabled ?? true, node_id: r?.node_id ?? null, user_id: r?.user_id ?? null };
      return r ? api(`${base}/routes/${r.id}`, "PATCH", body) : api(base + "/routes", "POST", body);
    },
    { onSuccess: saved },
  );
  useConsoleContext({ dialog: { title: r ? "编辑规则" : "添加规则" }, form: [field("outbound_tag", "出口", tag), field("domain", "域名", domain.split("\n").filter(Boolean).join("、")), field("ip", "IP", ip.split("\n").filter(Boolean).join("、")), field("priority", "优先级", priority)] });
  return (
    <Dialog open onOpenChange={(v) => !v && !save.busy && close()}>
      <DialogContent size="md">
        <DialogTitle>{r ? "编辑规则" : "添加规则"}</DialogTitle>
        <DialogDescription>每行一条：domain:netflix.com、geosite:disney、full:api.example.com；IP 如 1.1.1.1/32、geoip:jp。</DialogDescription>
        <Form busy={save.busy} errors={save.errors} onSubmit={() => void save.submit()}>
          <FormGrid>
            <Field label="出口">
              <Select required value={tag} onValueChange={(v) => setTag(String(v))}>
                <SelectTrigger><SelectValue>{choices.find((c) => c.tag === tag)?.tag ?? tag}</SelectValue></SelectTrigger>
                <SelectContent>{choices.map((c) => <SelectItem key={c.tag} value={c.tag}>{c.tag}{c.name ? `（${c.name}）` : ""}</SelectItem>)}</SelectContent>
              </Select>
            </Field>
            <Field label="优先级" hint="数字越小越先匹配。"><Input inputMode="numeric" value={priority} onChange={(e) => setPriority(e.target.value.replace(/\D/g, ""))} /></Field>
          </FormGrid>
          <Field label="域名"><Textarea rows={4} className="font-mono text-xs" value={domain} onChange={(e) => setDomain(e.target.value)} /></Field>
          <Field label="IP"><Textarea rows={2} className="font-mono text-xs" value={ip} onChange={(e) => setIp(e.target.value)} /></Field>
          <DialogFooter error={save.error}>
            <Button type="button" variant="outline" disabled={save.busy} onClick={close}>取消</Button>
            <Button type="submit" disabled={save.busy} busy={save.busy} busyText="正在保存…">保存</Button>
          </DialogFooter>
        </Form>
      </DialogContent>
    </Dialog>
  );
}
