// 订阅链接与自建链式代理：管理员（按用户）与用户本人共用。base 为 "/admin/users/<id>" 或 "/me"。
// 每条链接一张卡片，右上角是「复制」与展开更多操作的三角；生效时间（开始、结束都可不填）与拉取记录都在这里。
import { useState, type ReactNode } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { field, useConsoleContext } from "../lib/console-context";
import { dataObject } from "./object-link";
import {
  Badge,
  Button,
  CheckboxField,
  CodeBlock,
  Collapsible,
  CopyButton,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogTitle,
  Field,
  Form,
  Input,
  Notice,
  SectionHeader,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
  SkeletonList,
  SplitButton,
  Switch,
  SwitchField,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  useToast,
  useConfirm,
  SecretText,
} from "@weir/ui";
import { CirclePause, CirclePlay, ExternalLink, History, Pencil, Plus, QrCode, RefreshCw, KeyRound, Trash2 } from "lucide-react";
import { QRCodeSVG } from "qrcode.react";
import { api } from "../api";
import { useFormAction, useRowAction } from "../lib/action";
import { bytes, date } from "../lib/models";
import { fromLocalInput, localInput, type ChainItem, type ChainSpec, type LinkItem, type LinkState, type LinkStats, type Pull, type SourceResolved } from "../lib/core";
import { ChainExitFields, exitErrorFields, exitKindName, probeText } from "./chain-exit";
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
  return useQuery({
    queryKey: ["chains", base],
    queryFn: async () => (await api(base + "/chains")) as { items: ChainItem[]; allow_chain: boolean },
  });
}

export const stateText: Record<LinkState, [string, "ok" | "warn" | "bad"]> = {
  active: ["生效中", "ok"],
  not_started: ["未开始", "warn"],
  expired: ["已结束", "bad"],
};

// windowText 说明链接什么时候能用；结束时间被套餐到期提前时写明。
export function windowText(l: Pick<LinkItem, "starts_at" | "ends_at" | "effective_ends_at">) {
  const end = l.effective_ends_at ?? l.ends_at;
  const byPlan = !!l.effective_ends_at && l.effective_ends_at !== l.ends_at;
  if (!l.starts_at && !end) return "一直可用";
  const from = l.starts_at ? date(l.starts_at) + " 起" : "现在起";
  const to = end ? "到 " + date(end) + (byPlan ? "（套餐到期）" : "") : "不限结束";
  return `${from}，${to}`;
}

export function LinkPanel({
  base,
  nodes,
  showCredentials,
  inlineQr,
  planEnds,
  stats,
  title = "订阅链接",
  description = "每条链接凭据独立、共用同一份额度；最多 10 条。",
}: {
  base: string;
  nodes: NodeOption[];
  showCredentials?: boolean;
  // inlineQr：刚建好的用户直接在链接下显示二维码，不用再点开。
  inlineQr?: boolean;
  // planEnds：用户的到期时间，链接的结束时间不能晚于它
  planEnds?: string | null;
  // stats：订阅管理页传入的拉取统计（按链接编号）
  stats?: Map<number, LinkStats>;
  title?: ReactNode;
  description?: ReactNode;
}) {
  const toast = useToast();
  const client = useQueryClient();
  const links = useLinks(base);
  const chains = useChains(base);
  const [edit, setEdit] = useState<LinkItem | "new" | null>(null);
  // 链接变了，订阅管理页上方的统计（生效中的链接数等）跟着刷新
  const refetch = () => {
    void links.refetch();
    void client.invalidateQueries({ queryKey: ["link-stats"] });
  };
  return (
    <section className="stack">
      <SectionHeader
        title={title}
        description={description}
        actions={
          <Button size="sm" ripple disabled={(links.data?.length ?? 0) >= 10} onClick={() => setEdit("new")}>
            <Plus />
            新链接
          </Button>
        }
      />
      {links.isPending || links.error ? (
        <State
          loading={links.isPending}
          error={links.error}
          onRetry={() => links.refetch()}
          skeleton={<SkeletonList rows={2} lines={2} className="stack" rowClassName="rounded-[var(--weir-radius)] border border-[var(--weir-line)] p-3" />}
        />
      ) : links.data.length === 0 ? (
        <State empty="还没有链接" />
      ) : (
        links.data.map((l) => (
          <LinkCard key={l.id} base={base} link={l} stats={stats?.get(l.id)} showCredentials={showCredentials} inlineQr={inlineQr} onEdit={() => setEdit(l)} refetch={refetch} />
        ))
      )}
      {edit && (
        <LinkForm
          base={base}
          value={edit}
          nodes={nodes}
          chains={chains.data?.items ?? []}
          planEnds={planEnds}
          close={() => setEdit(null)}
          saved={() => {
            setEdit(null);
            toast("链接已保存", { tone: "ok" });
            refetch();
          }}
        />
      )}
    </section>
  );
}

