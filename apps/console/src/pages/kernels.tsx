// 内核：中心从上游拉取 Xray / sing-box / mihomo 并按官方摘要校验，再推送给 VPS；配置按内核驱动生成。
// 架构不用选：按中心本机与已接入 VPS 的架构一起下载。分区记在地址栏 ?tab=（版本 / 驱动 / 订阅检查 / dsh）。
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useSearchParams } from "react-router-dom";
import { Badge, Button, Card, CardContent, CodeBlock, Dialog, DialogContent, DialogDescription, DialogTitle, Notice, PageHeader, SectionHeader, SkeletonCards, SkeletonList, Tabs, TabsList, TabsPanel, TabsTab, useConfirm } from "@weir/ui";
import { Download, Star, Trash2 } from "lucide-react";
import { api } from "../api";
import { useRowAction } from "../lib/action";
import { State } from "../components/feedback";
import { DshVersions } from "../components/dsh";
import { SubCheckBadge, type SubCheck } from "../components/sub-check";
import { JobLine, latestJob, useJobs, useStartedJob, type Job } from "../components/job";
import { DriversPanel } from "../components/drivers";
import { date } from "../lib/models";
import { ObjectLink, dataObject } from "../components/object-link";
import { useConsoleContext } from "../lib/console-context";

interface KernelVersion {
  version: string;
  arches: Record<string, boolean>;
  default: boolean;
  pinned_by: string[] | null;
  running_on: string[] | null;
  pinned_refs?: { id: number; name: string }[];
  running_refs?: { id: number; name: string }[];
  driver?: string;
  driver_error?: string;
}
export interface KernelInfo {
  default: string;
  arches: string[];
  versions: KernelVersion[];
}
interface Release {
  version: string;
  published_at: string;
  downloads?: Record<string, number>;
  recommended: boolean;
  young: boolean;
  driver?: string;
  incompatible?: string;
}

export const kernelNames: Record<string, string> = { xray: "Xray", "sing-box": "sing-box", mihomo: "mihomo（订阅检查）" };
const order = ["xray", "sing-box", "mihomo"];
const tabs = [
  { key: "versions", label: "版本" },
  { key: "drivers", label: "驱动" },
  { key: "checks", label: "订阅检查" },
  { key: "dsh", label: "dsh" },
] as const;

export function KernelsPage() {
  const [params, setParams] = useSearchParams();
  const tab = tabs.some((t) => t.key === params.get("tab")) ? params.get("tab")! : "versions";
  useConsoleContext({ page: "内核", tab: tabs.find((t) => t.key === tab)?.label });
  return (
    <div className="stack">
      <PageHeader eyebrow="工作区" title="内核" description="内核只以独立程序运行，升级不需要更新 Weir。中心拉取后按官方摘要校验，再推送给 VPS；VPS 不访问 GitHub。每个版本怎么配置由内核驱动决定。" />
      <Tabs value={tab} onValueChange={(v) => setParams({ tab: String(v) }, { replace: true })}>
        <TabsList aria-label="内核分区">
          {tabs.map((t) => <TabsTab key={t.key} value={t.key}>{t.label}</TabsTab>)}
        </TabsList>
        <TabsPanel value="versions" className="stack"><Versions /></TabsPanel>
        <TabsPanel value="drivers" className="stack"><DriversPanel /></TabsPanel>
        <TabsPanel value="checks" className="stack"><SubChecks /></TabsPanel>
        <TabsPanel value="dsh" className="stack"><DshVersions /></TabsPanel>
      </Tabs>
    </div>
  );
}

