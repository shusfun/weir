// 证书：全部证书一览与手动上传（高级选项）。节点证书通常由系统按 VPS 自动准备（见 node-certs.tsx）。私钥只进不出。
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Badge, Button, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Field, Form, Input, Notice, Segmented, Textarea, useConfirm } from "@weir/ui";
import { api } from "../api";
import { useFormAction, useRowAction } from "../lib/action";
import { date } from "../lib/models";
import type { Cert } from "../lib/core";
import { State } from "./feedback";
import { CertHealth, certSource } from "./node-certs";
import { dataObject } from "./object-link";
import { field, useConsoleContext } from "../lib/console-context";

export function useCerts() {
  return useQuery({ queryKey: ["certs"], queryFn: async () => (await api("/admin/certs")).items as Cert[] });
}

export function CertManager({ open, close }: { open: boolean; close: () => void }) {
  const confirm = useConfirm();
  const certs = useCerts();
  const [mode, setMode] = useState<"upload" | "self">("upload");
  const [domain, setDomain] = useState("");
  const [certPem, setCertPem] = useState("");
  const [keyPem, setKeyPem] = useState("");
  const { run, isBusy } = useRowAction<number>({ refetch: certs.refetch });
  const add = useFormAction(() => api("/admin/certs", "POST", { domain, self_signed: mode === "self", cert_pem: certPem, key_pem: keyPem }), {
    ok: "证书已添加",
    refetch: certs.refetch,
    fields: { invalid_domain: "domain", invalid_certificate: "cert" },
    onSuccess: () => {
      setDomain("");
      setCertPem("");
      setKeyPem("");
    },
  });
  useConsoleContext(open ? { dialog: { title: "证书" }, list: { total: certs.data?.length ?? 0, items: (certs.data ?? []).slice(0, 20).map((c) => ({ type: "cert", name: c.domain, id: c.id })) }, form: [field("mode", "添加方式", mode === "upload" ? "上传" : "自签"), field("domain", "域名", domain), field("cert_pem", "证书", certPem, true), field("key_pem", "私钥", keyPem, true)] } : null);
  return (
    <Dialog open={open} onOpenChange={(o) => !o && !add.busy && close()}>
      <DialogContent size="lg">
        <DialogTitle>证书</DialogTitle>
        <DialogDescription>建节点时证书由系统按 VPS 自动准备：Trojan、WebSocket + TLS 用 VPS 公网 IP 的证书，Hysteria2、TUIC 每台 VPS 共用一张自签。这里可以手动上传证书，在建节点的「高级」里选用。</DialogDescription>
        {certs.isPending || certs.error ? (
          <State loading={certs.isPending} error={certs.error} onRetry={() => certs.refetch()} />
        ) : certs.data.length === 0 ? (
          <State empty="还没有证书" />
        ) : (
          <ul className="stack" aria-label="证书列表">
            {certs.data.map((c) => (
              <li key={c.id} {...dataObject({ type: "cert", id: c.id })} className="flex flex-wrap items-center justify-between gap-2">
                <span className="min-w-0">
                  <strong>{c.domain}</strong> <Badge variant="soft" tone="neutral">{certSource[c.source] ?? c.source}</Badge> <CertHealth cert={c} />
                  <small className="block text-[var(--weir-ink-3)]">{c.not_after ? `有效期至 ${date(c.not_after)}` : "有效期未知"}</small>
                  {c.last_error && <small className="block text-[var(--weir-bad-ink)]">{c.last_error}</small>}
                </span>
                <Button
                  size="sm"
                  variant="destructive"
                  disabled={isBusy(c.id)}
                  onClick={async () => (await confirm({ title: `删除证书「${c.domain}」？`, description: "仍被节点使用的证书不能删除。", confirmText: "删除", tone: "danger" })) && run(c.id, () => api("/admin/certs/" + c.id, "DELETE"), "证书已删除")}
                >
                  删除
                </Button>
              </li>
            ))}
          </ul>
        )}
        <Form busy={add.busy} errors={add.errors} onSubmit={() => void add.submit()}>
          <Segmented aria-label="添加方式" value={mode} onValueChange={setMode} options={[["upload", "上传证书"], ["self", "生成自签"]] as const} />
          <Field label="域名" name="domain" hint={mode === "self" ? "可填 VPS 的 IP；客户端靠订阅中的指纹校验。" : "需与证书一致。"}>
            <Input required value={domain} onChange={(e) => setDomain(e.target.value)} />
          </Field>
          {mode === "upload" && (
            <>
              <Field label="证书（PEM，含中间证书）" name="cert">
                <Textarea required rows={4} className="font-mono text-xs" value={certPem} onChange={(e) => setCertPem(e.target.value)} />
              </Field>
              {/* 私钥是粘贴的多行 PEM，需要核对粘贴内容，因此不做遮罩；保存后服务端加密，不再返回 */}
              <Field label="私钥（PEM）" hint="加密保存，不会再显示。">
                <Textarea required rows={4} spellCheck={false} autoComplete="off" className="font-mono text-xs" value={keyPem} onChange={(e) => setKeyPem(e.target.value)} />
              </Field>
            </>
          )}
          <DialogFooter error={add.error}>
            <Button type="button" variant="outline" disabled={add.busy} onClick={close}>关闭</Button>
            <Button type="submit" disabled={add.busy} busy={add.busy} busyText="正在保存…">添加证书</Button>
          </DialogFooter>
        </Form>
      </DialogContent>
    </Dialog>
  );
}
