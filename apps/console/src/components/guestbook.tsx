// 公开页留言：访客投稿（可匿名、可需审核），管理员在页面管理中审核、回复、隐藏与删除；留言设置变更需验证身份。
import { useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Button,
  Card,
  CardContent,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogTitle,
  Field,
  Form,
  Input,
  Notice,
  Pager,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
  SwitchField,
  Textarea,
} from "@weir/ui";
import { api, isConflict } from "../api";
import { useFormAction } from "../lib/action";
import { ConflictNotice, State } from "./feedback";
import { emptyReauth, Reauth, useReauthAction } from "./system-settings";

interface Config {
  enabled: boolean;
  allow_anonymous: boolean;
  require_audit: boolean;
}
interface Message {
  id: number;
  nickname: string;
  content: string;
  type: string;
  status: string;
  created_at: string;
  reply: string;
  reply_at: string | null;
  visible?: boolean;
  revision?: string;
}
interface Listing {
  items: Message[];
  page: number;
  page_size: number;
  total: number;
  config: { enabled: boolean; allow_anonymous: boolean };
}
const types: Record<string, string> = { general: "交流", feature: "功能建议", bug: "问题反馈" };
const statuses: Record<string, string> = { pending: "待审核", approved: "已审核", replied: "已回复" };
const configFlags = [
  ["enabled", "启用本页留言"],
  ["allow_anonymous", "允许匿名投稿"],
  ["require_audit", "新留言需要审核"],
] as const;

function Entry({ item, children }: { item: Message; children?: ReactNode }) {
  return (
    <Card>
      <CardContent className="stack">
        <div className="actions">
          <strong>{item.nickname}</strong>
          <span className="muted">
            {types[item.type]} · {statuses[item.status]} · {new Date(item.created_at).toLocaleString()}
          </span>
        </div>
        <p className="whitespace-pre-wrap wrap-anywhere">{item.content}</p>
        {item.reply && (
          <Notice>
            <strong>管理员回复</strong>
            <p className="whitespace-pre-wrap wrap-anywhere">{item.reply}</p>
            {item.reply_at && <small>{new Date(item.reply_at).toLocaleString()}</small>}
          </Notice>
        )}
        {children}
      </CardContent>
    </Card>
  );
}

