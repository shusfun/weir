// 订阅源里的节点作出口：单个节点（名称完全一致），或按名称正则筛选一组（全部成员下发，由内核自动选择与切换，最多 50 个）。
// 管理员可选任意订阅源；用户自建只能选分配给他的订阅组里出现的节点（由接口决定给哪些选项）。
import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { Badge, Field, Input, Notice, Segmented, Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@weir/ui";
import { api } from "../api";
import { State } from "./feedback";

export interface SourceChoice {
  source_id?: number;
  profile_id?: number;
  mode?: "node" | "filter";
  match?: string;
}
interface Option {
  kind: "source" | "profile";
  id: number;
  name: string;
  nodes: string[];
}

const maxMembers = 50;

function matches(nodes: string[], mode: string | undefined, match: string | undefined) {
  if (!match) return [];
  if (mode !== "filter") return nodes.filter((n) => n === match);
  try {
    const re = new RegExp(match);
    return nodes.filter((n) => re.test(n));
  } catch {
    return null;
  }
}

export function SourcePicker({ path, value, onChange }: { path: string; value: SourceChoice; onChange: (v: SourceChoice) => void }) {
  const q = useQuery({ queryKey: ["chain-sources", path], queryFn: async () => (await api(path)).items as Option[] });
  if (q.isPending || q.error) return <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} />;
  const options = q.data ?? [];
  if (options.length === 0) {
    return path.startsWith("/admin/chain-sources") ? (
      <div className="stack">
        <State empty="还没有订阅源" detail="先添加订阅源并拉取一次，再回来选组里的节点作出口。" />
        <Link className="self-start text-[13px] text-[var(--weir-accent-text)] underline-offset-4 hover:underline" to="/subscriptions?tab=sources" target="_blank">在新标签页打开「订阅源」</Link>
      </div>
    ) : <State empty="没有分配给这个用户的订阅组" detail="管理员把订阅组分配给用户后，就能选组里的节点作出口。" />;
  }
  const chosen = options.find((o) => (o.kind === "source" ? o.id === value.source_id : o.id === value.profile_id));
  const mode = value.mode ?? "filter";
  const hit = chosen ? matches(chosen.nodes, mode, value.match) : [];
  const select = (id: string) => {
    const o = options.find((x) => `${x.kind}:${x.id}` === id);
    if (o) onChange({ ...value, source_id: o.kind === "source" ? o.id : undefined, profile_id: o.kind === "profile" ? o.id : undefined, match: "" });
  };
  return (
    <>
      <Field label={options[0].kind === "source" ? "订阅源" : "订阅组"} name="source_id" required>
        <Select required value={chosen ? `${chosen.kind}:${chosen.id}` : ""} onValueChange={(v) => select(String(v))}>
          <SelectTrigger><SelectValue>{chosen?.name ?? "请选择"}</SelectValue></SelectTrigger>
          <SelectContent>{options.map((o) => <SelectItem key={`${o.kind}:${o.id}`} value={`${o.kind}:${o.id}`}>{o.name}（{o.nodes.length} 个节点）</SelectItem>)}</SelectContent>
        </Select>
      </Field>
      <Field label="选择方式">
        <Segmented aria-label="选择方式" value={mode} onValueChange={(m) => onChange({ ...value, mode: m, match: "" })} options={[["filter", "按名称筛选一组"], ["node", "单个节点"]] as const} />
      </Field>
      {mode === "node" ? (
        <Field label="节点" name="match" required>
          <Select required value={value.match ?? ""} onValueChange={(v) => onChange({ ...value, mode, match: String(v) })}>
            <SelectTrigger><SelectValue>{value.match || "请选择"}</SelectValue></SelectTrigger>
            <SelectContent>{(chosen?.nodes ?? []).map((n) => <SelectItem key={n} value={n}>{n}</SelectItem>)}</SelectContent>
          </Select>
        </Field>
      ) : (
        <Field label="名称筛选（正则）" name="match" required hint="例如 ^HK 或 香港|HK" customError={hit === null ? "不是有效的正则" : hit.length > maxMembers ? `匹配到 ${hit.length} 个，一组最多 ${maxMembers} 个，请收紧条件` : ""}>
          <Input required value={value.match ?? ""} onChange={(e) => onChange({ ...value, mode, match: e.target.value })} />
        </Field>
      )}
      {chosen && hit && hit.length > 0 && hit.length <= maxMembers && (
        <Notice role="status">
          {mode === "filter" ? `这一组有 ${hit.length} 个节点，全部下发给 VPS，由内核按延迟自动选、出故障自动切换：` : "使用节点："}
          <span className="mt-1 flex flex-wrap gap-1">{hit.slice(0, 12).map((n) => <Badge key={n} variant="soft" tone="neutral">{n}</Badge>)}{hit.length > 12 && <small>等 {hit.length} 个</small>}</span>
        </Notice>
      )}
      {chosen && value.match && hit && hit.length === 0 && <Notice tone="warn">现在没有匹配的节点（支持 VLESS / VMess / Trojan / SOCKS5 / HTTP）。</Notice>}
    </>
  );
}
