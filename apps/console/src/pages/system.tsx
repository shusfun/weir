// 系统设置：AI 模型服务（Anthropic / OpenAI 协议，可自定义 Base URL；Laya 为其中一种）。Key 只写不读。
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Button, Card, CardContent, Field, Input, Notice, PageHeader, SectionHeader, Segmented, useToast } from "@migate/ui";
import { api } from "../api";
import { State } from "../components/feedback";
import { AnnouncementEditor } from "../components/global-announcement";
import { ClientCatalogEditor } from "../components/client-catalog";
import { ClientMirrorsEditor } from "../components/client-mirrors";
import { APITokensCard, BackupCard, NotifyCard, SecurityCard, SMTPCard } from "../components/system-settings";

interface ProviderView {
  configured: boolean;
  protocol: string;
  base_url: string;
  model: string;
  key_set: boolean;
}

const protocols = [["anthropic", "Anthropic"], ["openai", "OpenAI"], ["laya", "Laya"]] as const;
type Protocol = (typeof protocols)[number][0];
const examples: Record<Protocol, [string, string]> = {
  anthropic: ["https://api.anthropic.com", "claude-sonnet-5-5"],
  openai: ["https://api.openai.com/v1", "gpt-5"],
  laya: ["https://laya.example.com", "observer"],
};

export function SystemPage() {
  const q = useQuery({ queryKey: ["ai-provider"], queryFn: () => api("/admin/ai/provider") as Promise<ProviderView> });
  return (
    <div className="stack">
      <PageHeader eyebrow="系统" title="系统设置" />
      {q.isPending || q.error ? <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} /> : <AIProvider key={String(q.dataUpdatedAt)} value={q.data} saved={() => q.refetch()} />}
      <SecurityCard />
      <SMTPCard />
      <NotifyCard />
      <BackupCard />
      <APITokensCard />
      <Card><CardContent><AnnouncementEditor /></CardContent></Card>
      <Card><CardContent><ClientCatalogEditor /></CardContent></Card>
      <Card><CardContent><ClientMirrorsEditor /></CardContent></Card>
    </div>
  );
}

function AIProvider({ value, saved }: { value: ProviderView; saved: () => void }) {
  const toast = useToast();
  const [protocol, setProtocol] = useState<Protocol>((value.protocol as Protocol) || "anthropic");
  const [baseURL, setBaseURL] = useState(value.base_url);
  const [model, setModel] = useState(value.model);
  const [key, setKey] = useState("");
  const [password, setPassword] = useState("");
  const [code, setCode] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  async function run(fn: () => Promise<unknown>, ok: string) {
    setBusy(true);
    setError("");
    try {
      await fn();
      toast(ok, { tone: "ok" });
      setKey("");
      setPassword("");
      setCode("");
      saved();
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <Card>
      <CardContent className="stack">
        <SectionHeader title="AI 配置助手" description="用于节点调整草稿。模型只看到去掉密码、UUID 与密钥后的参数；模型不可用时不影响任何基础功能。" />
        {value.configured ? <Notice tone="ok">已配置：{value.protocol} · {value.model}</Notice> : <Notice role="status">尚未配置，节点上的「AI 调整」不可用。</Notice>}
        <form className="stack" onSubmit={(e) => { e.preventDefault(); void run(() => api("/admin/ai/provider", "PUT", { protocol, base_url: baseURL, model, key, password, code }), "已保存"); }}>
          <Segmented aria-label="接口协议" value={protocol} onValueChange={setProtocol} options={protocols} />
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label="Base URL" htmlFor="ai-base" hint="可填兼容该协议的网关地址。"><Input id="ai-base" required value={baseURL} placeholder={examples[protocol][0]} onChange={(e) => setBaseURL(e.target.value)} /></Field>
            <Field label="模型" htmlFor="ai-model"><Input id="ai-model" required value={model} placeholder={examples[protocol][1]} onChange={(e) => setModel(e.target.value)} /></Field>
          </div>
          <Field label="API Key" htmlFor="ai-key" hint={value.key_set ? "已保存；留空则沿用。" : "加密保存，不会再显示。"}>
            <Input id="ai-key" type="password" autoComplete="off" required={!value.key_set} value={key} onChange={(e) => setKey(e.target.value)} />
          </Field>
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label="当前密码" htmlFor="ai-pass" hint="修改模型服务需要验证身份。"><Input id="ai-pass" type="password" autoComplete="current-password" required value={password} onChange={(e) => setPassword(e.target.value)} /></Field>
            <Field label="二次验证码（如已开启）" htmlFor="ai-code"><Input id="ai-code" inputMode="numeric" autoComplete="one-time-code" value={code} onChange={(e) => setCode(e.target.value)} /></Field>
          </div>
          {error && <Notice tone="bad">{error}</Notice>}
          <div className="actions">
            <Button type="submit" disabled={busy}>{busy ? "正在保存…" : "保存"}</Button>
            {value.configured && <Button type="button" variant="destructive" disabled={busy} onClick={() => window.confirm("删除模型服务配置？") && run(() => api("/admin/ai/provider", "DELETE"), "已删除")}>删除配置</Button>}
          </div>
        </form>
      </CardContent>
    </Card>
  );
}
