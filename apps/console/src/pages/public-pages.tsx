import { useFormGuard } from "../lib/dirty";
// 公开页面：匿名可访问的介绍页（/p/<slug>），展示已分享的订阅组卡片与留言板。
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { useState } from "react";
import { useQuery } from "../lib/query";
import { Badge, Button, Card, CardContent, CopyButton, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Field, Form, FormGrid, FormSection, Input, Notice, PageHeader, PageState, PhoneFrame, Readout, SectionHeader, Segmented, SkeletonList, Select, SelectContent, SelectItem, SelectTrigger, SelectValue, SwitchField, SwitchGroup, Textarea, Tabs, TabsList, TabsPanel, TabsTab, useToast, useConfirm } from "@weir/ui";
import { ArrowUpRight, MessageSquare, Plus, Trash2 } from "lucide-react";
import { api } from "../api";
import { useFormAction, useRowAction } from "../lib/action";
import { State } from "../components/feedback";
import { GuestbookManager } from "../components/guestbook";
import type { Profile } from "../lib/subscriptions";
import { dataObject, useStringDialogParam } from "../components/object-link";
import { field, useConsoleContext } from "../lib/console-context";
import { distributionSections } from "../shell/navigation";
import { ClientCatalogEditor } from "../components/client-catalog";
import { AnnouncementEditor } from "../components/global-announcement";
import { SettingsDialogProvider } from "../components/system-settings";

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
  const detail = useStringDialogParam("page");
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
        <TabsPanel value="clients" className="stack"><SettingsDialogProvider value={true}><ClientCatalogEditor /></SettingsDialogProvider></TabsPanel>
        <TabsPanel value="announcements" className="stack"><SettingsDialogProvider value={true}><AnnouncementEditor /></SettingsDialogProvider></TabsPanel>
      </Tabs>
      {detail.value && <Dialog open onOpenChange={o => !o && detail.close()}><DialogContent size="xl"><DialogTitle>公开页面</DialogTitle><DialogDescription>管理页面内容、分享与留言。</DialogDescription><Segmented aria-label="公开页详情分区" value={params.get("detail_tab") || "content"} onValueChange={value => setParams(p => { p.set("detail_tab", value); return p; }, { replace: true })} options={[["content", "内容"], ["share", "分享"]]} /><PublicPageDetailView key={detail.value} id={detail.value} tab={params.get("detail_tab") || "content"} /></DialogContent></Dialog>}
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
  useConsoleContext({ object: item ? { type: "page", key: item.slug, name: item.data.title } : undefined, dialog: { title: "公开页详情" } });
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
        {pages.map((p) => {
          const active = p.id === selected.id;
          const url = `${location.origin}/p/${p.slug}`;
          return (
            <Card key={p.id} {...dataObject({ type: "page", key: p.slug })} data-selected={active || undefined} className={active ? "border-[var(--weir-accent)]" : undefined}>
              <CardContent className="space-y-3">
                <button
                  type="button"
                  aria-pressed={active}
                  onClick={() => onSelect(p.id)}
                  className="weir-press flex min-h-[var(--weir-tap)] touch:min-h-[var(--weir-tap-touch)] w-full cursor-pointer items-center justify-between gap-2 border-0 bg-transparent p-0 text-left text-inherit"
                >
                  <strong className="truncate text-headline font-semibold">{p.data.title || "未命名页面"}</strong>
                  <Badge variant="status" tone={p.data.enabled ? "ok" : "neutral"}>{p.data.enabled ? "已发布" : "草稿"}</Badge>
                </button>
                <div className="truncate rounded-[var(--weir-radius-9)] border border-[var(--weir-line-soft)] bg-[var(--weir-well)] px-3 py-2 font-mono text-fine text-[var(--weir-ink-2)]">
                  {location.host}/p/{p.slug}
                </div>
                <small className="block text-[var(--weir-ink-3)]">
                  {p.data.cards?.length ?? 0} 张订阅卡片 · {p.data.show_login ? "显示登录入口" : "纯公开展示"}
                </small>
                <div className="flex flex-wrap items-center justify-between gap-1.5 pt-0.5">
                  <span className="flex flex-wrap items-center gap-1">
                    <Button size="sm" variant="outline" onClick={() => onEdit(p)}>编辑</Button>
                    {p.data.enabled && <CopyButton size="sm" variant="outline" value={url}>复制地址</CopyButton>}
                    <Button size="sm" variant="ghost" onClick={() => onGuestbook(p)}><MessageSquare />留言板</Button>
                    {p.data.enabled && <Button size="sm" variant="ghost" render={<a href={"/p/" + p.slug} target="_blank" rel="noopener noreferrer" />}>打开<ArrowUpRight /></Button>}
                  </span>
                  <Button size="icon-sm" variant="ghost" aria-label={"删除 " + p.data.title} disabled={busy(p.id)} busy={busy(p.id)} onClick={() => onRemove(p)}><Trash2 /></Button>
                </div>
              </CardContent>
            </Card>
          );
        })}
      </div>
      <div className="stack">
        <SectionPreview device={device} setDevice={setDevice} page={selected} />
      </div>
    </div>
  );
}

