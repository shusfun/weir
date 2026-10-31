// 测速下载：浏览器对 GitHub 直连与镜像测速，自动选最快可校验来源，下载后核对 SHA-256。
import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChoiceCard, CodeBlock, Field, Notice,
  Button, Dialog, DialogContent, DialogTitle, DialogDescription, DialogFooter, Progress, StatusLamp,
  Select, SelectTrigger, SelectValue, SelectContent, SelectItem, SectionHeader, Radio, RadioGroup, FormGrid,
} from "@weir/ui";
import { Download, Gauge, RefreshCw } from "lucide-react";
import { api, clause } from "../api";
import { State } from "./feedback";
import type { CatalogClient } from "../lib/models";
import { assetsFor, candidates, detectPlatform, downloadVerified, probe, rank, speed, type ProbeResult, type ReleaseAsset } from "../lib/download";

interface Release { status: string; version?: string; url?: string; assets?: ReleaseAsset[] }

// 中心读 GitHub Release 的结果（status 不是 current 时）
const releaseReason: Record<string, string> = {
  pending: "中心正在向 GitHub 查询，稍后再打开",
  not_found: "GitHub 上没有这个仓库的正式发布",
  rate_limited: "中心访问 GitHub 的次数超过限制，稍后再试",
  not_configured: "这个客户端没有配置发布仓库",
  unknown: "GitHub 没有返回可用的发布信息（中心连不上 GitHub，或返回内容无法解析）",
};
const platformNames: Record<string, string> = { windows: "Windows", macos: "macOS", linux: "Linux", android: "Android" };

export function ClientDownload({ client, primary = false }: { client: CatalogClient; primary?: boolean }) {
  const [open, setOpen] = useState(false);
  const label = "下载 " + client.name;
  if (!client.repo)
    return <Button variant={primary ? "default" : "outline"} size={primary ? "default" : "sm"} onClick={() => window.open(client.url, "_blank", "noopener,noreferrer")}><Download />{label}</Button>;
  return (
    <>
      <Button variant={primary ? "default" : "outline"} size={primary ? "default" : "sm"} onClick={() => setOpen(true)}><Gauge />测速下载</Button>
      {open && <DownloadDialog client={client} close={() => setOpen(false)} />}
    </>
  );
}

