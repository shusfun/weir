import { useState } from "react";
import { Button, CheckboxField, Collapsible, Field, Form, FormGrid, Input, Notice, PasswordInput, SwitchField } from "@weir/ui";
import { api } from "../../../api";
import { useQuery } from "../../../lib/query";
import { useFormAction } from "../../../lib/action";
import { field, useConsoleContext } from "../../../lib/console-context";
import { useDraft, useUnsavedChanges } from "../../../lib/dirty";
import { State } from "../../../components/feedback";

interface ConverterSettings {
  enabled: boolean;
  endpoint: string;
  authorization_set: boolean;
  preferred_gate_id: number;
  max_bytes: number;
}

export function ConverterSettingsPanel() {
  const q = useQuery({ queryKey: ["subscription-converter"], queryFn: () => api("/admin/subscriptions/converter") as Promise<ConverterSettings> });
  return <Collapsible title="外部转换后端">
    {q.isPending || q.error ? <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} /> : <ConverterForm value={q.data} saved={() => void q.refetch()} />}
  </Collapsible>;
}

function ConverterForm({ value, saved }: { value: ConverterSettings; saved: () => void }) {
  const draft = useDraft(value);
  const cfg = draft.value, setCfg = draft.set;
  const [authorization, setAuthorization] = useState("");
  const [clear, setClear] = useState(false);
  const change = (patch: Partial<ConverterSettings>) => setCfg((old) => ({ ...old, ...patch }));
  useUnsavedChanges(authorization !== "" || clear);
  useConsoleContext({ tab: "外部转换", form: [field("endpoint", "转换后端", cfg.endpoint), field("authorization", "后端授权值", "", true)] });
  const save = useFormAction(() => api("/admin/subscriptions/converter", "PUT", {
    enabled: cfg.enabled, endpoint: cfg.endpoint.trim(), preferred_gate_id: cfg.preferred_gate_id,
    max_bytes: cfg.max_bytes, authorization, clear_authorization: clear,
  }), { ok: "已保存外部转换后端", fields: { converter_endpoint_invalid: "endpoint", converter_gate_invalid: "preferred_gate_id", converter_settings_invalid: "max_bytes" }, onSuccess: () => { draft.commit(); setAuthorization(""); setClear(false); saved(); } });
  return <Form busy={save.busy} errors={save.errors} onSubmit={() => void save.submit()}>
    <SwitchField label="启用外部转换" checked={cfg.enabled} onCheckedChange={(enabled) => change({ enabled })} />
    {cfg.enabled && <>
      <Notice tone="warn">选择外部引擎的订阅会把处理后的节点（包括节点密码和 UUID）、目标格式及远程模板地址交给你配置的后端。请使用可信的 subconverter / FatSheep 兼容后端；客户端由中心直接返回结果。</Notice>
      <Field name="endpoint" label="完整转换端点" hint="例如你自己的 HTTPS /sub 地址，不带查询参数；不预置公共后端。"><Input required type="url" value={cfg.endpoint} onChange={(e) => change({ endpoint: e.target.value })} /></Field>
      <Field name="authorization" label="Authorization（Secret）" hint={value.authorization_set ? "已设置；留空保留原值。" : "可选；输入完整 Bearer 或 Basic 授权值。"}><PasswordInput value={authorization} disabled={clear} autoComplete="new-password" onChange={(e) => setAuthorization(e.target.value)} /></Field>
      {value.authorization_set && <CheckboxField label="移除已保存的授权值" checked={clear} onCheckedChange={(next) => { setClear(next); if (next) setAuthorization(""); }} />}
      <FormGrid>
        <Field name="preferred_gate_id" label="优先 VPS ID" hint="0 表示由中心调度；不可用时按候选顺序改派。"><Input type="number" min={0} step={1} value={cfg.preferred_gate_id} onChange={(e) => change({ preferred_gate_id: Number(e.target.value) })} /></Field>
        <Field name="max_bytes" label="最大输出字节" hint="1 KiB–10 MiB；传输和解压均受限。"><Input type="number" min={1024} max={10485760} step={1} value={cfg.max_bytes} onChange={(e) => change({ max_bytes: Number(e.target.value) })} /></Field>
      </FormGrid>
      <Notice>在默认设置或订阅组中选择「外部」。远程 ini 模板受支持；无模板时规则等级须为「不加规则」。本地模板、DNS 覆盖、地区覆盖及不支持的格式会明确报错。内联转换输入上限为 64 KiB 请求地址，超限不回退。</Notice>
    </>}
    <div className="actions"><Button type="submit" busy={save.busy} busyText="正在保存…">保存转换后端</Button></div>
    {save.error && <Notice tone="bad">{save.error}</Notice>}
  </Form>;
}
