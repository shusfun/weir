// 全局公告：登录用户与匿名公开页分别选择受众；编辑区校验通过后直接保存。
import { useState, type SetStateAction } from "react";
import { useDraft } from "../lib/dirty";
import { useQuery } from "../lib/query";
import {
  Field,
  Form,
  FormGrid,
  Notice,
  Button,
  Input,
  Textarea,
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
  SwitchField,
} from "@weir/ui";
import { api, isConflict } from "../api";
import { ConflictNotice, State } from "./feedback";
import { useFormAction } from "../lib/action";
import { PublishedAnnouncement, type NoticeNode, type PublishedNotice } from "./published-announcement";
import { field, useConsoleContext } from "../lib/console-context";
import { Section } from "./system-settings";

interface Announcement {
  content_format?: string;
  enabled: boolean;
  authenticated: boolean;
  public: boolean;
  title: string;
  content: string;
  type: string;
  dismissible: boolean;
}
interface Document {
  announcement: Announcement;
  revision: string;
}
const names: Record<string, string> = { info: "通知", success: "成功", warning: "提醒", error: "重要" };
const initial: Announcement = {
  enabled: false,
  authenticated: false,
  public: false,
  title: "",
  content: "",
  type: "info",
  dismissible: true,
};
export function GlobalAnnouncement({ audience }: { audience: "authenticated" | "public" }) {
  const q = useQuery({
    queryKey: ["global-announcement", audience],
    queryFn: () =>
      api(audience === "public" ? "/announcements/public" : "/announcements") as Promise<{
        announcement: PublishedNotice | null;
      }>,
    retry: false,
    refetchInterval: 30000,
  });
  if (q.isPending)
    return (
      <p className="muted" role="status">
        正在读取服务公告…
      </p>
    );
  if (q.isError)
    return (
      <Notice tone="warn" role="alert">
        服务公告暂不可用：{String(q.error)}
        <Button variant="ghost" size="sm" onClick={() => void q.refetch()}>
          重试公告
        </Button>
      </Notice>
    );
  return q.data.announcement ? (
    <div className="global-announcement">
      <PublishedAnnouncement notice={q.data.announcement} scope={"global-" + audience} />
    </div>
  ) : null;
}
export function AnnouncementEditor() {
  const q = useQuery({ queryKey: ["announcement-admin"], queryFn: () => api("/admin/announcement") as Promise<Document>, refetchOnWindowFocus: false });
  const editor = useDraft({ announcement: q.data?.announcement ?? initial, revision: q.data?.revision ?? "" });
  const draft = editor.value.announcement;
  const setDraft = (value: SetStateAction<Announcement>) => editor.set(previous => ({ ...previous, announcement: typeof value === "function" ? value(previous.announcement) : value }));
  const [notice, setNotice] = useState("");
  const html = draft.content_format === "html";
  const preview = useQuery({
    queryKey: ["announcement-markup-preview", draft.content, draft.content_format],
    queryFn: () => api("/admin/announcement/preview", "POST", { content: draft.content, format: draft.content_format }) as Promise<{ nodes: NoticeNode[] }>,
    enabled: html && !!draft.content,
    retry: false,
    refetchOnWindowFocus: false,
  });
  const save = useFormAction(() => api("/admin/announcement", "PUT", { announcement: draft, revision: editor.value.revision }), {
    invalidate: [["announcement-admin"], ["global-announcement"]],
    onSuccess: () => { editor.commit(); setNotice(draft.enabled ? "公告已发布，仅向选定受众显示。" : "公告已撤下，草稿已保留。"); },
  });
  const dirty = editor.changed;
  const change = (p: Partial<Announcement>) => {
    setDraft((v) => ({ ...v, ...p }));
    setNotice("");
  };
  // 发布前的规则：正文必填、至少一个受众、富文本须通过服务端白名单
  const contentError = draft.enabled && !draft.content.trim() ? "发布公告需要填写正文" : html && preview.isError ? "富文本包含不支持的标签、属性或链接，请修正后再发布" : "";
  const audienceError = draft.enabled && !draft.authenticated && !draft.public ? "发布公告需要至少选择一个受众" : "";
  const previewReady = !html || (!!preview.data && !preview.isError);
  useConsoleContext({ tab: "分发", form: [field("announcement_enabled", "启用全局公告", draft.enabled ? "是" : "否")] });
  return (
    <Section title="全局公告" description="统一发布服务通知；登录用户与匿名公开页分别选择，页面自己的公告保持独立。">
      <div className="stack announcement-editor">
        <div>
          <h3>全局公告</h3>
          <p>统一发布服务通知。登录用户与匿名公开页分别选择；页面自己的公告保持独立。</p>
        </div>
        {notice && <Notice role="status">{notice}</Notice>}
        {q.isPending || q.isError ? (
          <State loading={q.isPending} error={q.error} onRetry={() => void q.refetch()} />
        ) : (
          <Form busy={save.busy} errors={save.errors} onSubmit={() => void save.submit()}>
            <SwitchField label="发布全局公告" checked={draft.enabled} onCheckedChange={(enabled) => change({ enabled })} />
            <FormGrid>
              <Field label="全局公告标题">
                <Input maxLength={200} value={draft.title} onChange={(e) => change({ title: e.target.value })} placeholder="服务通知" />
              </Field>
              <Field label="全局公告类型">
                <Select value={draft.type} onValueChange={(type) => type && change({ type })}>
                  <SelectTrigger>
                    <SelectValue>{names[draft.type]}</SelectValue>
                  </SelectTrigger>
                  <SelectContent>
                    {Object.entries(names).map(([value, name]) => (
                      <SelectItem key={value} value={value}>
                        {name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
            </FormGrid>
            <Field label="全局公告内容格式" hint="富文本支持加粗、斜体、段落、换行、列表、代码与 HTTPS 链接；拒绝脚本、样式、图片和事件属性。">
              <Select value={draft.content_format || "text"} onValueChange={(f) => f && change({ content_format: f === "text" ? undefined : f })}>
                <SelectTrigger>
                  <SelectValue>{html ? "受限富文本" : "纯文本"}</SelectValue>
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="text">纯文本</SelectItem>
                  <SelectItem value="html">受限富文本</SelectItem>
                </SelectContent>
              </Select>
            </Field>
            <Field label={html ? "全局公告正文（受限富文本）" : "全局公告正文（纯文本）"} required={draft.enabled} customError={contentError} hint={html && preview.isFetching ? "正在验证富文本…" : undefined}>
              <Textarea rows={6} maxLength={8192} value={draft.content} onChange={(e) => change({ content: e.target.value })} />
            </Field>
            <fieldset className="resource-selector grid gap-2">
              <legend>显示受众</legend>
              <Field customError={audienceError}>
                <div className="grid gap-2">
                  <SwitchField label="向登录用户显示" checked={draft.authenticated} onCheckedChange={(authenticated) => change({ authenticated })} />
                  <SwitchField label="向匿名公开页显示" description="匿名公开页任何人均可查看。请勿填写私有订阅、账户信息或密钥。" checked={draft.public} onCheckedChange={(value) => change({ public: value })} />
                </div>
              </Field>
            </fieldset>
            <SwitchField
              label="允许关闭全局公告"
              description="标题、正文或类型更新后重新提示；关闭偏好只保存在当前浏览器。纯文本不执行 HTML；富文本使用服务端白名单结构。"
              checked={draft.dismissible}
              onCheckedChange={(dismissible) => change({ dismissible })}
            />
            {draft.content && previewReady ? (
              <div className="announcement-preview">
                <small>公告预览{!draft.enabled && " · 草稿未发布"}</small>
                <PublishedAnnouncement
                  preview
                  scope="global-preview"
                  notice={{
                    notice: draft.content,
                    notice_title: draft.title,
                    notice_type: draft.type,
                    notice_dismissible: draft.dismissible,
                    notice_nodes: html ? preview.data?.nodes : undefined,
                  }}
                />
              </div>
            ) : (
              <p className="muted">填写正文后在此预览。</p>
            )}
            <div className="actions">
              <Button type="submit" disabled={save.busy || !dirty || (html && !!draft.content && preview.isFetching)} busy={save.busy} busyText="正在保存…">
                保存公告
              </Button>
              {dirty && <small>有未保存更改</small>}
            </div>
            <ConflictNotice error={save.raw} onReload={() => void q.refetch()} />
            {save.error && !isConflict(save.raw) && <Notice tone="bad">{save.error}</Notice>}
          </Form>
        )}
      </div>
    </Section>
  );
}
