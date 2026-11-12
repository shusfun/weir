import { PendingExits } from "../components/pending-exits";
// 版本档案与中心二进制缓存分开显示，VPS 优先按固定档案直接下载。
// 分区记在地址栏 ?tab=（版本 / 驱动 / 订阅检查 / dsh）。
import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useQuery } from "../lib/query";
import { useSearchParams } from "react-router-dom";
import { Badge, Button, Card, CodeBlock, InsightBanner, Dialog, DialogContent, DialogDescription, DialogTitle, InsetGroup, InsetRow, InsetRows, JobPlaceholder, PageHeader, SkeletonCards, SkeletonList, Table, VersionCell, TableBody, TableCell, TableHead, TableHeader, TableRow, Tabs, TabsList, TabsPanel, TabsTab, useConfirm } from "@weir/ui";
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
import { Link } from "react-router-dom";
import { TextLink } from "@weir/ui";
import { useConsoleContext } from "../lib/console-context";

interface KernelVersion {
  version: string;
  arches: Record<string, boolean>;
  center_binaries?: Record<string, boolean>;
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
interface Recommended {
  recommended: { version: string; reason: string; latest: string };
  current: string;
  cached: boolean;
  update: boolean;
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

export const kernelNames: Record<string, string> = { xray: "Xray", "sing-box": "sing-box", mihomo: "mihomo（连接实测与订阅检查）" };
const kernelUses: Record<string, string> = { xray: "运行 VLESS 类节点，创建这类节点时自动下载。", "sing-box": "运行 SS2022、Hysteria2、TUIC、ShadowTLS 节点，创建这类节点时自动下载；中心还用它检查 sing-box 订阅。", mihomo: "中心检查 Clash 订阅、实测节点；按需自动下发到 VPS，只在测试时临时运行。" };
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
      <KernelHeader />
      {tab === "versions" && <KernelUpgrade />}
      {tab === "versions" && <KernelMatrix />}
      <Tabs value={tab} onValueChange={(v) => setParams({ tab: String(v) }, { replace: true })}>
        <TabsList aria-label="内核分区">
          {tabs.map((t) => <TabsTab key={t.key} value={t.key}>{t.label}</TabsTab>)}
        </TabsList>
        <TabsPanel value="versions" className="stack"><Versions /></TabsPanel>
        <TabsPanel value="drivers" className="stack"><DriversPanel /></TabsPanel>
        <TabsPanel value="checks" className="stack"><SubChecks /></TabsPanel>
        <TabsPanel value="dsh" className="stack"><TextLink render={<Link to="/system?tab=ai" />}>返回系统设置的「AI 与 dsh」</TextLink><DshVersions /></TabsPanel>
      </Tabs>
      <PendingExits all />
    </div>
  );
}

// 页头摘要（稿子 CKernels）：几台 VPS、几个内核、几个可升级
function KernelHeader() {
  const gates = useQuery({ queryKey: ["gates"], queryFn: async () => (await api("/admin/gates")).items as { id: number }[] });
  const kernels = useQuery({ queryKey: ["kernels"], queryFn: async () => (await api("/admin/kernels")).kernels as Record<string, KernelInfo> });
  const recs = useQueries(order);
  const ups = recs.filter((r) => r.data?.update).length;
  return (
    <PageHeader
      size="data"
      title="内核"
      description={<span className="weir-sum"><span><b>{gates.data?.length ?? "未知"}</b> 台 VPS</span><span><b>{kernels.data ? Object.keys(kernels.data).length : "未知"}</b> 个内核</span>{ups > 0 && <span className="text-[var(--weir-warn-ink)]">{ups} 个可升级</span>}<span>升级内核不需要更新 Weir</span></span>}
    />
  );
}

function useQueries(names: readonly string[]) {
  return names.map((kernel) =>
    // eslint-disable-next-line react-hooks/rules-of-hooks -- 内核名单是固定常量，调用顺序不变
    useQuery({ queryKey: ["kernel-recommended", kernel], queryFn: () => api(`/admin/kernels/${kernel}/recommended`) as Promise<Recommended>, retry: false, staleTime: 5 * 60_000 }),
  );
}

// 可升级横幅：只取第一个有推荐更新的内核；升级要确认，点了跳到下面对应的「下载并设为默认」
function KernelUpgrade() {
  const recs = useQueries(order);
  const i = recs.findIndex((r) => r.data?.update);
  if (i < 0) return null;
  const r = recs[i].data!;
  return (
    <InsightBanner
      icon="↑"
      laya={false}
      title={`${kernelNames[order[i]] ?? order[i]} ${r.recommended.version} 可用`}
      confidence={null}
      progress={r.recommended.reason || "升级会让用到它的节点重启一次"}
      className="max-w-none"
      action={<Button onClick={() => document.querySelector(`[data-object="kernel:${order[i]}"]`)?.scrollIntoView({ behavior: "smooth", block: "center" })}>去升级</Button>}
    />
  );
}

