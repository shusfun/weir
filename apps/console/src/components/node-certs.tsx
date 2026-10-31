// 节点证书：加 VPS 不需要加 DNS 解析。Trojan、WS + TLS 用 VPS 公网 IP 的证书（1Panel → 中心 ACME），
// 都不可用时用全项目共用的「节点证书域名」兜底；Hysteria2 / TUIC 每台 VPS 共用一张 10 年自签。
import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Badge, Button, Collapsible, Field, Form, FormGrid, Input, Notice, PasswordInput, SectionHeader, SwitchField } from "@weir/ui";
import { RefreshCw } from "lucide-react";
import { api } from "../api";
import { useFormAction, useRowAction } from "../lib/action";
import { ActionsWait, WaitText } from "./wait";
import { date } from "../lib/models";
import type { Cert, Gate } from "../lib/core";
import { State } from "./feedback";
import { emptyReauth, Reauth, Section, useReauthAction } from "./system-settings";
import { useCerts } from "./cert-manager";
import { dataObject } from "./object-link";

export const certSource: Record<string, string> = {
  upload: "手动上传",
  self_signed: "自签（订阅固定指纹）",
  acme: "IP 证书（中心签发）",
  panel: "IP 证书（1Panel）",
  edge: "节点证书域名",
};

const health: Record<string, [string, "ok" | "warn" | "bad" | "neutral"]> = {
  ready: ["正常", "ok"],
  renewing: ["待续签", "warn"],
  pending: ["申请中", "neutral"],
  failed: ["续签失败", "bad"],
  expired: ["已过期", "bad"],
  unknown: ["未知", "neutral"],
};

export function CertHealth({ cert }: { cert: Cert }) {
  const [label, tone] = health[cert.health ?? "unknown"] ?? health.unknown;
  return <Badge variant="soft" tone={tone}>{label}</Badge>;
}

// NodeCertCard：系统设置里的节点证书域名与 ACME 账户邮箱。
export function NodeCertCard() {
  const q = useQuery({ queryKey: ["node-certs"], queryFn: () => api("/admin/node-certs") as Promise<{ edge_domain: string; email: string; staging: boolean; center_origin: string }> });
  const [draft, setDraft] = useState<{ edge_domain: string; email: string; staging: boolean } | null>(null);
  const v = draft ?? q.data;
  const save = useFormAction(() => api("/admin/node-certs", "PUT", v), {
    ok: "已保存",
    fields: { invalid_edge_domain: "edge_domain", invalid_email: "email" },
    onSuccess: () => {
      setDraft(null);
      void q.refetch();
    },
  });
  const center = q.data?.center_origin ? new URL(q.data.center_origin).hostname : "中心";
  return (
    <Section
      title="节点证书"
      description="Trojan、WebSocket + TLS 节点默认用 VPS 公网 IP 的证书，不需要为每台 VPS 加解析：VPS 上有 1Panel 时经 1Panel 申请；否则 80 端口空闲时由中心向 Let's Encrypt 申请（约 6 天有效，到期前自动续签）。两者都不可用时，用下面的节点证书域名兜底。"
    >
      {q.isPending || q.error || !v ? <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} /> : (
        <Form busy={save.busy} errors={save.errors} onSubmit={() => void save.submit()}>
          <FormGrid>
            <Field label="节点证书域名（兜底）" name="edge_domain" hint={`例如 edge.你的域名；加一条解析指向${center}，并在反代里把它的 80 端口也转发到中心。`}>
              <Input autoComplete="off" placeholder="edge.example.com" value={v.edge_domain} onChange={(e) => setDraft({ ...v, edge_domain: e.target.value })} />
            </Field>
            <Field label="ACME 账户邮箱（可选）" name="email" hint="证书即将过期、签发方有通知时发到这里。">
              <Input type="email" value={v.email} onChange={(e) => setDraft({ ...v, email: e.target.value })} />
            </Field>
          </FormGrid>
          <SwitchField label="使用 Let's Encrypt 测试环境（签出的证书客户端不信任，只用于排查）" checked={v.staging} onCheckedChange={(on) => setDraft({ ...v, staging: on })} />
          <Notice tone="warn">
            节点证书域名是兜底方案：同一把私钥会分发到所有用到它的 VPS，其中一台泄露会影响全部；这个域名指向中心，会暴露节点与中心之间的关联。节点连接地址仍是各 VPS 的 IP，这个域名只作 SNI。
          </Notice>
          <div className="actions"><Button type="submit" disabled={save.busy} busy={save.busy} busyText="正在保存…">保存</Button></div>
          {save.error && <Notice tone="bad">{save.error}</Notice>}
        </Form>
      )}
    </Section>
  );
}

