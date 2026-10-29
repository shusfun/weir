// 安装包加速镜像：管理员维护前缀列表（如 https://mirror.example/），保存需重新验证；
// 用户浏览器把 GitHub 下载地址拼在前缀后测速，下载后仍按 SHA-256 校验。
import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Field, Form, Notice, SectionHeader, Button, Card, CardContent, Dialog, DialogContent, DialogTitle, DialogDescription, DialogFooter, Input, Textarea, PasswordInput } from "@weir/ui";
import { Save } from "lucide-react";
import { api, ApiError, isConflict } from "../api";
import { State, ConflictNotice } from "./feedback";
import { emptyReauth, useReauthAction } from "./system-settings";
import { field, useConsoleContext } from "../lib/console-context";

interface Mirrors { mirrors: string[]; revision: string }

// 这些错误说的是镜像列表本身：关闭确认框，标在列表下方
const listErrors = new Set(["invalid_mirror", "mirror_limit"]);
const parse = (text: string) => text.split("\n").map((v) => v.trim()).filter(Boolean);

export function ClientMirrorsEditor() {
  const query = useQuery({ queryKey: ["client-mirrors"], queryFn: () => api("/client-mirrors") as Promise<Mirrors> });
  const self = useQuery({ queryKey: ["self-account"], queryFn: () => api("/me") });
  const [draft, setDraft] = useState("");
  const [confirm, setConfirm] = useState(false);
  const [re, setRe] = useState(emptyReauth);
  const [notice, setNotice] = useState("");
  const [listError, setListError] = useState("");
  const save = useReauthAction(() => api("/admin/client-mirrors", "PUT", { mirrors: parse(draft), revision: query.data!.revision, ...re }), {
    invalidate: [["client-mirrors"]],
    onSuccess: () => {
      setConfirm(false);
      setRe(emptyReauth);
      setNotice("加速镜像已保存。");
    },
    onError: (e) => {
      // 验证码只能用一次，失败后清空
      setRe((v) => ({ ...v, code: "" }));
      if (e instanceof ApiError && listErrors.has(e.code)) {
        setConfirm(false);
        setListError(e.message);
      }
    },
  });
  useEffect(() => {
    if (query.data) setDraft(query.data.mirrors.join("\n"));
  }, [query.data?.revision]);
  useConsoleContext({ dialog: confirm ? { title: "确认保存加速镜像" } : undefined, form: [field("mirrors", "加速镜像", draft.split("\n").filter(Boolean).length + " 条"), field("password", "当前密码", re.password, true)] });
  if (query.isPending) return <State loading />;
  if (query.error) return <State error={query.error} onRetry={() => query.refetch()} />;
  const list = parse(draft);
  const dirty = list.join("\n") !== query.data.mirrors.join("\n");
  const conflict = isConflict(save.raw);

  return (
    <Card>
      <CardContent className="stack">
        <SectionHeader flush level="h3" title="安装包加速镜像" description="每行一个前缀，最多 8 个，须为 HTTPS 并以 / 结尾。用户下载时会和 GitHub 直连一起测速；镜像内容仍按 GitHub 公布的 SHA-256 校验，不一致会被拒绝。" />
        <Field label="加速镜像前缀" error={listError} customError={list.length > 8 ? "加速镜像最多 8 个" : ""}>
          <Textarea
            rows={4}
            value={draft}
            placeholder="https://mirror.example/"
            onChange={(e) => {
              setDraft(e.target.value);
              setNotice("");
              setListError("");
            }}
          />
        </Field>
        {notice && <Notice role="status">{notice}</Notice>}
        <div className="actions justify-end">
          <Button
            disabled={!dirty || list.length > 8}
            onClick={() => {
              save.reset();
              setConfirm(true);
            }}
          >
            <Save />
            保存镜像
          </Button>
        </div>
      </CardContent>
      <Dialog open={confirm} onOpenChange={(v) => !save.busy && setConfirm(v)}>
        <DialogContent size="md">
          <DialogTitle>确认保存加速镜像</DialogTitle>
          <DialogDescription>共 {list.length} 个镜像。验证管理员身份后保存；其他管理员已修改时会拒绝覆盖。</DialogDescription>
          <Form busy={save.busy} errors={save.errors} onSubmit={() => void save.submit()}>
            <Field label="管理员当前密码" name="password">
              <PasswordInput autoComplete="current-password" value={re.password} onChange={(e) => setRe({ ...re, password: e.target.value })} required />
            </Field>
            {self.data?.totp_enrolled && (
              <Field label="二次验证码或恢复码" name="code">
                <Input autoComplete="one-time-code" value={re.code} onChange={(e) => setRe({ ...re, code: e.target.value })} required />
              </Field>
            )}
            <ConflictNotice
              error={save.raw}
              onReload={() => {
                setConfirm(false);
                void query.refetch();
              }}
            />
            {save.error && !conflict && <Notice tone="bad">{save.error}</Notice>}
            <DialogFooter>
              <Button type="button" variant="outline" disabled={save.busy} onClick={() => setConfirm(false)}>取消</Button>
              <Button type="submit" disabled={save.busy}>{save.busy ? "保存中…" : "确认保存"}</Button>
            </DialogFooter>
          </Form>
        </DialogContent>
      </Dialog>
    </Card>
  );
}
