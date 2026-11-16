// 单个节点以连接身份引用，分组按名称正则筛选，全部成员由内核自动切换。
// 管理员可选任意订阅源；用户自建只能选分配给他的订阅组里出现的节点（由接口决定给哪些选项）。
import { Link } from "react-router-dom";
import { useQuery } from "../lib/query";
import { Badge, CheckboxField, Field, Input, Notice, Segmented, Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@weir/ui";
import { api } from "../api";
import { State } from "./feedback";

export interface SourceChoice {
  source_id?: number;
  profile_id?: number;
  mode?: "node" | "filter";
  match?: string;
  identity?: string;
  insecure_identities?: string[];
}
interface SourceNode { identity: string; name: string; requires_confirmation: boolean }
interface Option {
  kind: "source" | "profile";
  id: number;
  name: string;
  nodes: string[];
  node_items: SourceNode[];
}

const maxMembers = 50;

function matches(nodes: SourceNode[], mode: string, value: SourceChoice) {
  if (mode !== "filter") return nodes.filter(n => value.identity ? n.identity === value.identity : n.name === value.match);
  if (!value.match) return [];
  try {
    const re = new RegExp(value.match);
    return nodes.filter((n) => re.test(n.name));
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
        <Link className="self-start text-body weir-hit relative text-[var(--weir-accent-text)] underline-offset-4 transition-opacity hover:underline active:opacity-60" to="/subscriptions?tab=sources" target="_blank">在新标签页打开「订阅源」</Link>
      </div>
    ) : <State empty="没有分配给这个用户的订阅组" detail="管理员把订阅组分配给用户后，就能选组里的节点作出口。" />;
  }
  const chosen = options.find((o) => (o.kind === "source" ? o.id === value.source_id : o.id === value.profile_id));
  const mode = value.mode ?? "filter";
  const hit = chosen ? matches(chosen.node_items, mode, value) : [];
  const select = (id: string) => {
    const o = options.find((x) => `${x.kind}:${x.id}` === id);
    if (o) onChange({ ...value, source_id: o.kind === "source" ? o.id : 0, profile_id: o.kind === "profile" ? o.id : 0, match: "", identity: "", insecure_identities: [] });
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
        <Segmented aria-label="选择方式" value={mode} onValueChange={(m) => onChange({ ...value, mode: m, match: "", identity: "", insecure_identities: [] })} options={[["filter", "按名称筛选一组"], ["node", "单个节点"]] as const} />
      </Field>
      {mode === "node" ? (
        <Field label="节点" name="match" required>
          <Select required value={value.identity || (hit?.length === 1 ? hit[0].identity : "")} onValueChange={(v) => { const n = chosen?.node_items.find(n => n.identity === v); if (n) onChange({ ...value, mode, match: n.name, identity: n.identity, insecure_identities: [] }); }}>
            <SelectTrigger><SelectValue>{hit?.[0]?.name || value.match || "请选择"}</SelectValue></SelectTrigger>
            <SelectContent>{(chosen?.node_items ?? []).map(n => <SelectItem key={n.identity} value={n.identity}>{n.name}{chosen!.node_items.filter(other => other.name === n.name).length > 1 ? ` · ${n.identity.slice(0,7)}` : ""}</SelectItem>)}</SelectContent>
          </Select>
        </Field>
      ) : (
        <Field label="名称筛选（正则）" name="match" required hint="例如 ^HK 或 香港|HK" customError={hit === null ? "不是有效的正则" : hit.length > maxMembers ? `匹配到 ${hit.length} 个，一组最多 ${maxMembers} 个，请收紧条件` : ""}>
          <Input required value={value.match ?? ""} onChange={(e) => onChange({ ...value, mode, match: e.target.value, identity: "" })} />
        </Field>
      )}
      {chosen && hit && hit.length > 0 && hit.length <= maxMembers && (
        <Notice role="status">
          {mode === "filter" ? `这一组有 ${hit.length} 个节点，全部下发给 VPS，由内核按延迟自动选、出故障自动切换：` : "使用节点："}
          <span className="mt-1 flex flex-wrap gap-1">{hit.slice(0, 12).map(n => <Badge key={n.identity} variant="soft" tone="neutral">{n.name}</Badge>)}{hit.length > 12 && <small>等 {hit.length} 个</small>}</span>
        </Notice>
      )}
      {chosen && (value.match || value.identity) && hit && hit.length === 0 && <Notice tone="warn">选中的连接身份当前不可用。改名不会影响引用，其他同名节点不会替代它。</Notice>}
      {hit && hit.length <= maxMembers && hit.filter(n => n.requires_confirmation).map(n => <CheckboxField key={n.identity} label={`确认「${n.name}」跳过证书校验`} description="无法验证远端身份。只确认这条连接；未确认的分组成员不会参与出口。" checked={(value.insecure_identities ?? []).includes(n.identity)} onCheckedChange={checked => onChange({...value, insecure_identities: checked ? [...new Set([...(value.insecure_identities ?? []), n.identity])] : (value.insecure_identities ?? []).filter(id => id !== n.identity)})} />)}
    </>
  );
}
