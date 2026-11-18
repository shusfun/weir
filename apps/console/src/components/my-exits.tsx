import { useState } from "react";
import { ActionGroup, Badge, Button, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Form, InsetGroup, InsetRow, InsetRows, Notice, PageState } from "@weir/ui";
import { api, ApiError } from "../api";
import { ExitFields, exitErrorFields, probeText } from "./chain-exit";
import { State } from "./feedback";
import { WaitText } from "./wait";
import { useFormAction } from "../lib/action";
import { useConsoleContext } from "../lib/console-context";
import { useQuery } from "../lib/query";
import { exitInput, exitTitle, useExitCatalog } from "../lib/exits";
import { objectStatus, type ExitItem, type ExitSpec } from "../lib/core";

import { ObjectLink, dataObject, useDialogParam } from "./object-link";
import { ExitProbe } from "./exit-probe";

export function MyExits({ canSave }: { canSave: boolean }) {
  const exits = useExitCatalog(false);
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
    <InsetGroup title="我的代理资料" description={canSave ? "先保存地址和凭据。在节点里使用时，才由那台 VPS 验证。" : "管理员已关闭自建链式代理。你仍可查看已有资料。"} actions={canSave && <Button size="sm" onClick={() => setEdit("new")}>添加代理</Button>}>
      {!exits.data ? <State loading={exits.isPending} error={exits.error} onRetry={() => exits.refetch()} /> : exits.data.items.length === 0 ? <State empty="还没有自己的代理" /> : (
        <InsetRows>
          {exits.data.items.map((e) => {
            const centerProbe = (e.probes ?? []).find((p) => p.location === "center");
            return (
              <InsetRow key={e.id} className="flex flex-wrap items-center justify-between gap-3" {...dataObject({ type: "exit", id: e.id })}>
                <div className="min-w-0 flex-1 basis-[221px] space-y-1">
                  <div className="flex flex-wrap items-center gap-2">
                    <strong>{canSave ? <ObjectLink to={{ type: "exit", id: e.id, name: exitTitle(e) }} href={`/proxy?tab=nodes&exit=${e.id}`} /> : exitTitle(e)}</strong>
                    <Badge variant="soft" tone={e.udp === "supported" ? "ok" : e.udp === "unsupported" ? "warn" : "neutral"}>
                      {e.udp === "supported" ? "支持 UDP" : e.udp === "unsupported" ? "仅 TCP" : "UDP 未知"}
                    </Badge>
                  </div>
                  <div className="text-meta text-[var(--weir-ink-3)] break-words">
                    {centerProbe ? probeText(centerProbe) : objectStatus(e.object_state)[2]}
                  </div>
                  {(e.uses ?? []).length > 0 && (
                    <div className="flex flex-wrap gap-1.5 pt-0.5">
                      {(e.uses ?? []).map((use) => (
                        <Badge key={use.gate_id} variant="soft" tone={use.status === "applied" ? "ok" : use.status === "kept" ? "warn" : "neutral"}>
                          {use.name ?? `VPS ${use.gate_id}`} · {use.status === "applied" ? "已生效" : use.status === "kept" ? "保留原配置" : "待验证"}{use.probe ? ` · ${probeText(use.probe)}` : ""}
                        </Badge>
                      ))}
                    </div>
                  )}
                </div>
                <ActionGroup wrap={false}>
                  {canSave && <Button size="sm" variant="outline" onClick={() => setEdit(e)}>编辑</Button>}
                  <Button size="sm" variant="ghost" onClick={() => setProbe(e)}>实测</Button>
                </ActionGroup>
              </InsetRow>
            );
          })}
        </InsetRows>
      )}
      {canSave && edit && <MyExitEditor key={edit === "new" ? "new" : edit.id} value={edit} close={() => setEdit(null)} saved={() => { setEdit(null); void exits.refetch(); }} />}
      {probe && <ExitProbe exit={probe} base="/me" loading={gates.isPending} error={gates.error} retry={() => void gates.refetch()} gates={[...new Map((gates.data ?? []).map((n) => [n.gate_id, n.gate_name ?? `VPS ${n.gate_id}`])).entries()].map(([id, name]) => ({ id, name }))} close={() => setProbe(null)} saved={() => void exits.refetch()} />}
    </InsetGroup>
  );
}

function MyExitEditor({ value, close, saved }: { value: ExitItem | "new"; close: () => void; saved: () => void }) {
  const old = value === "new" ? undefined : value;
  const [spec, setSpec] = useState<ExitSpec>(old ? exitInput(old) : { kind: "socks5" });
  const [applications, setApplications] = useState<ExitItem["applications"]>();
  const save = useFormAction(async (track) => api(old ? `/me/exits/${old.id}` : "/me/exits", old ? "PATCH" : "POST", spec, track), { fields: { ...exitErrorFields(spec.kind), invalid_remark: "remark" }, invalidate: [["exits"]], onSuccess: (result: ExitItem) => { setApplications(result.applications); if (!result.applications?.some(item => item.status !== "applied")) saved(); } });
  useConsoleContext({ dialog: { title: old ? "编辑代理" : "添加代理" } });
  return <Dialog open onOpenChange={(o) => !o && !save.busy && close()}><DialogContent size="lg"><DialogTitle>{old ? "编辑代理" : "添加代理"}</DialogTitle><DialogDescription>只保存资料，不要求先选 VPS，也不要求先测通。</DialogDescription><Form busy={save.busy} errors={save.errors} onSubmit={() => void save.submit()}><ExitFields value={spec} onChange={setSpec} admin={false} sourcePath="/me/chain-sources" nodes={[]} nodeHint="另一台 VPS 的节点在新建链式时选择" savedSecrets={old ? { password: old.has_password, link: old.has_link } : undefined} />{applications?.length ? <Notice>{applications.map(item => `${item.name ?? `VPS ${item.gate_id}`}：${item.status === "applied" ? "已替换" : item.status === "waiting" ? "等待重试" : item.status === "kept" ? "保留原配置" : "未应用"}${item.reason ? `，${item.reason}` : ""}`).join("；")}</Notice> : null}<DialogFooter error={save.error} status={<WaitText wait={save.wait} />}><Button type="button" variant="ghost" onClick={close} disabled={save.busy}>取消</Button><Button type="submit" busy={save.busy} busyText="正在保存…">保存</Button></DialogFooter></Form></DialogContent></Dialog>;
}