function KernelMatrix() {
  const gates = useQuery({ queryKey: ["gates"], queryFn: async () => (await api("/admin/gates")).items as { id: number; name: string; version?: string; arch?: string; public_host?: string; kernels_running?: Record<string, string> }[] });
  const kernels = useQuery({ queryKey: ["kernels"], queryFn: async () => (await api("/admin/kernels")).kernels as Record<string, KernelInfo> });
  if (gates.isPending) return <State loading />;
  if (gates.error) return <State error={gates.error} onRetry={() => void gates.refetch()} />;
  const def = (k: string) => kernels.data?.[k]?.default;
  // 运行版本与默认版本不同、且默认更新时，标成可升级；升级影响写会重启哪个内核
  const upgrade = (k: string, running?: string) => (running && def(k) && def(k) !== running ? def(k) : null);
  return (
    <Card className="overflow-hidden p-0">
      <Table>
        <TableHeader><TableRow><TableHead className="w-[251px]">VPS</TableHead><TableHead>Xray</TableHead><TableHead>sing-box</TableHead><TableHead>mihomo（中心）</TableHead><TableHead>升级影响</TableHead></TableRow></TableHeader>
        <TableBody>
          {(gates.data ?? []).map((g) => {
            const run = g.kernels_running;
            const ups = (["xray", "sing-box"] as const).filter((k) => upgrade(k, run?.[k]));
            return (
              <TableRow key={g.id}>
                <TableCell><b>{g.name}</b><div className="font-[family-name:var(--weir-mono)] text-fine text-[var(--weir-ink-3)]">{g.public_host || "地址未知"} · Gate {g.version || "未知"}</div></TableCell>
                {(["xray", "sing-box"] as const).map((k) => <TableCell key={k}><VersionCell version={run ? (run[k] ?? "") : null} upgrade={upgrade(k, run?.[k])} /></TableCell>)}
                <TableCell className="text-meta text-[var(--weir-ink-3)]">—</TableCell>
                <TableCell className={ups.length ? "text-meta text-[var(--weir-warn-ink)]" : "text-meta text-[var(--weir-ink-3)]"}>{!run ? "未知" : ups.length ? `${ups.map((k) => kernelNames[k]).join("、")} 重启一次` : "无需升级"}</TableCell>
              </TableRow>
            );
          })}
          <TableRow>
            <TableCell><b>中心</b><div className="text-fine text-[var(--weir-ink-3)]">节点实测与订阅检查</div></TableCell>
            <TableCell className="text-meta text-[var(--weir-ink-3)]">—</TableCell>
            <TableCell className="text-meta text-[var(--weir-ink-3)]">—</TableCell>
            <TableCell><VersionCell version={kernels.data ? def("mihomo") ?? "" : null} /></TableCell>
            <TableCell className="text-meta text-[var(--weir-ink-3)]">不影响节点</TableCell>
          </TableRow>
        </TableBody>
      </Table>
    </Card>
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
    (await confirm({ title: `删除 ${kernelNames[kernel]} ${version}？`, description: "删除中心的发行档案与二进制缓存；以后需要时可以重新准备。", confirmText: "删除", tone: "danger" })) &&
    run("del:" + kernel + version, () => api(`/admin/kernels/${kernel}/versions/${encodeURIComponent(version)}`, "DELETE"), "已删除");
  if (q.isPending || q.error) return <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} skeleton={<SkeletonCards count={3} lines={3} />} />;
  return (
    <>
      {Object.entries(q.data).sort(([a], [b]) => order.indexOf(a) - order.indexOf(b)).map(([kernel, k]) => {
        const job = latestJob(jobs.data, `kernel:${kernel}:`);
        return (
          <InsetGroup
            key={kernel}
            {...dataObject({ type: "kernel", key: kernel })}
            title={kernelNames[kernel] ?? kernel}
            description={`${kernelUses[kernel] ?? ""} ${k.default ? `默认版本 ${k.default}。` : "首次准备稳定版的发行档案；已有版本继续遵循锁定、默认与运行版本规则。"} 需要的架构：${k.arches.join("、")}。中心自用与过渡期旧 VPS 才预下载二进制，新 VPS 自行下载并完成实际检查。`}
            actions={<Button size="sm" variant="outline" onClick={() => setReleases(kernel)}><Download />拉取版本</Button>}
          >
              <JobLine job={job} idle="没有进行中的下载" onFinish={() => void q.refetch()} />
              {k.versions.length > 0 && <UpdateAdvice kernel={kernel} onSetDefault={setDefault} busy={isBusy} />}
              {k.versions.length === 0 ? <small>尚未准备版本</small> : (
                <InsetRows as="ul">
                  {k.versions.map((v) => {
                    const users = [...(v.pinned_refs ?? []).map((g) => ({ ...g, how: "锁定" })), ...(v.running_refs ?? []).map((g) => ({ ...g, how: "运行中" }))];
                    const missing = k.arches.filter((a) => !v.arches[a]);
                    return (
                      <InsetRow as="li" key={v.version} {...dataObject({ type: "kernel", key: `${kernel}@${v.version}` })} className="flex flex-wrap items-center gap-2">
                        <code className="text-meta">{v.version}</code>
                        {v.default && <Badge variant="soft" tone="ok">默认</Badge>}
                        {missing.length > 0 && <Badge variant="soft" tone="warn">缺 {missing.join("、")}</Badge>}
                        {missing.length === 0 && <Badge variant="soft" tone="neutral">版本已就绪</Badge>}
                        <small className="text-[var(--weir-ink-3)]">中心二进制：{v.center_binaries ? Object.entries(v.center_binaries).filter(([, cached]) => cached).map(([arch]) => arch).join("、") || "未缓存" : "未知"}</small>
                        {kernel !== "mihomo" && (v.driver ? <Badge variant="soft" tone="neutral">驱动 <ObjectLink to={{ type: "driver", key: v.driver, name: v.driver }} /></Badge> : <Badge variant="soft" tone="bad" title={v.driver_error}>没有驱动 · <ObjectLink to={{ type: "driver", key: "new", name: "新增驱动" }} /></Badge>)}
                        {users.length > 0 && <small className="text-[var(--weir-ink-3)]">{users.map((g, i) => <span key={g.how + g.id}>{i > 0 && "、"}<ObjectLink to={{ type: "gate", id: g.id, name: g.name }} />（{g.how}）</span>)}</small>}
                        <span className="ml-auto flex gap-1">
                          {missing.length > 0 && <Button size="xs" variant="ghost" disabled={isBusy()} busy={isBusy("fetch:" + kernel + v.version)} busyText="正在开始…" onClick={() => void run("fetch:" + kernel + v.version, async () => started((await api(`/admin/kernels/${kernel}/fetch`, "POST", { version: v.version })).job.id), `开始下载 ${missing.join("、")}`)}><Download />补下</Button>}
                          {!v.default && <Button size="xs" variant="ghost" disabled={isBusy() || (kernel !== "mihomo" && !v.driver)} busy={isBusy("default:" + kernel + v.version)} busyText="正在设置…" onClick={() => void setDefault(kernel, v.version)}><Star />设为默认</Button>}
                          {!v.default && users.length === 0 && <Button size="xs" variant="ghost" aria-label={`删除 ${v.version}`} disabled={isBusy()} busy={isBusy("del:" + kernel + v.version)} busyText="正在删除" onClick={() => void remove(kernel, v.version)}><Trash2 /></Button>}
                        </span>
                      </InsetRow>
                    );
                  })}
                </InsetRows>
              )}
          </InsetGroup>
        );
      })}
      {releases && <ReleasesDialog kernel={releases} info={q.data[releases]} jobs={jobs.data} close={() => setReleases(null)} fetched={() => void q.refetch()} onSetDefault={setDefault} />}
    </>
  );
}