function Versions() {
  const q = useQuery({ queryKey: ["kernels"], queryFn: async () => (await api("/admin/kernels")).kernels as Record<string, KernelInfo> });
  const jobs = useJobs("kernel");
  const started = useStartedJob();
  const [releases, setReleases] = useState<string | null>(null);
  const { run, isBusy } = useRowAction<string>({ refetch: q.refetch });
  const confirm = useConfirm();
  const setDefault = (kernel: string, version: string) =>
    run("default:" + kernel + version, () => api("/admin/kernels/defaults", "PUT", { [kernel]: version }), `${kernelNames[kernel]} 默认版本：${version}`, {
      detail: () => "未锁定版本的 VPS 会在数秒内切换。",
      fail: "设置失败",
    });
  const remove = async (kernel: string, version: string) =>
    (await confirm({ title: `删除 ${kernelNames[kernel]} ${version}？`, description: "只删除中心的缓存；以后需要时可以再下载。", confirmText: "删除", tone: "danger" })) &&
    run("del:" + kernel + version, () => api(`/admin/kernels/${kernel}/versions/${encodeURIComponent(version)}`, "DELETE"), "已删除");
  if (q.isPending || q.error) return <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} skeleton={<SkeletonCards count={3} lines={3} />} />;
  return (
    <>
      {Object.entries(q.data).sort(([a], [b]) => order.indexOf(a) - order.indexOf(b)).map(([kernel, k]) => {
        const job = latestJob(jobs.data, `kernel:${kernel}:`);
        return (
          <Card key={kernel} {...dataObject({ type: "kernel", key: kernel })}>
            <CardContent className="stack">
              <SectionHeader
                title={kernelNames[kernel] ?? kernel}
                description={`${k.default ? `默认版本 ${k.default}` : "未设置默认版本：VPS 保持正在运行的版本，新部署的 VPS 使用已下载的最新版本。下载新版本不会自动替换。"}需要的架构：${k.arches.join("、")}（按中心与已接入的 VPS 自动决定）。`}
                actions={<Button size="sm" variant="outline" onClick={() => setReleases(kernel)}><Download />拉取版本</Button>}
              />
              <JobLine job={job} idle="没有进行中的下载" onFinish={() => void q.refetch()} />
              {k.versions.length === 0 ? <small>尚未下载</small> : (
                <ul className="m-0 list-none space-y-2 p-0">
                  {k.versions.map((v) => {
                    const users = [...(v.pinned_refs ?? []).map((g) => ({ ...g, how: "锁定" })), ...(v.running_refs ?? []).map((g) => ({ ...g, how: "运行中" }))];
                    const missing = k.arches.filter((a) => !v.arches[a]);
                    return (
                      <li key={v.version} {...dataObject({ type: "kernel", key: `${kernel}@${v.version}` })} className="flex flex-wrap items-center gap-2 rounded-[var(--weir-radius)] border border-[var(--weir-line)] px-3 py-2">
                        <code className="text-xs">{v.version}</code>
                        {v.default && <Badge variant="soft" tone="ok">默认</Badge>}
                        {missing.length > 0 && <Badge variant="soft" tone="warn">缺 {missing.join("、")}</Badge>}
                        {kernel !== "mihomo" && (v.driver ? <Badge variant="soft" tone="neutral">驱动 <ObjectLink to={{ type: "driver", key: v.driver, name: v.driver }} /></Badge> : <Badge variant="soft" tone="bad" title={v.driver_error}>没有驱动 · <ObjectLink to={{ type: "driver", key: "new", name: "新增驱动" }} /></Badge>)}
                        {users.length > 0 && <small className="text-[var(--weir-ink-3)]">{users.map((g, i) => <span key={g.how + g.id}>{i > 0 && "、"}<ObjectLink to={{ type: "gate", id: g.id, name: g.name }} />（{g.how}）</span>)}</small>}
                        <span className="ml-auto flex gap-1">
                          {missing.length > 0 && <Button size="xs" variant="ghost" disabled={isBusy()} busy={isBusy("fetch:" + kernel + v.version)} busyText="正在开始…" onClick={() => void run("fetch:" + kernel + v.version, async () => started((await api(`/admin/kernels/${kernel}/fetch`, "POST", { version: v.version })).job.id), `开始下载 ${missing.join("、")}`)}><Download />补下</Button>}
                          {!v.default && <Button size="xs" variant="ghost" disabled={isBusy() || (kernel !== "mihomo" && !v.driver)} busy={isBusy("default:" + kernel + v.version)} busyText="正在设置…" onClick={() => void setDefault(kernel, v.version)}><Star />设为默认</Button>}
                          {!v.default && users.length === 0 && <Button size="xs" variant="ghost" aria-label={`删除 ${v.version}`} disabled={isBusy()} busy={isBusy("del:" + kernel + v.version)} busyText="正在删除" onClick={() => void remove(kernel, v.version)}><Trash2 /></Button>}
                        </span>
                      </li>
                    );
                  })}
                </ul>
              )}
            </CardContent>
          </Card>
        );
      })}
      {releases && <ReleasesDialog kernel={releases} arches={q.data[releases]?.arches ?? []} jobs={jobs.data} close={() => setReleases(null)} fetched={() => void q.refetch()} />}
    </>
  );
}