// GateCerts：VPS 详情里的证书与 1Panel。1Panel 的 API 密钥只交给这台 VPS，中心不保存。
export function GateCerts({ gate }: { gate: Gate }) {
  const certs = useCerts();
  const client = useQueryClient();
  const mine = (certs.data ?? []).filter((c) => c.gate_id === gate.id);
  const { run, isBusy, waitFor } = useRowAction<number | string>({ refetch: certs.refetch });
  const panel = gate.panel ?? {};
  const [port, setPort] = useState("");
  const [https, setHttps] = useState(false);
  const [key, setKey] = useState("");
  const [re, setRe] = useState(emptyReauth);
  const save = useReauthAction((track) => api(`/admin/gates/${gate.id}/panel`, "PUT", { port: Number(port), https, api_key: key, ...re }, track) as Promise<{ version?: string }>, {
    ok: "1Panel 已连接",
    wait: { label: `正在让 ${gate.name} 连接本机 1Panel 并保存密钥`, max: 60 },
    fields: { invalid_port: "panel_port", invalid_api_key: "panel_key", panel_failed: "panel_key" },
    onSuccess: () => {
      setKey("");
      setRe(emptyReauth);
      void client.invalidateQueries({ queryKey: ["gates"] });
    },
  });
  const panelState = panel.configured
    ? panel.error ? <Badge variant="soft" tone="bad">连接失败</Badge> : <Badge variant="soft" tone="ok">已连接{panel.version ? ` ${panel.version}` : ""}</Badge>
    : panel.detected ? <Badge variant="soft" tone="warn">已检测到，未填密钥</Badge> : <Badge variant="soft" tone="neutral">{gate.panel ? "未检测到" : "未知"}</Badge>;
  return (
    <>
      <SectionHeader title="证书" description="这台 VPS 上节点使用的证书。IP 证书约 6 天有效，剩余三分之一时自动续签；已过期或状态未知的节点不会写进订阅。" />
      {certs.isPending || certs.error ? <State loading={certs.isPending} error={certs.error} onRetry={() => certs.refetch()} /> : mine.length === 0 ? <small>还没有证书：建 Trojan、WebSocket + TLS 或 QUIC 节点时自动准备。</small> : (
        <ul className="stack">
          {mine.map((c) => (
            <li key={c.id} {...dataObject({ type: "cert", id: c.id })} className="rounded-[var(--weir-radius)] border border-[var(--weir-line)] p-3 text-sm">
              <div className="flex flex-wrap items-center gap-2">
                <strong>{c.domain}</strong>
                <Badge variant="soft" tone="neutral">{certSource[c.source] ?? c.source}</Badge>
                <CertHealth cert={c} />
                <small className="text-[var(--weir-ink-3)]">{waitFor(c.id) ? <WaitText wait={waitFor(c.id)} /> : c.not_after ? `有效期至 ${date(c.not_after)}` : "有效期未知"}</small>
                {c.source === "acme" && (
                  <Button className="ml-auto" size="xs" variant="outline" disabled={isBusy()} busy={isBusy(c.id)} busyText="正在续签…" onClick={() => void run(c.id, (track) => api(`/admin/certs/${c.id}/renew`, "POST", undefined, track), "已续签", { wait: { label: `正在向证书机构签发证书（${c.domain}）`, max: 150 } })}><RefreshCw />立即续签</Button>
                )}
              </div>
              {c.last_error && <small className="mt-1 block text-[var(--weir-bad-ink)]">{c.last_error}</small>}
            </li>
          ))}
        </ul>
      )}
      <SectionHeader title="1Panel" description="VPS 上装有 1Panel（v2.0.16 及以上）时，Gate 只经 127.0.0.1 调用它申请本机 IP 证书，由 1Panel 应答验证并自动续签。Gate 只调用证书接口，不改 1Panel 的网站配置。" actions={panelState} />
      {panel.error && <Notice tone="bad">{String(panel.error)}</Notice>}
      <Collapsible title={panel.configured ? "更换 1Panel API 密钥" : "填写 1Panel API 密钥"}>
        <Form busy={save.busy} errors={save.errors} onSubmit={() => void save.submit()}>
          <Notice tone="warn">1Panel 的 API 密钥不能限定权限，等同这台机器的 root。它只交给这台 VPS 保存（只有 root 可读），中心只转发一次，不落库、不写日志、不进审计详情。请在 1Panel 的 API 接口白名单里加入 127.0.0.1。</Notice>
          <FormGrid>
            <Field label="1Panel 端口" name="panel_port" required>
              <Input inputMode="numeric" autoComplete="off" data-1p-ignore data-lpignore="true" required value={port} onChange={(e) => setPort(e.target.value.replace(/\D/g, ""))} />
            </Field>
            <Field label="API 密钥" name="panel_key" required>
              <PasswordInput autoComplete="off" required value={key} onChange={(e) => setKey(e.target.value)} />
            </Field>
          </FormGrid>
          <SwitchField label="1Panel 使用 HTTPS" checked={https} onCheckedChange={setHttps} />
          <Reauth value={re} onChange={setRe} />
          <div className="actions">
            {panel.configured && (
              <Button type="button" variant="outline" disabled={isBusy("panel") || save.busy || !gate.online} busy={isBusy("panel")} busyText="正在移除…" onClick={() => void run("panel", () => api(`/admin/gates/${gate.id}/panel`, "DELETE"), "已移除 1Panel 密钥")}>移除密钥</Button>
            )}
            <Button type="submit" disabled={save.busy || !gate.online} busy={save.busy} busyText="正在测试连接…">测试并保存</Button>
            <ActionsWait wait={save.wait} />
          </div>
          {save.error && <Notice tone="bad">{save.error}</Notice>}
          {!gate.online && <small className="text-[var(--weir-ink-3)]">VPS 离线时无法填写。</small>}
        </Form>
      </Collapsible>
    </>
  );
}
