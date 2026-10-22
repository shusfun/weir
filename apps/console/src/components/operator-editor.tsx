import { useState, useRef, useEffect } from "react";
import { ReorderButtons, Field, Notice, SectionHeader,
  Button,
  Input,
  Label,
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
  Switch,
  Textarea,
} from "@migate/ui";
import { Plus, Trash2 } from "lucide-react";
import { api } from "../api";
import { OperatorChoices, PatternRules, SortFields } from "./operator-fields";
export interface Operator {
  type: string;
  enabled?: boolean;
  params: Record<string, any>;
}
const names: Record<string, string> = {
  filter: "筛选节点",
  rename: "重命名",
  sort: "排序",
  dedup: "去重",
  script: "受限脚本",
};
const defaults: Record<string, Record<string, any>> = {
  filter: {
    include: { enabled: true, rules: [""] },
    exclude: { enabled: false, rules: [] },
  },
  rename: { template: { enabled: true, template: "{region:zh}-{index:02}" } },
  sort: { keys: [{ key: "name", order: "asc" }] },
  dedup: { includeProtocol: true },
  script: { dsl: [] },
};
export function OperatorEditor({
  value,
  change,
}: {
  value: Operator[];
  change: (v: Operator[]) => void;
}) {
  const [kind, K] = useState("filter");
  const [input, I] = useState("");
  const [output, O] = useState("");
  const [error, E] = useState("");
  const [busy, B] = useState(false);
  function set(i: number, patch: Partial<Operator>) {
    change(value.map((o, n) => (i === n ? { ...o, ...patch } : o)));
  }
  function move(i: number, n: number) {
    const a = [...value];
    [a[i], a[i + n]] = [a[i + n], a[i]];
    change(a);
  }
  const field = (label: string, content: string, save: (v: string) => void) => (
    <Field label={label}>
      <Input
        aria-label={label}
        value={content}
        onChange={(e) => save(e.target.value)}
      />
    </Field>
  );
  return (
    <div className="stack">
      <div className="actions">
        <Select value={kind} onValueChange={(v) => K(v ?? "filter")}>
          <SelectTrigger aria-label="添加处理步骤">
            <SelectValue>{names[kind]}</SelectValue>
          </SelectTrigger>
          <SelectContent>
            {Object.entries(names).map(([v, n]) => (
              <SelectItem value={v} key={v}>
                {n}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Button
          type="button"
          variant="outline"
          onClick={() =>
            change([
              ...value,
              {
                type: kind,
                enabled: true,
                params: structuredClone(defaults[kind]),
              },
            ])
          }
        >
          <Plus size={14} />
          添加步骤
        </Button>
      </div>
      {value.map((op, i) => (
        <section className="operator-step" key={i}>
          <SectionHeader size="sm" flush title={<>{i + 1}. {names[op.type] ?? op.type}</>} actions={<div className="actions">
              <Switch
                aria-label={"启用步骤 " + (i + 1)}
                checked={op.enabled !== false}
                onCheckedChange={(v) => set(i, { enabled: v })}
              />
              <ReorderButtons label={"步骤 " + (i + 1)} onUp={() => move(i, -1)} onDown={() => move(i, 1)} upDisabled={i === 0} downDisabled={i === value.length - 1} />
              <Button
                type="button"
                variant="ghost"
                size="icon-sm"
                aria-label={"删除步骤 " + (i + 1)}
                onClick={() => change(value.filter((_, n) => n !== i))}
              >
                <Trash2 size={14} />
              </Button>
            </div>} />
          {op.type === "filter" && (
            <div className="stack">
              {[
                ["include", "包含名称规则"],
                ["exclude", "排除名称规则"],
              ].map(([key, label]) => (
                <div className="stack" key={key}>
                  <Label className="selector-option">
                    <Switch
                      checked={op.params[key]?.enabled === true}
                      onCheckedChange={(enabled) =>
                        set(i, {
                          params: {
                            ...op.params,
                            [key]: { ...op.params[key], enabled },
                          },
                        })
                      }
                    />
                    启用{label}
                  </Label>
                  <PatternRules
                    label={label}
                    value={op.params[key]?.rules}
                    save={(rules) =>
                      set(i, {
                        params: {
                          ...op.params,
                          [key]: { ...op.params[key], rules },
                        },
                      })
                    }
                  />
                </div>
              ))}
              <OperatorChoices
                label="协议筛选"
                type="protocol"
                value={op.params.protocols ?? {}}
                save={(protocols) =>
                  set(i, { params: { ...op.params, protocols } })
                }
              />
              <OperatorChoices
                label="地区筛选"
                type="region"
                value={op.params.regions ?? {}}
                save={(regions) =>
                  set(i, { params: { ...op.params, regions } })
                }
              />
            </div>
          )}
          {op.type === "rename" && (
            <div className="stack">
              <Label className="selector-option">
                <Switch
                  checked={op.params.regex?.enabled === true}
                  onCheckedChange={(enabled) =>
                    set(i, {
                      params: {
                        ...op.params,
                        regex: { ...op.params.regex, enabled },
                      },
                    })
                  }
                />
                启用正则替换
              </Label>
              <PatternRules
                label="重命名规则"
                value={op.params.regex?.rules}
                replacement
                save={(rules) =>
                  set(i, {
                    params: {
                      ...op.params,
                      regex: { ...op.params.regex, rules },
                    },
                  })
                }
              />
              <Label className="selector-option">
                <Switch
                  checked={op.params.template?.enabled === true}
                  onCheckedChange={(enabled) =>
                    set(i, {
                      params: {
                        ...op.params,
                        template: { ...op.params.template, enabled },
                      },
                    })
                  }
                />
                启用名称模板
              </Label>
              <p className="muted">可用字段：{"{name}、{originalName}、{cleanName}、{multiplier}、{group}、{provider}、{region:zh}、{index:02}"}。分组 / 来源仅用于处理，不增加用户授权；倍率是名称提示，不修改计费。</p>
              {field(
                "名称模板",
                op.params.template?.template ?? "",
                (template) =>
                  set(i, {
                    params: {
                      ...op.params,
                      template: { ...op.params.template, template },
                    },
                  }),
              )}
              <Select
                value={
                  op.params.template?.indexScope ??
                  op.params.template?.scope ??
                  "region"
                }
                onValueChange={(indexScope) =>
                  set(i, {
                    params: {
                      ...op.params,
                      template: { ...op.params.template, indexScope },
                    },
                  })
                }
              >
                <SelectTrigger className="min-w-[100px] flex-1" aria-label="重命名编号范围">
                  <SelectValue>
                    {
                      (
                        {
                          global: "全局",
                          protocol: "按协议",
                          region: "按地区",
                          regionProtocol: "按地区与协议",
                        } as Record<string, string>
                      )[
                        op.params.template?.indexScope ??
                          op.params.template?.scope ??
                          "region"
                      ]
                    }
                  </SelectValue>
                </SelectTrigger>
                <SelectContent>
                  {[
                    ["global", "全局"],
                    ["protocol", "按协议"],
                    ["region", "按地区"],
                    ["regionProtocol", "按地区与协议"],
                  ].map(([v, n]) => (
                    <SelectItem key={v} value={v}>
                      {n}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Label>起始编号</Label>
              <Input
                aria-label="重命名起始编号"
                type="number"
                value={
                  op.params.template?.offset ??
                  op.params.template?.indexStart ??
                  1
                }
                onChange={(e) =>
                  set(i, {
                    params: {
                      ...op.params,
                      template: {
                        ...op.params.template,
                        offset: Number(e.target.value),
                      },
                    },
                  })
                }
              />
            </div>
          )}
          {op.type === "sort" && (
            <SortFields
              value={op.params}
              save={(params) => set(i, { params })}
            />
          )}
          {op.type === "dedup" && (
            <Label className="selector-option">
              <Switch
                checked={op.params.includeProtocol !== false}
                onCheckedChange={(v) =>
                  set(i, { params: { ...op.params, includeProtocol: v } })
                }
              />
              去重键包含协议
            </Label>
          )}
          {op.type === "script" && (
            <JsonField
              label="受限 DSL 指令数组"
              value={op.params.dsl ?? []}
              save={(v) => set(i, { params: { dsl: v } })}
            />
          )}
          <details>
            <summary>高级参数（保留兼容字段）</summary>
            <JsonField
              label={"步骤 " + (i + 1) + " 参数"}
              value={op.params}
              save={(v) => set(i, { params: v })}
            />
          </details>
        </section>
      ))}
      <details className="pipeline-preview">
        <summary>处理前后对照</summary>
        <p>仅在后台转换，不运行 JavaScript。结果包含节点凭据，勿公开截图。</p>
        <Field label="测试节点链接（每行一条）" htmlFor="pipeline-input"><Textarea
          id="pipeline-input"
          mono
          value={input}
          onChange={(e) => I(e.target.value)}
          rows={4}
        /></Field>
        <Button
          type="button"
          variant="outline"
          disabled={busy || !input.trim()}
          onClick={async () => {
            B(true);
            E("");
            O("");
            try {
              const r = await api("/admin/subscriptions/preview", "POST", {
                links: input.split(/\r?\n/).filter(Boolean),
                operators: value,
                target: "links",
              });
              O(r.output || "处理后为空");
            } catch (e) {
              E(String(e));
            } finally {
              B(false);
            }
          }}
        >
          {busy ? "正在处理…" : "预览处理结果"}
        </Button>
        {error && <Notice tone="bad">{error}</Notice>}
        {output && (
          <Textarea
            mono
            aria-label="处理后结果"
            readOnly
            value={output}
            rows={4}
          />
        )}
      </details>
    </div>
  );
}
function JsonField({
  label,
  value,
  save,
}: {
  label: string;
  value: any;
  save: (v: any) => void;
}) {
  const [text, T] = useState(JSON.stringify(value, null, 2));
  const [error, E] = useState("");
  const committed = useRef(JSON.stringify(value));
  useEffect(() => {
    const next = JSON.stringify(value);
    if (next !== committed.current) {
      committed.current = next;
      T(JSON.stringify(value, null, 2));
      E("");
    }
  }, [value]);
  return (
    <Field label={label}>
      <Textarea
        aria-label={label}
        value={text}
        onChange={(e) => {
          T(e.target.value);
          try {
            const parsed = JSON.parse(e.target.value);
            if (!parsed || typeof parsed !== "object")
              throw Error("object required");
            committed.current = JSON.stringify(parsed);
            save(parsed);
            e.target.setCustomValidity("");
            E("");
          } catch {
            e.target.setCustomValidity("请修正 JSON 后再保存");
            E("JSON 尚未完整，未应用到此步骤");
          }
        }}
        rows={5}
      />
      {error && <Notice tone="bad">{error}</Notice>}
    </Field>
  );
}
