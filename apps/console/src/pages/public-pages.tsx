import { useFormGuard } from "../lib/dirty";
// 公开页面：匿名可访问的介绍页（/p/<slug>），展示已分享的订阅组卡片与留言板。
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { useState } from "react";
import { useQuery } from "../lib/query";
import { Badge, Button, Card, CardContent, CopyButton, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Field, Form, FormGrid, Input, Notice, PageHeader, PageState, PhoneFrame, Readout, SectionHeader, Segmented, SkeletonList, Select, SelectContent, SelectItem, SelectTrigger, SelectValue, SwitchField, Textarea, Tabs, TabsList, TabsPanel, TabsTab, useToast, useConfirm } from "@weir/ui";
import { ArrowUpRight, MessageSquare, Plus, Trash2 } from "lucide-react";
import { api } from "../api";
import { useFormAction, useRowAction } from "../lib/action";
import { State } from "../components/feedback";
import { GuestbookManager } from "../components/guestbook";
import type { Profile } from "../lib/subscriptions";
import { dataObject } from "../components/object-link";
import { field, useConsoleContext } from "../lib/console-context";
import { distributionSections } from "../shell/navigation";
import { ClientCatalogEditor } from "../components/client-catalog";
import { AnnouncementEditor } from "../components/global-announcement";

interface PageCard {
  profile_id: number;
  target: string;
  title: string;
}
interface PageData {
  enabled: boolean;
  title: string;
  description: string;
  notice: string;
  notice_title: string;
  notice_type: string;
  notice_dismissible: boolean;
  show_login: boolean;
  theme: string;
  show_share: boolean;
  cards: PageCard[] | null;
}
interface PageItem {
  id: number;
  slug: string;
  data: PageData;
}

const blank: PageData = { enabled: false, title: "", description: "", notice: "", notice_title: "", notice_type: "info", notice_dismissible: true, show_login: true, theme: "violet", show_share: false, cards: [] };
const targets = [["clash", "Clash"], ["singbox", "sing-box"], ["surge", "Surge"], ["loon", "Loon"], ["quanx", "QuanX"], ["egern", "Egern"], ["base64", "Base64"]] as const;

export function PublicPagesPage() {
  const [params, setParams] = useSearchParams();
  const tab = distributionSections.some((s) => s.key === params.get("tab")) ? params.get("tab")! : distributionSections[0].key;
  const confirm = useConfirm();
  const toast = useToast();
  const q = useQuery({ queryKey: ["public-pages"], queryFn: async () => (await api("/admin/public-pages")).items as PageItem[] });
  const [edit, setEdit] = useState<PageItem | "new" | null>(null);
  const [guestbook, setGuestbook] = useState<PageItem | null>(null);
  const [preview, setPreview] = useState<number | null>(null);
  const { run, isBusy } = useRowAction<number>({ refetch: q.refetch });
  async function remove(p: PageItem) {
    if (!(await confirm({ title: `删除页面「${p.data.title}」？`, description: "它的留言会一并删除。", confirmText: "删除", tone: "danger" }))) return;
    await run(p.id, () => api("/admin/public-pages/" + p.id, "DELETE"), "页面已删除", { fail: "删除失败" });
  }
  useConsoleContext({ page: "公开页面", tab: distributionSections.find((s) => s.key === tab)?.label, list: tab === "pages" ? { total: q.data?.length ?? 0, items: (q.data ?? []).slice(0, 20).map((p) => ({ type: "page", name: p.data.title, key: p.slug })) } : undefined });
  return (
    <div className="stack">
      <PageHeader size="data" title="公开页面" description={tab === "pages" && q.data ? `${q.data.filter((p) => p.data.enabled).length} 个已发布 · ${q.data.filter((p) => !p.data.enabled).length} 个草稿 · 7 天访问未知` : "管理匿名介绍页、客户端目录和登录后显示的全局公告。"} actions={tab === "pages" ? <Button onClick={() => setEdit("new")}><Plus />新建公开页</Button> : undefined} />
      <Tabs value={tab} onValueChange={(v) => setParams({ tab: String(v) }, { replace: true })}>
        <TabsList aria-label="公开页面分区">{distributionSections.map((s) => <TabsTab key={s.key} value={s.key}>{s.label}</TabsTab>)}</TabsList>
        <TabsPanel value="pages" className="stack">
      {q.isPending || q.error ? (
        <State
          loading={q.isPending}
          error={q.error}
          onRetry={() => q.refetch()}
          skeleton={<Card><CardContent><SkeletonList rows={3} lines={2} className="stack" rowClassName="flex items-center justify-between gap-2" /></CardContent></Card>}
        />
      ) : q.data.length === 0 ? (
        <State empty="还没有公开页面" detail="公开页面是不用登录就能看的介绍页，可以展示分享的订阅组和留言板。点上方「新建公开页」开始。" />
      ) : (
        <PageBoard pages={q.data} selected={q.data.find((p) => p.id === preview) ?? q.data.find((p) => p.data.enabled) ?? q.data[0]} onSelect={setPreview} onEdit={setEdit} onGuestbook={setGuestbook} onRemove={(p) => void remove(p)} busy={isBusy} />
      )}
        </TabsPanel>
        <TabsPanel value="clients" className="stack"><ClientCatalogEditor /></TabsPanel>
        <TabsPanel value="announcements" className="stack"><AnnouncementEditor /></TabsPanel>
      </Tabs>
      {edit && <PageForm value={edit} close={() => setEdit(null)} saved={() => { setEdit(null); toast("已保存", { tone: "ok" }); void q.refetch(); }} />}
      {guestbook && <GuestbookManager page={{ id: guestbook.id, data: { ...guestbook.data } }} close={() => setGuestbook(null)} />}
    </div>
  );
}

