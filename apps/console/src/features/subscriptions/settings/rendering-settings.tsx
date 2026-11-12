import { Button, Field, FormGrid, Input, Segmented, SwitchField, Textarea } from "@weir/ui";
import { Plus, Trash2 } from "lucide-react";
import type { RenderingSettings, RegionOverride } from "../../../lib/subscriptions";

export const renderingErrorFields = {
	invalid_filter_include: "filter_include",
	invalid_filter_exclude: "filter_exclude",
  invalid_dns_mode: "dns_mode",
  invalid_custom_dns: "custom_dns",
  invalid_region_overrides: "region_overrides",
};

export function RenderingSettingsFields({ value, change, inherit = false }: {
  value: RenderingSettings;
  change: (patch: Partial<RenderingSettings>) => void;
  inherit?: boolean;
}) {
  const rules = value.region_overrides ?? [];
  const updateRule = (index: number, patch: Partial<RegionOverride>) =>
    change({ region_overrides: rules.map((rule, i) => i === index ? { ...rule, ...patch } : rule) });
  return (
    <section className="stack" data-tour="subscription-rendering">
      <FormGrid>
        {([
          ["enable_udp", "输出中开启 UDP"],
          ["enable_tfo", "输出中开启 TFO"],
          ["skip_cert_verify", "跳过证书校验（不推荐）"],
        ] as const).map(([key, label]) => inherit ? (
          <Field key={key} label={label}>
            <Segmented aria-label={label}
              value={value[key] === undefined ? "inherit" : value[key] ? "on" : "off"}
              options={[["inherit", "默认"], ["on", "开"], ["off", "关"]] as const}
              onValueChange={(v) => change({ [key]: v === "inherit" ? undefined : v === "on" })} />
          </Field>
        ) : (
          <SwitchField key={key} label={label} checked={!!value[key]}
            onCheckedChange={(v) => change({ [key]: v })} />
        ))}
        <Field label="DNS 环境" name="dns_mode">
          <Segmented aria-label="DNS 环境" value={value.dns_mode || (inherit ? "inherit" : "clean")}
            options={inherit
              ? [["inherit", "默认"], ["clean", "干净"], ["polluted", "受污染"]]
              : [["clean", "干净"], ["polluted", "受污染"]]}
            onValueChange={(v) => change({ dns_mode: v === "inherit" ? undefined : v })} />
        </Field>
      </FormGrid>
      <Field label="自定义 DNS" name="custom_dns"
        hint="填写 YAML 对象，仅受支持的安全字段会进入输出；留空使用所选 DNS 环境的默认值。">
        {inherit && <SwitchField label="覆盖默认 DNS 配置" checked={value.custom_dns !== undefined}
          onCheckedChange={(on) => change({ custom_dns: on ? "" : undefined })} />}
        {(!inherit || value.custom_dns !== undefined) && (
          <Textarea aria-label="自定义 DNS YAML" mono rows={5} maxLength={32768}
            value={value.custom_dns ?? ""} onChange={(e) => change({ custom_dns: e.target.value })} />
        )}
      </Field>
      <Field label="地区覆盖规则" name="region_overrides"
        hint="按顺序匹配节点名称，首条命中生效。使用 RE2 正则；标志支持 i、m、s，默认 i。最多 64 条。">
        {inherit && <SwitchField label="覆盖默认地区规则" checked={value.region_overrides !== undefined}
          onCheckedChange={(on) => change({ region_overrides: on ? [] : undefined })} />}
        {(!inherit || value.region_overrides !== undefined) && <div className="stack">
          {rules.map((rule, index) => <div key={index} className="stack">
            <FormGrid>
              <Field label={`规则 ${index + 1} · 名称正则`}>
                <Input required maxLength={512} value={rule.pattern}
                  onChange={(e) => updateRule(index, { pattern: e.target.value })} />
              </Field>
              <Field label={`规则 ${index + 1} · 地区`}>
                <Input required maxLength={80} value={rule.region}
                  onChange={(e) => updateRule(index, { region: e.target.value })} />
              </Field>
              <Field label={`规则 ${index + 1} · 标志`}>
                <Input maxLength={3} pattern="[ims]*" value={rule.flags ?? "i"}
                  onChange={(e) => updateRule(index, { flags: e.target.value })} />
              </Field>
            </FormGrid>
            <Button type="button" variant="outline" size="sm" aria-label={`删除地区规则 ${index + 1}`}
              onClick={() => change({ region_overrides: rules.filter((_, i) => i !== index) })}>
              <Trash2 />删除规则 {index + 1}
            </Button>
          </div>)}
          <Button type="button" variant="outline" size="sm" disabled={rules.length >= 64}
            onClick={() => change({ region_overrides: [...rules, { pattern: "", region: "", flags: "i" }] })}>
            <Plus />添加地区规则
          </Button>
        </div>}
      </Field>
    </section>
  );
}
