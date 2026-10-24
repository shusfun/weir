// 内核：中心从上游拉取 Xray / sing-box / mihomo 并按官方摘要校验，再推送给 VPS；配置按内核版本档案生成。
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Badge, Button, Card, CardContent, CodeBlock, Dialog, DialogContent, DialogDescription, DialogTitle, Notice, PageHeader, SectionHeader, Segmented, SkeletonCards, SkeletonList, useToast } from "@weir/ui";
import { Download, Star } from "lucide-react";
import { api } from "../api";
import { State } from "../components/feedback";
import { DshVersions } from "../components/dsh";
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
  const toast = useToast();
  const q = useQuery({ queryKey: ["kernels"], queryFn: async () => (await api("/admin/kernels")).kernels as Record<string, KernelInfo> });
  const profiles = useQuery({ queryKey: ["kernel-profiles"], queryFn: async () => (await api("/admin/kernels/profiles")).profiles as Record<string, string> });
  const [releases, setReleases] = useState<string | null>(null);
  const [busy, setBusy] = useState("");
  async function setDefault(kernel: string, version: string) {
    setBusy(kernel + version);
    try {
      await api("/admin/kernels/defaults", "PUT", { [kernel]: version });
      toast(`${names[kernel]} 默认版本：${version}`, { tone: "ok", description: "未锁定版本的 VPS 会在数秒内切换。" });
      await q.refetch();
    } catch (e) {
      toast("设置失败", { description: String(e), tone: "bad" });
    } finally {
      setBusy("");
    }
  }
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
                          {k.default === v ? <Badge variant="soft" tone="ok">默认</Badge> : kernel !== "mihomo" && <Button size="xs" variant="ghost" disabled={busy === kernel + v} onClick={() => void setDefault(kernel, v)}><Star />设为默认</Button>}
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
      <DshVersions />
      <Card>
        <CardContent className="stack">
          <SectionHeader title="版本档案" description="每个档案声明某一内核版本范围支持的协议、传输与安全层，以及生成配置时要去掉的字段；超出档案的组合会被明确拒绝，不会静默降级。" />
          {profiles.isPending || profiles.error ? <State loading={profiles.isPending} error={profiles.error} onRetry={() => profiles.refetch()} /> : (
            <div className="stack">
              {Object.entries(profiles.data).sort(([a], [b]) => a.localeCompare(b)).map(([name, text]) => (
                <details key={name}>
                  <summary className="cursor-pointer text-sm"><code>{name}</code></summary>
                  <CodeBlock className="mt-2 block overflow-auto whitespace-pre text-xs">{text}</CodeBlock>
                </details>
              ))}
            </div>
          )}
        </CardContent>
      </Card>
      {releases && <ReleasesDialog kernel={releases} close={() => setReleases(null)} fetched={() => void q.refetch()} />}
    </div>
  );
}

function ReleasesDialog({ kernel, close, fetched }: { kernel: string; close: () => void; fetched: () => void }) {
  const toast = useToast();
  const q = useQuery({ queryKey: ["kernel-releases", kernel], queryFn: async () => (await api(`/admin/kernels/${kernel}/releases`)).releases as Release[] });
  const [arch, setArch] = useState<Arch>("amd64");
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  async function fetchVersion(version: string) {
    setBusy(version);
    setError("");
    try {
      // 后台下载：每 2 秒查询一次，最多等 25 分钟。
      await api(`/admin/kernels/${kernel}/fetch`, "POST", { version, arch });
      const query = `/admin/kernels/${kernel}/fetch?version=${encodeURIComponent(version)}&arch=${arch}`;
      for (let i = 0; i < 750; i++) {
        await new Promise((r) => setTimeout(r, 2000));
        const st = await api(query);
        if (st.status === "done") {
          toast(`已下载 ${kernel} ${version}（${arch}）`, { tone: "ok", description: "SHA-256 " + String(st.sha256).slice(0, 16) + "…" });
          fetched();
          return;
        }
        if (st.status === "failed") throw new Error("下载失败：" + st.error);
        if (st.status === "none") throw new Error("下载状态已丢失，请重试。");
      }
      throw new Error("下载超时，请稍后查看缓存版本。");
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy("");
    }
  }
  return (
    <Dialog open onOpenChange={(o) => !o && !busy && close()}>
      <DialogContent size="lg">
        <DialogTitle>{names[kernel] ?? kernel} · 上游版本</DialogTitle>
        <DialogDescription>{kernel === "sing-box" ? "sing-box 来自本项目 CI 跟随上游标签编译（带 with_v2ray_api，按用户统计流量需要）。" : "来自上游 GitHub 正式发布。"}下载经镜像回退，摘要不一致即丢弃。</DialogDescription>
        <Segmented aria-label="架构" value={arch} onValueChange={setArch} options={[["amd64", "amd64"], ["arm64", "arm64"]] as const} />
        {q.isPending || q.error ? <State loading={q.isPending} error={q.error} onRetry={() => q.refetch()} skeleton={<SkeletonList rows={5} lines={1} className="stack" rowClassName="flex items-center justify-between gap-2" />} /> : q.data.length === 0 ? <State empty="没有可用版本" /> : (
          <ul className="stack mg-focus-gutter max-h-[50vh] overflow-y-auto">
            {q.data.map((r) => (
              <li key={r.version} className="flex items-center justify-between gap-2">
                <span><code>{r.version}</code><small className="ml-2 text-[var(--mg-ink-3)]">{date(r.published_at)}</small></span>
                <Button size="sm" variant="outline" disabled={!!busy} onClick={() => void fetchVersion(r.version)}>{busy === r.version ? "正在下载…" : "下载"}</Button>
              </li>
            ))}
          </ul>
        )}
        {error && <Notice tone="bad">{error}</Notice>}
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
