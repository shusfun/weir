import { useId, useState } from "react";
import { Field, SectionHeader,
  Button,
  Input,
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
  Textarea,
} from "@migate/ui";

// Business composition of existing UIAble controls; MiSub RuleSection UX.
export function SourceFilters({
  fields,
  change,
}: {
  fields: Record<string, any>;
  change: (key: string, value: any) => void;
}) {
  const id = useId();
  const canVisualize = !["filter_include", "filter_exclude"].some((key) =>
    String(fields[key] ?? "")
      .split("\n")
      .some((line) => {
        const value = line.trim().toLowerCase();
        return (
          value &&
          (value === "---" ||
            (key === "filter_include"
              ? !value.startsWith("keep:")
              : value.startsWith("keep:")))
        );
      }),
  );
  const [mode, M] = useState("exclude"),
    [keyword, K] = useState(""),
    [protocol, P] = useState("ss"),
    [advanced, A] = useState(!canVisualize);
  const entries = (key: string) =>
    String(fields[key] ?? "")
      .split("\n")
      .filter(Boolean);
  function add(value: string) {
    const key = mode === "include" ? "filter_include" : "filter_exclude";
    if (entries(key).some((line) => line.trim() === "---")) {
      A(true);
      return;
    }
    const line = mode === "include" ? "keep:" + value : value;
    if (entries(key).includes(line)) return;
    change(key, [...entries(key), line].join("\n"));
  }
  return (
    <section className="stack source-filter-fields" aria-label="订阅源节点筛选">
      <SectionHeader level="h3" align="start" title="节点筛选" description="先保留，再排除；仅作用于此源。空规则保留全部。更改后直接用于组预览。" actions={<Button
          type="button"
          variant="ghost"
          disabled={advanced && !canVisualize}
          onClick={() => A(!advanced)}
        >
          {advanced ? "可视化编辑" : "高级规则"}
        </Button>} />
      {advanced && !canVisualize && (
        <p className="muted">
          当前含混合 / 分隔规则，请在高级模式编辑，避免改变原有顺序。
        </p>
      )}
      {advanced ? (
        <>
          <Field label="源保留规则" htmlFor={id + "include"}><Textarea
            id={id + "include"}
            value={fields.filter_include ?? ""}
            onChange={(e) => change("filter_include", e.target.value)}
          /></Field>
          <Field label="源排除规则" htmlFor={id + "exclude"}><Textarea
            id={id + "exclude"}
            value={fields.filter_exclude ?? ""}
            onChange={(e) => change("filter_exclude", e.target.value)}
          /></Field>
          <p className="muted">
            每行一个 RE2 名称表达式或 proto:ss,trojan。keep: 标记保留；---
            后为保留区。错误表达式会拒绝保存，不会忽略。
          </p>
        </>
      ) : (
        <>
          <Field label="筛选动作" htmlFor={id + "mode"}><Select value={mode} onValueChange={(v) => v && M(v)}>
            <SelectTrigger id={id + "mode"}>
              <SelectValue>
                {mode === "include" ? "仅保留匹配节点" : "排除匹配节点"}
              </SelectValue>
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="include">仅保留匹配节点</SelectItem>
              <SelectItem value="exclude">排除匹配节点</SelectItem>
            </SelectContent>
          </Select></Field>
          <Field label="节点名称关键词" htmlFor={id + "keyword"}><Input
            id={id + "keyword"}
            maxLength={100}
            value={keyword}
            onChange={(e) => K(e.target.value)}
            placeholder="例如 香港；按文字匹配，忽略大小写"
          /></Field>
          <div className="actions">
            <Button
              type="button"
              variant="outline"
              disabled={!keyword.trim()}
              onClick={() => {
                add(keyword.trim().replace(/[.*+?^${}()|[\]\\]/g, "\\$&"));
                K("");
              }}
            >
              添加关键词
            </Button>
            {["香港", "日本", "美国", "剩余流量", "到期时间"].map((value) => (
              <Button
                key={value}
                type="button"
                variant="ghost"
                size="sm"
                onClick={() => add(value)}
              >
                {value}
              </Button>
            ))}
          </div>
          <Field label="节点协议" htmlFor={id + "protocol"}><Select value={protocol} onValueChange={(v) => v && P(v)}>
            <SelectTrigger id={id + "protocol"}>
              <SelectValue>{protocol.toUpperCase()}</SelectValue>
            </SelectTrigger>
            <SelectContent>
              {[
                "ss",
                "vmess",
                "vless",
                "trojan",
                "hysteria2",
                "tuic",
                "anytls",
                "http",
                "https",
                "socks5",
              ].map((value) => (
                <SelectItem key={value} value={value}>
                  {value}
                </SelectItem>
              ))}
            </SelectContent>
          </Select></Field>
          <Button
            type="button"
            variant="outline"
            onClick={() => add("proto:" + protocol)}
          >
            添加协议规则
          </Button>
          {["filter_include", "filter_exclude"].map((key) => (
            <div className="stack" key={key}>
              <strong>
                {key === "filter_include" ? "保留规则" : "排除规则"}
              </strong>
              {entries(key).length ? (
                entries(key).map((line, index) => (
                  <div className="actions" key={index}>
                    <code className="source-filter-rule">
                      {line
                        .replace(/^keep:/i, "仅保留 · ")
                        .replace(/proto:/i, "协议 · ")}
                    </code>
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      aria-label={`删除${key === "filter_include" ? "保留" : "排除"}规则 ${index + 1}`}
                      onClick={() =>
                        change(
                          key,
                          entries(key)
                            .filter((_, i) => i !== index)
                            .join("\n"),
                        )
                      }
                    >
                      移除
                    </Button>
                  </div>
                ))
              ) : (
                <small className="muted">未设置</small>
              )}
            </div>
          ))}
        </>
      )}
    </section>
  );
}
