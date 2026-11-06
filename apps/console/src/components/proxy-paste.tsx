import { useState, type CSSProperties } from "react";
import { Button, Field, Textarea } from "@weir/ui";
import { parseExitText, type ParsedProxy } from "../lib/exit-parse";

// 原文可能含完整凭据，只保留在组件内存，不注册到控制台上下文。
export function ProxyPaste({ onApply }: { onApply: (proxy: ParsedProxy) => void }) {
  const [raw, setRaw] = useState("");
  const [visible, setVisible] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const apply = (text: string) => {
    if (!text.trim()) { setError(""); return; }
    const r = parseExitText(text);
    if (r.type !== "proxy") {
      setError(r.type === "link" ? "这是一条节点链接，请改选「节点链接」" : r.type === "subscription" ? "这是订阅地址，不能作出口" : r.message);
      setNotice("");
      return;
    }
    onApply(r.proxy);
    setRaw("");
    setVisible(false);
    setError("");
    setNotice("已填入下方，可直接修改。" + r.notice);
  };
  return (
    <Field label="粘贴代理信息（可选）" error={error} customError={error} hint="支持整段供应商说明、地址:端口:用户名:密码、空格分隔及代理 URL。一次粘贴一个代理。">
      <Textarea aria-label="粘贴代理信息" rows={3} autoComplete="off" spellCheck={false} value={raw}
        style={{ WebkitTextSecurity: visible ? "none" : "disc" } as CSSProperties}
        placeholder="粘贴后点“解析并填入”"
        onChange={e => { setRaw(e.target.value); setError(""); setNotice(""); }} />
      <div className="flex flex-wrap items-center gap-2">
        <Button type="button" size="sm" disabled={!raw.trim()} onClick={() => apply(raw)}>解析并填入</Button>
        <Button type="button" variant="ghost" size="sm" aria-pressed={visible} onClick={() => setVisible(v => !v)}>{visible ? "隐藏原文" : "显示原文"}</Button>
        <span role="status" className="text-[var(--weir-ink-3)]">{notice}</span>
      </div>
    </Field>
  );
}