// LinkCard：一条链接。右上角「复制」是最常用的操作，三角里是其余操作；危险操作都要再确认。
function LinkCard({ base, link: l, stats, showCredentials, inlineQr, onEdit, refetch }: {
  base: string;
  link: LinkItem;
  stats?: LinkStats;
  showCredentials?: boolean;
  inlineQr?: boolean;
  onEdit: () => void;
  refetch: () => void;
}) {
  const confirm = useConfirm();
  const toast = useToast();
  const [qr, setQr] = useState(false);
  const [pulls, setPulls] = useState(false);
  const { run: act, isBusy } = useRowAction<number>({ refetch: async () => refetch() });
  const busy = isBusy(l.id);
  const [stateLabel, stateTone] = stateText[l.state ?? "active"];
  const rotate = async (credentials: boolean) => {
    const ok = await confirm(
      credentials
        ? { title: "更换该链接在所有节点上的凭据？", description: "已导入的客户端需要重新更新订阅。", confirmText: "更换", tone: "danger" }
        : { title: "更换订阅地址？", description: "旧地址立即失效，已导入的客户端节点仍可用。", confirmText: "更换", tone: "danger" },
    );
    if (ok) act(l.id, () => api(`${base}/links/${l.id}/rotate`, "POST", credentials ? { credentials: true } : {}), credentials ? "凭据已更换" : "订阅地址已更换");
  };
  return (
    <div {...dataObject({ type: "link", id: l.id })} className="stack rounded-[var(--weir-radius)] border border-[var(--weir-line)] p-3">
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <strong className="flex flex-wrap items-center gap-1.5">
            {l.name || "未命名"}
            {!l.enabled ? <Badge variant="soft" tone="warn">已停用</Badge> : <Badge variant="soft" tone={stateTone}>{stateLabel}</Badge>}
          </strong>
          <small className="block text-[var(--weir-ink-3)]">
            {windowText(l)} · {l.all_nodes ? "全部节点" : "部分节点"} · {l.nodes} 个节点 · 已用 {bytes(l.used)}
          </small>
          <small className="block text-[var(--weir-ink-3)]">
            {stats
              ? <>近 24 小时拉取 <span className="tabular-nums">{stats.pulls_24h}</span> 次 · 最近 {stats.last_pull ? `${date(stats.last_pull.at)}（${stats.last_pull.client || "未识别的客户端"}）` : "尚无记录"}</>
              : <>最近获取 {date(l.last_used_at)}</>}
          </small>
        </div>
        <SplitButton
          className="shrink-0"
          main={<CopyButton size="sm" variant="outline" ripple value={l.url} onError={() => toast("复制失败", { description: "请在下方地址框里手动选择复制。", tone: "warn" })}>复制</CopyButton>}
          disabled={busy}
          items={[
            { label: "二维码", icon: <QrCode />, onSelect: () => setQr(true) },
            { label: "一键导入页", icon: <ExternalLink />, onSelect: () => window.open(l.url, "_blank", "noopener,noreferrer") },
            { label: "编辑与生效时间", icon: <Pencil />, onSelect: onEdit },
            { label: "拉取记录", icon: <History />, onSelect: () => setPulls(true) },
            "separator",
            l.enabled
              ? { label: "停用", icon: <CirclePause />, onSelect: () => act(l.id, () => api(`${base}/links/${l.id}`, "PATCH", { enabled: false }), "链接已停用") }
              : { label: "启用", icon: <CirclePlay />, onSelect: () => act(l.id, () => api(`${base}/links/${l.id}`, "PATCH", { enabled: true }), "链接已启用") },
            { label: "换地址", icon: <RefreshCw />, onSelect: () => void rotate(false) },
            { label: "换凭据", icon: <KeyRound />, onSelect: () => void rotate(true) },
            { label: "删除", icon: <Trash2 />, danger: true, onSelect: async () => (await confirm({ title: `删除链接「${l.name || "未命名"}」？`, confirmText: "删除", tone: "danger" })) && act(l.id, () => api(`${base}/links/${l.id}`, "DELETE"), "链接已删除") },
          ]}
        />
      </div>
      <Input readOnly className="font-mono text-xs" aria-label="订阅地址" value={l.url} onFocus={(e) => e.currentTarget.select()} />
      {inlineQr && l.enabled && (
        <div className="qr-code mx-auto">
          <QRCodeSVG value={l.url} size={180} title={(l.name || "订阅链接") + " 二维码"} />
        </div>
      )}
      {showCredentials && (l.entries?.length ?? 0) > 0 && (
        <Collapsible title={`节点与凭据原文（${l.entries!.length}）`}>
          <ul className="stack">
            {l.entries!.map((e) => (
              <li key={`${e.node_id}-${e.chain_id ?? 0}`} className="stack">
                <small>
                  <strong>{e.name}</strong>
                  {e.chain_id ? "（链式代理）" : ""} · {e.host}:{e.port}
                  {e.uuid ? ` · UUID ${e.uuid}` : ""}
                  {e.password && <> · 密码 <SecretText value={e.password} /></>}
                </small>
                {e.uri ? <CodeBlock className="block break-all text-xs">{e.uri}</CodeBlock> : <small className="text-[var(--weir-bad-ink)]">{e.error}</small>}
              </li>
            ))}
          </ul>
        </Collapsible>
      )}
      <Dialog open={qr} onOpenChange={setQr}>
        <DialogContent size="sm">
          <DialogTitle>扫码导入</DialogTitle>
          <DialogDescription>{l.name || "订阅链接"}</DialogDescription>
          <div className="qr-code mx-auto">
            <QRCodeSVG value={l.url} size={200} title="订阅二维码" />
          </div>
        </DialogContent>
      </Dialog>
      {pulls && <PullsDialog base={base} link={l} close={() => setPulls(false)} />}
    </div>
  );
}

