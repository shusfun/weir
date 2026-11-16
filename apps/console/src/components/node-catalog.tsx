import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Badge, Button, CheckboxField, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Field, Form, Notice, SectionHeader, Select, SelectContent, SelectItem, SelectTrigger, SelectValue, SwitchField } from "@weir/ui";
import { api } from "../api";
import { useQuery } from "../lib/query";
import { useFormAction, useRowAction } from "../lib/action";
import { useConsoleContext } from "../lib/console-context";
import { State } from "./feedback";
import type { UserItem } from "../lib/core";
import { useDialogParam } from "./object-link";

export interface CatalogNode {
  ref: string; kind: "node" | "source" | "manual" | "chain"; id: number; name: string; protocol: string;
  source_id?: number; gate_id?: number; owner_id?: number; selected: boolean; authorized: boolean; chain_entry: boolean;
  state: { status: string; reason: string; observed_at: string | null; next: string[] };
}
interface Catalog { items: CatalogNode[]; revision: string }
const origin = { node: "自有 VPS", source: "订阅源", manual: "手动节点", chain: "本人链式" };

export function CatalogGrants({ user, close }: { user: Pick<UserItem, "id" | "name" | "email">; close: () => void }) {
  const q = useQuery({ queryKey: ["node-catalog", user.id], queryFn: () => api(`/admin/users/${user.id}/node-catalog`) as Promise<Catalog> });
  useConsoleContext({ dialog: { title: "节点授权" }, object: { type: "user", id: user.id, name: user.name || user.email } });
  return <Dialog open onOpenChange={(open) => !open && close()}><DialogContent size="lg">
    <DialogTitle>{user.name || user.email} · 节点授权</DialogTitle>
    <DialogDescription>订阅始终与当前授权取交集。只有自有 VPS 节点可作为链式入口；用户链式节点保持本人所有。</DialogDescription>
    {q.isPending || q.error ? <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} /> : <CatalogGrantForm key={q.data.revision} data={q.data} uid={user.id} close={close} />}
  </DialogContent></Dialog>;
}

function CatalogGrantForm({ data, uid, close }: { data: Catalog; uid: number; close: () => void }) {
  const client = useQueryClient();
  const [refs, setRefs] = useState(data.items.filter((item) => item.authorized).map((item) => item.ref));
  const save = useFormAction(() => api(`/admin/users/${uid}/node-catalog/grants`, "PUT", { refs, revision: data.revision }), {
    ok: "节点授权已更新", onSuccess: async () => { await client.invalidateQueries({ queryKey: ["node-catalog"] }); await client.invalidateQueries({ queryKey: ["users"] }); close(); },
  });
  return <Form busy={save.busy} errors={save.errors} onSubmit={() => void save.submit()}>
    {data.items.length === 0 && <State empty="还没有可分配的节点" />}
    {data.items.map((item) => <CheckboxField key={item.ref} label={`${item.name || item.ref} · ${origin[item.kind]}`} checked={refs.includes(item.ref)} disabled={!item.selected || item.kind === "chain" || save.busy}
      onCheckedChange={(checked) => setRefs((old) => checked ? [...old, item.ref] : old.filter((ref) => ref !== item.ref))} description={!item.selected ? "请先在节点目录选用" : item.state.reason} />)}
    <DialogFooter error={save.error}><Button type="button" variant="outline" disabled={save.busy} onClick={close}>取消</Button><Button type="submit" busy={save.busy} busyText="正在保存…">保存授权</Button></DialogFooter>
  </Form>;
}

export function ExternalNodeCatalog() {
  const q = useQuery({ queryKey: ["node-catalog"], queryFn: () => api("/admin/node-catalog") as Promise<Catalog> });
  const users = useQuery({ queryKey: ["users"], queryFn: async () => (await api("/admin/users")).items as UserItem[] });
  const [userID, setUserID] = useState("");
  const grantDialog = useDialogParam("grant", users.data, users.isSuccess);
  const { run, isBusy } = useRowAction<string>({ refetch: q.refetch });
  const items = q.data?.items.filter((item) => item.kind !== "node" && item.kind !== "chain") ?? [];
  return <section className="stack" data-tour="node-catalog">
    <SectionHeader flush title="机场与手动节点" description="选用后可分配订阅，也可作为自有 VPS 的落地。机场直用流量未知，不计入 Gate 用量。" />
    {users.error ? <State error={users.error} onRetry={() => users.refetch()} /> : <Field label="为用户分配节点"><div className="actions">
      <div className="min-w-0"><Select value={userID} onValueChange={(value) => setUserID(value ?? "")} disabled={users.isPending}><SelectTrigger><SelectValue placeholder="选择用户" /></SelectTrigger><SelectContent>{users.data?.map((u) => <SelectItem key={u.id} value={String(u.id)}>{u.name || u.email}</SelectItem>)}</SelectContent></Select></div>
      <Button variant="outline" disabled={!userID} onClick={() => grantDialog.open(Number(userID))}>调整授权</Button>
    </div></Field>}
    {q.isPending || q.error ? <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} /> : items.length === 0 ? <State empty="还没有机场或手动节点" detail="添加订阅源并刷新，或添加手动节点后会显示在这里。" /> : items.map((item) => <div key={item.ref} className="weir-set-row">
      <div><strong>{item.name || item.ref}</strong> <Badge variant="soft" tone="neutral">{origin[item.kind]} · {item.protocol}</Badge><small className="block text-[var(--weir-ink-3)]">{item.state.reason}</small></div>
      {item.kind === "source" ? <SwitchField label={`选用 ${item.name || item.ref}`} checked={item.selected} disabled={isBusy()} onCheckedChange={(selected) => void run(item.ref, () => api(`/admin/node-catalog/source/${item.id}/selection`, "PUT", { selected }), selected ? "已选用" : "已取消选用")} /> : <span>可分配</span>}
    </div>)}
    <Notice>源内节点改名不改变身份；已消失的节点保留引用并显示不可用。链式入口仍只能选择用户获授权的自有 VPS 节点。</Notice>
    {grantDialog.current && <CatalogGrants user={grantDialog.current} close={grantDialog.close} />}
  </section>;
}
