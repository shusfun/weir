// 公开页面：匿名可访问的介绍页（/p/<slug>），展示已分享的订阅组卡片与留言板。
import { Link } from "react-router-dom";
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Badge, Button, Card, CardContent, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Field, Form, FormGrid, Input, Notice, PageHeader, Segmented, SkeletonList, Select, SelectContent, SelectItem, SelectTrigger, SelectValue, SwitchField, Textarea, useToast, useConfirm } from "@weir/ui";
import { ArrowUpRight, MessageSquare, Plus, Trash2 } from "lucide-react";
import { api } from "../api";
import { useFormAction, useRowAction } from "../lib/action";
import { State } from "../components/feedback";
import { GuestbookManager } from "../components/guestbook";
import type { Profile } from "../lib/subscriptions";
import { dataObject } from "../components/object-link";
import { field, useConsoleContext } from "../lib/console-context";

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
  const confirm = useConfirm();
  const toast = useToast();
  const q = useQuery({ queryKey: ["public-pages"], queryFn: async () => (await api("/admin/public-pages")).items as PageItem[] });
  const [edit, setEdit] = useState<PageItem | "new" | null>(null);
  const [guestbook, setGuestbook] = useState<PageItem | null>(null);
  const { run, isBusy } = useRowAction<number>({ refetch: q.refetch });
  async function remove(p: PageItem) {
    if (!(await confirm({ title: `删除页面「${p.data.title}」？`, description: "它的留言会一并删除。", confirmText: "删除", tone: "danger" }))) return;
    await run(p.id, () => api("/admin/public-pages/" + p.id, "DELETE"), "页面已删除", { fail: "删除失败" });
  }
  useConsoleContext({ page: "公开页面", list: { total: q.data?.length ?? 0, items: (q.data ?? []).slice(0, 20).map((p) => ({ type: "page", name: p.data.title, key: p.slug })) } });
  return (
    <div className="stack">
      <PageHeader eyebrow="订阅与分发" title="公开页面" description="匿名可访问的介绍页，可展示已分享的订阅组与留言板。" actions={<Button onClick={() => setEdit("new")}><Plus />新建页面</Button>} />
      {q.isPending || q.error ? (
        <State
          loading={q.isPending}
          error={q.error}
          onRetry={() => q.refetch()}
          skeleton={<Card><CardContent><SkeletonList rows={3} lines={2} className="stack" rowClassName="flex items-center justify-between gap-2" /></CardContent></Card>}
        />
      ) : q.data.length === 0 ? (
        <State empty="还没有公开页面" />
      ) : (
        <Card>
          <CardContent>
            <ul className="stack">
              {q.data.map((p) => (
                <li key={p.id} {...dataObject({ type: "page", key: p.slug })} className="flex flex-wrap items-center justify-between gap-2">
                  <span className="min-w-0">
                    <strong>{p.data.title}</strong> <Badge variant="soft" tone={p.data.enabled ? "ok" : "neutral"}>{p.data.enabled ? "已发布" : "草稿"}</Badge>
                    <small className="block text-[var(--weir-ink-3)]">/p/{p.slug} · {p.data.cards?.length ?? 0} 张订阅卡片</small>
                  </span>
                  <span className="flex flex-wrap gap-1.5">
                    {p.data.enabled && <Button size="sm" variant="outline" render={<a href={"/p/" + p.slug} target="_blank" rel="noopener noreferrer" />}>打开<ArrowUpRight /></Button>}
                    <Button size="sm" variant="outline" onClick={() => setGuestbook(p)}><MessageSquare />留言板</Button>
                    <Button size="sm" variant="outline" onClick={() => setEdit(p)}>编辑</Button>
                    <Button size="icon-sm" variant="destructive" aria-label={"删除 " + p.data.title} disabled={isBusy()} busy={isBusy(p.id)} busyText="正在删除" onClick={() => void remove(p)}><Trash2 /></Button>
                  </span>
                </li>
              ))}
            </ul>
          </CardContent>
        </Card>
      )}
      {edit && <PageForm value={edit} close={() => setEdit(null)} saved={() => { setEdit(null); toast("已保存", { tone: "ok" }); void q.refetch(); }} />}
      {guestbook && <GuestbookManager page={{ id: guestbook.id, data: { ...guestbook.data } }} close={() => setGuestbook(null)} />}
    </div>
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
  const save = useFormAction(() => (p ? api("/admin/public-pages/" + p.id, "PATCH", { data: d }) : api("/admin/public-pages", "POST", { slug: slug.trim(), data: d })), {
    fields: { invalid_slug: "slug", slug_taken: "slug" },
    onSuccess: saved,
  });
  const shared = (profiles.data ?? []).filter((x) => x.shared);
  useConsoleContext({ dialog: { title: p ? "编辑页面" : "新建页面" }, object: p ? { type: "page", name: p.data.title, key: p.slug } : undefined, form: [field("slug", "地址名", slug), field("title", "标题", d.title), field("enabled", "发布", d.enabled ? "是" : "否"), field("cards", "订阅卡片", String(d.cards?.length ?? 0))] });
  return (
    <Dialog open onOpenChange={(o) => !o && !save.busy && close()}>
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
            {(profiles.data ?? []).length > 0 && shared.length === 0 && <small className="text-[var(--weir-ink-3)]">当前没有已分享的订阅组；先分享订阅组，卡片才会显示：<Link className="text-[var(--weir-accent-text)] underline-offset-4 hover:underline" to="/subscriptions?tab=profiles" target="_blank">在新标签页打开「订阅源 → 订阅组」</Link>。</small>}
          </fieldset>
          <DialogFooter error={save.error}>
            <Button type="button" variant="outline" disabled={save.busy} onClick={close}>取消</Button>
            <Button type="submit" disabled={save.busy} busy={save.busy} busyText="正在保存…">保存</Button>
          </DialogFooter>
        </Form>
      </DialogContent>
    </Dialog>
  );
}