// SubChecks：中心用 mihomo -t / sing-box check 检查生成的 Clash / sing-box 订阅；这里列出各订阅最近一次的结果。
function SubChecks() {
  const q = useQuery({ queryKey: ["sub-checks"], queryFn: () => api("/admin/sub-checks") as Promise<{ kernels: Record<string, { available: boolean; version?: string; error?: string }>; items: SubCheck[] }>, refetchInterval: 60000 });
  return (
    <Card>
      <CardContent className="stack">
        <SectionHeader title="订阅检查" description="客户端拉取 Clash / sing-box 订阅后，中心在后台用 mihomo -t 与 sing-box check 检查生成的内容（同一内容 24 小时内只检查一次）。检查不影响订阅输出；内核尚未下载或中心不是 Linux 时显示「未知」。" />
        {q.isPending || q.error ? <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} skeleton={<SkeletonList rows={2} />} /> : (
          <div className="stack">
            <div className="flex flex-wrap gap-3 text-[13px]">
              {Object.entries(q.data.kernels).map(([k, v]) => (
                <span key={k}>{k}：{v.available ? <code>{v.version}</code> : <span className="text-[var(--weir-ink-3)]">不可用（{v.error}）</span>}</span>
              ))}
            </div>
            {q.data.items.length === 0 ? <small className="text-[var(--weir-ink-3)]">还没有检查记录：客户端拉取 Clash / sing-box 订阅后会出现在这里。</small> : (
              <ul className="m-0 list-none space-y-2 p-0">
                {q.data.items.map((c) => (
                  <li key={c.ref + c.target} className="rounded-[var(--weir-radius)] border border-[var(--weir-line)] px-3 py-2">
                    <div className="flex flex-wrap items-center gap-2">
                      <strong className="text-[13px]">{c.name}</strong>
                      <SubCheckBadge check={c} />
                      {c.version && <code className="text-xs">{c.version}</code>}
                      <small className="ml-auto text-[var(--weir-ink-3)]">{date(c.at)}</small>
                    </div>
                    {c.error && <CodeBlock className="mt-2 block max-h-40 overflow-auto whitespace-pre-wrap text-xs">{c.error}</CodeBlock>}
                  </li>
                ))}
              </ul>
            )}
          </div>
        )}
      </CardContent>
    </Card>
  );
}