export function PublicPageDetailView({ id, tab }: { id: string; tab: string }) {
  const navigate = useNavigate();
  const confirm = useConfirm();
  const toast = useToast();
  const q = useQuery({ queryKey: ["public-pages"], queryFn: async () => (await api("/admin/public-pages")).items as PageItem[] });
  const [edit, setEdit] = useState(false);
  const [guestbook, setGuestbook] = useState(false);
  const { run, isBusy } = useRowAction<number>({ refetch: q.refetch });
  const item = q.data?.find((p) => p.slug === id || String(p.id) === id);
  if (q.isPending) return <State loading />;
  if (q.error) return <State error={q.error} onRetry={() => void q.refetch()} />;
  if (!item) return <PageState kind="empty" title="没有这个公开页" detail="它可能已被删除，或你没有权限查看。" action={<Link to="/pages">返回公开页面</Link>} />;
  async function remove() {
    if (!(await confirm({ title: `删除页面「${item!.data.title}」？`, description: "它的留言会一并删除。", confirmText: "删除", tone: "danger" }))) return;
    const removed = await run(item!.id, () => api("/admin/public-pages/" + item!.id, "DELETE"), "页面已删除", { fail: "删除失败" });
    if (removed) navigate("/pages");
  }
  const cards = item.data.cards?.length;
  return (
    <section className="stack">
      <div className="flex flex-wrap gap-1.5">
        {item.data.enabled && <Button size="sm" variant="outline" render={<a href={"/p/" + item.slug} target="_blank" rel="noopener noreferrer" />}>打开<ArrowUpRight /></Button>}
        <Button size="sm" variant="outline" onClick={() => setGuestbook(true)}><MessageSquare />留言板</Button>
        <Button size="sm" variant="outline" onClick={() => setEdit(true)}>编辑</Button>
        <Button size="sm" variant="destructive" disabled={isBusy()} busy={isBusy(item.id)} busyText="正在删除" onClick={() => void remove()}><Trash2 />删除</Button>
      </div>
      {tab === "share" ? (
        <div className="grid gap-3 sm:grid-cols-2">
          <Readout label="公开地址" value={item.data.enabled ? `/p/${item.slug}` : null} caption={item.data.enabled ? "已发布" : "未发布时没有公开地址"} />
          <Readout label="允许复制地址与二维码" value={item.data.show_share ? "允许" : "不允许"} />
        </div>
      ) : (
        <div className="grid gap-3 sm:grid-cols-3">
          <Readout label="状态" value={item.data.enabled ? "已发布" : "草稿"} />
          <Readout label="地址" value={item.slug ? `/p/${item.slug}` : null} />
          <Readout label="订阅卡片" value={cards ?? null} />
          <Readout label="标题" value={item.data.title || null} />
          <Readout label="说明" value={item.data.description || null} />
          <Readout label="登录入口" value={item.data.show_login ? "显示" : "不显示"} />
        </div>
      )}
      {edit && <PageForm value={item} close={() => setEdit(false)} saved={() => { setEdit(false); toast("已保存", { tone: "ok" }); void q.refetch(); }} />}
      {guestbook && <GuestbookManager page={{ id: item.id, data: { ...item.data } }} close={() => setGuestbook(false)} />}
    </section>
  );
}

