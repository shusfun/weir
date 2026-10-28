// 处理链编辑器：筛选 / 重命名 / 排序 / 去重 / 受限脚本，按顺序执行；可在后台预览处理前后的节点。
import { useState, useRef, useEffect } from "react";
import { ReorderButtons, Collapsible, Field, Notice, SectionHeader, Button, Input, Select, SelectTrigger, SelectValue, SelectContent, SelectItem, Switch, SwitchField, Textarea } from "@weir/ui";
import { Plus, Trash2 } from "lucide-react";
import { api } from "../api";
import { useFormAction } from "../lib/action";
import { OperatorChoices, PatternRules, SortFields, type ChoiceParams, type PatternRule, type SortKey, type SortParams } from "./operator-fields";

type Rules = { enabled?: boolean; rules?: PatternRule[] | string };
// 各类步骤的参数；未列出的兼容字段原样保留（高级参数里可见）
export type OperatorParams = {
  include?: Rules;
  exclude?: Rules;
  protocols?: ChoiceParams;
  regions?: ChoiceParams;
  regex?: Rules;
  template?: { enabled?: boolean; template?: string; indexScope?: string; scope?: string; offset?: number; indexStart?: number };
  keys?: SortKey[];
  nameIgnoreEmoji?: boolean;
  includeProtocol?: boolean;
  dsl?: unknown[];
  [extra: string]: unknown;
};
export interface Operator {
  type: string;
  enabled?: boolean;
  params: OperatorParams;
}

const names: Record<string, string> = {
  filter: "筛选节点",
  rename: "重命名",
  sort: "排序",
  dedup: "去重",
  script: "受限脚本",
};
const defaults: Record<string, OperatorParams> = {
  filter: {
    include: { enabled: true, rules: [""] },
    exclude: { enabled: false, rules: [] },
  },
  rename: { template: { enabled: true, template: "{region:zh}-{index:02}" } },
  sort: { keys: [{ key: "name", order: "asc" }] },
  dedup: { includeProtocol: true },
  script: { dsl: [] },
};
const scopes: Record<string, string> = { global: "全局", protocol: "按协议", region: "按地区", regionProtocol: "按地区与协议" };