function ReleasesDialog({ kernel, arches, jobs, close, fetched }: { kernel: string; arches: string[]; jobs?: Job[]; close: () => void; fetched: () => void }) {
  const q = useQuery({ queryKey: ["kernel-releases", kernel], queryFn: () => api(`/admin/kernels/${kernel}/releases`) as Promise<{ releases: Release[]; arches: string[]; recommended: { version: string; reason: string } }> });
  const [job, setJob] = useState<string | null>(null);
  const started = useStartedJob();
  const { run, isBusy } = useRowAction<string>();
  const download = (version: string) =>
    run(version, async () => setJob(started((await api(`/admin/kernels/${kernel}/fetch`, "POST", { version })).job.id)), `开始下载 ${kernelNames[kernel]} ${version}`, { fail: "下载没有开始" });
  return (
    <Dialog open onOpenChange={(o) => !o && close()}>
      <DialogContent size="lg">
        <DialogTitle>{kernelNames[kernel] ?? kernel} · 上游版本</DialogTitle>
        <DialogDescription>
          {kernel === "sing-box" ? "sing-box 来自本项目 CI 跟随上游标签编译（带 with_v2ray_api，按用户统计流量需要）；下载量取上游 SagerNet/sing-box。" : "来自上游 GitHub 正式发布。"}
          下载经镜像回退、可续传，摘要不一致即丢弃。一次下载 {arches.join("、") || "需要的架构"}。关掉窗口下载照常进行。
        </DialogDescription>
        <JobLine id={job} job={latestJob(jobs, `kernel:${kernel}:`)} idle="点版本右侧的「下载」开始；进度显示在这里" onFinish={fetched} />
        {q.isPending || q.error ? <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} skeleton={<SkeletonList rows={5} lines={1} className="stack" rowClassName="flex items-center justify-between gap-2" />} /> : q.data.releases.length === 0 ? <State empty="没有可用版本" /> : (
          <ul className="stack weir-focus-gutter max-h-[50vh] overflow-y-auto">
            {q.data.releases.map((r) => (
              <li key={r.version} className="flex flex-wrap items-center justify-between gap-2">
                <span className="flex flex-wrap items-center gap-2">
                  <code>{r.version}</code>
                  {r.recommended && <Badge variant="soft" tone="ok" title={q.data.recommended?.reason}>推荐</Badge>}
                  {kernel !== "mihomo" && (r.driver ? <Badge variant="soft" tone="neutral">驱动 <ObjectLink to={{ type: "driver", key: r.driver, name: r.driver }} /></Badge> : <Badge variant="soft" tone="warn">没有驱动</Badge>)}
                  {r.incompatible && <Badge variant="soft" tone="warn" title={r.incompatible}>有不兼容</Badge>}
                  {r.young && <small className="text-[var(--weir-ink-3)]">发布不满 14 天</small>}
                  <small className="tabular-nums text-[var(--weir-ink-3)]">{date(r.published_at)}{r.downloads ? ` · ${q.data.arches.join("+")} 下载 ${q.data.arches.reduce((n, a) => n + (r.downloads?.[a] ?? 0), 0)}` : ""}</small>
                </span>
                <Button size="sm" variant="outline" disabled={isBusy()} onClick={() => void download(r.version)}>下载</Button>
                {r.recommended && q.data.recommended?.reason && <small className="basis-full text-[var(--weir-ink-3)]">推荐理由：{q.data.recommended.reason}</small>}
              </li>
            ))}
          </ul>
        )}
      </DialogContent>
    </Dialog>
  );
}

// KernelDiagnostics 显示 VPS 上某个内核的运行状态、日志分类计数与最近日志（IP 已遮盖）。
export function KernelDiagnostics({ result }: { result: { running?: boolean; version?: string; counts?: Record<string, number>; tail?: string[]; truncated?: boolean } }) {
  return (
    <div className="stack">
      <small>{result.running ? "运行中" : "未运行"}{result.version ? ` · ${result.version}` : ""}{result.counts ? " · " + Object.entries(result.counts).map(([k, v]) => `${k} ${v}`).join("，") : ""}</small>
      <CodeBlock className="block max-h-64 overflow-auto whitespace-pre text-xs">{(result.tail ?? []).join("\n") || "（没有日志）"}</CodeBlock>
      {result.truncated && <small className="text-[var(--weir-ink-3)]">只显示最近 50 行。</small>}
    </div>
  );
}
