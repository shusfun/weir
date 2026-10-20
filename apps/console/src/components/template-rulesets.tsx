import { useId } from "react";
import {
  Button,
  Card,
  CardContent,
  Label,
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from "@migate/ui";
import { Plus, Trash2 } from "lucide-react";
import { Field } from "./form-field";
type Row = Record<string, any>;
const labels: Record<string, string> = {
  yaml: "YAML",
  text: "纯文本",
  source: "sing-box JSON",
  binary: "sing-box SRS",
  domain: "域名",
  ipcidr: "IP 网段",
  classical: "混合规则",
};
export function TemplateRuleSets({
  fields,
  change,
}: {
  fields: Row;
  change: (data: Row) => void;
}) {
  const id = useId(),
    sets: Row[] = fields.rule_sets ?? [],
    rules: Row[] = fields.rules ?? [],
    sing = fields.target === "singbox";
  function update(index: number, patch: Row) {
    change({
      ...fields,
      rule_sets: sets.map((s, i) => (i === index ? { ...s, ...patch } : s)),
    });
  }
  const select = (
    label: string,
    value: string,
    options: string[],
    set: (value: string) => void,
    key: string,
  ) => (
    <div className="field">
      <Label htmlFor={id + key}>{label}</Label>
      <Select value={value} onValueChange={(v) => v && set(String(v))}>
        <SelectTrigger id={id + key}>
          <SelectValue>{labels[value] ?? value}</SelectValue>
        </SelectTrigger>
        <SelectContent>
          {options.map((v) => (
            <SelectItem key={v} value={v}>
              {labels[v] ?? v}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
  return (
    <section className="stack">
      <div className="section-heading">
        <div>
          <h3>远程规则集</h3>
          <p>
            客户端通过 HTTPS
            下载，中心不获取规则文件。仅填写公开、不带凭据的地址；保存后在匹配规则中选择引用。
          </p>
        </div>
        <Button
          type="button"
          variant="outline"
          onClick={() =>
            change({
              ...fields,
              rule_sets: [
                ...sets,
                {
                  name: "rules-" + (sets.length + 1),
                  url: "",
                  format: sing ? "source" : "yaml",
                  ...(!sing ? { behavior: "domain" } : {}),
                  interval: 86400,
                },
              ],
            })
          }
        >
          <Plus size={14} />
          添加远程规则集
        </Button>
      </div>
      {sets.map((set, i) => {
        const used = rules.some(
          (r) => r.type === "RULE-SET" && r.value === set.name,
        );
        return (
          <Card key={i}>
            <CardContent className="stack">
              <div className="section-heading">
                <strong>远程规则集 {i + 1}</strong>
                <Button
                  type="button"
                  size="sm"
                  variant="ghost"
                  disabled={used}
                  aria-label={`删除远程规则集 ${i + 1}`}
                  onClick={() =>
                    change({
                      ...fields,
                      rule_sets: sets.filter((_, j) => i !== j),
                    })
                  }
                >
                  <Trash2 size={14} />
                  删除
                </Button>
              </div>
              <div className="template-form-grid">
                <Field
                  name={id + "name" + i}
                  label={`规则集 ${i + 1} 名称`}
                  value={set.name}
                  onChange={(name) =>
                    change({
                      ...fields,
                      rule_sets: sets.map((s, j) =>
                        i === j ? { ...s, name } : s,
                      ),
                      rules: rules.map((r) =>
                        r.type === "RULE-SET" && r.value === set.name
                          ? { ...r, value: name }
                          : r,
                      ),
                    })
                  }
                />
                {select(
                  `规则集 ${i + 1} 文件格式`,
                  set.format,
                  sing ? ["source", "binary"] : ["yaml", "text"],
                  (format) =>
                    update(i, {
                      format,
                      ...(sing
                        ? { behavior: undefined }
                        : { behavior: set.behavior || "domain" }),
                    }),
                  "format" + i,
                )}
                {!sing &&
                  select(
                    `规则集 ${i + 1} 内容类型`,
                    set.behavior ?? "domain",
                    ["domain", "ipcidr", "classical"],
                    (behavior) => update(i, { behavior }),
                    "behavior" + i,
                  )}
                <Field
                  name={id + "interval" + i}
                  label={`规则集 ${i + 1} 刷新间隔（小时）`}
                  type="number"
                  value={(set.interval ?? 86400) / 3600}
                  onChange={(value) =>
                    update(i, { interval: Number(value) * 3600 })
                  }
                />
              </div>
              <Field
                name={id + "url" + i}
                label={`规则集 ${i + 1} HTTPS 地址`}
                type="url"
                value={set.url}
                onChange={(url) => update(i, { url })}
              />
              <p>
                {used
                  ? "已被匹配规则引用；先移除引用才能删除。"
                  : "尚未引用：在下方添加“远程规则集”匹配规则。"}{" "}
                格式必须与实际远程文件一致。刷新范围 1–168 小时。
              </p>
            </CardContent>
          </Card>
        );
      })}
    </section>
  );
}