function PageBoard({ pages, selected, onSelect, onEdit, onGuestbook, onRemove, busy }: { pages: PageItem[]; selected: PageItem; onSelect: (id: number) => void; onEdit: (p: PageItem) => void; onGuestbook: (p: PageItem) => void; onRemove: (p: PageItem) => void; busy: (id: number) => boolean }) {
  const [device, setDevice] = useState("phone");
  return (
    <div className="public-page-board" data-device={device}>
      <div className="stack">
        {pages.map((p) => (
          <Card key={p.id} {...dataObject({ type: "page", key: p.slug })} data-selected={p.id === selected.id || undefined}>
            <CardContent className="stack">
              <Button variant="ghost" className="public-page-select" aria-pressed={p.id === selected.id} onClick={() => onSelect(p.id)}>
                <strong>{p.data.title || "未命名"}</strong>
                <Badge variant="soft" tone={p.data.enabled ? "ok" : "neutral"}>{p.data.enabled ? "已发布" : "草稿"}</Badge>
              </Button>
              <small className="text-[var(--weir-ink-3)]">/p/{p.slug} · {p.data.cards?.length ?? 0} 张订阅卡片 · 访问次数未知</small>
              <span className="flex flex-wrap gap-1.5">
                <Button size="sm" variant="outline" onClick={() => onEdit(p)}>编辑</Button>
                {p.data.enabled && <CopyButton size="sm" variant="outline" value={location.origin + "/p/" + p.slug}>复制地址</CopyButton>}
                <Button size="sm" variant="outline" onClick={() => onGuestbook(p)}><MessageSquare />留言板</Button>
                {p.data.enabled && <Button size="sm" variant="outline" render={<a href={"/p/" + p.slug} target="_blank" rel="noopener noreferrer" />}>打开<ArrowUpRight /></Button>}
                <Button size="icon-sm" variant="destructive" aria-label={"删除 " + p.data.title} disabled={busy(p.id)} busy={busy(p.id)} busyText="正在删除" onClick={() => onRemove(p)}><Trash2 /></Button>
              </span>
            </CardContent>
          </Card>
        ))}
      </div>
      <div className="stack">
        <SectionPreview device={device} setDevice={setDevice} page={selected} />
      </div>
    </div>
  );
}

function SectionPreview({ page, device, setDevice }: { page: PageItem; device: string; setDevice: (v: string) => void }) {
  return (
    <>
      <SectionHeader title="实时预览" description="保存后，已发布的页面会显示在这里。" />
      <Segmented aria-label="预览宽度" value={device} onValueChange={setDevice} options={[["phone", "手机"], ["desktop", "电脑"]] as const} />
      <PhoneFrame
        title={page.data.title || "公开页预览"}
        device={device === "desktop" ? "desktop" : "phone"}
        src={page.data.enabled ? "/p/" + page.slug : null}
        fallback={<div className="stack"><strong>{page.data.title || "未命名"}</strong><p>{page.data.description || "还没有介绍"}</p><small>草稿没有公开地址，这里只显示已保存的标题和介绍。</small></div>}
      />
    </>
  );
}

const flags = [
  ["enabled", "发布页面"],
  ["show_login", "显示登录入口"],
  ["show_share", "允许复制页面地址与二维码"],
  ["notice_dismissible", "允许关闭公告"],
] as const;

