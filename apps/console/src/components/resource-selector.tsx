import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { OptionRow, ReorderButtons, SearchInput, Button, Checkbox} from "@migate/ui";
import { X } from "lucide-react";
import { client } from "../lib/client";
import { State } from "./feedback";
export function ResourceSelector({
  kind,
  label,
  value,
  onChange,
  excludeIds = [],
}: {
  kind: string;
  label: string;
  value: number[];
  onChange: (v: number[]) => void;
  excludeIds?: number[];
}) {
  const [search, S] = useState("");
  const q = useQuery({
    queryKey: ["selection", kind, search],
    queryFn: async () =>
      kind === "users"
        ? (
            await client.users(
              "?page=1&page_size=100&q=" + encodeURIComponent(search),
            )
          ).items.map((u) => ({ id: u.id, name: u.email }))
        : (await client.resources(kind)).items.map((r) => ({
            id: r.id,
            name: String(r.data.name ?? r.key),
          })),
  });
  const options = (q.data ?? []).filter((o) => !excludeIds.includes(o.id));
  const title = (id: number) =>
    options.find((o) => o.id === id)?.name ?? "已选择 #" + id;
  function move(i: number, by: number) {
    const a = [...value];
    [a[i], a[i + by]] = [a[i + by], a[i]];
    onChange(a);
  }
  return (
    <fieldset className="resource-selector">
      <legend>
        {label} · 已选 {value.length}
      </legend>
      <SearchInput
        className="w-full"
        aria-label={"搜索" + label}
        value={search}
        onChange={(e) => S(e.target.value)}
        placeholder={"搜索" + label + "…"}
      />
      {q.isPending ? (
        <State loading />
      ) : q.error ? (
        <State error={q.error} onRetry={() => q.refetch()} />
      ) : (
        <div className="selector-options">
          {options
            .filter((o) => o.name.toLowerCase().includes(search.toLowerCase()))
            .map((o) => (
              <OptionRow compact key={o.id}>
                <Checkbox
                  checked={value.includes(o.id)}
                  onCheckedChange={(v) =>
                    onChange(
                      v ? [...value, o.id] : value.filter((id) => id !== o.id),
                    )
                  }
                />
                <span>{o.name}</span>
              </OptionRow>
            ))}
          {!options.length && <p>暂无可选资源</p>}
        </div>
      )}
      {value.length > 0 && (
        <div className="selection-order">
          {value.map((id, i) => (
            <div className="selection-row" key={id}>
              <span>
                {i + 1}. {title(id)}
              </span>
              <ReorderButtons label={title(id)} onUp={() => move(i, -1)} onDown={() => move(i, 1)} upDisabled={i === 0} downDisabled={i === value.length - 1} />
              <Button
                type="button"
                variant="ghost"
                size="icon-sm"
                aria-label={"移除 " + title(id)}
                onClick={() => onChange(value.filter((v) => v !== id))}
              >
                <X size={14} />
              </Button>
            </div>
          ))}
        </div>
      )}
    </fieldset>
  );
}