// UpdateAdvice：更新建议，固定一行。规则与「上游版本」里的「推荐」相同；单独请求，GitHub 慢或连不上不拖累内核页。
// 升级会切换 VPS 上的内核，只建议：点了再确认才做。读不到时写「未知」，不当成已是最新。
function UpdateAdvice({ kernel, onSetDefault, busy }: { kernel: string; onSetDefault: (kernel: string, version: string) => Promise<unknown>; busy: (key?: string) => boolean }) {
  const q = useQuery({ queryKey: ["kernel-recommended", kernel], queryFn: () => api(`/admin/kernels/${kernel}/recommended`) as Promise<Recommended>, retry: false, staleTime: 5 * 60_000 });
  const started = useStartedJob();
  const client = useQueryClient();
  const { run, isBusy } = useRowAction<string>({ invalidate: [["kernels"], ["kernel-recommended", kernel]] });
  const confirm = useConfirm();
  const r = q.data?.recommended;
  const name = kernelNames[kernel] ?? kernel;
  const apply = async () => {
    if (!r) return;
    const yes = await confirm({ title: `把 ${name} 默认版本换成 ${r.version}？`, description: q.data!.cached ? "未锁定版本的 VPS 会在数秒内切换。" : "先下载这个版本，下载成功后设为默认，未锁定版本的 VPS 随之切换；下载失败时默认版本不变。", confirmText: q.data!.cached ? "设为默认" : "下载并设为默认" });
    if (!yes) return;
    if (q.data!.cached) return onSetDefault(kernel, r.version).then(() => client.invalidateQueries({ queryKey: ["kernel-recommended", kernel] }));
    return run("apply", async () => started((await api(`/admin/kernels/${kernel}/fetch`, "POST", { version: r.version, set_default: true })).job.id), `开始下载 ${name} ${r.version}`, { fail: "下载没有开始" });
  };
  const muted = "min-w-0 flex-1 truncate text-meta text-[var(--weir-ink-3)]";
  if (q.isPending) return <JobPlaceholder text="正在查询推荐版本…" />;
  if (q.error) return <JobPlaceholder text={"推荐版本未知：" + (q.error as Error).message} />;
  if (!r?.version) return <JobPlaceholder text={"推荐版本未知" + (r?.reason ? "：" + r.reason : "")} />;
  const latest = r.latest && r.latest !== r.version && r.latest !== q.data.current ? `最新 ${r.latest} 发布不满 14 天或没有适用驱动，暂不建议。` : "";
  if (!q.data.update) return <JobPlaceholder text={(q.data.current === r.version ? `已是推荐版本 ${r.version}。` : `当前 ${q.data.current} 比推荐版本 ${r.version} 新。`) + latest} />;
  return (
    <div className="flex min-h-9 min-w-0 flex-wrap items-center gap-2">
      <Badge variant="soft" tone="warn">建议更新到 {r.version}</Badge>
      <small className={muted} title={r.reason}>{r.reason}</small>
      <Button size="xs" variant="outline" disabled={isBusy() || busy()} busy={isBusy("apply")} busyText="正在开始…" onClick={() => void apply()}>{q.data.cached ? "设为默认" : "下载并设为默认"}</Button>
    </div>
  );
}

