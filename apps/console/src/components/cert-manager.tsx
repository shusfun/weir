// 证书：上传域名证书或生成自签证书（QUIC 节点用指纹固定）。私钥只进不出。
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Badge, Button, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Field, Input, Notice, Segmented, Textarea } from "@weir/ui";
import { api } from "../api";
import { date } from "../lib/models";
import type { Cert } from "../lib/core";
import { State } from "./feedback";

export function useCerts() {
  return useQuery({ queryKey: ["certs"], queryFn: async () => (await api("/admin/certs")).items as Cert[] });
}

export function CertManager({ open, close }: { open: boolean; close: () => void }) {
  const certs = useCerts();
  const [mode, setMode] = useState<"upload" | "self">("upload");
  const [domain, setDomain] = useState("");
  const [certPem, setCertPem] = useState("");
  const [keyPem, setKeyPem] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  async function run(fn: () => Promise<unknown>) {
    setBusy(true);
    setError("");
    try {
      await fn();
      await certs.refetch();
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  }
  const submit = () =>
    run(async () => {
      await api("/admin/certs", "POST", { domain, self_signed: mode === "self", cert_pem: certPem, key_pem: keyPem });
      setDomain("");
      setCertPem("");
      setKeyPem("");
    });
  return (
    <Dialog open={open} onOpenChange={(o) => !o && !busy && close()}>
      <DialogContent size="lg">
        <DialogTitle>证书</DialogTitle>
        <DialogDescription>WebSocket / Trojan 节点需要域名证书；Hysteria2、TUIC 未选证书时会自动自签。</DialogDescription>
        {certs.isPending || certs.error ? (
          <State loading={certs.isPending} error={certs.error} onRetry={() => certs.refetch()} />
        ) : certs.data.length === 0 ? (
          <State empty="还没有证书" />
        ) : (
          <ul className="stack" aria-label="证书列表">
            {certs.data.map((c) => (
              <li key={c.id} className="flex flex-wrap items-center justify-between gap-2">
                <span className="min-w-0">
                  <strong>{c.domain}</strong> <Badge variant="soft" tone={c.source === "upload" ? "ok" : "neutral"}>{c.source === "upload" ? "上传" : c.source === "acme" ? "自动签发" : "自签"}</Badge>
                  <small className="block text-[var(--mg-ink-3)]">有效期至 {date(c.not_after)}</small>
                </span>
                <Button size="sm" variant="destructive" disabled={busy} onClick={() => run(() => api("/admin/certs/" + c.id, "DELETE"))}>删除</Button>
              </li>
            ))}
          </ul>
        )}
        <form className="stack" onSubmit={(e) => { e.preventDefault(); void submit(); }}>
          <Segmented aria-label="添加方式" value={mode} onValueChange={setMode} options={[["upload", "上传证书"], ["self", "生成自签"]] as const} />
          <Field label="域名" htmlFor="cert-domain" hint={mode === "self" ? "可填 VPS 的 IP；客户端靠订阅中的指纹校验。" : "需与证书一致。"}>
            <Input id="cert-domain" required value={domain} onChange={(e) => setDomain(e.target.value)} />
          </Field>
          {mode === "upload" && (
            <>
              <Field label="证书（PEM，含中间证书）" htmlFor="cert-pem">
                <Textarea id="cert-pem" required rows={4} className="font-mono text-xs" value={certPem} onChange={(e) => setCertPem(e.target.value)} />
              </Field>
              <Field label="私钥（PEM）" htmlFor="cert-key" hint="加密保存，不会再显示。">
                <Textarea id="cert-key" required rows={4} className="font-mono text-xs" value={keyPem} onChange={(e) => setKeyPem(e.target.value)} />
              </Field>
            </>
          )}
          {error && <Notice tone="bad">{error}</Notice>}
          <DialogFooter>
            <Button type="button" variant="outline" disabled={busy} onClick={close}>关闭</Button>
            <Button type="submit" disabled={busy}>{busy ? "正在保存…" : "添加证书"}</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
