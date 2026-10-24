// 订阅链接与自建链式代理：管理员（按用户）与用户本人共用。base 为 "/admin/users/<id>" 或 "/me"。
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Badge, Button, Checkbox, CodeBlock, CopyButton, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Field, Input, Label, Notice, SectionHeader, Segmented, Select, SelectContent, SelectItem, SelectTrigger, SelectValue, SkeletonList, Switch, useToast } from "@migate/ui";
import { Plus, QrCode, RefreshCw, Trash2 } from "lucide-react";
import { QRCodeSVG } from "qrcode.react";
import { api } from "../api";
import { bytes, date } from "../lib/models";
import type { ChainItem, ChainSpec, LinkItem } from "../lib/core";
import { State } from "./feedback";

export interface NodeOption {
  id: number;
  name: string;
  gate_name?: string;
}

export function useLinks(base: string) {
  return useQuery({ queryKey: ["links", base], queryFn: async () => (await api(base + "/links")).items as LinkItem[] });
}

export function useChains(base: string) {
  return useQuery({ queryKey: ["chains", base], queryFn: async () => (await api(base + "/chains")) as { items: ChainItem[]; allow_chain: boolean } });
}

export function LinkPanel({ base, nodes, showCredentials }: { base: string; nodes: NodeOption[]; showCredentials?: boolean }) {
  const toast = useToast();
  const links = useLinks(base);
  const chains = useChains(base);
  const [edit, setEdit] = useState<LinkItem | "new" | null>(null);
  const [qr, setQr] = useState<LinkItem | null>(null);
  const [busy, setBusy] = useState(0);
  async function act(id: number, fn: () => Promise<unknown>, ok: string) {
    setBusy(id);
    try {
      await fn();
      toast(ok, { tone: "ok" });
      await links.refetch();
    } catch (e) {
      toast("操作未完成", { description: String(e), tone: "bad" });
    } finally {
      setBusy(0);
    }
  }
  return (
    <section className="stack">
      <SectionHeader title="订阅链接" description="每条链接凭据独立、共用同一份额度；最多 10 条。" actions={<Button size="sm" disabled={(links.data?.length ?? 0) >= 10} onClick={() => setEdit("new")}><Plus />新链接</Button>} />
      {links.isPending || links.error ? (
        <State
          loading={links.isPending}
          error={links.error}
          onRetry={() => links.refetch()}
          skeleton={<SkeletonList rows={2} lines={2} className="stack" rowClassName="rounded-[var(--mg-radius)] border border-[var(--mg-line)] p-3" />}
        />
      ) : links.data.length === 0 ? (
        <State empty="还没有链接" />
      ) : (
        links.data.map((l) => (
          <div key={l.id} className="stack rounded-[var(--mg-radius)] border border-[var(--mg-line)] p-3">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <strong>{l.name || "未命名"} {!l.enabled && <Badge variant="soft" tone="warn">已停用</Badge>}</strong>
              <small className="text-[var(--mg-ink-3)]">{l.all_nodes ? "全部节点" : "部分节点"} · {l.nodes} 个节点 · 已用 {bytes(l.used)} · 最近获取 {date(l.last_used_at)}</small>
            </div>
            <div className="subscription-url">
              <Input readOnly className="font-mono text-xs" aria-label="订阅地址" value={l.url} />
              <CopyButton value={l.url}>复制</CopyButton>
            </div>
            <div className="actions">
              <Button size="sm" variant="outline" onClick={() => setQr(l)}><QrCode />二维码</Button>
              <Button size="sm" variant="outline" onClick={() => window.open(l.url, "_blank", "noopener,noreferrer")}>一键导入页</Button>
              <Button size="sm" variant="outline" onClick={() => setEdit(l)}>编辑</Button>
              <Button size="sm" variant="outline" disabled={busy === l.id} onClick={() => window.confirm("更换订阅地址？旧地址立即失效，已导入的客户端节点仍可用。") && act(l.id, () => api(`${base}/links/${l.id}/rotate`, "POST", {}), "订阅地址已更换")}><RefreshCw />换地址</Button>
              <Button size="sm" variant="outline" disabled={busy === l.id} onClick={() => window.confirm("更换该链接在所有节点上的凭据？已导入的客户端需要重新更新订阅。") && act(l.id, () => api(`${base}/links/${l.id}/rotate`, "POST", { credentials: true }), "凭据已更换")}>换凭据</Button>
              <Button size="sm" variant="destructive" disabled={busy === l.id} onClick={() => window.confirm(`删除链接「${l.name || "未命名"}」？`) && act(l.id, () => api(`${base}/links/${l.id}`, "DELETE"), "链接已删除")}><Trash2 />删除</Button>
            </div>
            {showCredentials && (l.entries?.length ?? 0) > 0 && (
              <details>
                <summary className="cursor-pointer text-sm">节点与凭据原文（{l.entries!.length}）</summary>
                <ul className="stack mt-2">
                  {l.entries!.map((e) => (
                    <li key={`${e.node_id}-${e.chain_id ?? 0}`} className="stack">
                      <small><strong>{e.name}</strong>{e.chain_id ? "（链式代理）" : ""} · {e.host}:{e.port}{e.uuid ? ` · UUID ${e.uuid}` : ""}{e.password ? ` · 密码 ${e.password}` : ""}</small>
                      {e.uri ? <CodeBlock className="block break-all text-xs">{e.uri}</CodeBlock> : <small className="text-[var(--mg-bad-ink)]">{e.error}</small>}
                    </li>
                  ))}
                </ul>
              </details>
            )}
          </div>
        ))
      )}
      {edit && <LinkForm base={base} value={edit} nodes={nodes} chains={chains.data?.items ?? []} close={() => setEdit(null)} saved={() => { setEdit(null); toast("链接已保存", { tone: "ok" }); void links.refetch(); }} />}
      <Dialog open={!!qr} onOpenChange={(o) => !o && setQr(null)}>
        <DialogContent size="sm">
          <DialogTitle>扫码导入</DialogTitle>
          <DialogDescription>{qr?.name || "订阅链接"}</DialogDescription>
          {qr && <div className="qr-code mx-auto"><QRCodeSVG value={qr.url} size={200} title="订阅二维码" /></div>}
        </DialogContent>
      </Dialog>
    </section>
  );
}

