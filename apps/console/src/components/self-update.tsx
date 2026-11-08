// 版本与更新：从 GitHub Release 自更新。中心先按「GitHub 加速」（含经 VPS 中转）预下载发布包，再交给 root 的更新服务：
// 按官方摘要校验、用包里的安装脚本安装，失败自动切回。可切回本机保留的上一版本，或重装最近 3 个正式版本之一。
// 整个过程在这里合成一条记录（useUpdateStatus），和其他后台任务同样显示；版本变了由 UpdateWatcher 刷新页面。
import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useQuery } from "../lib/query";
import { Badge, Button, CodeBlock, Collapsible, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Form, InsetRow, InsetRows, JobPlaceholder, JobView, Notice } from "@weir/ui";
import { api } from "../api";
import { date } from "../lib/models";
import { State } from "./feedback";
import { Section } from "./system-settings";
import { useFormAction, useRowAction } from "../lib/action";
import { useConsoleContext } from "../lib/console-context";
import { updateStatusKey, useUpdateStatus, type UpdateRun } from "./update-watcher";

interface Release { version: string; published_at: string; notes: string; url: string }
interface Releases { latest?: Release; has_update?: boolean; older?: Release[]; check_error?: string }

const phaseText: Record<UpdateRun["phase"], string> = { prefetch: "预下载", waiting: "等更新服务接手", downloading: "下载", installing: "安装", done: "完成", failed: "失败", cancelled: "已取消" };

const releasesKey = ["self-update-releases"];

