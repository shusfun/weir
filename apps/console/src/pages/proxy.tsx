import { PendingExits } from "../components/pending-exits";
import { hasUnsavedChanges, useUnsavedChanges } from "../lib/dirty";
import { ProxyGroups, ProxyMigration } from "../components/proxy-groups";
import { proxyTargets, proxyURL } from "../lib/proxy-targets";
import { CustomRulesCard } from "../components/custom-rules";
import { NodeSummary, ExitSummary } from "../components/node-summary";
import { useEffect, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { useQuery } from "../lib/query";
import { useNodeChoices } from "../lib/node-choices";
import {
  Badge, Button, Card, CardContent, CheckboxField, Chip, ChipLabel, ChoiceBoard, ChoiceCard, CodeBlock, Collapsible, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Field, Form, Input, InsetGroup, InsetRow, InsetRows, Notice, Segmented,
  PageHeader, Radio, RadioGroup, SectionHeader, Select, SelectContent, SelectItem, SelectTrigger, SelectValue, SkeletonCards, Tabs, TabsList, TabsPanel, TabsTab,
  TextLink, Table, TableBody, TableCell, TableHead, TableHeader, TableRow, useConfirm, type LampTone,
} from "@weir/ui";
import { Plus, Trash2 } from "lucide-react";
import { api, ApiError } from "../api";
import { useFormAction, useRowAction } from "../lib/action";
import { WaitText } from "../components/wait";
import { field, useConsoleContext } from "../lib/console-context";
import type { CustomRuleKind, ExitSpec, Health, ProxyType, ProxyView, UserItem } from "../lib/core";
import { ExitFields, exitErrorFields, exitKindName } from "../components/chain-exit";
import { ChainPanel, type NodeOption } from "../components/link-panel";
import { MyExits } from "../components/my-exits";
import { State } from "../components/feedback";
import { useStringDialogParam, dataObject, ObjectLink } from "../components/object-link";
import { rateText } from "../lib/linkmap";

const healthTone: Record<Health, LampTone> = { ok: "ok", down: "bad", unknown: "unknown" };
const healthText: Record<Health, string> = { ok: "可用", down: "不可用", unknown: "未知" };
const ruleKindShort: Record<CustomRuleKind, string> = { domain: "完整域名", domain_suffix: "域名后缀", domain_keyword: "关键词", ip_cidr: "IP 段" };


export function ProxyPage({ admin }: { admin: boolean }) {
  const client = useQueryClient();
  const [params, setParams] = useSearchParams();
  const uid = admin ? Number(params.get("user")) || null : null;
  const base = uid ? `/admin/users/${uid}` : "/me";
  const tab = ["clients", "rules", "nodes"].includes(params.get("tab") ?? "") ? params.get("tab")! : "clients";
  const changeParams = async (next: URLSearchParams) => { setParams(next); };
  const jumpTab = (nextTab: string) => {
    const p = new URLSearchParams(params);
    p.set("tab", nextTab);
    p.delete("proxy_group");
    p.delete("new_group");
    p.delete("type_exit");
    void changeParams(p);
    window.requestAnimationFrame(() => {
      document.getElementById("proxy-tabs")?.scrollIntoView({ behavior: "smooth", block: "start" });
    });
  };
  const view = useQuery({ queryKey: ["proxy", base], queryFn: () => api(proxyURL(base)) as Promise<ProxyView>, refetchInterval: 30_000 });
  const updateView = (updated: ProxyView) => {
    void client.cancelQueries({ queryKey: ["proxy", base], exact: true });
    client.setQueryData(["proxy", base], updated);
  };
  const nodes = useNodeOptions(admin, uid);
  useConsoleContext({
    page: admin ? "客户端分流" : "我的代理",
    tab: ({ clients: "客户端选择", rules: "全局清单", nodes: "个人链式与测速" })[tab],
    list: {
      filters: admin ? { 用户: uid ? String(uid) : "自己" } : undefined,
      total: view.data?.types.length ?? 0,
    },
    notices: (view.data?.types ?? []).filter((t) => (t.missing_exit ?? []).length > 0).map((t) => `${t.label} 有 ${t.missing_exit!.length} 个节点还没有加出口`),
  });
  const header = (
    <PageHeader
      size="data"
      title={admin ? "客户端分流" : "我的代理"}
      description={proxyLead(view.data)}
      actions={admin ? <div className="flex flex-wrap items-center gap-2"><span className="text-meta text-[var(--weir-ink-3)]">当前配置用户</span><UserPick value={uid} onChange={(id) => { const p = new URLSearchParams(params); if (id) p.set("user", String(id)); else p.delete("user"); p.delete("proxy_group"); p.delete("new_group"); p.delete("type_exit"); void changeParams(p); }} /></div> : undefined}
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
      <ProxyBoard base={base} view={v} admin={admin} onSaved={() => void view.refetch()} onUpdated={updateView} onJumpTab={jumpTab} />
      <ProxyMigration base={base} view={v} onSaved={() => view.refetch()} />
      <div id="proxy-tabs" className="scroll-mt-[calc(var(--weir-gap-41)*2)]">
        <Tabs value={tab} onValueChange={(value) => { const p = new URLSearchParams(params); p.set("tab", String(value)); p.delete("proxy_group"); p.delete("new_group"); p.delete("type_exit"); void changeParams(p); }}>
          <TabsList className="flex flex-wrap h-auto"><TabsTab value="clients">客户端选择</TabsTab><TabsTab value="rules">全局清单</TabsTab><TabsTab value="nodes">个人链式与测速</TabsTab></TabsList>
          <TabsPanel value="clients"><ProxyGroups key={base} base={base} view={v} admin={admin} onUpdated={updateView} /></TabsPanel>
          <TabsPanel value="rules" className="stack"><CustomRulesCard key={base} base={base} view={v} onSaved={() => view.refetch({ throwOnError: true })} /></TabsPanel>
          <TabsPanel value="nodes" className="stack">
            {admin && <Notice role="status">此处出口与链式代理仅对当前所选用户的订阅生效；若要给所有用户共用的节点挂出口，请前往 <Link className="underline" to="/gates">节点</Link> 页面设置。</Notice>}
            {!v.types.length ? <State empty="还没有可用的节点" detail="请先分配节点，再设置个人链式出口。" /> : <TypesCard base={base} view={v} nodes={nodes} admin={admin} onSaved={() => void view.refetch()} />}
            <PolicyCard key={base + v.policy.revision} base={base} view={v} onUpdated={updateView} />
            <RankingCard view={v} admin={admin} />
            {!admin && <MyExits canSave={v.allow_chain} />}
            <ChainPanel key={base} base={base} admin={!!uid} />
          </TabsPanel>
        </Tabs>
      </div>
      <PendingExits base={base} />
    </div>
  );
}

function proxyLead(view?: ProxyView) {
  if (!view) return "客户端下次更新订阅时生效。";
  const nodes = view.types.reduce((n, t) => n + t.nodes.length, 0);
  return <span className="weir-sum"><span>订阅含 <b>{nodes}</b> 个自有节点</span><span><b>{view.custom.active_count}</b> 条自定义分流</span><span>客户端下次更新订阅时生效</span></span>;
}

function ProxyBoard({ base, view, admin, onSaved, onUpdated, onJumpTab }: { base: string; view: ProxyView; admin: boolean; onSaved: () => void; onUpdated: (view: ProxyView) => void; onJumpTab: (tab: string) => void }) {
  const [fmt, setFmt] = useState("clash");
  const [expandFlows, setExpandFlows] = useState(false);
  const preview = useQuery({ queryKey: ["proxy-preview", view, fmt], queryFn: () => api(`/proxy/preview?user=${view.user_id}&target=${fmt}`) as Promise<{ body: string; link_name: string }> });
  const typeAction = useRowAction<string>();
  const disabled = new Set(view.policy.disabled ?? []);
  const toggleType = (key: string, on: boolean) => {
    const next = on ? [...disabled].filter((k) => k !== key) : [...disabled, key];
    return typeAction.run(key, async () => {
      try {
        const updated = await api(proxyURL(base), "PUT", { ...view.policy, disabled: next }) as ProxyView;
        onUpdated(updated);
        return updated;
      } catch (error) {
        if (error instanceof ApiError && error.code === "proxy_revision_conflict") onSaved();
        throw error;
      }
    }, on ? "已放进订阅" : "已从订阅里拿掉");
  };
  const targetMap = new Map(proxyTargets(view));
  const formatTarget = (raw?: string) => {
    if (!raw) return "未知";
    if (targetMap.has(raw)) return targetMap.get(raw)!;
    if (raw === "direct" || raw === "DIRECT") return "直连";
    if (raw === "proxy" || raw === "PROXY") return "PROXY";
    if (raw === "reject" || raw === "REJECT") return "拒绝";
    return raw;
  };
  const activeCustom = view.custom.entries.filter((entry) => entry.enabled);
  const shownCustom = expandFlows ? activeCustom : activeCustom.slice(0, 4);
  const builtinFlows = [
    { label: "局域网与本机", target: "直连" },
    { label: "国内域名与国内 IP", target: "直连" },
    { label: "其余流量", target: "PROXY" },
  ];
  return (
    <div className="grid items-start gap-[var(--weir-gap-29)] lg:grid-cols-[minmax(0,1.45fr)_minmax(0,1fr)]">
      <div className="stack">
        <Card className="px-[var(--weir-gap-25)] py-[var(--weir-gap-21)]">
          <div className="mb-[var(--weir-gap-17)] flex items-center justify-between gap-3"><b>订阅包含的协议</b><span className="text-meta text-[var(--weir-ink-3)]">点击拨杆纳入或移出</span></div>
          {!view.types.length ? <State empty="还没有可用的节点" /> : (
            <div className="grid gap-[var(--weir-gap-21)] sm:grid-cols-3">
              {view.types.map((type) => (
                <div key={type.key} className="flex items-center gap-[var(--weir-gap-13)]">
                  <button
                    type="button"
                    role="switch"
                    aria-checked={type.enabled}
                    aria-label={`${type.label} 放进订阅`}
                    disabled={typeAction.isBusy()}
                    onClick={() => void toggleType(type.key, !type.enabled)}
                    className="weir-lever weir-press cursor-pointer disabled:cursor-not-allowed disabled:opacity-50"
                    data-on={type.enabled || undefined}
                  ><i /></button>
                  <div className="min-w-0"><b className="block truncate">{type.label}</b><div className="text-fine text-[var(--weir-ink-3)]">{type.nodes.length} 个节点 · {type.enabled ? (type.exit ? "个人链式出口" : "直连 VPS") : "已移出"}</div></div>
                </div>
              ))}
            </div>
          )}
          <p className="mt-[var(--weir-gap-13)] mb-0 text-fine text-[var(--weir-ink-3)]">按协议给当前用户单独挂载出口可在下方 <TextLink render={<button type="button" onClick={() => onJumpTab("nodes")} />}>个人链式与测速</TextLink> 中设置</p>
        </Card>
        <Card className="overflow-hidden p-0">
          <div className="weir-set-row"><b>客户端流量走向</b><span className="flex flex-wrap items-center gap-1"><Button size="sm" variant="ghost" onClick={() => onJumpTab("rules")}>编辑全局清单（{activeCustom.length}）</Button>{admin && <Button size="sm" variant="ghost" render={<Link to="/rules" />}>服务端规则</Button>}</span></div>
          {shownCustom.map((entry) => {
            const label = formatTarget(entry.effective_target || entry.target);
            const isDirect = label === "直连";
            return (
              <div key={entry.id} className="weir-set-row">
                <span className="flex min-w-0 items-center gap-2">
                  <span className="truncate font-mono text-meta">{entry.value}</span>
                  <span className="shrink-0 text-fine text-[var(--weir-ink-3)]">{ruleKindShort[entry.kind]}</span>
                </span>
                <Badge variant="soft" tone={isDirect ? "neutral" : "info"}>{label}</Badge>
              </div>
            );
          })}
          {activeCustom.length > 4 && (
            <div className="weir-set-row py-2">
              <TextLink render={<button type="button" onClick={() => setExpandFlows((v) => !v)} />}>{expandFlows ? "收起自定义规则" : `展开其余 ${activeCustom.length - 4} 条自定义规则`}</TextLink>
            </div>
          )}
          {builtinFlows.map((flow) => (
            <div key={flow.label} className="weir-set-row">
              <span className="text-[var(--weir-ink-2)]">{flow.label}</span>
              <Badge variant="soft" tone={flow.target === "直连" ? "neutral" : "info"}>{flow.target}</Badge>
            </div>
          ))}
        </Card>
      </div>
      <div>
        <div className="mb-[var(--weir-gap-9)] flex flex-wrap items-center justify-between gap-2">
          <div>
            <b>订阅预览</b>
            {preview.data && <small className="ml-2 text-[var(--weir-ink-3)]">{preview.data.link_name || "默认"} · 不计拉取统计</small>}
          </div>
          <Segmented size="sm" aria-label="预览格式" value={fmt} onValueChange={setFmt} options={[["clash", "Clash"], ["base64", "小火箭"], ["singbox", "sing-box"]] as const} />
        </div>
        {preview.isPending || preview.error ? <State loading={preview.isPending} error={preview.error} onRetry={() => preview.refetch()} /> : <pre tabIndex={0} role="region" aria-label="代理配置预览" className="weir-paper max-h-[421px] overflow-auto">{preview.data.body}</pre>}
      </div>
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

// TypesCard：按协议类型给当前用户统一加出口（协议是否放入订阅已由顶部拨杆控制，此处不再重复放置开关）。
function TypesCard({ base, view, nodes, admin, onSaved }: { base: string; view: ProxyView; nodes: NodeOption[]; admin: boolean; onSaved: () => void }) {
  const confirm = useConfirm();
  const exitParam = useStringDialogParam("type_exit");
  const exitFor = view.types.find((t) => t.key === exitParam.value);
  const { run, isBusy } = useRowAction<string>({ refetch: async () => onSaved() });
  return (
    <InsetGroup title="按协议挂个人出口" description="给当前用户的整类协议节点统一挂载链式出口（每个节点由入口 VPS 实测通过后切换）。">
        <InsetRows>
          {view.types.map((t) => (
            <InsetRow key={t.key} className="stack" data-disabled={!t.enabled || undefined}>
              <div className="flex flex-wrap items-center justify-between gap-2">
                <strong className="flex min-w-0 items-center gap-2">
                  <span className="truncate">{t.label}</span>
                  <TypeCount type={t} />
                  {!t.enabled && <Badge variant="soft" tone="neutral">已从订阅移出</Badge>}
                </strong>
                <span className="flex items-center gap-2 text-meta">
                  <span className="text-[var(--weir-ink-2)]">
                    {t.exit ? <>出口：<ExitSummary exit={t.exit} /></> : "直接从 VPS 出站"}
                  </span>
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
              <div className="flex flex-wrap gap-1.5">
                {t.nodes.map((n) => {
                  const meta = n.health === "down" ? healthText.down : n.rate.minutes ? rateText(n.rate.p90_bps) : healthText[n.health];
                  return <ChipLabel key={n.id} tone={healthTone[n.health]} meta={meta} title={n.why}><ObjectLink to={{ type: "node", id: n.id, name: n.name }} href={admin ? undefined : `/proxy?tab=nodes&focus=${encodeURIComponent(`node:${n.id}`)}`}><NodeSummary node={n} compact /></ObjectLink></ChipLabel>;
                })}
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
    onDetach: () => { markSaved(); close(); },
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
        <DialogDescription>这一类的 {type.nodes.length} 个节点都经这个出口出去；由每个节点所在的 VPS 实测，全部通过后才切换；没通过的会留在页面上的「出口验证」列表里，可以在那里重试或放弃。</DialogDescription>
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



function PolicyCard({ base, view, onUpdated }: { base: string; view: ProxyView; onUpdated: (view: ProxyView) => void }) {
  const [refresh, setRefresh] = useState(String(view.policy.auto.refresh_minutes));
  const dirty = refresh !== String(view.policy.auto.refresh_minutes);
  useUnsavedChanges(dirty);
  const save = useFormAction<ProxyView>(() => api(proxyURL(base), "PUT", { ...view.policy, auto: { refresh_minutes: Number(refresh) } }) as Promise<ProxyView>, { fields: { invalid_refresh: "refresh" }, onSuccess: onUpdated, ok: "已保存刷新间隔" });
  useConsoleContext({form:[field("refresh", "节点列表刷新间隔", refresh)]});
  return <InsetGroup title="节点列表刷新"><Form busy={save.busy} errors={save.errors} onSubmit={() => void save.submit()}><div className="flex flex-wrap items-end gap-2"><Field name="refresh" label="节点列表刷新间隔（分钟）" required hint="30–1440 分钟；分组结构变化需更新完整订阅。" className="min-w-0 flex-1 basis-56"><Input type="number" min={30} max={1440} step={1} required value={refresh} onChange={(e) => setRefresh(e.target.value)} /></Field><Button type="submit" disabled={!dirty} busy={save.busy} busyText="正在保存">保存</Button></div>{save.error && <Notice tone="bad" role="status">{save.error}</Notice>}</Form></InsetGroup>;
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

