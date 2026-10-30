// DiffView：逐行差异（新增绿、删除红，未变的长段落折叠成一行）。驱动草稿、驱动历史与保存前检查共用。
export interface DiffLine {
  op: " " | "+" | "-" | "…";
  text: string;
}

const lineClass: Record<string, string> = {
  "+": "bg-[var(--weir-ok-soft)] text-[var(--weir-ok-ink)]",
  "-": "bg-[var(--weir-bad-soft)] text-[var(--weir-bad-ink)]",
  "…": "text-[var(--weir-ink-3)]",
  " ": "",
};

export function DiffView({ lines, empty = "没有改动。" }: { lines: DiffLine[] | null | undefined; empty?: string }) {
  if (!lines || !lines.some((l) => l.op === "+" || l.op === "-")) return <small className="text-[var(--weir-ink-3)]">{empty}</small>;
  return (
    <pre className="max-h-80 overflow-auto rounded-[var(--weir-radius)] border border-[var(--weir-line)] bg-[var(--weir-well)] p-2 text-xs leading-5" aria-label="改动">
      {lines.map((l, i) => <div key={i} className={lineClass[l.op] ?? ""}>{(l.op === "…" ? "  " : l.op + " ") + l.text}</div>)}
    </pre>
  );
}