export function OperatorEditor({ value, change }: { value: Operator[]; change: (v: Operator[]) => void }) {
  const [kind, setKind] = useState("filter");
  const [input, setInput] = useState("");
  const [output, setOutput] = useState("");
  const preview = useFormAction(
    () => api("/admin/misub/operators/preview", "POST", { links: input.split(/\r?\n/).filter(Boolean), operators: value }) as Promise<{ output?: string }>,
    { onSuccess: (r) => setOutput(r.output || "处理后为空") },
  );
  function set(i: number, patch: Partial<Operator>) {
    change(value.map((o, n) => (i === n ? { ...o, ...patch } : o)));
  }
  function setParam<K extends keyof OperatorParams>(i: number, op: Operator, key: K, next: OperatorParams[K]) {
    set(i, { params: { ...op.params, [key]: next } });
  }
  function move(i: number, n: number) {
    const a = [...value];
    [a[i], a[i + n]] = [a[i + n], a[i]];
    change(a);
  }
  return (
    <div className="stack">
      <div className="actions">
        <Select value={kind} onValueChange={(v) => setKind(v ?? "filter")}>
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
        <Button type="button" variant="outline" onClick={() => change([...value, { type: kind, enabled: true, params: structuredClone(defaults[kind]) }])}>
          <Plus size={14} />
          添加步骤
        </Button>
      </div>
      {value.map((op, i) => {
        const tpl = op.params.template ?? {};
        const scope = tpl.indexScope ?? tpl.scope ?? "region";
        return (
          <section className="operator-step" key={i}>
            <SectionHeader
              size="sm"
              flush
              title={<>{i + 1}. {names[op.type] ?? op.type}</>}
              actions={
                <div className="actions">
                  {/* 列表行内的独立开关，名称由 aria-label 给出 */}
                  <Switch aria-label={"启用步骤 " + (i + 1)} checked={op.enabled !== false} onCheckedChange={(v) => set(i, { enabled: v })} />
                  <ReorderButtons label={"步骤 " + (i + 1)} onUp={() => move(i, -1)} onDown={() => move(i, 1)} upDisabled={i === 0} downDisabled={i === value.length - 1} />
                  <Button type="button" variant="ghost" size="icon-sm" aria-label={"删除步骤 " + (i + 1)} onClick={() => change(value.filter((_, n) => n !== i))}>
                    <Trash2 size={14} />
                  </Button>
                </div>
              }
            />
            {op.type === "filter" && (
              <div className="stack">
                {(
                  [
                    ["include", "包含名称规则"],
                    ["exclude", "排除名称规则"],
                  ] as const
                ).map(([key, label]) => (
                  <div className="stack" key={key}>
                    <SwitchField label={"启用" + label} checked={op.params[key]?.enabled === true} onCheckedChange={(enabled) => setParam(i, op, key, { ...op.params[key], enabled })} />
                    <PatternRules label={label} value={op.params[key]?.rules} save={(rules) => setParam(i, op, key, { ...op.params[key], rules })} />
                  </div>
                ))}
                <OperatorChoices label="协议筛选" type="protocol" value={op.params.protocols ?? {}} save={(protocols) => setParam(i, op, "protocols", protocols)} />
                <OperatorChoices label="地区筛选" type="region" value={op.params.regions ?? {}} save={(regions) => setParam(i, op, "regions", regions)} />
              </div>
            )}
            {op.type === "rename" && (
              <div className="stack">
                <SwitchField label="启用正则替换" checked={op.params.regex?.enabled === true} onCheckedChange={(enabled) => setParam(i, op, "regex", { ...op.params.regex, enabled })} />
                <PatternRules label="重命名规则" value={op.params.regex?.rules} replacement save={(rules) => setParam(i, op, "regex", { ...op.params.regex, rules })} />
                <SwitchField label="启用名称模板" checked={tpl.enabled === true} onCheckedChange={(enabled) => setParam(i, op, "template", { ...tpl, enabled })} />
                <p className="muted">
                  可用字段：{"{name}、{originalName}、{cleanName}、{multiplier}、{group}、{provider}、{region:zh}、{index:02}"}。分组 / 来源仅用于处理，不增加用户授权；倍率是名称提示，不修改计费。
                </p>
                <Field label="名称模板">
                  <Input value={tpl.template ?? ""} onChange={(e) => setParam(i, op, "template", { ...tpl, template: e.target.value })} />
                </Field>
                <div className="grid gap-3 sm:grid-cols-2">
                  <Field label="编号范围">
                    <Select value={scope} onValueChange={(indexScope) => setParam(i, op, "template", { ...tpl, indexScope: String(indexScope) })}>
                      <SelectTrigger>
                        <SelectValue>{scopes[scope]}</SelectValue>
                      </SelectTrigger>
                      <SelectContent>
                        {Object.entries(scopes).map(([v, n]) => (
                          <SelectItem key={v} value={v}>
                            {n}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </Field>
                  <Field label="起始编号">
                    <Input type="number" value={tpl.offset ?? tpl.indexStart ?? 1} onChange={(e) => setParam(i, op, "template", { ...tpl, offset: Number(e.target.value) })} />
                  </Field>
                </div>
              </div>
            )}
            {op.type === "sort" && <SortFields value={op.params as SortParams} save={(params) => set(i, { params: params as OperatorParams })} />}
            {op.type === "dedup" && (
              <SwitchField label="去重键包含协议" checked={op.params.includeProtocol !== false} onCheckedChange={(v) => setParam(i, op, "includeProtocol", v)} />
            )}
            {op.type === "script" && <JsonField label="受限 DSL 指令数组" value={op.params.dsl ?? []} save={(v) => set(i, { params: { dsl: v as unknown[] } })} />}
            <Collapsible title="高级参数（保留兼容字段）">
              <JsonField label={"步骤 " + (i + 1) + " 参数"} value={op.params} save={(v) => set(i, { params: v as OperatorParams })} />
            </Collapsible>
          </section>
        );
      })}
      <Collapsible title="处理前后对照" className="pipeline-preview">
        <div className="stack">
          <p>仅在后台转换，不运行 JavaScript。结果包含节点凭据，勿公开截图。</p>
          <Field label="测试节点链接（每行一条）">
            <Textarea mono value={input} onChange={(e) => setInput(e.target.value)} rows={4} />
          </Field>
          <div className="actions">
            <Button
              type="button"
              variant="outline"
              disabled={preview.busy || !input.trim()}
              onClick={() => {
                setOutput("");
                void preview.submit();
              }}
            >
              {preview.busy ? "正在处理…" : "预览处理结果"}
            </Button>
          </div>
          {preview.error && <Notice tone="bad">{preview.error}</Notice>}
          {output && (
            <Field label="处理后结果">
              <Textarea mono readOnly value={output} rows={4} />
            </Field>
          )}
        </div>
      </Collapsible>
    </div>
  );
}

// JSON 参数编辑：解析通过才应用到步骤；未完成时作为字段错误显示，并阻止所在表单提交
function JsonField({ label, value, save }: { label: string; value: unknown; save: (v: object) => void }) {
  const [text, setText] = useState(JSON.stringify(value, null, 2));
  const [error, setError] = useState("");
  const committed = useRef(JSON.stringify(value));
  useEffect(() => {
    const next = JSON.stringify(value);
    if (next !== committed.current) {
      committed.current = next;
      setText(JSON.stringify(value, null, 2));
      setError("");
    }
  }, [value]);
  return (
    <Field label={label} customError={error}>
      <Textarea
        mono
        value={text}
        rows={5}
        onChange={(e) => {
          setText(e.target.value);
          try {
            const parsed: unknown = JSON.parse(e.target.value);
            if (!parsed || typeof parsed !== "object") throw Error("object required");
            committed.current = JSON.stringify(parsed);
            save(parsed);
            setError("");
          } catch {
            setError("JSON 尚未完整，未应用到此步骤");
          }
        }}
      />
    </Field>
  );
}