export function UpdateCard() {
  const client = useQueryClient();
  const status = useUpdateStatus();
  // 版本列表单独读：GitHub 慢或连不上时，进度照常显示
  const releases = useQuery({ queryKey: releasesKey, queryFn: () => api("/admin/system/update") as Promise<Releases>, retry: 1 });
  const { run: act, isBusy } = useRowAction<string>();
  const [target, setTarget] = useState<{ action: "update" | "rollback"; version?: string } | null>(null);
  const view = status.data;
  const run = view?.run;
  const running = run?.status === "running";
  const rel = releases.data;
  // 中心重启期间状态接口读不到：保留最后的进度，只说明在等
  const waitingCenter = running && !!status.error;
  useConsoleContext({ notices: [view ? `当前版本 ${view.current}${rel?.has_update && rel.latest ? `，可更新到 ${rel.latest.version}` : ""}` : "版本未知", ...(running && run ? [`正在${run.action === "rollback" ? "切回" : "更新"}：${phaseText[run.phase]}`] : []), ...(rel?.check_error ? ["无法读取 GitHub 发布：" + rel.check_error] : [])] });
  const recheck = () => act("check", async () => { client.setQueryData(releasesKey, await api("/admin/system/update?force=1")); }, undefined, { fail: "检查失败" });
  const started = () => { setTarget(null); void client.invalidateQueries({ queryKey: updateStatusKey }); };
  const blocked = !!view?.blocker || running;
  const older = (rel?.older ?? []).filter((r) => r.version !== view?.previous);
  return (
    <Section title="版本与下载" description="内核、Gate 安装、中心更新和客户端下载都按下方 GitHub 来源顺序下载；root 的更新服务按官方摘要校验后安装，启动失败自动切回上一版本。">
      {status.isPending || (!view && status.error) || !view ? <State loading={status.isPending} error={status.error} onRetry={() => status.refetch()} /> : (
        <InsetRows>
          <InsetRow className="stack">
            <div className="flex flex-wrap items-center gap-2">
              <span>当前版本 <code>{view.current}</code></span>
              {rel?.latest && (rel.has_update ? <Badge variant="soft" tone="warn">有新版本 {rel.latest.version}</Badge> : <Badge variant="soft" tone="ok">已是最新</Badge>)}
              {releases.isPending && <small className="text-[var(--weir-ink-3)]">正在读取 GitHub 发布…</small>}
              <Button className="ml-auto" size="sm" variant="outline" disabled={isBusy()} busy={isBusy("check")} busyText="正在检查…" onClick={() => void recheck()}>重新检查</Button>
            </div>
            {(rel?.check_error || releases.error) && <Notice tone="warn">无法读取 GitHub 发布：{rel?.check_error ?? (releases.error as Error).message}</Notice>}
            {view.blocker && <Notice tone="info">{view.blocker}。</Notice>}
          </InsetRow>
          <InsetRow>
            {run ? (
              <JobView
                job={run}
                className="border-0 p-0"
                note={waitingCenter ? "中心正在重启，正在等它恢复" : undefined}
                disabled={isBusy() || !run.cancellable}
                cancelBusy={isBusy("cancel")}
                onCancel={() => void act("cancel", () => api("/me/jobs/" + encodeURIComponent(run.id) + "/cancel", "POST"), "已请求取消", { fail: "取消失败" })}
              />
            ) : <JobPlaceholder text="还没有在这里更新过；更新的进度与日志会显示在这里。" />}
          </InsetRow>
          {rel?.latest && rel.has_update && (
            <InsetRow>
              <Collapsible defaultOpen title={<span>{rel.latest.version} 更新说明 <small className="text-[var(--weir-ink-3)]">{date(rel.latest.published_at)}</small></span>}>
                <CodeBlock className="block max-h-64 overflow-auto whitespace-pre-wrap text-meta">{rel.latest.notes || "（没有说明）"}</CodeBlock>
              </Collapsible>
            </InsetRow>
          )}
          {((rel?.latest && rel.has_update) || view.previous || older.length > 0) && <InsetRow className="actions">
            {rel?.latest && rel.has_update && <Button disabled={blocked} onClick={() => setTarget({ action: "update", version: rel.latest!.version })}>更新到 {rel.latest.version}</Button>}
            {view.previous && <Button variant="outline" disabled={blocked} onClick={() => setTarget({ action: "rollback" })}>切回上一版本 {view.previous}</Button>}
            {older.map((r) => <Button key={r.version} variant="outline" disabled={blocked} onClick={() => setTarget({ action: "update", version: r.version })}>重装 {r.version}</Button>)}
          </InsetRow>}
        </InsetRows>
      )}
      {target && view && <ConfirmUpdate target={target} current={view.current} previous={view.previous} close={() => setTarget(null)} started={started} />}
    </Section>
  );
}

function ConfirmUpdate({ target, current, previous, close, started }: { target: { action: "update" | "rollback"; version?: string }; current: string; previous: string; close: () => void; started: () => void }) {
  const to = target.action === "rollback" ? previous : target.version!;
  const go = useFormAction(() => api("/admin/system/update", "POST", target), { ok: "已开始，进度显示在下方", onSuccess: started });
  useConsoleContext({ dialog: { title: target.action === "rollback" ? "切回 " + to : "更新到 " + to } });
  return (
    <Dialog open onOpenChange={(o) => !o && !go.busy && close()}>
      <DialogContent>
        <DialogTitle>{target.action === "rollback" ? `切回 ${to}？` : `更新到 ${to}？`}</DialogTitle>
        <DialogDescription>
          当前 {current}。安装时服务会重启，控制台短暂不可用；新版本启动失败会自动切回 {current}。Gate 之后从中心自更新到同一版本。
        </DialogDescription>
        <Form busy={go.busy} errors={go.errors} onSubmit={() => void go.submit()}>
          <DialogFooter error={go.error}>
            <Button type="button" variant="ghost" disabled={go.busy} onClick={close}>取消</Button>
            <Button type="submit" disabled={go.busy} busy={go.busy} busyText="正在提交…">{target.action === "rollback" ? "切回" : "更新"}</Button>
          </DialogFooter>
        </Form>
      </DialogContent>
    </Dialog>
  );
}