const themeNames: Record<string, string> = { violet: "紫罗兰", ocean: "海蓝", forest: "森林" };

function SectionPreview({ page, device, setDevice }: { page: PageItem; device: string; setDevice: (v: string) => void }) {
  const cards = page.data.cards ?? [];
  return (
    <>
      <SectionHeader flush title="实时预览" description={page.data.enabled ? "访客实际打开页面时的画面" : "草稿未对外公开，以下为排版预览"} actions={<Segmented size="sm" aria-label="预览宽度" value={device} onValueChange={setDevice} options={[["phone", "手机"], ["desktop", "电脑"]] as const} />} />
      <PhoneFrame
        title={page.data.title || "公开页预览"}
        device={device === "desktop" ? "desktop" : "phone"}
        src={page.data.enabled ? "/p/" + page.slug : null}
        fallback={
          <div className="space-y-4 p-2">
            <div className="flex items-center justify-between gap-2">
              <Badge variant="soft" tone="neutral">草稿 · {themeNames[page.data.theme] ?? "默认主题"}</Badge>
              {page.data.show_login && <small className="text-[var(--weir-ink-3)]">登录入口</small>}
            </div>
            {page.data.notice && (
              <div className="rounded-[var(--weir-radius)] border border-[var(--weir-line-soft)] bg-[var(--weir-well)] p-3 text-meta">
                {page.data.notice_title && <strong className="mb-1 block">{page.data.notice_title}</strong>}
                <span className="text-[var(--weir-ink-2)]">{page.data.notice}</span>
              </div>
            )}
            <div className="space-y-1.5">
              <h3 className="m-0 text-title2 font-bold">{page.data.title || "未命名公开页"}</h3>
              <p className="m-0 text-meta text-[var(--weir-ink-2)]">{page.data.description || "点左侧「编辑」填写公开介绍与订阅卡片。"}</p>
            </div>
            {cards.length > 0 ? (
              <div className="space-y-2">
                {cards.slice(0, 4).map((c, i) => (
                  <div key={i} className="rounded-[var(--weir-radius)] border border-[var(--weir-line-soft)] bg-[var(--weir-panel)] px-3.5 py-3">
                    <strong className="block text-body">{c.title || `订阅卡片 #${i + 1}`}</strong>
                    <small className="font-mono text-fine text-[var(--weir-ink-3)]">{targets.find(([k]) => k === c.target)?.[1] ?? c.target}</small>
                  </div>
                ))}
              </div>
            ) : (
              <div className="rounded-[var(--weir-radius)] border border-dashed border-[var(--weir-line)] p-4 text-center text-meta text-[var(--weir-ink-3)]">
                暂未添加订阅卡片
              </div>
            )}
            <small className="block pt-2 text-center text-fine text-[var(--weir-ink-3)]">开启「发布页面」保存后生成公开地址 /p/{page.slug}</small>
          </div>
        }
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
          <FormSection title="页面标识与外观">
            <FormGrid>
              <Field label="访问路径" name="slug" hint="小写字母、数字与 -，创建后不可修改" customError={/^[a-z0-9-]*$/.test(slug) ? "" : "地址只能使用小写字母、数字与 -"}>
                <Input required disabled={!!p} placeholder="例如：guide" value={slug} onChange={(e) => setSlug(e.target.value.toLowerCase())} />
              </Field>
              <Field label="页面标题"><Input required maxLength={200} placeholder="欢迎使用" value={d.title} onChange={(e) => set({ title: e.target.value })} /></Field>
            </FormGrid>
            <Field label="页面介绍"><Textarea rows={2} placeholder="简要说明页面用途或接入步骤" value={d.description} onChange={(e) => set({ description: e.target.value })} /></Field>
            <Field label="页面主题色"><Segmented aria-label="页面主题" value={d.theme || "violet"} onValueChange={(theme) => set({ theme })} options={[["violet", "紫罗兰"], ["ocean", "海蓝"], ["forest", "森林"]] as const} /></Field>
          </FormSection>

          <FormSection title="展示与顶栏公告">
            <FormGrid>
              <SwitchGroup>
                {flags.slice(0, 2).map(([k, label]) => (
                  <SwitchField key={k} placement="end" label={label} checked={d[k]} onCheckedChange={(v) => set({ [k]: v })} />
                ))}
              </SwitchGroup>
              <SwitchGroup>
                {flags.slice(2).map(([k, label]) => (
                  <SwitchField key={k} placement="end" label={label} checked={d[k]} onCheckedChange={(v) => set({ [k]: v })} />
                ))}
              </SwitchGroup>
            </FormGrid>
            <FormGrid>
              <Field label="公告标题（可选）"><Input maxLength={200} placeholder="维护通知 / 使用提醒" value={d.notice_title} onChange={(e) => set({ notice_title: e.target.value })} /></Field>
              <Field label="公告样式"><Segmented aria-label="公告类型" value={d.notice_type || "info"} onValueChange={(notice_type) => set({ notice_type })} options={[["info", "通知"], ["success", "成功"], ["warning", "提醒"], ["error", "警告"]] as const} /></Field>
            </FormGrid>
            <Field label="公告正文（可选）"><Textarea rows={2} value={d.notice} onChange={(e) => set({ notice: e.target.value })} /></Field>
          </FormSection>

          <FormSection
            title={`公开订阅卡片（${cards.length} / 24）`}
            description="仅展示仍在匿名分享期内的外部订阅组"
            actions={
              <Button type="button" size="xs" variant="outline" disabled={cards.length >= 24 || !profiles.data || profiles.data.length === 0} onClick={() => set({ cards: [...cards, { profile_id: (shared[0] ?? profiles.data?.[0])?.id ?? 0, target: "clash", title: "" }] })}>
                <Plus />添加卡片
              </Button>
            }
          >
            {profiles.error && <State error={profiles.error} onRetry={() => profiles.refetch()} />}
            {cards.length === 0 ? (
              <div className="rounded-[var(--weir-radius)] border border-[var(--weir-line-soft)] bg-[var(--weir-well)] px-4 py-5 text-center text-meta text-[var(--weir-ink-3)]">
                暂未添加订阅卡片。可只作纯说明页，或点右上角「添加卡片」挂载已分享的订阅组。
              </div>
            ) : (
              <div className="grid gap-2.5">
                {cards.map((c, i) => (
                  <div key={i} className="grid items-start gap-x-2.5 gap-y-2 rounded-[var(--weir-radius)] border border-[var(--weir-line-soft)] bg-[var(--weir-well)] p-3 sm:grid-cols-[minmax(0,1fr)_125px_minmax(0,1fr)_auto]">
                    <Field label="订阅组">
                      <Select required value={c.profile_id ? String(c.profile_id) : ""} onValueChange={(v) => setCard(i, { profile_id: Number(v) })}>
                        <SelectTrigger><SelectValue>{(profiles.data ?? []).find((x) => x.id === c.profile_id)?.name ?? "请选择"}</SelectValue></SelectTrigger>
                        <SelectContent>{(profiles.data ?? []).map((x) => <SelectItem key={x.id} value={String(x.id)}>{x.name}{x.shared ? "" : "（未分享）"}</SelectItem>)}</SelectContent>
                      </Select>
                    </Field>
                    <Field label="输出格式">
                      <Select value={c.target} onValueChange={(v) => setCard(i, { target: String(v) })}>
                        <SelectTrigger><SelectValue>{targets.find(([k]) => k === c.target)?.[1] ?? c.target}</SelectValue></SelectTrigger>
                        <SelectContent>{targets.map(([k, l]) => <SelectItem key={k} value={k}>{l}</SelectItem>)}</SelectContent>
                      </Select>
                    </Field>
                    <Field label="显示名称（可选）"><Input placeholder="留空沿用组名" value={c.title} onChange={(e) => setCard(i, { title: e.target.value })} /></Field>
                    <Button type="button" size="icon-sm" variant="ghost" className="sm:mt-[25px]" aria-label={`移除第 ${i + 1} 张卡片`} onClick={() => set({ cards: cards.filter((_, n) => n !== i) })}><Trash2 /></Button>
                  </div>
                ))}
              </div>
            )}
            {(profiles.data ?? []).length > 0 && shared.length === 0 && (
              <small className="text-[var(--weir-ink-3)]">
                当前还没有开启匿名分享的订阅组；请先前往 <Link className="weir-hit relative text-[var(--weir-accent-text)] underline-offset-4 transition-opacity hover:underline active:opacity-60" to="/groups?tab=profiles" target="_blank">外部订阅组</Link> 开启分享。
              </small>
            )}
          </FormSection>

          <DialogFooter error={save.error}>
            <Button type="button" variant="ghost" disabled={save.busy} onClick={() => { if (!save.busy) void guard.close(); }}>取消</Button>
            <Button type="submit" disabled={save.busy} busy={save.busy} busyText="正在保存…">保存</Button>
          </DialogFooter>
        </Form>
      </DialogContent>
    </Dialog>
  );
}
