// 快速跳转（Ctrl/⌘ + K）：页面、设备、用户、订阅组按名称直达。只在打开时读取列表。
import { useEffect, useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useQuery } from "../lib/query";
import { PageState, ScrollArea, Dialog, DialogContent, DialogTitle, Kbd, Button } from "@weir/ui";
import { Search, Server, User, CornerDownLeft, type LucideIcon } from "lucide-react";
import { api } from "../api";
import { navigation, visibleFor } from "./navigation";
import { reopenDrafts } from "../components/draft-confirm";

interface Entry {
  id: string;
  group: string;
  label: string;
  hint?: string;
  to?: string;
  run?: () => void;
  icon: LucideIcon;
}

export function CommandPalette({ open, onOpenChange, admin }: { open: boolean; onOpenChange: (v: boolean) => void; admin: boolean }) {
  const navigate = useNavigate();
  const [query, setQuery] = useState("");
  const [active, setActive] = useState(0);
  // 快速跳转只要名称：查询键与列表页分开（列表页缓存的是数组，这里是 { items }，同一个键会互相覆盖、让列表页崩掉）
  const devices = useQuery({ queryKey: ["palette", "gates"], queryFn: async () => (await api("/admin/gates")) as { items: { id: number; name: string }[] }, enabled: open && admin });
  const users = useQuery({ queryKey: ["palette", "users"], queryFn: async () => (await api("/admin/users")) as { items: { id: number; email: string; name: string }[] }, enabled: open && admin });
  const drafts = useQuery({ queryKey: ["ai-drafts"], queryFn: async () => (await api("/admin/ai/drafts")).drafts as { id: string; explain: string; steps?: { title: string }[] | null }[], enabled: open && admin });

  useEffect(() => {
    if (open) {
      setQuery("");
      setActive(0);
    }
  }, [open]);

  const entries = useMemo(() => {
    const q = query.trim().toLowerCase();
    const actionsOnly = q.startsWith(">");
    const text = actionsOnly ? q.slice(1).trim() : q;
    const match = (value: string) => !text || value.toLowerCase().includes(text);
    const list: Entry[] = navigation
      .filter((item) => (admin || visibleFor(item, false)) && match(item.label))
      .map((item) => ({ id: "nav:" + item.key, group: "操作", label: item.label, hint: item.group, to: item.path, icon: item.icon }));
    if (!actionsOnly) {
      for (const d of devices.data?.items ?? [])
        if (match(d.name || "") || text === String(d.id))
          list.push({ id: "gate:" + d.id, group: "对象", label: d.name || "未命名 VPS", hint: "VPS", to: "/gates/" + d.id, icon: Server });
      for (const u of users.data?.items ?? [])
        if (text && (match(u.email) || match(u.name || "")))
          list.push({ id: "user:" + u.id, group: "对象", label: u.email, hint: u.name || "用户", to: "/users/" + u.id, icon: User });
      for (const draft of drafts.data ?? [])
        if (match(draft.explain || draft.id))
          list.push({ id: "draft:" + draft.id, group: "待处理", label: draft.steps?.[0]?.title || "确认草稿", hint: "运行观测", run: reopenDrafts, icon: Search });
    }
    return list.slice(0, 40);
  }, [query, admin, devices.data, users.data, drafts.data]);

  useEffect(() => setActive(0), [query]);

  function go(entry: Entry | undefined, newTab = false) {
    if (!entry) return;
    onOpenChange(false);
    entry.run?.();
    if (!entry.to) return;
    if (newTab) window.open(entry.to, "_blank", "noopener");
    else navigate(entry.to);
  }

  let lastGroup = "";
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent size="md" placement="palette" className="gap-0 p-0 max-sm:p-0" showCloseButton={false}>
        <DialogTitle className="sr-only">快速跳转</DialogTitle>
        <div className="command-search">
          <Search size={17} />
          <input
            autoFocus
            role="combobox"
            aria-expanded="true"
            aria-controls="command-list"
            aria-activedescendant={entries[active] ? "command-" + entries[active].id : undefined}
            aria-label="搜索页面、设备、用户或订阅组"
            placeholder="搜索、跳转或执行操作"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "ArrowDown") {
                e.preventDefault();
                setActive((v) => Math.min(entries.length - 1, v + 1));
              } else if (e.key === "ArrowUp") {
                e.preventDefault();
                setActive((v) => Math.max(0, v - 1));
              } else if (e.key === "Enter") {
                e.preventDefault();
                go(entries[active], e.metaKey || e.ctrlKey);
              }
            }}
          />
          <Kbd>Esc</Kbd>
        </div>
        {entries.length ? (
          <ScrollArea className="command-list-scroll">
          <ul className="command-list" id="command-list" role="listbox" aria-label="跳转结果">
            {entries.map((entry, index) => {
              const heading = entry.group !== lastGroup;
              lastGroup = entry.group;
              const Icon = entry.icon;
              return (
                <li key={entry.id} role="presentation">
                  {heading && <div className="command-group weir-engraved" role="presentation">{entry.group}</div>}
                  <div
                    id={"command-" + entry.id}
                    role="option"
                    aria-selected={index === active}
                    className="command-item"
                    onMouseEnter={() => setActive(index)}
                    onClick={() => go(entry)}
                  >
                    <Icon />
                    <span>{entry.label}</span>
                    {entry.hint && <small>{entry.hint}</small>}
                  </div>
                </li>
              );
            })}
          </ul>
          </ScrollArea>
        ) : users.error || devices.error ? (
          // 读不到 VPS 或用户列表时不能说「没有匹配项」
          <PageState
            size="sm"
            className="justify-center px-4 py-6"
            kind="error"
            title="搜索失败"
            detail={((users.error ?? devices.error) as Error).message}
            action={<Button size="sm" variant="outline" onClick={() => { void users.refetch(); void devices.refetch(); }}>重新加载</Button>}
          />
        ) : (
          <PageState
            size="sm"
            className="justify-center px-4 py-6"
            kind={users.isFetching || devices.isFetching ? "loading" : "empty"}
            title={users.isFetching || devices.isFetching ? "正在搜索…" : "没有匹配项"}
          />
        )}
        <div className="command-foot">
          <span><Kbd>↑</Kbd><Kbd>↓</Kbd> 选择</span>
          <span><Kbd><CornerDownLeft size={11} /></Kbd> 执行</span>
          <span>Ctrl 回车在新标签打开</span>
          <span>输入 &gt; 只搜操作</span>
        </div>
      </DialogContent>
    </Dialog>
  );
}