function DownloadDialog({ client, close }: { client: CatalogClient; close: () => void }) {
  const release = useQuery({ queryKey: ["client-release", client.id, client.repo], queryFn: () => api("/clients/" + encodeURIComponent(client.id) + "/release") as Promise<Release>, staleTime: 300000, retry: false });
  const mirrors = useQuery({ queryKey: ["client-mirrors"], queryFn: () => api("/client-mirrors") as Promise<{ mirrors: string[] }>, staleTime: 300000 });
  const supported = client.platforms.filter((p) => platformNames[p]);
  const [platform, setPlatform] = useState(() => (supported.includes(detectPlatform()) ? detectPlatform() : supported[0] ?? ""));
  const assets = useMemo(() => assetsFor(release.data?.assets ?? [], platform), [release.data, platform]);
  const [assetName, setAssetName] = useState("");
  const asset = assets.find((a) => a.name === assetName) ?? assets[0];
  const [results, setResults] = useState<ProbeResult[] | null>(null);
  const [chosen, setChosen] = useState("");
  const [progress, setProgress] = useState<number | null>(null);
  const [message, setMessage] = useState<{ tone: "ok" | "bad"; text: string } | null>(null);

  async function runProbe() {
    if (!asset) return;
    setResults(null);
    setMessage(null);
    const list = await Promise.all(candidates(asset, mirrors.data?.mirrors ?? []).map((c) => probe(c.url, c.mirror)));
    const ranked = rank(list);
    setResults([...ranked, ...list.filter((r) => r.error)]);
    setChosen(ranked[0]?.url ?? "");
  }
  // 打开或切换安装包后自动测速；镜像读取失败时只测 GitHub 直连
  const mirrorsSettled = !mirrors.isPending;
  useEffect(() => {
    if (asset && mirrorsSettled) void runProbe();
  }, [asset?.url, mirrorsSettled]);

  const pick = results?.find((r) => r.url === chosen);
  async function start() {
    if (!asset || !pick) return;
    setMessage(null);
    if (!pick.verifiable) {
      window.open(pick.url, "_blank", "noopener,noreferrer");
      setMessage({ tone: "ok", text: "此来源不允许浏览器读取内容，已在新窗口打开，无法自动校验。请下载后核对下方 SHA-256。" });
      return;
    }
    try {
      setProgress(0);
      const r = await downloadVerified(pick.url, asset, setProgress);
      setMessage({ tone: "ok", text: r.verified ? "下载完成，SHA-256 校验一致。" : "下载完成。GitHub 未公布 SHA-256，未能校验。" });
    } catch (e) {
      setMessage({ tone: "bad", text: e instanceof Error ? e.message : "下载失败，请重试。" });
    } finally {
      setProgress(null);
    }
  }

  const busy = progress !== null;
  return (
    <Dialog open onOpenChange={(v) => !v && !busy && close()}>
      <DialogContent size="md">
        <DialogTitle>下载 {client.name}</DialogTitle>
        <DialogDescription>在你的浏览器内测速，自动选择最快的可校验来源；下载后核对 GitHub 公布的 SHA-256。</DialogDescription>
        {mirrors.isError && <Notice tone="warn" role="status">加速镜像暂时不可用，本次只测速 GitHub 直连。</Notice>}
        {release.isPending || mirrors.isPending ? <State loading /> : release.data?.status !== "current" ? (
          <Notice tone="bad">暂时无法读取正式发布信息：{release.error ? clause(release.error) : releaseReason[release.data?.status ?? ""] ?? `中心返回的状态是「${release.data?.status}」`}。请改用发布页下载。</Notice>
        ) : (
          <div className="stack">
            <FormGrid>
              <Field label="平台">
                <Select value={platform} onValueChange={(v) => { setPlatform(String(v)); setAssetName(""); }}>
                  <SelectTrigger><SelectValue>{platformNames[platform] ?? platform}</SelectValue></SelectTrigger>
                  <SelectContent>{supported.map((p) => <SelectItem key={p} value={p}>{platformNames[p]}</SelectItem>)}</SelectContent>
                </Select>
              </Field>
              <Field label={"安装包 · " + release.data.version}>
                <Select value={asset?.name ?? ""} onValueChange={(v) => setAssetName(String(v))}>
                  <SelectTrigger><SelectValue>{asset?.name ?? "此平台没有安装包"}</SelectValue></SelectTrigger>
                  <SelectContent>{assets.map((a) => <SelectItem key={a.name} value={a.name}>{a.name}</SelectItem>)}</SelectContent>
                </Select>
              </Field>
            </FormGrid>
            {asset && (
              <RadioGroup className="download-sources" aria-label="下载来源" value={chosen} onValueChange={(v) => setChosen(String(v))} disabled={busy}>
                <SectionHeader size="sm" flush title="来源测速" actions={<Button variant="ghost" size="sm" disabled={!results || busy} onClick={runProbe}><RefreshCw />重新测速</Button>} />
                {!results ? <p role="status">正在测速…</p> : results.map((r, i) => (
                  <ChoiceCard key={r.url} className="download-source" selected={r.url === chosen} render={<label/>}>
                    <Radio value={r.url} disabled={!!r.error} />
                    <StatusLamp tone={r.error ? "bad" : r.verifiable ? "ok" : "warn"} />
                    <span>
                      <strong>{r.mirror ? "镜像 · " + r.host : "GitHub 直连"}{i === 0 && !r.error && " · 最快"}</strong>
                      <small>{r.error ?? (r.verifiable ? speed(r.bytesPerSecond) + " · 可校验" : (r.latencyMs ?? "?") + " ms · 不可自动校验")}</small>
                    </span>
                  </ChoiceCard>
                ))}
              </RadioGroup>
            )}
            {asset?.sha256 && <CodeBlock aria-label="SHA-256">SHA-256 {asset.sha256}</CodeBlock>}
            {busy && <Progress value={Math.round(progress * 100)} aria-label="下载进度" />}
            {message && <Notice tone={message.tone === "bad" ? "bad" : "info"} role={message.tone === "bad" ? "alert" : "status"}>{message.text}</Notice>}
          </div>
        )}
        <DialogFooter>
          <Button variant="outline" disabled={busy} onClick={() => window.open(release.data?.url ?? client.url, "_blank", "noopener,noreferrer")}>打开发布页</Button>
          <Button disabled={!pick || busy} onClick={start}><Download />{busy ? `下载中 ${Math.round((progress ?? 0) * 100)}%` : "下载"}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
