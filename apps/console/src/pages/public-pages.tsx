// 公开页面：匿名可访问的介绍页（/p/<slug>），展示已分享的订阅组卡片与留言板。
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Badge, Button, Card, CardContent, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Field, Input, Label, Notice, PageHeader, Segmented, SkeletonList, Select, SelectContent, SelectItem, SelectTrigger, SelectValue, Switch, Textarea, useToast } from "@migate/ui";
import { ArrowUpRight, MessageSquare, Plus, Trash2 } from "lucide-react";
import { api } from "../api";
import { State } from "../components/feedback";
import { GuestbookManager } from "../components/guestbook";
import type { Profile } from "../lib/misub";

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
  const toast = useToast();
  const q = useQuery({ queryKey: ["public-pages"], queryFn: async () => (await api("/admin/public-pages")).items as PageItem[] });
  const [edit, setEdit] = useState<PageItem | "new" | null>(null);
  const [guestbook, setGuestbook] = useState<PageItem | null>(null);
  const [busy, setBusy] = useState(0);
  async function remove(p: PageItem) {
    if (!window.confirm(`删除页面「${p.data.title}」？它的留言会一并删除。`)) return;
    setBusy(p.id);
    try {
      await api("/admin/public-pages/" + p.id, "DELETE");
      await q.refetch();
    } catch (e) {
      toast("删除失败", { description: String(e), tone: "bad" });
    } finally {
      setBusy(0);
    }
  }
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
                <li key={p.id} className="flex flex-wrap items-center justify-between gap-2">
                  <span className="min-w-0">
                    <strong>{p.data.title}</strong> <Badge variant="soft" tone={p.data.enabled ? "ok" : "neutral"}>{p.data.enabled ? "已发布" : "草稿"}</Badge>
                    <small className="block text-[var(--mg-ink-3)]">/p/{p.slug} · {p.data.cards?.length ?? 0} 张订阅卡片</small>
                  </span>
                  <span className="flex flex-wrap gap-1.5">
                    {p.data.enabled && <Button size="sm" variant="outline" render={<a href={"/p/" + p.slug} target="_blank" rel="noopener noreferrer" />}>打开<ArrowUpRight /></Button>}
                    <Button size="sm" variant="outline" onClick={() => setGuestbook(p)}><MessageSquare />留言板</Button>
                    <Button size="sm" variant="outline" onClick={() => setEdit(p)}>编辑</Button>
                    <Button size="icon-sm" variant="destructive" aria-label={"删除 " + p.data.title} disabled={busy === p.id} onClick={() => void remove(p)}><Trash2 /></Button>
                  </span>
                </li>
              ))}
            </ul>
          </CardContent>
        </Card>
      )}
      {edit && <PageForm value={edit} close={() => setEdit(null)} saved={() => { setEdit(null); toast("已保存", { tone: "ok" }); void q.refetch(); }} />}
      {guestbook && <GuestbookManager page={{ id: guestbook.id, data: guestbook.data as unknown as Record<string, unknown> }} close={() => setGuestbook(null)} />}
    </div>
  );
}

