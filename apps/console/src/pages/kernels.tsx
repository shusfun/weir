// 内核：中心从上游拉取 Xray / sing-box / mihomo 并按官方摘要校验，再推送给 VPS；配置按内核版本档案生成。
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Badge, Button, Card, CardContent, CodeBlock, Dialog, DialogContent, DialogDescription, DialogTitle, PageHeader, SectionHeader, Segmented, SkeletonCards, SkeletonList, Collapsible } from "@weir/ui";
import { Download, Star } from "lucide-react";
import { api } from "../api";
import { useRowAction } from "../lib/action";
import { State } from "../components/feedback";
import { DshVersions } from "../components/dsh";
import { SubCheckBadge, type SubCheck } from "../components/sub-check";
import { date } from "../lib/models";

type Arch = "amd64" | "arm64";
interface KernelInfo {
  default: string;
  cached: Record<Arch, string[] | null>;
}
interface Release {
  version: string;
  published_at: string;
}

const names: Record<string, string> = { xray: "Xray", "sing-box": "sing-box", mihomo: "mihomo（订阅检查）" };

export function KernelsPage() {
  const q = useQuery({ queryKey: ["kernels"], queryFn: async () => (await api("/admin/kernels")).kernels as Record<string, KernelInfo> });
  const profiles = useQuery({ queryKey: ["kernel-profiles"], queryFn: async () => (await api("/admin/kernels/profiles")).profiles as Record<string, string> });
  const [releases, setReleases] = useState<string | null>(null);
  const { run, isBusy } = useRowAction<string>({ refetch: q.refetch });
  const setDefault = (kernel: string, version: string) =>
    run(kernel + version, () => api("/admin/kernels/defaults", "PUT", { [kernel]: version }), `${names[kernel]} 默认版本：${version}`, {
      detail: () => "未锁定版本的 VPS 会在数秒内切换。",
      fail: "设置失败",
    });
  return (
    <div className="stack">
      <PageHeader eyebrow="工作区" title="内核" description="内核只以独立程序运行，升级不需要更新 Weir。中心拉取后按官方摘要校验，再推送给 VPS；VPS 不访问 GitHub。" />
      {q.isPending || q.error ? (
        <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} skeleton={<SkeletonCards count={3} lines={3} />} />
      ) : (
        Object.entries(q.data).map(([kernel, k]) => (
          <Card key={kernel}>
            <CardContent className="stack">
              <SectionHeader title={names[kernel] ?? kernel} description={k.default ? `默认版本 ${k.default}` : "未设置默认版本：VPS 保持正在运行的版本，新部署的 VPS 使用已下载的最新版本。下载新版本不会自动替换。"} actions={<Button size="sm" variant="outline" onClick={() => setReleases(kernel)}><Download />拉取版本</Button>} />
              {(["amd64", "arm64"] as Arch[]).map((arch) => (
                <div key={arch} className="stack">
                  <small className="text-[var(--mg-ink-3)]">{arch}</small>
                  {(k.cached[arch] ?? []).length === 0 ? <small>尚未下载</small> : (
                    <div className="flex flex-wrap gap-2">
                      {(k.cached[arch] ?? []).map((v) => (
                        <span key={v} className="flex items-center gap-1 rounded-[var(--mg-radius)] border border-[var(--mg-line)] px-2 py-1">
                          <code className="text-xs">{v}</code>
                          {k.default === v ? <Badge variant="soft" tone="ok">默认</Badge> : <Button size="xs" variant="ghost" disabled={isBusy(kernel + v)} onClick={() => void setDefault(kernel, v)}><Star />设为默认</Button>}
                        </span>
                      ))}
                    </div>
                  )}
                </div>
              ))}
            </CardContent>
          </Card>
        ))
      )}
      <SubChecks />
      <DshVersions />
      <Card>
        <CardContent className="stack">
          <SectionHeader title="版本档案" description="每个档案声明某一内核版本范围支持的协议、传输与安全层，以及生成配置时要去掉的字段；超出档案的组合会被明确拒绝，不会静默降级。" />
          {profiles.isPending || profiles.error ? <State loading={profiles.isPending} error={profiles.error} onRetry={() => profiles.refetch()} /> : (
            <div className="stack">
              {Object.entries(profiles.data).sort(([a], [b]) => a.localeCompare(b)).map(([name, text]) => (
                <Collapsible key={name} title={<code>{name}</code>}>
                  <CodeBlock className="block overflow-auto whitespace-pre text-xs">{text}</CodeBlock>
                </Collapsible>
              ))}
            </div>
          )}
        </CardContent>
      </Card>
      {releases && <ReleasesDialog kernel={releases} close={() => setReleases(null)} fetched={() => void q.refetch()} />}
    </div>
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
                <span key={k}>{k}：{v.available ? <code>{v.version}</code> : <span className="text-[var(--mg-ink-3)]">不可用（{v.error}）</span>}</span>
              ))}
            </div>
            {q.data.items.length === 0 ? <small className="text-[var(--mg-ink-3)]">还没有检查记录：客户端拉取 Clash / sing-box 订阅后会出现在这里。</small> : (
              <ul className="m-0 list-none space-y-2 p-0">
                {q.data.items.map((c) => (
                  <li key={c.ref + c.target} className="rounded-[var(--mg-radius)] border border-[var(--mg-line)] px-3 py-2">
                    <div className="flex flex-wrap items-center gap-2">
                      <strong className="text-[13px]">{c.name}</strong>
                      <SubCheckBadge check={c} />
                      {c.version && <code className="text-xs">{c.version}</code>}
                      <small className="ml-auto text-[var(--mg-ink-3)]">{date(c.at)}</small>
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

// 后台下载：每 2 秒查询一次，最多等 25 分钟。返回校验通过的 SHA-256。
async function fetchKernel(kernel: string, version: string, arch: Arch): Promise<string> {
  await api(`/admin/kernels/${kernel}/fetch`, "POST", { version, arch });
  const query = `/admin/kernels/${kernel}/fetch?version=${encodeURIComponent(version)}&arch=${arch}`;
  for (let i = 0; i < 750; i++) {
    await new Promise((r) => setTimeout(r, 2000));
    const st = await api(query);
    if (st.status === "done") return String(st.sha256);
    if (st.status === "failed") throw new Error("下载失败：" + st.error);
    if (st.status === "none") throw new Error("下载状态已丢失，请重试。");
  }
  throw new Error("下载超时，请稍后查看缓存版本。");
}

function ReleasesDialog({ kernel, close, fetched }: { kernel: string; close: () => void; fetched: () => void }) {
  const q = useQuery({ queryKey: ["kernel-releases", kernel], queryFn: async () => (await api(`/admin/kernels/${kernel}/releases`)).releases as Release[] });
  const [arch, setArch] = useState<Arch>("amd64");
  const { run, isBusy } = useRowAction<string>({ refetch: fetched });
  const download = (version: string) =>
    run(version, () => fetchKernel(kernel, version, arch), `已下载 ${kernel} ${version}（${arch}）`, {
      detail: (sha) => "SHA-256 " + String(sha).slice(0, 16) + "…",
      fail: `${kernel} 下载未完成`,
    });
  return (
    <Dialog open onOpenChange={(o) => !o && !isBusy() && close()}>
      <DialogContent size="lg">
        <DialogTitle>{names[kernel] ?? kernel} · 上游版本</DialogTitle>
        <DialogDescription>{kernel === "sing-box" ? "sing-box 来自本项目 CI 跟随上游标签编译（带 with_v2ray_api，按用户统计流量需要）。" : "来自上游 GitHub 正式发布。"}下载经镜像回退，摘要不一致即丢弃。</DialogDescription>
        <Segmented aria-label="架构" value={arch} onValueChange={setArch} options={[["amd64", "amd64"], ["arm64", "arm64"]] as const} />
        {q.isPending || q.error ? <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} skeleton={<SkeletonList rows={5} lines={1} className="stack" rowClassName="flex items-center justify-between gap-2" />} /> : q.data.length === 0 ? <State empty="没有可用版本" /> : (
          <ul className="stack mg-focus-gutter max-h-[50vh] overflow-y-auto">
            {q.data.map((r) => (
              <li key={r.version} className="flex items-center justify-between gap-2">
                <span><code>{r.version}</code><small className="ml-2 text-[var(--mg-ink-3)]">{date(r.published_at)}</small></span>
                <Button size="sm" variant="outline" disabled={isBusy()} onClick={() => void download(r.version)}>{isBusy(r.version) ? "正在下载…" : "下载"}</Button>
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
      {result.truncated && <small className="text-[var(--mg-ink-3)]">只显示最近 50 行。</small>}
    </div>
  );
}
