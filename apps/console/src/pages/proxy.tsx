import { PendingExits } from "../components/pending-exits";
import { hasUnsavedChanges, useUnsavedChanges } from "../lib/dirty";
import { ProxyGroups, ProxyMigration } from "../components/proxy-groups";
import { proxyURL } from "../lib/proxy-targets";
import { CustomRulesCard } from "../components/custom-rules";
import { NodeSummary, ExitSummary } from "../components/node-summary";
import { useEffect, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { useQuery } from "../lib/query";
import { useNodeChoices } from "../lib/node-choices";
import {
  Badge, Button, Card, CardContent, CheckboxField, Chip, ChipLabel, ChoiceCard, Collapsible, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Field, Form, Input, InsetGroup, InsetRow, InsetRows, Notice,
  PageHeader, Radio, RadioGroup, SectionHeader, Select, SelectContent, SelectItem, SelectTrigger, SelectValue, SkeletonCards, Switch, Tabs, TabsList, TabsPanel, TabsTab,
  Table, TableBody, TableCell, TableHead, TableHeader, TableRow, useConfirm, type LampTone,
} from "@weir/ui";
import { Plus, Route, Trash2 } from "lucide-react";
import { api, ApiError } from "../api";
import { useFormAction, useRowAction } from "../lib/action";
import { WaitText } from "../components/wait";
import { field, useConsoleContext } from "../lib/console-context";
import type { ExitSpec, Health, NodeItem, ProxyType, ProxyView, UserItem } from "../lib/core";
import { ExitFields, exitErrorFields, exitKindName } from "../components/chain-exit";
import { ChainPanel, SourceSummary, type NodeOption } from "../components/link-panel";
import { State } from "../components/feedback";
import { useStringDialogParam, dataObject, ObjectLink } from "../components/object-link";
import { objectHref } from "../lib/objects";
import { rateText } from "../lib/linkmap";
import type { RateStat } from "../lib/core";

const healthTone: Record<Health, LampTone> = { ok: "ok", down: "bad", unknown: "unknown" };
const healthText: Record<Health, string> = { ok: "可用", down: "不可用", unknown: "未知" };


export function ProxyPage({ admin }: { admin: boolean }) {
  const [params, setParams] = useSearchParams();
  const uid = admin ? Number(params.get("user")) || null : null;
  const base = uid ? `/admin/users/${uid}` : "/me";
  const tab = ["clients", "rules", "nodes"].includes(params.get("tab") ?? "") ? params.get("tab")! : "clients";
  const confirm = useConfirm();
  const changeParams = async (next: URLSearchParams) => { setParams(next); };
  const view = useQuery({ queryKey: ["proxy", base], queryFn: () => api(proxyURL(base)) as Promise<ProxyView>, refetchInterval: 30_000 });
  const nodes = useNodeOptions(admin, uid);
  useConsoleContext({
    page: "代理",
    tab: ({clients:"客户端选择",rules:"全局清单",nodes:"节点与出口"})[tab],
    list: {
      filters: admin ? { 用户: uid ? String(uid) : "自己" } : undefined,
      total: view.data?.types.length ?? 0,
    },
    notices: (view.data?.types ?? []).filter((t) => (t.missing_exit ?? []).length > 0).map((t) => `${t.label} 有 ${t.missing_exit!.length} 个节点还没有加出口`),
  });
  const header = (
    <PageHeader
      title="代理"
      description="管理客户端选择、全局清单和节点出口。客户端下次更新订阅时生效。"
      actions={admin ? <div className="flex flex-wrap items-center gap-2"><span>查看用户</span><UserPick value={uid} onChange={(id) => { const p = new URLSearchParams(params); if (id) p.set("user", String(id)); else p.delete("user"); p.delete("proxy_group"); p.delete("new_group"); p.delete("type_exit"); void changeParams(p); }} /></div> : undefined}
    />
  );
  if (view.isPending || view.error)
    return (
      <div className="stack">
        {header}
        <State loading={view.isPending} error={view.error} onRetry={() => view.refetch()} skeleton={<SkeletonCards count={3} lines={2} />} />
      </div>
    );
  const v = view.data;
  return (
    <div className="stack">
      {header}
      <ProxyMigration base={base} view={v} onSaved={() => view.refetch()} />
      <Tabs value={tab} onValueChange={(value) => { const p = new URLSearchParams(params); p.set("tab", String(value)); p.delete("proxy_group"); p.delete("new_group"); p.delete("type_exit"); void changeParams(p); }}>
        <TabsList className="flex flex-wrap h-auto"><TabsTab value="clients">客户端选择</TabsTab><TabsTab value="rules">全局清单</TabsTab><TabsTab value="nodes">节点与出口</TabsTab></TabsList>
        <TabsPanel value="clients"><ProxyGroups key={base} base={base} view={v} admin={admin} onSaved={() => view.refetch()} /></TabsPanel>
      <TabsPanel value="rules" className="stack"><CustomRulesCard key={base} base={base} view={v} onSaved={() => view.refetch({ throwOnError: true })} /><InsetGroup title="后续内置规则" description="清单从上到下优先匹配，随后依次匹配以下规则。"><InsetRows>{["局域网与本机 → 直连", "国内域名与国内 IP → 直连", "其余流量 → PROXY"].map((text) => <InsetRow key={text}>{text}</InsetRow>)}</InsetRows></InsetGroup></TabsPanel>
      <TabsPanel value="nodes" className="stack">
        {!v.types.length && <State empty="还没有可用的节点" detail="请先分配节点，再设置协议类型和出口。" />}
        <TypesCard base={base} view={v} nodes={nodes} admin={admin} onSaved={() => void view.refetch()} />
        <PolicyCard key={base + v.policy.revision} base={base} view={v} onSaved={() => void view.refetch()} />
        <RankingCard view={v} admin={admin} />
        <ChainPanel key={base} base={base} admin={!!uid} />
      </TabsPanel>
      </Tabs>
      <PendingExits base={base} />
    </div>
  );
}

function useNodeOptions(_admin: boolean, uid: number | null): NodeOption[] {
  return useNodeChoices(uid ? `/admin/users/${uid}` : "/me").data ?? [];
}

function UserPick({ value, onChange }: { value: number | null; onChange: (id: number | null) => void }) {
  const users = useQuery({ queryKey: ["users"], queryFn: async () => (await api("/admin/users")).items as UserItem[] });
  const current = users.data?.find((u) => u.id === value);
  return (
    <Select value={value ? String(value) : "self"} onValueChange={(v) => onChange(v === "self" ? null : Number(v))}>
      <SelectTrigger aria-label="查看哪个用户的代理设置" className="min-w-44">
        <SelectValue>{current ? current.name || current.email : "我自己"}</SelectValue>
      </SelectTrigger>
      <SelectContent>
        <SelectItem value="self">我自己</SelectItem>
        {(users.data ?? []).filter((u) => u.role !== "admin").map((u) => <SelectItem key={u.id} value={String(u.id)}>{u.name ? `${u.name}（${u.email}）` : u.email}</SelectItem>)}
      </SelectContent>
    </Select>
  );
}

// TypesCard：每个类型一块。开关决定放不放进订阅；出口给这一类的全部节点统一加（每个节点由入口 VPS 实测）。
function TypesCard({ base, view, nodes, admin, onSaved }: { base: string; view: ProxyView; nodes: NodeOption[]; admin: boolean; onSaved: () => void }) {
  const confirm = useConfirm();
  const navigate = useNavigate();
  const exitParam = useStringDialogParam("type_exit");
  const exitFor = view.types.find((t) => t.key === exitParam.value);
  const { run, isBusy } = useRowAction<string>({ refetch: async () => onSaved() });
  const disabled = new Set(view.policy.disabled ?? []);
  const toggle = (key: string, on: boolean) => {
    const next = on ? [...disabled].filter((k) => k !== key) : [...disabled, key];
    return run(key, () => api(proxyURL(base), "PUT", { ...view.policy, disabled: next }), on ? "已放进订阅" : "已从订阅里拿掉");
  };
  return (
    <InsetGroup title="节点类型" description="同一种协议方案的节点归为一类。关掉的类型不放进订阅；加了出口的类型，订阅里换成经出口的版本。">
        <InsetRows>
          {view.types.map((t) => (
            <InsetRow key={t.key} className="stack" data-disabled={!t.enabled || undefined}>
              <div className="flex items-center justify-between gap-2">
                <strong className="flex min-w-0 items-center gap-2">
                  <span className="truncate">{t.label}</span>
                  <TypeCount type={t} />
                </strong>
                <Switch aria-label={`${t.label} 放进订阅`} checked={t.enabled} disabled={isBusy(t.key) || isBusy("exit:" + t.key)} onCheckedChange={(on) => void toggle(t.key, on)} />
              </div>
              <div className="flex flex-wrap gap-1.5">
                {/* 管理员点节点打开它的详情；普通用户只看状况 */}
                {t.nodes.map((n) => {
                  const meta = n.health === "down" ? healthText.down : n.rate.minutes ? rateText(n.rate.p90_bps) : healthText[n.health];
                  return <ChipLabel key={n.id} tone={healthTone[n.health]} meta={meta} title={n.why}><ObjectLink to={{ type: "node", id: n.id, name: n.name }} href={admin ? undefined : `/proxy?tab=nodes&focus=${encodeURIComponent(`node:${n.id}`)}`}><NodeSummary node={n} compact /></ObjectLink></ChipLabel>;
                })}
              </div>
              <div className="flex flex-wrap items-center justify-between gap-2 text-meta">
                <span className="min-w-0 text-[var(--weir-ink-2)]">
                  出口：{t.exit ? <ExitSummary exit={t.exit} /> : "直接从 VPS 出去"}
                </span>
                <span className="flex gap-1.5">
                  {view.allow_chain && (
                    <Button size="sm" variant="outline" onClick={() => exitParam.open(t.key)}>
                      {t.exit ? "改出口" : <><Plus />加出口</>}
                    </Button>
                  )}
                  {t.exit && (
                    <Button size="icon-sm" variant="destructive" aria-label={`去掉 ${t.label} 的出口`} disabled={isBusy()} busy={isBusy("exit:" + t.key)} busyText="正在去掉"
                      onClick={async () => (await confirm({ title: `去掉「${t.label}」的出口？`, description: "这类节点恢复直接从 VPS 出去。", confirmText: "去掉", tone: "danger" })) && run("exit:" + t.key, () => api(proxyURL(base, `/types/${encodeURIComponent(t.key)}/exit`), "DELETE", { revision: view.policy.revision }), "已去掉出口")}>
                      <Trash2 />
                    </Button>
                  )}
                </span>
              </div>
              {(t.missing_exit ?? []).length > 0 && (
                <Notice tone="warn">新加的节点还没有经出口：{t.missing_exit!.join("、")}。点「改出口」再保存一次即可。</Notice>
              )}
            </InsetRow>
          ))}
        </InsetRows>
        {!view.allow_chain && <Notice role="status">{admin ? "这个用户没有开放链式代理，不能给类型加出口；可在用户详情里打开。" : "管理员没有开放链式代理，不能给类型加出口。"}</Notice>}
        {exitFor && <TypeExitDialog revision={view.policy.revision} base={base} type={exitFor} nodes={nodes} admin={admin && base !== "/me"} close={() => exitParam.close()} saved={() => { exitParam.close(); onSaved(); }} />}
    </InsetGroup>
  );
}

// TypeCount：确认可用的才算可用；状况未知的单独写出来，不算作可用（观测缺失不能显示为正常）。
function TypeCount({ type: t }: { type: ProxyType }) {
  const ok = t.nodes.filter((n) => n.health === "ok").length;
  const unknown = t.nodes.filter((n) => n.health === "unknown").length;
  const tone = ok === t.nodes.length ? "ok" : ok === 0 && unknown === 0 ? "bad" : "warn";
  return (
    <Badge variant="soft" tone={tone}>
      {ok} / {t.nodes.length} 可用{unknown > 0 ? ` · ${unknown} 未知` : ""}
    </Badge>
  );
}

// TypeExitDialog：给一个类型的全部节点加同一个出口；有一个节点连不通就整体不保存，逐个列出原因。
function TypeExitDialog({ base, type, nodes, admin, close, saved, revision }: { revision: number; base: string; type: ProxyType; nodes: NodeOption[]; admin: boolean; close: () => void; saved: () => void }) {
  const e = type.exit;
  const confirm = useConfirm();
  const [exit, setExit] = useState<ExitSpec>(
    e ? { remark:e.remark, kind: e.kind, address: e.address, port: e.port, username: e.username, password: e.password, link: e.link, node_id: e.node_id ?? undefined, ...(e.source ?? {}) } : { kind: "socks5" },
  );
  const [initial] = useState(() => JSON.stringify(exit));
  const dirty = JSON.stringify(exit) !== initial;
  const markSaved = useUnsavedChanges(dirty);
  const requestClose = async () => {
    if (save.busy && !save.detachable) return;
    if (!dirty || await confirm({title:"放弃未保存修改？",description:"出口尚未保存。",confirmText:"放弃修改",tone:"danger"})) {
      save.leave(); markSaved(); close();
    }
  };
  const save = useFormAction((track) => api(proxyURL(base, `/types/${encodeURIComponent(type.key)}/exit`), "PUT", { exit, revision }, track), {
    fields: exitErrorFields(exit.kind),
    onSuccess: () => { markSaved(); saved(); },
    wait: { label: `正在逐个实测「${type.label}」的 ${type.nodes.length} 个节点`, max: 45 * type.nodes.length },
  });
  const failed = save.raw instanceof ApiError && save.raw.code === "type_exit_failed" ? (save.raw.extra.nodes as { name: string; code: string; detail?: string }[] | undefined) : undefined;
  useConsoleContext({
    dialog: { title: `给「${type.label}」加出口` },
    form: [
      field("exit_kind", "出口类型", exitKindName[exit.kind]),
      ...(["socks5", "http", "https"].includes(exit.kind) ? [field("address", "代理地址", exit.address), field("port", "端口", exit.port), field("username", "用户名", exit.username), field("password", "代理密码", exit.password, true)] : []),
      ...(exit.kind === "link" ? [field("link", "节点链接", exit.link, true)] : []),
    ],
  });
  return (
    <Dialog open onOpenChange={(o) => { if (!o) void requestClose(); }}>
      <DialogContent size="md">
        <DialogTitle>{e ? "改" : "加"}出口：{type.label}</DialogTitle>
        <DialogDescription>这一类的 {type.nodes.length} 个节点都经这个出口出去；由每个节点所在的 VPS 实测，全部通过后才切换；未通过的候选可重试或放弃。</DialogDescription>
        <Form busy={save.busy} errors={save.errors} onSubmit={() => void save.submit()}>
          <ExitFields value={exit} onChange={setExit} admin={false} sourcePath={base === "/me" ? "/me/chain-sources" : `${base}/chain-sources`} nodes={nodes} nodeHint="出口需要你可用的另一个节点；这个节点自己作入口时直接出去。" />
          <DialogFooter error={failed ? <span className="block max-h-24 overflow-y-auto">{failed.map((f) => <span className="block" key={f.name}><strong>{f.name}</strong>：{f.detail || f.code}</span>)}</span> : save.error} status={save.wait && <WaitText wait={save.wait} />}>
            <Button type="button" variant="outline" disabled={save.busy} onClick={() => void requestClose()}>取消</Button>
            <Button type="submit" busy={save.busy} busyText="正在逐个实测…">保存</Button>
          </DialogFooter>
        </Form>
      </DialogContent>
    </Dialog>
  );
}



function PolicyCard({ base, view, onSaved }: { base: string; view: ProxyView; onSaved: () => void }) {
  const [refresh, setRefresh] = useState(String(view.policy.auto.refresh_minutes));
  const dirty = refresh !== String(view.policy.auto.refresh_minutes);
  useUnsavedChanges(dirty);
  const save = useFormAction(() => api(proxyURL(base), "PUT", { ...view.policy, auto: { refresh_minutes: Number(refresh) } }), { fields: { invalid_refresh: "refresh" }, onSuccess: onSaved, ok: "已保存刷新间隔" });
  useConsoleContext({form:[field("refresh", "节点列表刷新间隔", refresh)]});
  return <InsetGroup title="节点列表刷新"><Form busy={save.busy} errors={save.errors} onSubmit={() => void save.submit()}><Field name="refresh" label="节点列表刷新间隔（分钟）" required hint="30–1440 分钟。与分组探测间隔分开；分组结构变化需更新完整订阅。"><Input type="number" min={30} max={1440} step={1} required value={refresh} onChange={(e) => setRefresh(e.target.value)} /></Field><Button type="submit" disabled={!dirty} busy={save.busy} busyText="正在保存">保存</Button><div className="min-h-6" role="status">{save.error}</div></Form></InsetGroup>;
}

// 速度排名只供展示；客户端成员顺序由统一订阅计划决定。
function RankingCard({ view, admin }: { view: ProxyView; admin: boolean }) {
  return (
    <InsetGroup
      title="近 24 小时下载速度（有效分钟 P90）"
      description="近 24 小时已完成且下载至少 1 MiB 的有效分钟，按采样秒数计算速度后取 P90。速度仅作参考，不改变客户端节点与分组顺序。"
    >
        {view.ranking.length === 0 ? (
          <State empty="订阅里没有节点" detail="类型都关掉了，或节点都不可用。" />
        ) : (
          <div>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="w-14">名次</TableHead>
                  <TableHead>节点</TableHead>
                  <TableHead>类型</TableHead>
                  <TableHead>状况</TableHead>
                  <TableHead className="text-right">下载速度</TableHead>
                  <TableHead className="text-right">有效分钟</TableHead>
                  <TableHead>自动选择</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {view.ranking.map((r) => (
                  <TableRow key={r.ref} {...dataObject({ type: r.ref.startsWith("chain:") ? "chain" : "node", id: r.ref.startsWith("chain:") ? r.chain_id : r.node_id })}>
                    <TableCell className="tabular-nums">{r.rank || "—"}</TableCell>
                    <TableCell><ObjectLink href={admin ? undefined : `/proxy?tab=nodes&focus=${encodeURIComponent(r.ref)}`} to={{ type: r.ref.startsWith("chain:") ? "chain" : "node", id: r.ref.startsWith("chain:") ? r.chain_id : r.node_id, owner: view.user_id, name: r.name }} /></TableCell>
                    <TableCell>{view.types.find((t) => t.key === r.type)?.label ?? r.type}</TableCell>
                    <TableCell>
                      <Badge variant="soft" tone={r.health === "ok" ? "ok" : r.health === "down" ? "bad" : "neutral"}>{healthText[r.health]}</Badge>
                      {r.why && <p className="text-meta text-[var(--weir-ink-2)]">{r.why}</p>}
                    </TableCell>
                    <TableCell className="text-right tabular-nums">
                      {r.rate.minutes ? rateText(r.rate.p90_bps) : "未知"}
                      {r.rate.from === "all" && <p className="text-meta text-[var(--weir-ink-2)]">全部用户</p>}
                    </TableCell>
                    <TableCell className="text-right tabular-nums">{r.rate.minutes || "—"}</TableCell>
                    <TableCell>{r.auto ? "在" : <span className="text-[var(--weir-ink-3)]">不在</span>}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
    </InsetGroup>
  );
}

