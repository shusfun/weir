import { useState } from "react";
import { Button, Card, CardContent, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Form, Notice, PageState, SectionHeader } from "@weir/ui";
import { api, ApiError } from "../api";
import { ExitFields, exitErrorFields, probeText } from "./chain-exit";
import { State } from "./feedback";
import { WaitText } from "./wait";
import { useFormAction } from "../lib/action";
import { useConsoleContext } from "../lib/console-context";
import { useQuery } from "../lib/query";
import { exitInput, exitTitle } from "../lib/exits";
import { objectStatus, type ExitItem, type ExitSpec } from "../lib/core";

import { ObjectLink, dataObject, useDialogParam } from "./object-link";
import { ExitProbe } from "./exit-probe";

export function MyExits() {
  const exits = useQuery({ queryKey: ["my-exits"], queryFn: async () => await api("/me/exits") as { items: ExitItem[] } });
  const gates = useQuery({ queryKey: ["my-gates"], queryFn: async () => (await api("/me/nodes")).items as { gate_id: number; gate_name?: string }[] });
  const detail = useDialogParam("exit", exits.data?.items, exits.isSuccess);
  const [creating, setCreating] = useState(false);
  const edit = creating ? "new" : detail.current;
  const setEdit = (value: ExitItem | "new" | null) => {
    setCreating(value === "new");
    if (value && value !== "new") detail.open(value); else detail.close();
  };
  const [probe, setProbe] = useState<ExitItem | null>(null);
  useConsoleContext({ list: { total: exits.data?.items.length ?? 0, items: (exits.data?.items ?? []).slice(0, 20).map(e => ({ type: "exit", id: e.id, name: exitTitle(e) })) } });
  if (exits.error instanceof ApiError && exits.error.status === 403) return <PageState kind="forbidden" title="无权管理代理资料" detail={exits.error.message} />;
  return (
    <Card>
      <CardContent className="stack">
        <SectionHeader flush title="我的代理资料" description="先保存地址和凭据。在节点里使用时，才由那台 VPS 验证。" actions={<Button onClick={() => setEdit("new")}>添加代理</Button>} />
        {!exits.data ? <State loading={exits.isPending} error={exits.error} onRetry={() => exits.refetch()} /> : exits.data.items.length === 0 ? <State empty="还没有自己的代理" /> : exits.data.items.map((e) => (
          <div key={e.id} className="stack" {...dataObject({ type: "exit", id: e.id })}>
            <strong><ObjectLink to={{ type: "exit", id: e.id, name: exitTitle(e) }} href={`/proxy?tab=nodes&exit=${e.id}`} /></strong>
            <p>{objectStatus(e.object_state)[2]}</p>
            {(e.probes ?? []).filter((p) => p.location === "center").map((p) => <p key="center">{probeText(p)}</p>)}
            {(e.uses ?? []).map((use) => <p key={use.gate_id}>{use.name ?? `VPS ${use.gate_id}`}：{use.status === "applied" ? "已生效" : use.status === "kept" ? "保留原配置" : "未知"}{use.reason ? ` · ${use.reason}` : ""}{use.probe ? ` · ${probeText(use.probe)}` : " · 还没有这台 VPS 的实测"}</p>)}
            <div className="actions">
              <Button size="sm" variant="outline" onClick={() => setEdit(e)}>编辑</Button>
              <Button size="sm" variant="ghost" onClick={() => setProbe(e)}>实测</Button>
            </div>
          </div>
        ))}
        {edit && <MyExitEditor key={edit === "new" ? "new" : edit.id} value={edit} close={() => setEdit(null)} saved={() => { setEdit(null); void exits.refetch(); }} />}
        {probe && <ExitProbe exit={probe} base="/me" loading={gates.isPending} error={gates.error} retry={() => void gates.refetch()} gates={[...new Map((gates.data ?? []).map((n) => [n.gate_id, n.gate_name ?? `VPS ${n.gate_id}`])).entries()].map(([id, name]) => ({ id, name }))} close={() => setProbe(null)} saved={() => void exits.refetch()} />}
      </CardContent>
    </Card>
  );
}

function MyExitEditor({ value, close, saved }: { value: ExitItem | "new"; close: () => void; saved: () => void }) {
  const old = value === "new" ? undefined : value;
  const [spec, setSpec] = useState<ExitSpec>(old ? exitInput(old) : { kind: "socks5" });
  const [applications, setApplications] = useState<ExitItem["applications"]>();
  const save = useFormAction(async (track) => api(old ? `/me/exits/${old.id}` : "/me/exits", old ? "PATCH" : "POST", spec, track), { fields: { ...exitErrorFields(spec.kind), invalid_remark: "remark" }, onSuccess: (result: ExitItem) => { setApplications(result.applications); if (!result.applications?.some(item => item.status !== "applied")) saved(); } });
  useConsoleContext({ dialog: { title: old ? "编辑代理" : "添加代理" } });
  return <Dialog open onOpenChange={(o) => !o && !save.busy && close()}><DialogContent><DialogTitle>{old ? "编辑代理" : "添加代理"}</DialogTitle><DialogDescription>只保存资料，不要求先选 VPS，也不要求先测通。</DialogDescription><Form busy={save.busy} errors={save.errors} onSubmit={() => void save.submit()}><ExitFields value={spec} onChange={setSpec} admin={false} sourcePath="/me/chain-sources" nodes={[]} nodeHint="另一台 VPS 的节点在新建链式时选择" savedSecrets={old ? { password: old.has_password, link: old.has_link } : undefined} />{applications?.length ? <Notice>{applications.map(item => `${item.name ?? `VPS ${item.gate_id}`}：${item.status === "applied" ? "已替换" : item.status === "waiting" ? "等待重试" : item.status === "kept" ? "保留原配置" : "未应用"}${item.reason ? `，${item.reason}` : ""}`).join("；")}</Notice> : null}<DialogFooter error={save.error} status={<WaitText wait={save.wait} />}><Button type="button" variant="outline" onClick={close} disabled={save.busy}>取消</Button><Button type="submit" busy={save.busy} busyText="正在保存…">保存</Button></DialogFooter></Form></DialogContent></Dialog>;
}
