// 安装包加速镜像：管理员维护前缀列表（如 https://mirror.example/），保存需重新验证；
// 用户浏览器把 GitHub 下载地址拼在前缀后测速，下载后仍按 SHA-256 校验。
import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Field, Notice, SectionHeader, Button, Card, CardContent, Dialog, DialogContent, DialogTitle, DialogDescription, DialogFooter, Input, Textarea } from "@weir/ui";
import { Save } from "lucide-react";
import { api } from "../api";
import { State, ConflictNotice } from "./feedback";

interface Mirrors { mirrors: string[]; revision: string }

export function ClientMirrorsEditor() {
  const cache = useQueryClient();
  const query = useQuery({ queryKey: ["client-mirrors"], queryFn: () => api("/client-mirrors") as Promise<Mirrors> });
  const self = useQuery({ queryKey: ["self-account"], queryFn: () => api("/me") });
  const [draft, setDraft] = useState("");
  const [confirm, setConfirm] = useState(false);
  const [password, setPassword] = useState("");
  const [code, setCode] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [notice, setNotice] = useState("");
  useEffect(() => {
    if (query.data) setDraft(query.data.mirrors.join("\n"));
  }, [query.data?.revision]);
  if (query.isPending) return <State loading />;
  if (query.error) return <State error={query.error} onRetry={() => query.refetch()} />;
  const list = draft.split("\n").map((v) => v.trim()).filter(Boolean);
  const dirty = list.join("\n") !== query.data.mirrors.join("\n");

  async function save() {
    setBusy(true);
    setError(null);
    try {
      await api("/admin/client-mirrors", "PUT", { mirrors: list, revision: query.data!.revision, password, code });
      setConfirm(false);
      setPassword("");
      setCode("");
      setNotice("加速镜像已保存。");
      await cache.invalidateQueries({ queryKey: ["client-mirrors"] });
    } catch (e) {
      setError(e);
      setCode("");
    } finally {
      setBusy(false);
    }
  }

  return (
    <Card>
      <CardContent className="stack">
        <SectionHeader flush level="h3" title="安装包加速镜像" description="每行一个前缀，最多 8 个，须为 HTTPS 并以 / 结尾。用户下载时会和 GitHub 直连一起测速；镜像内容仍按 GitHub 公布的 SHA-256 校验，不一致会被拒绝。" />
        <Textarea aria-label="加速镜像前缀" rows={4} value={draft} placeholder="https://mirror.example/" onChange={(e) => { setDraft(e.target.value); setNotice(""); }} />
        {notice && <Notice role="status">{notice}</Notice>}
        <div className="actions" style={{ justifyContent: "flex-end" }}>
          <Button disabled={!dirty} onClick={() => { setError(null); setConfirm(true); }}><Save />保存镜像</Button>
        </div>
      </CardContent>
      <Dialog open={confirm} onOpenChange={(v) => !busy && setConfirm(v)}>
        <DialogContent size="md">
          <DialogTitle>确认保存加速镜像</DialogTitle>
          <DialogDescription>共 {list.length} 个镜像。验证管理员身份后保存；其他管理员已修改时会拒绝覆盖。</DialogDescription>
          <form className="stack" onSubmit={(e) => { e.preventDefault(); void save(); }}>
            <Field label="管理员当前密码" htmlFor="mirror-password">
              <Input id="mirror-password" type="password" autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} required />
            </Field>
            {self.data?.totp_enrolled && (
              <Field label="二次验证码或恢复码" htmlFor="mirror-code">
                <Input id="mirror-code" autoComplete="one-time-code" value={code} onChange={(e) => setCode(e.target.value)} required />
              </Field>
            )}
            <ConflictNotice error={error} onReload={() => { setConfirm(false); void query.refetch(); }} />
            {!!error && !(error as { conflict?: boolean }).conflict && <Notice tone="bad">{error instanceof Error ? error.message : "保存失败"}</Notice>}
            <DialogFooter>
              <Button type="button" variant="outline" disabled={busy} onClick={() => setConfirm(false)}>取消</Button>
              <Button type="submit" disabled={busy}>{busy ? "保存中…" : "确认保存"}</Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </Card>
  );
}
