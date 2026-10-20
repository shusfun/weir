import { useState } from "react";
import {
  Button,
  Checkbox,
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
import { ArrowUp, ArrowDown, Plus, Trash2 } from "lucide-react";
import { regionOptions } from "../lib/region-options";
type Data = Record<string, any>;
export const protocols = [
  ["vless", "VLESS"],
  ["vmess", "VMess"],
  ["trojan", "Trojan"],
  ["ss", "Shadowsocks"],
  ["ssr", "ShadowsocksR"],
  ["hysteria2", "Hysteria2"],
  ["tuic", "TUIC"],
  ["anytls", "AnyTLS"],
  ["socks5", "SOCKS5"],
  ["http", "HTTP"],
];
const regions = regionOptions;
export function OperatorChoices({
  label,
  value,
  save,
  type,
}: {
  label: string;
  value: Data;
  save: (v: Data) => void;
  type: "protocol" | "region";
}) {
  const [search, S] = useState("");
  const values: string[] = value.values ?? [];
  const defaults = type === "protocol" ? protocols : regions;
  const options = [
    ...defaults,
    ...values
      .filter((v) => !defaults.some(([key]) => key === v))
      .map((v) => [v, v]),
  ];
  return (
    <fieldset className="resource-selector">
      <legend>{label}</legend>
      <Label className="selector-option">
        <Switch
          checked={value.enabled === true}
          onCheckedChange={(enabled) => save({ ...value, enabled })}
        />
        启用{label}
      </Label>
      <Input
        aria-label={"搜索" + label}
        placeholder="搜索选项…"
        value={search}
        onChange={(e) => S(e.target.value)}
      />
      <div className="operator-choice-grid">
        {options
          .filter(([key, name]) =>
            (key + name).toLowerCase().includes(search.toLowerCase()),
          )
          .map(([key, name]) => (
            <Label className="selector-option" key={key}>
              <Checkbox
                checked={values.includes(key)}
                onCheckedChange={(checked) =>
                  save({
                    ...value,
                    values: checked
                      ? [...values, key]
                      : values.filter((v) => v !== key),
                  })
                }
              />
              {name}
            </Label>
          ))}
      </div>
      <small>已选 {values.length} 项。启用但未勾选时不保留任何节点。</small>
    </fieldset>
  );
}
export function PatternRules({
  label,
  value,
  save,
  replacement = false,
}: {
  label: string;
  value: unknown;
  save: (v: any[]) => void;
  replacement?: boolean;
}) {
  const rules: any[] =
    typeof value === "string"
      ? value.split(/\r?\n/).filter(Boolean)
      : Array.isArray(value)
        ? value
        : [];
  const patch = (index: number, key: string, next: string) =>
    save(
      rules.map((r, i) =>
        i === index
          ? { ...(typeof r === "string" ? { pattern: r } : r), [key]: next }
          : r,
      ),
    );
  return (
    <fieldset className="resource-selector">
      <legend>{label}</legend>
      <div className="stack">
        {rules.map((r, i) => {
          const row = typeof r === "string" ? { pattern: r } : r;
          return (
            <div className="operator-step" key={i}>
              <Input
                aria-label={`${label} ${i + 1} 匹配模式`}
                placeholder="RE2 正则表达式"
                value={row.pattern ?? ""}
                onChange={(e) => patch(i, "pattern", e.target.value)}
              />
              <Input
                aria-label={`${label} ${i + 1} 标志`}
                placeholder={replacement ? "gi" : "i"}
                value={row.flags ?? ""}
                onChange={(e) => patch(i, "flags", e.target.value)}
              />
              {replacement && (
                <Input
                  aria-label={`${label} ${i + 1} 替换文本`}
                  placeholder="$1 / $& / 模板字段"
                  value={row.replacement ?? ""}
                  onChange={(e) => patch(i, "replacement", e.target.value)}
                />
              )}
              <Button
                type="button"
                size="sm"
                variant="ghost"
                aria-label={`删除${label} ${i + 1}`}
                onClick={() => save(rules.filter((_, n) => n !== i))}
              >
                <Trash2 size={14} />
                删除规则
              </Button>
            </div>
          );
        })}
        <Button
          type="button"
          variant="outline"
          onClick={() =>
            save([
              ...rules,
              {
                pattern: "",
                flags: replacement ? "gi" : "i",
                ...(replacement ? { replacement: "" } : {}),
              },
            ])
          }
        >
          <Plus size={14} />
          添加{label}
        </Button>
        <small>
          逗号属于正则内容，不用作分隔符。服务器使用受限
          RE2；不支持的表达式会明确拒绝。
        </small>
      </div>
    </fieldset>
  );
}
const sortFields = [
  ["name", "名称"],
  ["server", "服务器"],
  ["port", "端口"],
  ["protocol", "协议"],
  ["region", "地区"],
];
export function SortFields({
  value,
  save,
}: {
  value: Data;
  save: (v: Data) => void;
}) {
  const keys: Data[] = value.keys ?? [];
  const change = (index: number, patch: Data) =>
    save({
      ...value,
      keys: keys.map((k, i) => (i === index ? { ...k, ...patch } : k)),
    });
  const move = (i: number, offset: number) => {
    const next = [...keys];
    [next[i], next[i + offset]] = [next[i + offset], next[i]];
    save({ ...value, keys: next });
  };
  return (
    <div className="stack">
      <Label className="selector-option">
        <Switch
          checked={value.nameIgnoreEmoji !== false}
          onCheckedChange={(nameIgnoreEmoji) =>
            save({ ...value, nameIgnoreEmoji })
          }
        />
        名称排序忽略国旗
      </Label>
      {keys.map((key, i) => (
        <fieldset className="resource-selector" key={i}>
          <legend>排序优先级 {i + 1}</legend>
          <div className="actions">
            <Select
              value={key.key}
              onValueChange={(v) => change(i, { key: v })}
            >
              <SelectTrigger aria-label={`排序字段 ${i + 1}`}>
                <SelectValue>
                  {sortFields.find(([k]) => k === key.key)?.[1] ?? key.key}
                </SelectValue>
              </SelectTrigger>
              <SelectContent>
                {sortFields.map(([v, name]) => (
                  <SelectItem value={v} key={v}>
                    {name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Select
              value={key.order ?? "asc"}
              onValueChange={(v) => change(i, { order: v })}
            >
              <SelectTrigger aria-label={`排序方向 ${i + 1}`}>
                <SelectValue>
                  {key.order === "desc" ? "降序" : "升序"}
                </SelectValue>
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="asc">升序</SelectItem>
                <SelectItem value="desc">降序</SelectItem>
              </SelectContent>
            </Select>
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              aria-label={`上移排序字段 ${i + 1}`}
              disabled={i === 0}
              onClick={() => move(i, -1)}
            >
              <ArrowUp size={14} />
            </Button>
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              aria-label={`下移排序字段 ${i + 1}`}
              disabled={i === keys.length - 1}
              onClick={() => move(i, 1)}
            >
              <ArrowDown size={14} />
            </Button>
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              aria-label={`删除排序字段 ${i + 1}`}
              onClick={() =>
                save({ ...value, keys: keys.filter((_, n) => n !== i) })
              }
            >
              <Trash2 size={14} />
            </Button>
          </div>
          <Label>自定义优先顺序（每行一项，留空使用默认）</Label>
          <Textarea
            aria-label={`自定义排序 ${i + 1}`}
            rows={3}
            value={(key.customOrder ?? []).join("\n")}
            onChange={(e) =>
              change(i, {
                customOrder: e.target.value.split(/\r?\n/).filter(Boolean),
              })
            }
          />
        </fieldset>
      ))}
      <Button
        type="button"
        variant="outline"
        disabled={keys.length >= 32}
        onClick={() =>
          save({ ...value, keys: [...keys, { key: "name", order: "asc" }] })
        }
      >
        <Plus size={14} />
        添加排序字段
      </Button>
    </div>
  );
}
