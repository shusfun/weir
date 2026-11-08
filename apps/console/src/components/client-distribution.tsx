import { Meter } from "@weir/ui";
import { ClientIcon } from "./client-icon";

export function ClientDistribution({ clients }: { clients: { name: string; count: number }[] }) {
  const sorted = [...clients].sort((a,b) => b.count - a.count || a.name.localeCompare(b.name));
  const total = sorted.reduce((n,c) => n+c.count,0);
  if (!total) return <p className="text-meta text-[var(--weir-ink-2)]">还没有拉取记录</p>;
  const rest = sorted.slice(5).reduce((n,c) => n+c.count,0);
  const rows = sorted.slice(0,5);
  if (rest) rows.push({ name: "其他", count: rest });
  return <ul className="stack gap-2" aria-label="客户端分布">{rows.map((c) => <li key={c.name} className="min-w-0">
    <div className="mb-1 flex items-center gap-2 text-meta"><ClientIcon icon={/android|rocket|loon|stash|egern/i.test(c.name) ? "phone" : "desktop"} /><span className="min-w-0 flex-1 truncate">{c.name || "未识别"}</span><span className="tabular-nums">{c.count} 次 · {Math.round(c.count/total*100)}%</span></div>
    <Meter label={`${c.name || "未识别"}占比`} value={c.count} max={total} />
  </li>)}</ul>;
}
