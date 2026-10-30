// 代理：管理员与普通用户共用一页。把已授权的节点按类型（协议方案）归类：选哪些类型放进订阅、给某个类型统一加出口；
// 按网站类别分流到某一组；自动选择由客户端测延迟，或由中心按各 VPS 的情况排序后只给排在前面的几个。
// 这些都写进订阅、由客户端执行（代理流量不经过中心），客户端下次更新订阅时生效。管理员可以选用户查看、修改他的设置。
import { useEffect, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import {
  Badge, Button, Card, CardContent, CheckboxField, Chip, ChipLabel, ChoiceCard, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Field, Form, Input, Notice,
  PageHeader, Radio, RadioGroup, SectionHeader, Select, SelectContent, SelectItem, SelectTrigger, SelectValue, SkeletonCards, Switch,
  Table, TableBody, TableCell, TableHead, TableHeader, TableRow, useConfirm, type LampTone,
} from "@weir/ui";
import { Plus, Route, Trash2 } from "lucide-react";
import { api, ApiError } from "../api";
import { useFormAction, useRowAction } from "../lib/action";
import { field, useConsoleContext } from "../lib/console-context";
import type { ChainSpec, Health, NodeItem, ProxyType, ProxyView, UserItem } from "../lib/core";
import { ChainExitFields, exitErrorFields, exitKindName } from "../components/chain-exit";
import { ChainPanel, SourceSummary, type NodeOption } from "../components/link-panel";
import { State } from "../components/feedback";
import { ObjectLink } from "../components/object-link";
import { objectHref } from "../lib/objects";
import { rateText } from "../lib/linkmap";
import type { RateStat } from "../lib/core";

const healthTone: Record<Health, LampTone> = { ok: "ok", down: "bad", unknown: "unknown" };
const healthText: Record<Health, string> = { ok: "可用", down: "不可用", unknown: "未知" };


export function ProxyPage({ admin }: { admin: boolean }) {
  const [params, setParams] = useSearchParams();
  const uid = admin ? Number(params.get("user")) || null : null;
  const base = uid ? `/admin/users/${uid}` : "/me";
  const view = useQuery({ queryKey: ["proxy", base], queryFn: () => api(base + "/proxy") as Promise<ProxyView>, refetchInterval: 30_000 });
  const nodes = useNodeOptions(admin, uid);
  useConsoleContext({
    page: "代理",
    list: {
      filters: admin ? { 用户: uid ? String(uid) : "自己" } : undefined,
      total: view.data?.types.length ?? 0,
    },
    notices: (view.data?.types ?? []).filter((t) => (t.missing_exit ?? []).length > 0).map((t) => `${t.label} 有 ${t.missing_exit!.length} 个节点还没有加出口`),
  });
  const header = (
    <PageHeader
      eyebrow="代理与订阅"
      title="代理"
      description="按类型管理已授权的节点：选哪些放进订阅、给类型加出口、按网站分流、自动选择。客户端下次更新订阅时生效。"
      actions={admin ? <UserPick value={uid} onChange={(id) => setParams((p) => { if (id) p.set("user", String(id)); else p.delete("user"); return p; }, { replace: true })} /> : undefined}
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
      {v.types.length === 0 ? (
        <State empty="还没有可用的节点" detail={admin ? "先在「VPS 与节点」里建节点，并分给这个用户。" : "请联系管理员分配节点。"} />
      ) : (
        <>
          <TypesCard base={base} view={v} nodes={nodes} admin={admin} onSaved={() => void view.refetch()} />
          <PolicyCard base={base} view={v} onSaved={() => void view.refetch()} />
          <RankingCard view={v} />
          <Card><CardContent><ChainPanel base={base} nodes={nodes} admin={!!uid} /></CardContent></Card>
        </>
      )}
    </div>
  );
}

function useNodeOptions(admin: boolean, uid: number | null): NodeOption[] {
  const mine = useQuery({ queryKey: ["me-nodes"], queryFn: async () => (await api("/me/nodes")).items as NodeOption[], enabled: !uid });
  const users = useQuery({ queryKey: ["users"], queryFn: async () => (await api("/admin/users")).items as UserItem[], enabled: admin && !!uid });
  const all = useQuery({ queryKey: ["nodes"], queryFn: async () => (await api("/admin/nodes")).items as NodeItem[], enabled: admin && !!uid });
  if (!uid) return mine.data ?? [];
  const u = users.data?.find((x) => x.id === uid);
  if (!u) return [];
  return (all.data ?? []).filter((n) => n.enabled && (u.all_nodes || (u.node_ids ?? []).includes(n.id))).map((n) => ({ id: n.id, name: n.name, gate_name: n.gate_name }));
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
  const [exitFor, setExitFor] = useState<ProxyType | null>(null);
  const { run, isBusy } = useRowAction<string>({ refetch: async () => onSaved() });
  const disabled = new Set(view.policy.disabled ?? []);
  const toggle = (key: string, on: boolean) => {
    const next = on ? [...disabled].filter((k) => k !== key) : [...disabled, key];
    return run(key, () => api(base + "/proxy", "PUT", { ...view.policy, disabled: next }), on ? "已放进订阅" : "已从订阅里拿掉");
  };
  return (
    <Card>
      <CardContent className="stack">
        <SectionHeader title="节点类型" description="同一种协议方案的节点归为一类。关掉的类型不放进订阅；加了出口的类型，订阅里换成经出口的版本。" />
        <div className="grid gap-3 lg:grid-cols-2">
          {view.types.map((t) => (
            <div key={t.key} className="stack rounded-[var(--weir-radius)] border border-[var(--weir-line)] p-3" data-disabled={!t.enabled || undefined}>
              <div className="flex items-center justify-between gap-2">
                <strong className="flex min-w-0 items-center gap-2">
                  <span className="truncate">{t.label}</span>
                  <TypeCount type={t} />
                </strong>
                <Switch aria-label={`${t.label} 放进订阅`} checked={t.enabled} disabled={isBusy(t.key)} onCheckedChange={(on) => void toggle(t.key, on)} />
              </div>
              <div className="flex flex-wrap gap-1.5">
                {/* 管理员点节点打开它的详情；普通用户只看状况 */}
                {t.nodes.map((n) => {
                  const meta = n.health === "down" ? healthText.down : n.rate.minutes ? rateText(n.rate.p90_bps) : healthText[n.health];
                  return admin ? (
                    <Chip key={n.id} tone={healthTone[n.health]} meta={meta} title={n.why} onClick={() => navigate(objectHref({ type: "node", id: n.id }))}>{n.name}</Chip>
                  ) : (
                    <ChipLabel key={n.id} tone={healthTone[n.health]} meta={meta} title={n.why}>{n.name}</ChipLabel>
                  );
                })}
              </div>
              <div className="flex flex-wrap items-center justify-between gap-2 text-[12.5px]">
                <span className="min-w-0 text-[var(--weir-ink-2)]">
                  出口：{t.exit ? <ExitSummary exit={t.exit} /> : "直接从 VPS 出去"}
                </span>
                <span className="flex gap-1.5">
                  {view.allow_chain && (
                    <Button size="sm" variant="outline" ripple onClick={() => setExitFor(t)}>
                      {t.exit ? "改出口" : <><Plus />加出口</>}
                    </Button>
                  )}
                  {t.exit && (
                    <Button size="icon-sm" variant="destructive" aria-label={`去掉 ${t.label} 的出口`} disabled={isBusy(t.key)}
                      onClick={async () => (await confirm({ title: `去掉「${t.label}」的出口？`, description: "这类节点恢复直接从 VPS 出去。", confirmText: "去掉", tone: "danger" })) && run(t.key, () => api(`${base}/proxy/types/${encodeURIComponent(t.key)}/exit`, "DELETE"), "已去掉出口")}>
                      <Trash2 />
                    </Button>
                  )}
                </span>
              </div>
              {(t.missing_exit ?? []).length > 0 && (
                <Notice tone="warn">新加的节点还没有经出口：{t.missing_exit!.join("、")}。点「改出口」再保存一次即可。</Notice>
              )}
            </div>
          ))}
        </div>
        {!view.allow_chain && <Notice role="status">{admin ? "这个用户没有开放链式代理，不能给类型加出口；可在用户详情里打开。" : "管理员没有开放链式代理，不能给类型加出口。"}</Notice>}
        {exitFor && <TypeExitDialog base={base} type={exitFor} nodes={nodes} admin={admin && base !== "/me"} close={() => setExitFor(null)} saved={() => { setExitFor(null); onSaved(); }} />}
      </CardContent>
    </Card>
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

function ExitSummary({ exit }: { exit: NonNullable<ProxyType["exit"]> }) {
  if (exit.kind === "source") return <SourceSummary choice={exit.source} />;
  if (exit.kind === "node") return <>经节点 {exit.node_id ? <ObjectLink to={{ type: "node", id: exit.node_id }} /> : ""}</>;
  if (exit.kind === "link") return <>节点链接 {exit.address}:{exit.port}</>;
  return <>{exitKindName[exit.kind]} {exit.address}:{exit.port}</>;
}

// TypeExitDialog：给一个类型的全部节点加同一个出口；有一个节点连不通就整体不保存，逐个列出原因。
function TypeExitDialog({ base, type, nodes, admin, close, saved }: { base: string; type: ProxyType; nodes: NodeOption[]; admin: boolean; close: () => void; saved: () => void }) {
  const e = type.exit;
  const [exit, setExit] = useState<ChainSpec>(
    e ? { kind: e.kind, address: e.address, port: e.port, username: e.username, password: e.password, link: e.link, node_id: e.node_id ?? undefined, ...(e.source ?? {}) } : { kind: "socks5" },
  );
  const [skip, setSkip] = useState(false);
  const save = useFormAction(() => api(`${base}/proxy/types/${encodeURIComponent(type.key)}/exit`, "PUT", { exit, skip_probe: admin && skip }), {
    fields: exitErrorFields(exit.kind),
    onSuccess: saved,
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
    <Dialog open onOpenChange={(o) => !o && !save.busy && close()}>
      <DialogContent size="md">
        <DialogTitle>{e ? "改" : "加"}出口：{type.label}</DialogTitle>
        <DialogDescription>这一类的 {type.nodes.length} 个节点都经这个出口出去；保存前由每个节点所在的 VPS 实测。</DialogDescription>
        <Form busy={save.busy} errors={save.errors} onSubmit={() => void save.submit()}>
          <ChainExitFields value={exit} onChange={setExit} admin={false} sourcePath={base === "/me" ? "/me/chain-sources" : `${base}/chain-sources`} nodes={nodes} nodeHint="出口需要你可用的另一个节点；这个节点自己作入口时直接出去。" />
          {admin && <CheckboxField label="跳过 VPS 实测（仅管理员）" checked={skip} onCheckedChange={setSkip} />}
          {failed && (
            <ul className="stack gap-1 text-[12.5px] text-[var(--weir-bad-ink)]">
              {failed.map((f) => <li key={f.name}><strong>{f.name}</strong>：{f.detail || f.code}</li>)}
            </ul>
          )}
          <DialogFooter error={failed ? undefined : save.error}>
            <Button type="button" variant="outline" disabled={save.busy} onClick={close}>取消</Button>
            <Button type="submit" ripple busy={save.busy} busyText="正在逐个实测…">保存</Button>
          </DialogFooter>
        </Form>
      </DialogContent>
    </Dialog>
  );
}

const targetBase: [string, string][] = [["auto", "自动选择"], ["select", "节点选择（手动）"], ["direct", "直连"], ["reject", "拒绝"]];

// PolicyCard：分流与自动选择。改完点保存，一次提交。
function PolicyCard({ base, view, onSaved }: { base: string; view: ProxyView; onSaved: () => void }) {
  const [rules, setRules] = useState<Record<string, string>>(() => Object.fromEntries(view.categories.map((c) => [c.key, c.target])));
  const [mode, setMode] = useState(view.policy.auto.mode);
  const [top, setTop] = useState(String(view.policy.auto.top));
  const [refresh, setRefresh] = useState(String(view.policy.auto.refresh_minutes));
  // 别处（类型开关、出口）保存后服务端值变了：没有改动时跟着更新
  const dirty = mode !== view.policy.auto.mode || top !== String(view.policy.auto.top) || refresh !== String(view.policy.auto.refresh_minutes) || view.categories.some((c) => rules[c.key] !== c.target);
  useEffect(() => {
    if (dirty) return;
    setRules(Object.fromEntries(view.categories.map((c) => [c.key, c.target])));
  }, [view]); // eslint-disable-line react-hooks/exhaustive-deps
  const targets = [...targetBase, ...view.types.filter((t) => t.enabled).map((t) => [t.key, t.label] as [string, string])];
  const refreshNum = Number(refresh);
  const refreshError = !refresh || refreshNum < view.min_refresh || refreshNum > 1440 ? `最短 ${view.min_refresh} 分钟、最长 1440 分钟` : "";
  const topError = mode === "ranked" && (!(Number(top) >= 1) || Number(top) > 20) ? "1–20 个" : "";
  const save = useFormAction(() => api(base + "/proxy", "PUT", { disabled: view.policy.disabled ?? [], rules, auto: { mode, top: Number(top), refresh_minutes: refreshNum } }), {
    ok: "已保存，客户端下次更新订阅时生效",
    fields: { invalid_refresh: "refresh", invalid_top: "top", invalid_rule_target: "rules" },
    onSuccess: onSaved,
  });
  useConsoleContext({
    form: [
      ...view.categories.map((c) => field("rule." + c.key, c.label, targets.find(([k]) => k === rules[c.key])?.[1] ?? rules[c.key])),
      field("auto.mode", "自动选择", mode === "ranked" ? "按速率排序" : "客户端测延迟"), field("auto.top", "进自动选择的节点数", top), field("auto.refresh", "节点列表刷新（分钟）", refresh),
    ],
  });
  return (
    <Card>
      <CardContent>
        <Form busy={save.busy} errors={save.errors} onSubmit={() => void save.submit()}>
          <SectionHeader title="分流" description="按网站类别走哪一组。规则写进订阅，由客户端执行；只给节点列表的客户端（v2rayN 等）用不上分流。" />
          <div className="grid gap-x-4 gap-y-3 sm:grid-cols-2" data-field="rules">
            {view.categories.map((c) => (
              <Field key={c.key} label={c.label} hint={c.hint}>
                <Select value={rules[c.key]} onValueChange={(v) => setRules({ ...rules, [c.key]: String(v) })}>
                  <SelectTrigger aria-label={c.label + " 走哪一组"}>
                    <SelectValue>{targets.find(([k]) => k === rules[c.key])?.[1] ?? "节点选择（手动）"}</SelectValue>
                  </SelectTrigger>
                  <SelectContent>
                    {targets.filter(([k]) => !(c.key === "final" && k === "reject")).map(([k, label]) => <SelectItem key={k} value={k}>{label}</SelectItem>)}
                  </SelectContent>
                </Select>
              </Field>
            ))}
          </div>
          <SectionHeader className="mt-2" title="自动选择" description="「自动选择」组里放哪些节点、客户端多久刷新一次节点列表。Clash 系客户端按这个间隔单独刷新节点，分组与规则不变。" />
          <RadioGroup value={mode} onValueChange={(v) => setMode(v as typeof mode)} aria-label="自动选择方式" className="grid gap-2 sm:grid-cols-2">
            {([
              ["latency", "客户端测延迟", "全部可用的节点都放进去，客户端每 5 分钟测一次延迟，用最快的。"],
              ["ranked", "按速率排序", "按各节点自己统计的下载速率（近 24 小时第 90 百分位）排，前几个按速率，留 1 个轮换位给还没有速率数据的节点；排名有缓冲，不会因为一点差别来回换。"],
            ] as const).map(([v, label, hint]) => (
              <ChoiceCard key={v} selected={mode === v} render={<label />} className="cursor-pointer">
                <span className="flex items-center gap-2"><Radio value={v} /><strong>{label}</strong></span>
                <small className="text-[var(--weir-ink-3)]">{hint}</small>
              </ChoiceCard>
            ))}
          </RadioGroup>
          <div className="grid gap-3 sm:grid-cols-2">
            {mode === "ranked" && (
              <Field label="放进自动选择的节点数" name="top" required hint="大于 1 时其中 1 个是轮换位。" customError={topError}>
                <Input inputMode="numeric" value={top} onChange={(e) => setTop(e.target.value.replace(/\D/g, ""))} />
              </Field>
            )}
            <Field label="节点列表刷新间隔（分钟）" name="refresh" required hint={`最短 ${view.min_refresh} 分钟；整份配置按小时更新。`} customError={refreshError}>
              <Input inputMode="numeric" value={refresh} onChange={(e) => setRefresh(e.target.value.replace(/\D/g, ""))} />
            </Field>
          </div>
          <div className="flex flex-wrap items-center gap-3">
            <Button type="submit" ripple disabled={!dirty || !!refreshError || !!topError} busy={save.busy} busyText="正在保存…"><Route />保存分流与自动选择</Button>
            {save.error && <small className="text-[var(--weir-bad-ink)]">{save.error}</small>}
          </div>
        </Form>
      </CardContent>
    </Card>
  );
}

// RankingCard：此刻的排序（与客户端下次拉取时一致）。速率来自各节点自己统计的流量，没有数据写「未知」。
function RankingCard({ view }: { view: ProxyView }) {
  const ranked = view.policy.auto.mode === "ranked";
  return (
    <Card>
      <CardContent className="stack">
        <SectionHeader
          title="节点状况与速率"
          description={ranked
            ? `按下载速率排：近 24 小时有流量的分钟取第 90 百分位，你自己没用过的节点用全部用户的速率补上。放 ${view.policy.auto.top} 个进「自动选择」${view.policy.auto.top > 1 ? "，其中 1 个轮流给还没有速率数据的节点" : ""}。`
            : "全部可用的节点都在「自动选择」里，由客户端测延迟。速率来自各节点自己统计的流量，只作参考。"}
        />
        {view.ranking.length === 0 ? (
          <State empty="订阅里没有节点" detail="类型都关掉了，或节点都不可用。" />
        ) : (
          <div className="overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="w-14">名次</TableHead>
                  <TableHead>节点</TableHead>
                  <TableHead>类型</TableHead>
                  <TableHead>状况</TableHead>
                  <TableHead className="text-right">下载速率</TableHead>
                  <TableHead className="text-right">有流量的分钟</TableHead>
                  <TableHead>自动选择</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {view.ranking.map((r) => (
                  <TableRow key={`${r.node_id}-${r.type}`}>
                    <TableCell className="tabular-nums">{r.rank || "—"}</TableCell>
                    <TableCell><ObjectLink to={{ type: "node", id: r.node_id, name: r.name }} /></TableCell>
                    <TableCell>{r.type}</TableCell>
                    <TableCell>
                      <Badge variant="soft" tone={r.health === "ok" ? "ok" : r.health === "down" ? "bad" : "neutral"}>{healthText[r.health]}</Badge>
                      {r.why && <small className="block text-[var(--weir-ink-3)]">{r.why}</small>}
                    </TableCell>
                    <TableCell className="text-right tabular-nums">
                      {r.rate.minutes ? rateText(r.rate.p90_bps) : "未知"}
                      {r.rate.from === "all" && <small className="block text-[var(--weir-ink-3)]">全部用户</small>}
                    </TableCell>
                    <TableCell className="text-right tabular-nums">{r.rate.minutes || "—"}</TableCell>
                    <TableCell>{r.exploring ? <Badge variant="soft" tone="info">轮换中</Badge> : r.auto ? "在" : <span className="text-[var(--weir-ink-3)]">不在</span>}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
      </CardContent>
    </Card>
  );
}