function PageForm({ value, close, saved }: { value: PageItem | "new"; close: () => void; saved: () => void }) {
  const p = value === "new" ? undefined : value;
  const profiles = useQuery({ queryKey: ["misub-profiles"], queryFn: async () => (await api("/admin/misub/profiles")).items as Profile[] });
  const [slug, setSlug] = useState(p?.slug ?? "");
  const [d, setD] = useState<PageData>({ ...blank, ...p?.data, cards: p?.data.cards ?? [] });
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const set = (patch: Partial<PageData>) => setD({ ...d, ...patch });
  const cards = d.cards ?? [];
  const setCard = (i: number, patch: Partial<PageCard>) => set({ cards: cards.map((c, n) => (n === i ? { ...c, ...patch } : c)) });
  async function submit() {
    setBusy(true);
    setError("");
    try {
      await (p ? api("/admin/public-pages/" + p.id, "PATCH", { data: d }) : api("/admin/public-pages", "POST", { slug: slug.trim(), data: d }));
      saved();
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  }
  const shared = (profiles.data ?? []).filter((x) => x.shared);
  return (
    <Dialog open onOpenChange={(o) => !o && !busy && close()}>
      <DialogContent size="lg">
        <DialogTitle>{p ? "编辑页面" : "新建页面"}</DialogTitle>
        <DialogDescription>页面任何人都能访问，请不要填写私人信息。订阅卡片只显示仍在分享期内的订阅组，下载时再次核对。</DialogDescription>
        <form className="stack" onSubmit={(e) => { e.preventDefault(); void submit(); }}>
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label="地址" htmlFor="pg-slug" hint="小写字母、数字与 -，创建后不能修改。"><Input id="pg-slug" required disabled={!!p} value={slug} onChange={(e) => setSlug(e.target.value.toLowerCase())} /></Field>
            <Field label="标题" htmlFor="pg-title"><Input id="pg-title" required maxLength={200} value={d.title} onChange={(e) => set({ title: e.target.value })} /></Field>
          </div>
          <Field label="介绍" htmlFor="pg-desc"><Textarea id="pg-desc" rows={3} value={d.description} onChange={(e) => set({ description: e.target.value })} /></Field>
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label="公告标题（可选）" htmlFor="pg-ntitle"><Input id="pg-ntitle" maxLength={200} value={d.notice_title} onChange={(e) => set({ notice_title: e.target.value })} /></Field>
            <Field label="公告类型"><Segmented aria-label="公告类型" value={d.notice_type || "info"} onValueChange={(notice_type) => set({ notice_type })} options={[["info", "通知"], ["success", "成功"], ["warning", "提醒"], ["error", "警告"]] as const} /></Field>
          </div>
          <Field label="公告正文（纯文本，可选）" htmlFor="pg-notice"><Textarea id="pg-notice" rows={2} value={d.notice} onChange={(e) => set({ notice: e.target.value })} /></Field>
          <Field label="主题"><Segmented aria-label="页面主题" value={d.theme || "violet"} onValueChange={(theme) => set({ theme })} options={[["violet", "紫罗兰"], ["ocean", "海蓝"], ["forest", "森林"]] as const} /></Field>
          <div className="grid gap-2 sm:grid-cols-2">
            <Label className="actions"><Switch aria-label="发布页面" checked={d.enabled} onCheckedChange={(enabled) => set({ enabled })} />发布页面</Label>
            <Label className="actions"><Switch aria-label="显示登录入口" checked={d.show_login} onCheckedChange={(show_login) => set({ show_login })} />显示登录入口</Label>
            <Label className="actions"><Switch aria-label="允许复制页面地址与二维码" checked={d.show_share} onCheckedChange={(show_share) => set({ show_share })} />允许复制页面地址与二维码</Label>
            <Label className="actions"><Switch aria-label="允许关闭公告" checked={d.notice_dismissible} onCheckedChange={(notice_dismissible) => set({ notice_dismissible })} />允许关闭公告</Label>
          </div>
          <fieldset className="stack">
            <legend className="text-sm">订阅卡片（最多 24 张）</legend>
            {cards.map((c, i) => (
              <div key={i} className="grid items-end gap-2 sm:grid-cols-[1fr_auto_1fr_auto]">
                <Field label="订阅组">
                  <Select value={String(c.profile_id)} onValueChange={(v) => setCard(i, { profile_id: Number(v) })}>
                    <SelectTrigger aria-label="订阅组"><SelectValue>{(profiles.data ?? []).find((x) => x.id === c.profile_id)?.name ?? "请选择"}</SelectValue></SelectTrigger>
                    <SelectContent>{(profiles.data ?? []).map((x) => <SelectItem key={x.id} value={String(x.id)}>{x.name}{x.shared ? "" : "（未分享，不显示）"}</SelectItem>)}</SelectContent>
                  </Select>
                </Field>
                <Field label="格式">
                  <Select value={c.target} onValueChange={(v) => setCard(i, { target: String(v) })}>
                    <SelectTrigger aria-label="格式"><SelectValue>{targets.find(([k]) => k === c.target)?.[1] ?? c.target}</SelectValue></SelectTrigger>
                    <SelectContent>{targets.map(([k, l]) => <SelectItem key={k} value={k}>{l}</SelectItem>)}</SelectContent>
                  </Select>
                </Field>
                <Field label="卡片标题（可选）"><Input aria-label="卡片标题" value={c.title} onChange={(e) => setCard(i, { title: e.target.value })} /></Field>
                <Button type="button" size="icon-sm" variant="destructive" aria-label="移除卡片" onClick={() => set({ cards: cards.filter((_, n) => n !== i) })}><Trash2 /></Button>
              </div>
            ))}
            <div className="actions">
              <Button type="button" size="sm" variant="outline" disabled={cards.length >= 24 || (profiles.data ?? []).length === 0} onClick={() => set({ cards: [...cards, { profile_id: (shared[0] ?? profiles.data?.[0])?.id ?? 0, target: "clash", title: "" }] })}><Plus />添加卡片</Button>
            </div>
            {(profiles.data ?? []).length > 0 && shared.length === 0 && <small className="text-[var(--mg-ink-3)]">当前没有已分享的订阅组；先在「订阅管理 → 订阅组」中分享，卡片才会显示。</small>}
          </fieldset>
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