function PageForm({ value, close, saved }: { value: PageItem | "new"; close: () => void; saved: () => void }) {
  const p = value === "new" ? undefined : value;
  const profiles = useQuery({ queryKey: ["subscription-profiles"], queryFn: async () => (await api("/admin/subscriptions/profiles")).items as Profile[] });
  const [slug, setSlug] = useState(p?.slug ?? "");
  const [d, setD] = useState<PageData>({ ...blank, ...p?.data, cards: p?.data.cards ?? [] });
  const set = (patch: Partial<PageData>) => setD({ ...d, ...patch });
  const cards = d.cards ?? [];
  const setCard = (i: number, patch: Partial<PageCard>) => set({ cards: cards.map((c, n) => (n === i ? { ...c, ...patch } : c)) });
  const guard = useFormGuard({ slug, d }, close);
  const save = useFormAction(() => (p ? api("/admin/public-pages/" + p.id, "PATCH", { data: d }) : api("/admin/public-pages", "POST", { slug: slug.trim(), data: d })), {
    fields: { invalid_slug: "slug", slug_taken: "slug" },
    onSuccess: () => { guard.commit(); saved(); },
  });
  const shared = (profiles.data ?? []).filter((x) => x.shared);
  useConsoleContext({ dialog: { title: p ? "编辑页面" : "新建页面" }, object: p ? { type: "page", name: p.data.title, key: p.slug } : undefined, form: [field("slug", "地址名", slug), field("title", "标题", d.title), field("enabled", "发布", d.enabled ? "是" : "否"), field("cards", "订阅卡片", String(d.cards?.length ?? 0))] });
  return (
    <Dialog open onOpenChange={(o) => !o && !save.busy && void guard.close()}>
      <DialogContent size="lg">
        <DialogTitle>{p ? "编辑页面" : "新建页面"}</DialogTitle>
        <DialogDescription>页面任何人都能访问，请不要填写私人信息。订阅卡片只显示仍在分享期内的订阅组，下载时再次核对。</DialogDescription>
        <Form busy={save.busy} errors={save.errors} onSubmit={() => void save.submit()}>
          <FormGrid>
            <Field label="地址" name="slug" hint="小写字母、数字与 -，创建后不能修改。" customError={/^[a-z0-9-]*$/.test(slug) ? "" : "地址只能使用小写字母、数字与 -"}>
              <Input required disabled={!!p} value={slug} onChange={(e) => setSlug(e.target.value.toLowerCase())} />
            </Field>
            <Field label="标题"><Input required maxLength={200} value={d.title} onChange={(e) => set({ title: e.target.value })} /></Field>
          </FormGrid>
          <Field label="介绍"><Textarea rows={3} value={d.description} onChange={(e) => set({ description: e.target.value })} /></Field>
          <FormGrid>
            <Field label="公告标题（可选）"><Input maxLength={200} value={d.notice_title} onChange={(e) => set({ notice_title: e.target.value })} /></Field>
            <Field label="公告类型"><Segmented aria-label="公告类型" value={d.notice_type || "info"} onValueChange={(notice_type) => set({ notice_type })} options={[["info", "通知"], ["success", "成功"], ["warning", "提醒"], ["error", "警告"]] as const} /></Field>
          </FormGrid>
          <Field label="公告正文（纯文本，可选）"><Textarea rows={2} value={d.notice} onChange={(e) => set({ notice: e.target.value })} /></Field>
          <Field label="主题"><Segmented aria-label="页面主题" value={d.theme || "violet"} onValueChange={(theme) => set({ theme })} options={[["violet", "紫罗兰"], ["ocean", "海蓝"], ["forest", "森林"]] as const} /></Field>
          <FormGrid>
            {flags.map(([k, label]) => (
              <SwitchField key={k} label={label} checked={d[k]} onCheckedChange={(v) => set({ [k]: v })} />
            ))}
          </FormGrid>
          <fieldset className="resource-selector grid gap-3">
            <legend>订阅卡片（最多 24 张）</legend>
            {cards.map((c, i) => (
              <div key={i} className="grid items-start gap-x-2 sm:grid-cols-[1fr_auto_1fr_auto]">
                <Field label="订阅组">
                  <Select required value={c.profile_id ? String(c.profile_id) : ""} onValueChange={(v) => setCard(i, { profile_id: Number(v) })}>
                    <SelectTrigger><SelectValue>{(profiles.data ?? []).find((x) => x.id === c.profile_id)?.name ?? "请选择"}</SelectValue></SelectTrigger>
                    <SelectContent>{(profiles.data ?? []).map((x) => <SelectItem key={x.id} value={String(x.id)}>{x.name}{x.shared ? "" : "（未分享，不显示）"}</SelectItem>)}</SelectContent>
                  </Select>
                </Field>
                <Field label="格式">
                  <Select value={c.target} onValueChange={(v) => setCard(i, { target: String(v) })}>
                    <SelectTrigger><SelectValue>{targets.find(([k]) => k === c.target)?.[1] ?? c.target}</SelectValue></SelectTrigger>
                    <SelectContent>{targets.map(([k, l]) => <SelectItem key={k} value={k}>{l}</SelectItem>)}</SelectContent>
                  </Select>
                </Field>
                <Field label="卡片标题（可选）"><Input value={c.title} onChange={(e) => setCard(i, { title: e.target.value })} /></Field>
                <Button type="button" size="icon-sm" variant="destructive" className="mt-[26px]" aria-label={`移除第 ${i + 1} 张卡片`} onClick={() => set({ cards: cards.filter((_, n) => n !== i) })}><Trash2 /></Button>
              </div>
            ))}
            <div className="actions">
              {profiles.error && <State error={profiles.error} onRetry={() => profiles.refetch()} />}
              <Button type="button" size="sm" variant="outline" disabled={cards.length >= 24 || !profiles.data || profiles.data.length === 0} onClick={() => set({ cards: [...cards, { profile_id: (shared[0] ?? profiles.data?.[0])?.id ?? 0, target: "clash", title: "" }] })}><Plus />添加卡片</Button>
            </div>
            {(profiles.data ?? []).length > 0 && shared.length === 0 && <small className="text-[var(--weir-ink-3)]">当前没有已分享的订阅组；先分享订阅组，卡片才会显示：<Link className="weir-hit relative text-[var(--weir-accent-text)] underline-offset-4 transition-opacity hover:underline active:opacity-60" to="/subscriptions?tab=profiles" target="_blank">在新标签页打开「订阅源 → 订阅组」</Link>。</small>}
          </fieldset>
          <DialogFooter error={save.error}>
            <Button type="button" variant="outline" disabled={save.busy} onClick={() => { if (!save.busy) void guard.close(); }}>取消</Button>
            <Button type="submit" disabled={save.busy} busy={save.busy} busyText="正在保存…">保存</Button>
          </DialogFooter>
        </Form>
      </DialogContent>
    </Dialog>
  );
}