export function PublicGuestbook({ slug }: { slug: string }) {
  const [page, setPage] = useState(1);
  const [nickname, setNickname] = useState("");
  const [content, setContent] = useState("");
  const [type, setType] = useState("general");
  const [notice, setNotice] = useState("");
  const base = "/pages/" + encodeURIComponent(slug) + "/guestbook";
  const q = useQuery({
    queryKey: ["public-guestbook", slug, page],
    queryFn: () => api(base + "?page=" + page) as Promise<Listing>,
    retry: false,
    refetchInterval: 30000,
  });
  const post = useFormAction(() => api(base, "POST", { nickname, content, type }) as Promise<{ pending: boolean }>, {
    fields: { invalid_guestbook: "content" },
    onSuccess: async (out) => {
      setContent("");
      setNotice(out.pending ? "留言已提交，等待管理员审核。" : "留言已发布。");
      setPage(1);
      await q.refetch();
    },
  });
  return (
    <section className="stack public-guestbook" aria-label="公开留言">
      <h2>留言与反馈</h2>
      {q.isPending ? (
        <State loading />
      ) : q.isError ? (
        <State error={q.error} onRetry={() => void q.refetch()} />
      ) : (
        <>
          <p>请勿提交密码、订阅地址或其他个人信息。昵称与留言经管理员确认后可向所有访客公开。</p>
          {!q.data.config.allow_anonymous && (
            <Notice>
              仅登录后可投稿。<a href="/login">前往登录</a>
            </Notice>
          )}
          <Form busy={post.busy} errors={post.errors} onSubmit={() => void post.submit()}>
            <Field label="昵称（可留空）">
              <Input maxLength={20} value={nickname} onChange={(e) => setNickname(e.target.value)} />
            </Field>
            <Field label="留言分类">
              <Select value={type} onValueChange={(v) => setType(v ?? "general")}>
                <SelectTrigger>
                  <SelectValue>{types[type]}</SelectValue>
                </SelectTrigger>
                <SelectContent>
                  {Object.entries(types).map(([v, n]) => (
                    <SelectItem key={v} value={v}>
                      {n}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
            <Field label="留言正文" name="content" hint={`${[...content].length} / 500 字`} customError={content && !content.trim() ? "请输入留言正文" : ""}>
              <Textarea rows={4} maxLength={500} required value={content} onChange={(e) => setContent(e.target.value)} />
            </Field>
            {post.error && <Notice tone="bad">{post.error}</Notice>}
            {notice && <Notice role="status">{notice}</Notice>}
            <div className="actions">
              <Button type="submit" disabled={post.busy}>{post.busy ? "正在提交…" : "提交留言"}</Button>
            </div>
          </Form>
          {q.data.items.length === 0 ? <p>暂无公开留言。</p> : q.data.items.map((item) => <Entry key={item.id} item={item} />)}
          <Pager variant="inline" name="留言" page={page} pageSize={20} total={q.data.total} busy={q.isFetching} onPage={setPage} />
        </>
      )}
    </section>
  );
}

type Mode = "" | "approve" | "hide" | "reply" | "delete" | "pending" | "approved" | "config";
const modeNames: Record<Exclude<Mode, "">, string> = {
  approve: "公开此留言",
  hide: "隐藏此留言",
  reply: "公开回复",
  delete: "永久删除留言",
  pending: "改为待审核",
  approved: "标记已审核",
  config: "保存留言设置",
};

export function GuestbookManager({ page: resource, close }: { page: { id: number; data: Record<string, unknown> }; close: () => void }) {
  const base = "/admin/public-pages/" + resource.id + "/guestbook";
  const [page, setPage] = useState(1);
  const [status, setStatus] = useState("");
  const [search, setSearch] = useState("");
  const [draft, setDraft] = useState<Config | null>(null);
  const [mode, setMode] = useState<Mode>("");
  const [selected, setSelected] = useState<Message | null>(null);
  const [reply, setReply] = useState("");
  const [re, setRe] = useState(emptyReauth);
  const [notice, setNotice] = useState("");
  const cfg = useQuery({
    queryKey: ["guestbook-config", resource.id],
    queryFn: () => api(base + "/config") as Promise<{ config: Config; revision: string }>,
    refetchOnWindowFocus: false,
  });
  const q = useQuery({
    queryKey: ["guestbook-manage", resource.id, page, status, search],
    queryFn: () => api(base + "/messages?page=" + page + "&status=" + status + "&search=" + encodeURIComponent(search)) as Promise<Listing>,
    refetchOnWindowFocus: false,
    staleTime: 0,
  });
  const value = draft ?? cfg.data?.config;
  function cancel() {
    setMode("");
    setSelected(null);
    setReply("");
    setRe(emptyReauth);
  }
  function begin(action: Mode, item: Message) {
    save.reset();
    setMode(action);
    setSelected(item);
    setReply(item.reply);
    setNotice("");
  }
  const done = () => {
    setNotice("留言操作已确认。");
    cancel();
  };
  // 留言设置需要验证身份；单条留言的审核 / 回复 / 删除不需要
  const saveConfig = useReauthAction(() => api(base + "/config", "PUT", { config: value, revision: cfg.data?.revision, ...re, confirm: true }), {
    invalidate: [["guestbook-manage", resource.id]],
    onSuccess: async () => {
      setDraft(null);
      await cfg.refetch();
      done();
    },
    onError: () => setRe((v) => ({ ...v, code: "" })),
  });
  const saveMessage = useFormAction(
    () => {
      const statusOnly = mode === "pending" || mode === "approved";
      return api(base + "/messages", "POST", {
        id: selected!.id,
        revision: selected!.revision,
        action: statusOnly ? "status" : mode,
        status: statusOnly ? mode : "",
        reply: mode === "reply" ? reply : "",
      });
    },
    { fields: { invalid_guestbook: "reply" }, invalidate: [["guestbook-manage", resource.id]], onSuccess: done },
  );
  const save = mode === "config" ? saveConfig : saveMessage;
  return (
    <Dialog open onOpenChange={(v) => !v && !save.busy && close()}>
      <DialogContent size="xl" className="guestbook-manager">
        <DialogTitle>留言管理 · {String(resource.data.title ?? resource.data.name ?? "")}</DialogTitle>
        <DialogDescription>仅本公开页。发布或回复后向所有访客显示；纯文本，不含用户账号或节点信息。页面撤下即停止匿名读取与投稿。</DialogDescription>
        {mode ? (
          <Form busy={save.busy} errors={save.errors} onSubmit={() => void save.submit()}>
            <h3>{modeNames[mode]}</h3>
            {selected && mode !== "config" && <Entry item={selected} />}
            {mode === "reply" && (
              <Field label="管理员回复正文" name="reply" customError={reply && !reply.trim() ? "请输入回复正文" : ""}>
                <Textarea rows={5} maxLength={1000} required value={reply} onChange={(e) => setReply(e.target.value)} />
              </Field>
            )}
            {mode === "delete" && <Notice tone="warn">删除无法撤销。仅删除此留言，不影响用户、授权或订阅。</Notice>}
            {mode === "pending" && <p>同时隐藏公开内容，待审核后可再次发布。</p>}
            {mode === "approved" && <p>只更新审核状态，保留当前可见性。公开请使用“公开此留言”。</p>}
            {mode === "config" && (
              <>
                <p>
                  启用：{value?.enabled ? "是" : "否"} · 匿名：{value?.allow_anonymous ? "允许" : "需登录"} · 新投稿：{value?.require_audit ? "需审核" : "直接公开"}
                </p>
                <Reauth value={re} onChange={setRe} />
              </>
            )}
            <ConflictNotice
              error={save.raw}
              onReload={() => {
                cancel();
                setDraft(null);
                void cfg.refetch();
                void q.refetch();
              }}
            />
            {save.error && !isConflict(save.raw) && <Notice tone="bad">{save.error}</Notice>}
            <div className="actions">
              <Button type="button" variant="outline" disabled={save.busy} onClick={cancel}>
                取消留言操作
              </Button>
              <Button type="submit" variant={mode === "delete" ? "destructive" : "default"} disabled={save.busy}>
                确认{modeNames[mode]}
              </Button>
            </div>
          </Form>
        ) : (
          <div className="stack">
            {cfg.isPending || cfg.isError ? (
              <State loading={cfg.isPending} error={cfg.error} onRetry={() => void cfg.refetch()} />
            ) : (
              value && (
                <section className="stack">
                  <h3>投稿与审核设置</h3>
                  {configFlags.map(([key, label]) => (
                    <SwitchField
                      key={key}
                      label={label}
                      checked={value[key]}
                      onCheckedChange={(v) => {
                        setDraft({ ...value, [key]: v });
                        setNotice("");
                      }}
                    />
                  ))}
                  <div className="actions">
                    <Button
                      disabled={!draft}
                      onClick={() => {
                        saveConfig.reset();
                        setMode("config");
                      }}
                    >
                      核对并保存留言设置
                    </Button>
                  </div>
                </section>
              )
            )}
            {notice && <Notice role="status">{notice}</Notice>}
            <div className="grid gap-3 sm:grid-cols-2">
              <Field label="搜索留言或昵称">
                <Input
                  value={search}
                  maxLength={200}
                  onChange={(e) => {
                    setSearch(e.target.value);
                    setPage(1);
                  }}
                />
              </Field>
              <Field label="审核状态">
                <Select
                  value={status || "all"}
                  onValueChange={(v) => {
                    setStatus(v === "all" ? "" : (v ?? ""));
                    setPage(1);
                  }}
                >
                  <SelectTrigger>
                    <SelectValue>{status ? statuses[status] : "全部状态"}</SelectValue>
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="all">全部状态</SelectItem>
                    {Object.entries(statuses).map(([v, n]) => (
                      <SelectItem key={v} value={v}>
                        {n}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
            </div>
            {q.isPending || q.isError ? (
              <State loading={q.isPending} error={q.error} onRetry={() => void q.refetch()} />
            ) : (
              <>
                {q.data.items.length === 0 ? (
                  <State empty="暂无符合条件的留言" />
                ) : (
                  q.data.items.map((item) => (
                    <Entry key={item.id} item={item}>
                      <p>{item.visible ? "当前公开" : "当前隐藏"}</p>
                      <div className="actions">
                        {([item.visible ? "hide" : "approve", "reply", "pending", "approved", "delete"] as const).map((a) => (
                          <Button key={a} variant={a === "delete" ? "destructive" : "outline"} onClick={() => begin(a, item)}>
                            {modeNames[a]}
                          </Button>
                        ))}
                      </div>
                    </Entry>
                  ))
                )}
                <Pager variant="inline" name="留言" page={page} pageSize={20} total={q.data.total} busy={q.isFetching} onPage={setPage} />
              </>
            )}
            <div className="actions">
              <Button
                variant="outline"
                onClick={() => {
                  setDraft(null);
                  setNotice("");
                  void cfg.refetch();
                  void q.refetch();
                }}
              >
                重新读取留言
              </Button>
            </div>
          </div>
        )}
        <DialogFooter>
          <Button variant="outline" disabled={save.busy} onClick={close}>
            关闭留言管理
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