function LinkForm({ base, value, nodes, chains, close, saved }: { base: string; value: LinkItem | "new"; nodes: NodeOption[]; chains: ChainItem[]; close: () => void; saved: () => void }) {
  const l = value === "new" ? undefined : value;
  const [name, setName] = useState(l?.name ?? "");
  const [all, setAll] = useState(l?.all_nodes ?? true);
  const [picked, setPicked] = useState<number[]>(l?.node_ids ?? []);
  const [pickedChains, setPickedChains] = useState<number[]>(l?.chain_ids ?? []);
  const [enabled, setEnabled] = useState(l?.enabled ?? true);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  async function submit() {
    setBusy(true);
    setError("");
    const body = { name: name.trim(), all_nodes: all, node_ids: all ? [] : picked, chain_ids: all ? [] : pickedChains, enabled };
    try {
      await (l ? api(`${base}/links/${l.id}`, "PATCH", body) : api(base + "/links", "POST", body));
      saved();
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  }
  const toggle = (list: number[], id: number, on: boolean) => (on ? [...list, id] : list.filter((x) => x !== id));
  return (
    <Dialog open onOpenChange={(o) => !o && !busy && close()}>
      <DialogContent size="md">
        <DialogTitle>{l ? "编辑链接" : "新链接"}</DialogTitle>
        <DialogDescription>默认包含全部可用节点与链式代理；也可只选其中几个，例如给某台设备单独一条。</DialogDescription>
        <form className="stack" onSubmit={(e) => { e.preventDefault(); void submit(); }}>
          <Field label="名称" htmlFor="link-name" hint="例如：手机、电脑。">
            <Input id="link-name" maxLength={100} value={name} onChange={(e) => setName(e.target.value)} />
          </Field>
          <Label className="actions"><Switch aria-label="全部节点与链式代理" checked={all} onCheckedChange={setAll} />全部节点与链式代理（以后新增的自动加入）</Label>
          {!all && (
            <fieldset className="stack">
              <legend className="text-sm">节点</legend>
              {nodes.map((n) => (
                <Label key={n.id} className="actions"><Checkbox checked={picked.includes(n.id)} onCheckedChange={(v) => setPicked(toggle(picked, n.id, !!v))} />{n.name}{n.gate_name && <small className="text-[var(--mg-ink-3)]">（{n.gate_name}）</small>}</Label>
              ))}
              {chains.length > 0 && <legend className="text-sm">链式代理</legend>}
              {chains.map((c) => (
                <Label key={c.id} className="actions"><Checkbox checked={pickedChains.includes(c.id)} onCheckedChange={(v) => setPickedChains(toggle(pickedChains, c.id, !!v))} />{c.name}</Label>
              ))}
            </fieldset>
          )}
          {l && <Label className="actions"><Switch aria-label="启用" checked={enabled} onCheckedChange={setEnabled} />启用</Label>}
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

// ChainPanel：用户自建链式代理 = 一个新节点（入口节点 → 家宽 SOCKS5 或另一节点）。出口只允许公网地址，保存前由入口 VPS 实测。
export function ChainPanel({ base, nodes, admin }: { base: string; nodes: NodeOption[]; admin?: boolean }) {
  const toast = useToast();
  const chains = useChains(base);
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(0);
  if (chains.isPending || chains.error) return <State loading={chains.isPending} error={chains.error} onRetry={() => chains.refetch()} />;
  const name = (id?: number | null) => nodes.find((n) => n.id === id)?.name ?? "节点";
  async function act(id: number, fn: () => Promise<unknown>, ok: string) {
    setBusy(id);
    try {
      await fn();
      toast(ok, { tone: "ok" });
      await chains.refetch();
    } catch (e) {
      toast("操作未完成", { description: String(e), tone: "bad" });
    } finally {
      setBusy(0);
    }
  }
  return (
    <section className="stack">
      <SectionHeader title="链式代理" description="经入口节点再从家宽或另一节点出去；流量只在入口计量一次。" actions={chains.data.allow_chain && <Button size="sm" disabled={chains.data.items.length >= 10 || nodes.length === 0} onClick={() => setOpen(true)}><Plus />新建</Button>} />
      {!chains.data.allow_chain && <Notice role="status">{admin ? "该用户未开放链式代理，可在编辑用户中打开。" : "管理员未开放链式代理。"}</Notice>}
      {chains.data.items.length === 0 ? (
        <State empty="还没有链式代理" />
      ) : (
        <ul className="stack">
          {chains.data.items.map((c) => (
            <li key={c.id} className="flex flex-wrap items-center justify-between gap-2">
              <span className="min-w-0">
                <strong>{c.name}</strong> {!c.enabled && <Badge variant="soft" tone="warn">已停用</Badge>}
                <small className="block text-[var(--mg-ink-3)]">
                  {c.entry_name ?? name(c.entry_node_id)} → {c.exit_kind === "socks5" ? `SOCKS5 ${c.exit_address}:${c.exit_port}${admin && c.exit_username ? `（${c.exit_username} / ${c.exit_password ?? ""}）` : ""}` : name(c.exit_node_id)}
                </small>
              </span>
              <span className="actions">
                <Switch aria-label={"启用 " + c.name} checked={c.enabled} disabled={busy === c.id} onCheckedChange={(v) => act(c.id, () => api(`${base}/chains/${c.id}`, "PATCH", { enabled: v }), v ? "已启用" : "已停用")} />
                <Button size="icon-sm" variant="destructive" aria-label={"删除 " + c.name} disabled={busy === c.id} onClick={() => window.confirm(`删除链式代理「${c.name}」？`) && act(c.id, () => api(`${base}/chains/${c.id}`, "DELETE"), "已删除")}><Trash2 /></Button>
              </span>
            </li>
          ))}
        </ul>
      )}
      {open && <ChainForm base={base} nodes={nodes} admin={admin} close={() => setOpen(false)} saved={() => { setOpen(false); toast("链式代理已创建", { tone: "ok" }); void chains.refetch(); }} />}
    </section>
  );
}

function ChainForm({ base, nodes, admin, close, saved }: { base: string; nodes: NodeOption[]; admin?: boolean; close: () => void; saved: () => void }) {
  const [name, setName] = useState("");
  const [entry, setEntry] = useState(nodes[0]?.id ?? 0);
  const [exit, setExit] = useState<ChainSpec>({ kind: "socks5" });
  const [skip, setSkip] = useState(false);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const set = (p: Partial<ChainSpec>) => setExit({ ...exit, ...p });
  async function submit() {
    setBusy(true);
    setError("");
    try {
      await api(base + "/chains", "POST", { name: name.trim(), entry_node_id: entry, exit, skip_probe: admin && skip });
      saved();
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <Dialog open onOpenChange={(o) => !o && !busy && close()}>
      <DialogContent size="md">
        <DialogTitle>新建链式代理</DialogTitle>
        <DialogDescription>会作为一个新节点出现在订阅里。保存前由入口 VPS 实测出口是否可用。</DialogDescription>
        <form className="stack" onSubmit={(e) => { e.preventDefault(); void submit(); }}>
          <Field label="名称" htmlFor="chain-name"><Input id="chain-name" required maxLength={100} value={name} onChange={(e) => setName(e.target.value)} /></Field>
          <Field label="入口节点">
            <Select value={String(entry)} onValueChange={(v) => setEntry(Number(v))}>
              <SelectTrigger aria-label="入口节点"><SelectValue>{nodes.find((n) => n.id === entry)?.name ?? "请选择"}</SelectValue></SelectTrigger>
              <SelectContent>{nodes.map((n) => <SelectItem key={n.id} value={String(n.id)}>{n.name}</SelectItem>)}</SelectContent>
            </Select>
          </Field>
          <Segmented aria-label="出口类型" value={exit.kind as "socks5" | "node"} onValueChange={(kind) => setExit({ kind })} options={[["socks5", "家宽 SOCKS5"], ["node", "另一节点"]] as const} />
          {exit.kind === "socks5" ? (
            <div className="grid gap-3 sm:grid-cols-2">
              <Field label="公网地址" htmlFor="cx-addr"><Input id="cx-addr" required value={exit.address ?? ""} onChange={(e) => set({ address: e.target.value.trim() })} /></Field>
              <Field label="端口" htmlFor="cx-port"><Input id="cx-port" inputMode="numeric" required value={exit.port ? String(exit.port) : ""} onChange={(e) => set({ port: Number(e.target.value.replace(/\D/g, "")) || undefined })} /></Field>
              <Field label="用户名（可选）" htmlFor="cx-user"><Input id="cx-user" autoComplete="off" value={exit.username ?? ""} onChange={(e) => set({ username: e.target.value })} /></Field>
              <Field label="密码（可选）" htmlFor="cx-pass"><Input id="cx-pass" type="password" autoComplete="new-password" value={exit.password ?? ""} onChange={(e) => set({ password: e.target.value })} /></Field>
            </div>
          ) : (
            <Field label="出口节点">
              <Select value={exit.node_id ? String(exit.node_id) : ""} onValueChange={(v) => set({ node_id: Number(v) })}>
                <SelectTrigger aria-label="出口节点"><SelectValue>{nodes.find((n) => n.id === exit.node_id)?.name ?? "请选择"}</SelectValue></SelectTrigger>
                <SelectContent>{nodes.filter((n) => n.id !== entry).map((n) => <SelectItem key={n.id} value={String(n.id)}>{n.name}</SelectItem>)}</SelectContent>
              </Select>
            </Field>
          )}
          {admin && <Label className="actions"><Checkbox checked={skip} onCheckedChange={(v) => setSkip(!!v)} />跳过 VPS 实测（仅管理员）</Label>}
          {error && <Notice tone="bad">{error}</Notice>}
          <DialogFooter>
            <Button type="button" variant="outline" disabled={busy} onClick={close}>取消</Button>
            <Button type="submit" disabled={busy || !entry}>{busy ? "正在实测并保存…" : "保存"}</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