const pullStatus: Record<Pull["status"], [string, "ok" | "warn" | "bad"]> = {
  ok: ["正常", "ok"],
  not_started: ["未到开始时间", "warn"],
  expired: ["已过结束时间", "warn"],
  failed: ["生成失败", "bad"],
};

// PullsDialog：最近的拉取记录（最多 200 条、保留 30 天）。
function PullsDialog({ base, link: l, close }: { base: string; link: LinkItem; close: () => void }) {
  const q = useQuery({ queryKey: ["pulls", base, l.id], queryFn: async () => (await api(`${base}/links/${l.id}/pulls`)).items as Pull[] });
  useConsoleContext({ dialog: { title: "拉取记录" }, object: { type: "link", name: l.name || "未命名", id: l.id } });
  return (
    <Dialog open onOpenChange={(o) => !o && close()}>
      <DialogContent size="xl">
        <DialogTitle>拉取记录：{l.name || "未命名"}</DialogTitle>
        <DialogDescription>每次客户端更新订阅记一条，保留 30 天。来源地址只有你和管理员能看到。</DialogDescription>
        {q.isPending || q.error ? (
          <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} />
        ) : q.data.length === 0 ? (
          <State empty="还没有拉取记录" detail="把订阅地址导入客户端并更新一次后，这里会出现记录。" />
        ) : (
          <div className="overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>时间</TableHead>
                  <TableHead>客户端</TableHead>
                  <TableHead>格式</TableHead>
                  <TableHead>来源</TableHead>
                  <TableHead>结果</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {q.data.map((p, i) => {
                  const [text, tone] = pullStatus[p.status] ?? [p.status, "warn"];
                  return (
                    <TableRow key={i}>
                      <TableCell className="whitespace-nowrap tabular-nums">{date(p.at)}</TableCell>
                      <TableCell title={p.user_agent}>{p.client || <span className="text-[var(--weir-ink-3)]">未识别</span>}</TableCell>
                      <TableCell>{p.target}</TableCell>
                      <TableCell>
                        <span className="font-mono text-xs">{p.ip}</span>
                        <small className="block text-[var(--weir-ink-3)]">{p.place ? `${p.place}${p.org ? " · " + p.org : ""}` : "位置未知"}</small>
                      </TableCell>
                      <TableCell><Badge variant="soft" tone={tone}>{text}</Badge></TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}

function LinkForm({
  base,
  value,
  nodes,
  chains,
  planEnds,
  close,
  saved,
}: {
  base: string;
  value: LinkItem | "new";
  nodes: NodeOption[];
  chains: ChainItem[];
  planEnds?: string | null;
  close: () => void;
  saved: () => void;
}) {
  const l = value === "new" ? undefined : value;
  const [name, setName] = useState(l?.name ?? "");
  const [all, setAll] = useState(l?.all_nodes ?? true);
  const [picked, setPicked] = useState<number[]>(l?.node_ids ?? []);
  const [pickedChains, setPickedChains] = useState<number[]>(l?.chain_ids ?? []);
  const [enabled, setEnabled] = useState(l?.enabled ?? true);
  const [start, setStart] = useState(localInput(l?.starts_at));
  const [end, setEnd] = useState(localInput(l?.ends_at));
  const save = useFormAction(
    () => {
      const body = { name: name.trim(), all_nodes: all, node_ids: all ? [] : picked, chain_ids: all ? [] : pickedChains, enabled, starts_at: fromLocalInput(start), ends_at: fromLocalInput(end) };
      return l ? api(`${base}/links/${l.id}`, "PATCH", body) : api(base + "/links", "POST", body);
    },
    { fields: { link_window_order: "ends_at", link_window_exceeds_plan: "ends_at", invalid_link_window: "starts_at" }, onSuccess: saved },
  );
  const toggle = (list: number[], id: number, on: boolean) => (on ? [...list, id] : list.filter((x) => x !== id));
  const order = start && end && new Date(start) >= new Date(end) ? "结束时间要晚于开始时间" : "";
  const overPlan = planEnds && end && new Date(end) > new Date(planEnds) ? `不能晚于套餐到期时间 ${date(planEnds)}` : "";
  useConsoleContext({
    dialog: { title: l ? "编辑链接" : "新链接" },
    object: l ? { type: "link", name: l.name || "未命名", id: l.id } : undefined,
    form: [
      field("name", "名称", name), field("all_nodes", "全部节点", all ? "是" : "否"), field("node_ids", "选中的节点", nodes.filter((n) => picked.includes(n.id)).map((n) => n.name).join("、")),
      field("chain_ids", "选中的链式代理", chains.filter((c) => pickedChains.includes(c.id)).map((c) => c.name).join("、")), field("enabled", "启用", enabled ? "是" : "否"),
      field("starts_at", "开始时间", start || "不限"), field("ends_at", "结束时间", end || "不限"),
    ],
  });
  return (
    <Dialog open onOpenChange={(o) => !o && !save.busy && close()}>
      <DialogContent size="md">
        <DialogTitle>{l ? "编辑链接" : "新链接"}</DialogTitle>
        <DialogDescription>默认包含全部可用节点与链式代理；也可只选其中几个，例如给某台设备单独一条。</DialogDescription>
        <Form busy={save.busy} errors={save.errors} onSubmit={() => void save.submit()}>
          <Field label="名称" hint="例如：手机、电脑。">
            <Input maxLength={100} value={name} onChange={(e) => setName(e.target.value)} />
          </Field>
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label="开始时间" name="starts_at" hint="不填为现在就能用。">
              <Input type="datetime-local" value={start} onChange={(e) => setStart(e.target.value)} />
            </Field>
            <Field label="结束时间" name="ends_at" hint={planEnds ? `不填为到套餐到期（${date(planEnds)}）。` : "不填为不限。"} customError={order || overPlan}>
              <Input type="datetime-local" value={end} onChange={(e) => setEnd(e.target.value)} />
            </Field>
          </div>
          <small className="-mt-2 text-[var(--weir-ink-3)]">不在这段时间里拉不到节点，VPS 也停用这条链接的凭据，已导入的客户端随之不能用。</small>
          <SwitchField label="全部节点与链式代理" description="以后新增的自动加入。" checked={all} onCheckedChange={setAll} />
          {!all && (
            <>
              <fieldset className="grid gap-2">
                <legend className="mb-1 text-[12.5px] font-medium text-[var(--weir-ink-2)]">节点</legend>
                {nodes.map((n) => (
                  <CheckboxField
                    key={n.id}
                    label={<>{n.name}{n.gate_name && <small className="text-[var(--weir-ink-3)]">（{n.gate_name}）</small>}</>}
                    checked={picked.includes(n.id)}
                    onCheckedChange={(v) => setPicked(toggle(picked, n.id, v))}
                  />
                ))}
              </fieldset>
              {chains.length > 0 && (
                <fieldset className="grid gap-2">
                  <legend className="mb-1 text-[12.5px] font-medium text-[var(--weir-ink-2)]">链式代理</legend>
                  {chains.map((c) => (
                    <CheckboxField key={c.id} label={c.name} checked={pickedChains.includes(c.id)} onCheckedChange={(v) => setPickedChains(toggle(pickedChains, c.id, v))} />
                  ))}
                </fieldset>
              )}
            </>
          )}
          {l && <SwitchField label="启用" checked={enabled} onCheckedChange={setEnabled} />}
          <DialogFooter error={save.error}>
            <Button type="button" variant="outline" disabled={save.busy} onClick={close}>取消</Button>
            <Button type="submit" disabled={save.busy || !!order || !!overPlan} busy={save.busy} busyText="正在保存…">保存</Button>
          </DialogFooter>
        </Form>
      </DialogContent>
    </Dialog>
  );
}

// ChainPanel：用户自建链式代理 = 一个新节点（入口节点 → 订阅组里的节点、SOCKS5 / HTTP(S) 代理、另一节点或一条分享链接）。
// 出口只允许公网地址，不接受订阅地址；保存前由入口 VPS 实测，记下实际出口 IP。
// SourceSummary：订阅源出口一行说明（单个节点 / 一组，及当前用的节点）。
export function SourceSummary({ choice, resolved }: { choice?: { mode?: string; match?: string } | null; resolved?: SourceResolved }) {
  const names = resolved?.resolved ?? null;
  return (
    <>
      订阅节点 {choice?.mode === "filter" ? `一组「${choice?.match ?? ""}」` : `「${choice?.match ?? ""}」`}
      {names === null ? "（还没有解析到可用节点，暂时丢弃）" : choice?.mode === "filter" ? `（${names.length} 个，内核自动选择与切换）` : ""}
    </>
  );
}

export function ChainPanel({ base, nodes, admin }: { base: string; nodes: NodeOption[]; admin?: boolean }) {
  const confirm = useConfirm();
  const toast = useToast();
  const chains = useChains(base);
  const [open, setOpen] = useState(false);
  const { run: act, isBusy } = useRowAction<number>({ refetch: chains.refetch });
  if (chains.isPending || chains.error)
    return <State loading={chains.isPending} error={chains.error} onRetry={() => chains.refetch()} />;
  const name = (id?: number | null) => nodes.find((n) => n.id === id)?.name ?? "节点";
  return (
    <section className="stack">
      <SectionHeader
        title="链式代理"
        description="经入口节点再从订阅节点、代理或另一节点出去；流量只在入口计量一次。"
        actions={
          chains.data.allow_chain && (
            <Button
              size="sm"
              disabled={chains.data.items.length >= 10 || nodes.length === 0}
              onClick={() => setOpen(true)}
            >
              <Plus />
              新建
            </Button>
          )
        }
      />
      {!chains.data.allow_chain && (
        <Notice role="status">{admin ? "该用户未开放链式代理，可在编辑用户中打开。" : "管理员未开放链式代理。"}</Notice>
      )}
      {chains.data.items.length === 0 ? (
        <State empty="还没有链式代理" />
      ) : (
        <ul className="stack">
          {chains.data.items.map((c) => (
            <li key={c.id} {...dataObject({ type: "chain", id: c.id })} className="flex flex-wrap items-center justify-between gap-2">
              <span className="min-w-0">
                <strong>{c.name}</strong>{" "}
                {!c.enabled && (
                  <Badge variant="soft" tone="warn">
                    已停用
                  </Badge>
                )}
                <small className="block text-[var(--weir-ink-3)]">
                  {c.entry_name ?? name(c.entry_node_id)} →{" "}
                  {c.exit_kind === "socks5" || c.exit_kind === "http" || c.exit_kind === "https" ? (
                    <>
                      {exitKindName[c.exit_kind]} {c.exit_address}:{c.exit_port}
                      {admin && c.exit_username && (
                        <>
                          {" "}（{c.exit_username} / <SecretText value={c.exit_password ?? ""} />）
                        </>
                      )}
                    </>
                  ) : c.exit_kind === "link" ? (
                    <>
                      分享链接 {c.exit_address}:{c.exit_port}
                      {admin && c.exit_link && <> （<SecretText value={c.exit_link} />）</>}
                    </>
                  ) : c.exit_kind === "source" ? (
                    <SourceSummary choice={c.exit_source} resolved={c.source} />
                  ) : (
                    name(c.exit_node_id)
                  )}
                </small>
                <small className="block text-[var(--weir-ink-3)]">{probeText(c.probe)}</small>
              </span>
              <span className="actions">
                <Switch
                  aria-label={"启用 " + c.name}
                  checked={c.enabled}
                  disabled={isBusy(c.id)}
                  onCheckedChange={(v) =>
                    act(c.id, () => api(`${base}/chains/${c.id}`, "PATCH", { enabled: v }), v ? "已启用" : "已停用")
                  }
                />
                <Button
                  size="icon-sm"
                  variant="destructive"
                  aria-label={"删除 " + c.name}
                  disabled={isBusy(c.id)}
                  onClick={async () =>
                    (await confirm({ title: `删除链式代理「${c.name}」？`, confirmText: "删除", tone: "danger" })) &&
                    act(c.id, () => api(`${base}/chains/${c.id}`, "DELETE"), "已删除")
                  }
                >
                  <Trash2 />
                </Button>
              </span>
            </li>
          ))}
        </ul>
      )}
      {open && (
        <ChainForm
          base={base}
          nodes={nodes}
          admin={admin}
          close={() => setOpen(false)}
          saved={() => {
            setOpen(false);
            toast("链式代理已创建", { tone: "ok" });
            void chains.refetch();
          }}
        />
      )}
    </section>
  );
}

function ChainForm({
  base,
  nodes,
  admin,
  close,
  saved,
}: {
  base: string;
  nodes: NodeOption[];
  admin?: boolean;
  close: () => void;
  saved: () => void;
}) {
  const [name, setName] = useState("");
  const [entry, setEntry] = useState(nodes[0]?.id ?? 0);
  // 订阅组里的节点排第一个；分给用户的订阅组为空时默认填代理。
  const groups = useQuery({ queryKey: ["chain-sources", base], queryFn: async () => (await api(base === "/me" ? "/me/chain-sources" : `${base}/chain-sources`)).items as unknown[] });
  const [exit, setExit] = useState<ChainSpec | null>(null);
  const current: ChainSpec = exit ?? (groups.data && groups.data.length === 0 ? { kind: "socks5" } : { kind: "source", mode: "filter" });
  const [skip, setSkip] = useState(false);
  const save = useFormAction(() => api(base + "/chains", "POST", { name: name.trim(), entry_node_id: entry, exit: current, skip_probe: admin && skip }), {
    fields: { entry_not_allowed: "entry", ...exitErrorFields(current.kind) },
    onSuccess: saved,
  });
  useConsoleContext({
    dialog: { title: "新建链式代理" },
    form: [
      field("name", "名称", name), field("entry_node_id", "入口节点", nodes.find((n) => n.id === entry)?.name), field("exit_kind", "出口类型", exitKindName[current.kind]),
      ...(["socks5", "http", "https"].includes(current.kind) ? [field("address", "代理地址", current.address), field("port", "端口", current.port), field("username", "用户名", current.username), field("password", "代理密码", current.password, true)] : []),
      ...(current.kind === "link" ? [field("link", "分享链接", current.link, true)] : []),
    ],
  });
  return (
    <Dialog open onOpenChange={(o) => !o && !save.busy && close()}>
      <DialogContent size="md">
        <DialogTitle>新建链式代理</DialogTitle>
        <DialogDescription>会作为一个新节点出现在订阅里。保存前由入口 VPS 实测出口是否可用。</DialogDescription>
        <Form busy={save.busy} errors={save.errors} onSubmit={() => void save.submit()}>
          <Field label="名称" required>
            <Input required maxLength={100} value={name} onChange={(e) => setName(e.target.value)} />
          </Field>
          <Field label="入口节点" name="entry" required>
            <Select required value={entry ? String(entry) : ""} onValueChange={(v) => setEntry(Number(v))}>
              <SelectTrigger>
                <SelectValue>{nodes.find((n) => n.id === entry)?.name ?? "请选择"}</SelectValue>
              </SelectTrigger>
              <SelectContent>
                {nodes.map((n) => (
                  <SelectItem key={n.id} value={String(n.id)}>
                    {n.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
          <ChainExitFields
            value={current}
            onChange={setExit}
            admin={false}
            sourcePath={base === "/me" ? "/me/chain-sources" : `${base}/chain-sources`}
            nodes={nodes.filter((n) => n.id !== entry)}
            nodeHint="出口需要你可用的另一个节点。"
          />
          {admin && <CheckboxField label="跳过 VPS 实测（仅管理员）" checked={skip} onCheckedChange={setSkip} />}
          <DialogFooter error={save.error}>
            <Button type="button" variant="outline" disabled={save.busy} onClick={close}>
              取消
            </Button>
            <Button type="submit" disabled={save.busy} busy={save.busy} busyText="正在实测并保存…">保存</Button>
          </DialogFooter>
        </Form>
      </DialogContent>
    </Dialog>
  );
}