// SubChecks：中心用 mihomo -t / sing-box check 检查生成的 Clash / sing-box 订阅；这里列出各订阅最近一次的结果。
function SubChecks() {
  const q = useQuery({ queryKey: ["sub-checks"], queryFn: () => api("/admin/sub-checks") as Promise<{ kernels: Record<string, { available: boolean; version?: string; error?: string }>; items: SubCheck[] }>, refetchInterval: 60000 });
  return (
    <InsetGroup title="订阅检查" description="客户端拉取 Clash / sing-box 订阅后，中心在后台用 mihomo -t 与 sing-box check 检查生成的内容（同一内容 24 小时内只检查一次）。检查不影响订阅输出；内核尚未下载或中心不是 Linux 时显示「未知」。">
        {q.isPending || q.error ? <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} skeleton={<SkeletonList rows={2} />} /> : (
          <div className="stack">
            <div className="flex flex-wrap gap-3 text-body">
              {Object.entries(q.data.kernels).map(([k, v]) => (
                <span key={k}>{k}：{v.available ? <code>{v.version}</code> : <span className="text-[var(--weir-ink-3)]">不可用（{v.error}）</span>}</span>
              ))}
            </div>
            {q.data.items.length === 0 ? <small className="text-[var(--weir-ink-3)]">还没有检查记录：客户端拉取 Clash / sing-box 订阅后会出现在这里。</small> : (
              <ul className="group-list">
                {q.data.items.map((c) => (
                  <li key={c.ref + c.target} className="group-row">
                    <div className="flex flex-wrap items-center gap-2">
                      <strong className="text-body">{c.name}</strong>
                      <SubCheckBadge check={c} />
                      {c.version && <code className="text-meta">{c.version}</code>}
                      <small className="ml-auto text-[var(--weir-ink-3)]">{date(c.at)}</small>
                    </div>
                    {c.error && <CodeBlock className="mt-2 block max-h-40 overflow-auto whitespace-pre-wrap text-meta">{c.error}</CodeBlock>}
                  </li>
                ))}
              </ul>
            )}
          </div>
        )}
    </InsetGroup>
  );
}

