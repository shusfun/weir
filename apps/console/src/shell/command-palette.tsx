// 快速跳转（Ctrl/⌘ + K）：页面、设备、用户、订阅组按名称直达。只在打开时读取列表。
import { useEffect, useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { PageState, Dialog, DialogContent, DialogTitle, Kbd } from "@migate/ui";
import { Search, Server, User, CornerDownLeft, type LucideIcon } from "lucide-react";
import { api } from "../api";
import { navigation } from "./navigation";

interface Entry {
  id: string;
  group: string;
  label: string;
  hint?: string;
  to: string;
  icon: LucideIcon;
}

export function CommandPalette({ open, onOpenChange, admin }: { open: boolean; onOpenChange: (v: boolean) => void; admin: boolean }) {
  const navigate = useNavigate();
  const [query, setQuery] = useState("");
  const [active, setActive] = useState(0);
  const devices = useQuery({ queryKey: ["gates"], queryFn: async () => (await api("/admin/gates")) as { items: { id: number; name: string }[] }, enabled: open && admin });
  const users = useQuery({ queryKey: ["users"], queryFn: async () => (await api("/admin/users")) as { items: { id: number; email: string; name: string }[] }, enabled: open && admin });

  useEffect(() => {
    if (open) {
      setQuery("");
      setActive(0);
    }
  }, [open]);

  const entries = useMemo(() => {
    const q = query.trim().toLowerCase();
    const hit = (text: string) => !q || text.toLowerCase().includes(q);
    const list: Entry[] = navigation
      .filter((item) => (admin || !item.admin) && hit(item.label))
      .map((item) => ({ id: "nav:" + item.key, group: "页面", label: item.label, hint: item.group, to: item.path, icon: item.icon }));
    for (const d of devices.data?.items ?? [])
      if (hit(d.name || "") || q === String(d.id))
        list.push({ id: "gate:" + d.id, group: "VPS", label: d.name || "未命名 VPS", hint: "#" + d.id, to: "/gates", icon: Server });
    for (const u of users.data?.items ?? [])
      if (q && (hit(u.email) || hit(u.name || "")))
        list.push({ id: "user:" + u.id, group: "用户", label: u.email, hint: u.name || "#" + u.id, to: "/users", icon: User });
    return list.slice(0, 40);
  }, [query, admin, devices.data, users.data]);

  useEffect(() => setActive(0), [query]);

  function go(entry: Entry | undefined) {
    if (!entry) return;
    onOpenChange(false);
    navigate(entry.to);
  }

  let lastGroup = "";
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent size="md" placement="top" className="gap-0 p-0 max-sm:p-0" showCloseButton={false}>
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
            placeholder="搜索页面、设备、用户或订阅组…"
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
                go(entries[active]);
              }
            }}
          />
          <Kbd>Esc</Kbd>
        </div>
        {entries.length ? (
          <ul className="command-list" id="command-list" role="listbox" aria-label="跳转结果">
            {entries.map((entry, index) => {
              const heading = entry.group !== lastGroup;
              lastGroup = entry.group;
              const Icon = entry.icon;
              return (
                <li key={entry.id} role="presentation">
                  {heading && <div className="command-group mg-engraved" role="presentation">{entry.group}</div>}
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
          <span><Kbd><CornerDownLeft size={11} /></Kbd> 打开</span>
        </div>
      </DialogContent>
    </Dialog>
  );
}
