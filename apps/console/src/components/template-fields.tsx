import { useId, useState } from "react";
import {
  Button,
  Card,
  CardContent,
  Checkbox,
  Label,
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
  Dialog,
  DialogContent,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@migate/ui";
import { ArrowUp, ArrowDown, Trash2, Plus } from "lucide-react";
import { TemplateRuleSets } from "./template-rulesets";
import { Field } from "./form-field";
type Row = Record<string, any>;
const typeLabels: Record<string, string> = {
  clash: "Clash / Mihomo",
  singbox: "sing-box",
  select: "手动选择",
  "url-test": "自动测速",
  fallback: "故障转移",
  "load-balance": "负载均衡",
  DOMAIN: "完整域名",
  "DOMAIN-SUFFIX": "域名后缀",
  "DOMAIN-KEYWORD": "域名关键词",
  "IP-CIDR": "IPv4 网段",
  "IP-CIDR6": "IPv6 网段",
  FINAL: "最终策略",
  "RULE-SET": "远程规则集",
};
export function OutputTemplateFields({
  fields,
  change,
}: {
  fields: Row;
  change: (data: Row) => void;
}) {
  const id = useId();
  const [preset, PS] = useState<string | null>(null);
  const groups: Row[] = Array.isArray(fields.groups) ? fields.groups : [];
  const rules: Row[] = Array.isArray(fields.rules) ? fields.rules : [];
  const select = (
    label: string,
    value: string,
    options: string[],
    set: (v: string) => void,
    key: string,
  ) => (
    <div className="field">
      <Label htmlFor={id + key}>{label}</Label>
      <Select value={value} onValueChange={(v) => v && set(String(v))}>
        <SelectTrigger id={id + key}>
          <SelectValue>{typeLabels[value] ?? value}</SelectValue>
        </SelectTrigger>
        <SelectContent>
          {options.map((v) => (
            <SelectItem key={v} value={v}>
              {typeLabels[v] ?? v}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
  const update = (key: string, index: number, patch: Row) =>
    change({
      ...fields,
      [key]: fields[key].map((v: Row, i: number) =>
        i === index ? { ...v, ...patch } : v,
      ),
    });
  const remove = (key: string, index: number) =>
    change({
      ...fields,
      [key]: fields[key].filter((_: Row, i: number) => i !== index),
    });
  const move = (i: number, delta: number) => {
    const next = [...rules];
    [next[i], next[i + delta]] = [next[i + delta], next[i]];
    change({ ...fields, rules: next });
  };
  function applyPreset() {
    const auto = preset === "auto",
      bucket = preset === "region" || preset === "protocol";
    change({
      ...fields,
      auto_groups: bucket ? preset : "",
      groups: [
        ...(auto
          ? [{ name: "AUTO", type: "url-test", members: ["$nodes"] }]
          : []),
        {
          name: "PROXY",
          type: "select",
          members: bucket
            ? ["$auto", "$nodes", "DIRECT"]
            : auto
              ? ["AUTO", "$nodes", "DIRECT"]
              : ["$nodes", "DIRECT"],
        },
      ],
      rules: [{ type: "FINAL", policy: "PROXY" }],
    });
    PS(null);
  }
  return (
    <div className="template-fields stack">
      <div className="template-settings-row">
        {select(
          "输出格式",
          fields.target ?? "clash",
          ["clash", "singbox"],
          (target) => change({ ...fields, target }),
          "target",
        )}
        <div className="actions">
          <Button type="button" variant="outline" onClick={() => PS("manual")}>
            手动选择预设
          </Button>
          <Button type="button" variant="outline" onClick={() => PS("auto")}>
            自动测速预设
          </Button>
          <Button type="button" variant="outline" onClick={() => PS("region")}>
            地区分组预设
          </Button>
          <Button
            type="button"
            variant="outline"
            onClick={() => PS("protocol")}
          >
            协议分组预设
          </Button>
        </div>
      </div>
      {fields.auto_groups && (
        <p className="notice">
          已启用{fields.auto_groups === "region" ? "地区" : "协议"}
          自动分组，仅使用当前订阅节点。地区由节点名称 /
          服务器关键词识别，不代表实际地理位置；应用其他预设可取消。
        </p>
      )}
      <section className="stack">
        <div className="section-heading">
          <div>
            <h3>策略组</h3>
            <p>组合当前已授权节点与其他策略组。模板不能增加节点权限。</p>
          </div>
          <Button
            type="button"
            variant="outline"
            onClick={() =>
              change({
                ...fields,
                groups: [
                  ...groups,
                  {
                    name: "Group-" + (groups.length + 1),
                    type: "select",
                    members: ["$nodes"],
                  },
                ],
              })
            }
          >
            <Plus size={14} />
            添加策略组
          </Button>
        </div>
        {groups.map((g, i) => {
          const allowed = [
            "$nodes",
            ...(fields.auto_groups && g.type === "select" ? ["$auto"] : []),
            ...(g.type === "select"
              ? ["DIRECT", ...(fields.target !== "singbox" ? ["REJECT"] : [])]
              : []),
            ...groups.filter((_, j) => j !== i).map((v) => v.name),
          ];
          const options = [...new Set([...allowed, ...(g.members ?? [])])];
          return (
            <Card key={i} className="template-group">
              <CardContent>
                <div className="section-heading">
                  <strong>策略组 {i + 1}</strong>
                  <Button
                    size="sm"
                    type="button"
                    variant="ghost"
                    onClick={() => remove("groups", i)}
                    aria-label={`删除策略组 ${i + 1}`}
                  >
                    <Trash2 size={14} />
                    删除
                  </Button>
                </div>
                <div className="template-form-grid">
                  <Field
                    name={id + "group" + i}
                    label={`策略组 ${i + 1} 名称`}
                    value={g.name}
                    onChange={(name) =>
                      change({
                        ...fields,
                        groups: groups.map((item, j) =>
                          j === i
                            ? { ...item, name }
                            : {
                                ...item,
                                members: (item.members ?? []).map(
                                  (member: string) =>
                                    member === g.name ? name : member,
                                ),
                              },
                        ),
                        rules: rules.map((rule) =>
                          rule.policy === g.name
                            ? { ...rule, policy: name }
                            : rule,
                        ),
                      })
                    }
                  />
                  {select(
                    `策略组 ${i + 1} 类型`,
                    g.type,
                    [
                      "select",
                      "url-test",
                      ...(fields.target === "clash"
                        ? ["fallback", "load-balance"]
                        : []),
                    ],
                    (type) =>
                      update("groups", i, {
                        type,
                        members:
                          type === "select"
                            ? g.members
                            : (g.members ?? []).filter(
                                (m: string) =>
                                  m !== "DIRECT" &&
                                  m !== "REJECT" &&
                                  m !== "direct" &&
                                  m !== "$auto",
                              ),
                      }),
                    "type" + i,
                  )}
                </div>
                <fieldset className="template-member-options">
                  <legend>策略组 {i + 1} 成员</legend>
                  {options.map((member) => (
                    <Label key={member} className="selector-option">
                      <Checkbox
                        checked={(g.members ?? []).includes(member)}
                        onCheckedChange={(checked) =>
                          update("groups", i, {
                            members: checked
                              ? [...(g.members ?? []), member]
                              : (g.members ?? []).filter(
                                  (v: string) => v !== member,
                                ),
                          })
                        }
                      />
                      <span>
                        {member === "$nodes"
                          ? "当前订阅节点"
                          : member === "$auto"
                            ? "自动生成的策略组"
                            : member === "DIRECT"
                              ? "直连"
                              : member === "REJECT"
                                ? "拒绝连接"
                                : member}
                        {!allowed.includes(member) ? "（引用已失效）" : ""}
                      </span>
                    </Label>
                  ))}
                </fieldset>
                <Field
                  required={false}
                  name={id + "filter" + i}
                  label={`策略组 ${i + 1} 节点名称正则`}
                  value={g.filter ?? ""}
                  onChange={(filter) => update("groups", i, { filter })}
                />
              </CardContent>
            </Card>
          );
        })}
      </section>
      <TemplateRuleSets fields={fields} change={change} />
      <section className="stack">
        <div className="section-heading">
          <div>
            <h3>匹配规则</h3>
            <p>从上到下匹配；最终策略保持在末尾。</p>
          </div>
          <Button
            type="button"
            variant="outline"
            onClick={() => {
              const next = [...rules];
              const n = next.findIndex((v) => v.type === "FINAL");
              next.splice(n < 0 ? next.length : n, 0, {
                type: "DOMAIN-SUFFIX",
                value: "example.org",
                policy: "DIRECT",
              });
              change({ ...fields, rules: next });
            }}
          >
            <Plus size={14} />
            添加规则
          </Button>
        </div>
        {rules.map((r, i) => (
          <Card key={i} className="template-rule">
            <CardContent>
              <div className="section-heading">
                <strong>规则 {i + 1}</strong>
                <div className="actions">
                  <Button
                    size="sm"
                    type="button"
                    variant="ghost"
                    disabled={i === 0 || r.type === "FINAL"}
                    aria-label={`上移规则 ${i + 1}`}
                    onClick={() => move(i, -1)}
                  >
                    <ArrowUp size={14} />
                  </Button>
                  <Button
                    size="sm"
                    type="button"
                    variant="ghost"
                    disabled={
                      i >= rules.length - 1 ||
                      r.type === "FINAL" ||
                      rules[i + 1]?.type === "FINAL"
                    }
                    aria-label={`下移规则 ${i + 1}`}
                    onClick={() => move(i, 1)}
                  >
                    <ArrowDown size={14} />
                  </Button>
                  <Button
                    size="sm"
                    type="button"
                    variant="ghost"
                    disabled={r.type === "FINAL"}
                    aria-label={`删除规则 ${i + 1}`}
                    onClick={() => remove("rules", i)}
                  >
                    <Trash2 size={14} />
                  </Button>
                </div>
              </div>
              <div className="template-form-grid">
                {r.type === "FINAL" ? (
                  <div className="field">
                    <Label>规则 {i + 1} 类型</Label>
                    <strong>最终策略</strong>
                  </div>
                ) : (
                  select(
                    `规则 ${i + 1} 类型`,
                    r.type,
                    [
                      "DOMAIN",
                      "DOMAIN-SUFFIX",
                      "DOMAIN-KEYWORD",
                      "IP-CIDR",
                      "IP-CIDR6",
                      "RULE-SET",
                    ],
                    (type) =>
                      update("rules", i, {
                        type,
                        value:
                          type === "RULE-SET"
                            ? ((fields.rule_sets ?? [])[0]?.name ?? "")
                            : r.type === "RULE-SET"
                              ? ""
                              : r.value,
                      }),
                    "rule" + i,
                  )
                )}
                {select(
                  `规则 ${i + 1} 策略`,
                  r.policy,
                  ["DIRECT", "REJECT", ...groups.map((g) => g.name)],
                  (policy) => update("rules", i, { policy }),
                  "policy" + i,
                )}
                {r.type === "RULE-SET"
                  ? select(
                      `规则 ${i + 1} 规则集`,
                      r.value ?? "",
                      (fields.rule_sets ?? []).map((s: Row) => s.name),
                      (value) => update("rules", i, { value }),
                      "ruleset" + i,
                    )
                  : r.type !== "FINAL" && (
                      <Field
                        name={id + "value" + i}
                        label={`规则 ${i + 1} 匹配值`}
                        value={r.value ?? ""}
                        onChange={(value) => update("rules", i, { value })}
                      />
                    )}
              </div>
            </CardContent>
          </Card>
        ))}
      </section>
      <section className="template-dns stack">
        <div>
          <h3>DNS 与加密解析</h3>
          <p>由客户端解析；中心不会请求这些地址，TLS 证书始终验证。</p>
        </div>
        <Field
          required={false}
          name={id + "dns"}
          label="DNS 服务器（逗号分隔；IP、https:// 或 tls://）"
          value={(fields.dns ?? []).join(",")}
          onChange={(value) =>
            change({
              ...fields,
              dns: value
                .split(",")
                .map((v) => v.trim())
                .filter(Boolean),
            })
          }
        />
        <Field
          required={false}
          name={id + "bootstrap"}
          label="引导 DNS IP（加密 DNS 使用域名时必填，逗号分隔）"
          value={(fields.dns_bootstrap ?? []).join(",")}
          onChange={(value) =>
            change({
              ...fields,
              dns_bootstrap: value
                .split(",")
                .map((v) => v.trim())
                .filter(Boolean),
            })
          }
        />
        <p>
          DoH：https://1.1.1.1/dns-query；DoT：tls://1.1.1.1。留空沿用客户端默认值；sing-box
          使用第一个引导 IP。
        </p>
      </section>
      <Dialog open={!!preset} onOpenChange={(v) => !v && PS(null)}>
        <DialogContent>
          <DialogTitle>替换策略组与规则</DialogTitle>
          <DialogDescription>
            应用
            {preset === "region"
              ? "地区分组"
              : preset === "protocol"
                ? "协议分组"
                : preset === "auto"
                  ? "自动测速"
                  : "手动选择"}
            预设会替换当前策略组和匹配规则。名称、输出格式和 DNS
            设置保留，保存模板后才生效。
          </DialogDescription>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => PS(null)}>
              取消
            </Button>
            <Button type="button" onClick={applyPreset}>
              应用预设
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