function ReleasesDialog({ kernel, info, jobs, close, fetched, onSetDefault }: { kernel: string; info?: KernelInfo; jobs?: Job[]; close: () => void; fetched: () => void; onSetDefault: (kernel: string, version: string) => Promise<unknown> }) {
  const arches = info?.arches ?? [];
  const q = useQuery({ queryKey: ["kernel-releases", kernel], queryFn: () => api(`/admin/kernels/${kernel}/releases`) as Promise<{ releases: Release[]; arches: string[]; recommended: { version: string; reason: string } }> });
  const [job, setJob] = useState<string | null>(null);
  const started = useStartedJob();
  const { run, isBusy } = useRowAction<string>();
  const download = (version: string) =>
    run(version, async () => setJob(started((await api(`/admin/kernels/${kernel}/fetch`, "POST", { version })).job.id)), `开始下载 ${kernelNames[kernel]} ${version}`, { fail: "下载没有开始" });
  const local = (version: string) => info?.versions.find((v) => v.version === version);
  return (
    <Dialog open onOpenChange={(o) => !o && close()}>
      <DialogContent size="lg">
        <DialogTitle>{kernelNames[kernel] ?? kernel} · 上游版本</DialogTitle>
        <DialogDescription>
          {kernel === "sing-box" ? "sing-box 来自本项目 CI 跟随上游标签编译（带 with_v2ray_api，按用户统计流量需要）；下载量取上游 SagerNet/sing-box。" : "来自上游 GitHub 正式发布。"}
          为 {arches.join("、") || "需要的架构"} 固定发行来源、大小和摘要；VPS 按 GitHub 加速来源直接下载，失败才使用中心缓存。中心自用及旧 VPS 所需二进制会预下载。关掉窗口任务照常进行。
        </DialogDescription>
        <JobLine id={job} job={latestJob(jobs, `kernel:${kernel}:`)} idle="点版本右侧的「下载」开始；进度显示在这里" onFinish={fetched} />
        {q.isPending || q.error ? <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} skeleton={<SkeletonList rows={5} lines={1} className="stack" rowClassName="flex items-center justify-between gap-2" />} /> : q.data.releases.length === 0 ? <State empty="没有可用版本" /> : (
          <ul className="group-list">
            {q.data.releases.map((r) => {
              const have = local(r.version);
              const complete = !!have && arches.every((a) => have.arches[a]);
              return (
              <li key={r.version} className="group-row flex flex-wrap items-center justify-between gap-2 pr-1">
                <span className="flex min-w-0 flex-1 flex-wrap items-center gap-2">
                  <code>{r.version}</code>
                  {r.recommended && <Badge variant="soft" tone="ok" title={q.data.recommended?.reason}>推荐</Badge>}
                  {have?.default && <Badge variant="soft" tone="ok">默认</Badge>}
                  {kernel !== "mihomo" && (r.driver ? <Badge variant="soft" tone="neutral">驱动 <ObjectLink to={{ type: "driver", key: r.driver, name: r.driver }} /></Badge> : <Badge variant="soft" tone="warn">没有驱动</Badge>)}
                  {r.incompatible && <Badge variant="soft" tone="warn" title={r.incompatible}>有不兼容</Badge>}
                  {r.young && <small className="text-[var(--weir-ink-3)]">发布不满 14 天</small>}
                  <small className="tabular-nums text-[var(--weir-ink-3)]">{date(r.published_at)}{r.downloads ? ` · ${q.data.arches.join("+")} 下载 ${q.data.arches.reduce((n, a) => n + (r.downloads?.[a] ?? 0), 0)}` : ""}</small>
                </span>
                {!complete ? <Button size="sm" variant="outline" disabled={isBusy()} busy={isBusy(r.version)} busyText="正在开始…" onClick={() => void download(r.version)}>{have ? "补下" : "下载"}</Button>
                  : have.default ? <small className="text-[var(--weir-ink-3)]">版本已就绪</small>
                  : <Button size="sm" variant="outline" disabled={isBusy() || (kernel !== "mihomo" && !have.driver)} busy={isBusy("default:" + r.version)} busyText="正在设置…" onClick={() => void run("default:" + r.version, () => onSetDefault(kernel, r.version))}>设为默认</Button>}
                {r.recommended && q.data.recommended?.reason && <small className="basis-full text-[var(--weir-ink-3)]">推荐理由：{q.data.recommended.reason}</small>}
              </li>
            );})}
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
      <CodeBlock className="block max-h-64 overflow-auto whitespace-pre text-meta">{(result.tail ?? []).join("\n") || "（没有日志）"}</CodeBlock>
      {result.truncated && <small className="text-[var(--weir-ink-3)]">只显示最近 50 行。</small>}
    </div>
  );
}
