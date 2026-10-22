import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Field, Notice, PageState, Readout, SectionHeader,
  Badge,
  Button,
  Card,
  CardContent,
  Checkbox,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogTitle,
  Label,
  Progress,
  ProgressLabel,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@migate/ui";
import { api } from "../api";
import { State, Status } from "./feedback";

type GateRollout = { id?: number; name: string; version: string; rollout: string };
type ReleaseJob = { stage?: string; message?: string; request_pending?: boolean };
type ReleaseState = {
  current_version: string;
  latest_version: string;
  update_available: boolean;
  release_url: string;
  status: string;
  message?: string;
  gates: GateRollout[];
  job?: ReleaseJob;
};

const rolloutText: Record<string, string> = {
  pending: "待推送",
  running: "推送中",
  applied: "已是目标版本",
  failed: "推送失败",
  // 中心没有可推送的 Gate 安装包时无法判断
  unknown: "目标版本未知",
};

const scopeText: Record<string, string> = {
  retain: "保留数据和内核",
  cores: "移除托管内核",
  purge: "备份后清除本项目数据",
};

const stageValue: Record<string, number> = {
  requested: 15,
  downloading: 40,
  verifying: 60,
  verified: 70,
  installing: 85,
  restarting: 95,
};

function releaseTag(version: string) {
  const text = version.trim();
  return text.startsWith("v") ? text : "v" + text;
}

export function ReleaseUpdateCard() {
  const cache = useQueryClient();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [mode, setMode] = useState("retain");
  const [scopeConfirmed, setScopeConfirmed] = useState(false);
  const [open, setOpen] = useState(false);
  const [waitingSince, setWaitingSince] = useState(0);
  const [countdown, setCountdown] = useState(0);
  const [uninstalling, setUninstalling] = useState<GateRollout | null>(null);
  const query = useQuery({
    queryKey: ["release-status"],
    queryFn: () => api("/admin/release") as Promise<ReleaseState>,
    refetchOnWindowFocus: false,
    refetchInterval: open ? 2000 : false,
  });
  const data = query.data;
  const job = data?.job;
  const stage = job?.stage || "idle";
  const stalled = stage === "requested" && !!job?.request_pending && waitingSince > 0 && Date.now() - waitingSince > 20000;
  const failed = stage === "failed" || stalled;
  const restarting = stage === "restarting";
  useEffect(() => {
    if (!open || !restarting) return;
    setCountdown(8);
    const timer = window.setInterval(() => {
      setCountdown((value) => (value > 0 ? value - 1 : 0));
    }, 1000);
    return () => window.clearInterval(timer);
  }, [open, restarting]);
  useEffect(() => {
    if (!open || !restarting || countdown > 0) return;
    let stopped = false;
    (async () => {
      for (let attempt = 0; attempt < 5 && !stopped; attempt += 1) {
        try {
          const health = await fetch("/healthz", { cache: "no-store" });
          if (health.ok) {
            window.location.reload();
            return;
          }
        } catch {
          // 服务重启期间连接会中断。
        }
        await new Promise((resolve) => window.setTimeout(resolve, 1000));
      }
      if (!stopped) window.location.reload();
    })();
    return () => {
      stopped = true;
    };
  }, [open, restarting, countdown]);
  if (query.isPending) return <State loading />;
  if (!data) return <State error={query.error} onRetry={() => query.refetch()} />;
  const tag = releaseTag(data.latest_version || "");
  const confirm = async () => {
    setBusy(true);
    setError("");
    setWaitingSince(Date.now());
    try {
      await api("/admin/release", "POST", { confirm: true, tag });
      await cache.invalidateQueries({ queryKey: ["release-status"] });
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "发布请求失败");
    } finally {
      setBusy(false);
    }
  };
  const uninstallConfirmed = async () => {
    if (!uninstalling?.id) return;
    setBusy(true);
    setError("");
    try {
      await api(`/admin/devices/${uninstalling.id}/uninstall`, "POST", { confirm: true, mode, scope_confirmed: scopeConfirmed });
      await cache.invalidateQueries({ queryKey: ["release-status"] });
      setUninstalling(null);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "卸载请求失败");
    } finally {
      setBusy(false);
    }
  };
  const progress = stageValue[stage] || 10;
  const progressText = stalled ? "安装单元没有开始" : job?.message || "已提交，等待安装单元";
  const running = waitingSince > 0 && !failed && stage !== "idle";
  const unknown = data.status === "unknown";
  const statusBadge = unknown ? (
    <Badge variant="soft" tone="warn">版本信息不可用</Badge>
  ) : data.update_available ? (
    <Badge variant="soft" tone="info">有新版本</Badge>
  ) : data.status === "ok" ? (
    <Badge variant="soft" tone="ok">已是最新</Badge>
  ) : (
    <Badge variant="soft">未检查</Badge>
  );
  const gates = data.gates.filter((gate) => gate.id);
  return (
    <div className="stack">
      <Card>
        <CardContent className="stack">
          <SectionHeader
            title="中心版本"
            description="中心校验 GitHub Release 安装包后替换自己，再把同一份 Gate 程序推送给各 Gate。Gate 不访问 GitHub。"
            actions={statusBadge}
          />
          <div className="instrument-row" data-count="3">
            {/* 开发构建的版本名较长，窄屏缩小字号避免拆词 */}
            <Readout className="max-sm:[&_.mg-readout-value]:text-lg" label="当前版本" value={data.current_version || "未知"} />
            <Readout
              label="最新正式版本"
              value={data.latest_version || "未知"}
              caption={data.release_url ? <a className="underline underline-offset-3" href={data.release_url} target="_blank" rel="noopener noreferrer">查看 Release 说明</a> : undefined}
            />
            <Readout label="已部署 Gate" value={String(gates.length)} caption="仅统计已配对的设备" />
          </div>
          {/* 版本信息不可用只影响自动升级判断，页面其余数据已正常读取，不显示为加载失败 */}
          {unknown && (
            <Notice tone="warn" className="flex flex-wrap items-center justify-between gap-3">
              <span>{data.message || "发布信息不可用，已跳过自动升级判断"}</span>
              <Button type="button" size="sm" variant="outline" disabled={query.isFetching} onClick={() => void query.refetch()}>重新检查</Button>
            </Notice>
          )}
          {error && !open && !uninstalling && <Notice tone="bad">{error}</Notice>}
          <div className="flex justify-end">
            <Button type="button" disabled={busy || !data.update_available} onClick={() => { setOpen(true); setError(""); setWaitingSince(0); }}>
              {data.update_available ? "更新到 " + tag : "暂无可用更新"}
            </Button>
          </div>
        </CardContent>
      </Card>
      <Card>
        <CardContent className="stack">
          <SectionHeader
            title="Gate 版本与卸载"
            description="心跳上报的 Gate 程序版本与推送状态。卸载在 Gate 上执行，先选择清除范围。"
          />
          <Field label="卸载范围" htmlFor="uninstall-scope" className="max-w-sm">
            <Select value={mode} onValueChange={(value) => { if (value == null) return; setMode(String(value)); setScopeConfirmed(false); }}>
              <SelectTrigger id="uninstall-scope">
                <SelectValue>{scopeText[mode]}</SelectValue>
              </SelectTrigger>
              <SelectContent>
                {Object.entries(scopeText).map(([value, text]) => <SelectItem key={value} value={value}>{text}</SelectItem>)}
              </SelectContent>
            </Select>
          </Field>
          {mode !== "retain" && (
            <Label>
              <Checkbox checked={scopeConfirmed} onCheckedChange={(checked) => setScopeConfirmed(checked === true)} />
              我已确认清除范围：{scopeText[mode]}
            </Label>
          )}
          {gates.length ? (
            <div className="list-rows">
              {gates.map((gate) => (
                <div className="list-row" key={gate.id}>
                  <div>
                    <strong>{gate.name || "未命名 Gate"}</strong>
                    <small>心跳版本 {gate.version || "未知"}</small>
                  </div>
                  <div className="row-actions">
                    <Status value={gate.rollout} label={rolloutText[gate.rollout] || "未知"} />
                    <Button
                      type="button"
                      size="sm"
                      variant="destructive"
                      disabled={busy || (mode !== "retain" && !scopeConfirmed)}
                      onClick={() => { setError(""); setUninstalling(gate); }}
                    >
                      卸载此 Gate
                    </Button>
                  </div>
                </div>
              ))}
            </div>
          ) : (
            <PageState size="sm" kind="empty" title="没有已配对的 Gate" detail={data.gates.length ? "设备列表读取失败，推送状态未知。" : undefined} />
          )}
        </CardContent>
      </Card>
      {/* 卸载会停止该 Gate 上的代理服务，必须二次确认 */}
      <Dialog open={!!uninstalling} onOpenChange={(next) => { if (!busy && !next) setUninstalling(null); }}>
        <DialogContent size="md">
          <DialogTitle>卸载 {uninstalling?.name || "Gate"}</DialogTitle>
          <DialogDescription>
            清除范围：{scopeText[mode]}。卸载后该 Gate 上的入站立即停止服务，已授权用户将无法通过它连接；需要重新安装并配对才能恢复。
          </DialogDescription>
          {error && <Notice tone="bad">{error}</Notice>}
          <DialogFooter>
            <Button type="button" variant="outline" disabled={busy} onClick={() => setUninstalling(null)}>取消</Button>
            <Button type="button" variant="destructive" disabled={busy} onClick={() => void uninstallConfirmed()}>
              {busy ? "正在提交…" : "确认卸载"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      <Dialog open={open} onOpenChange={(next) => { if (!busy && !restarting) setOpen(next); }}>
          <DialogContent size="md">
            <DialogTitle>确认更新到 {tag}</DialogTitle>
            <DialogDescription>当前版本 {data.current_version || "未知"}。确认后校验安装包，替换中心并重启，再推送同一份 Gate。更新期间页面会短暂中断。</DialogDescription>
            {running && (
              <Progress value={progress} aria-label="更新进度">
                <ProgressLabel>{restarting && countdown > 0 ? `即将重启（${countdown}s）` : progressText}</ProgressLabel>
              </Progress>
            )}
            {failed && <Notice tone="bad">{stalled ? "安装单元没有开始。请确认更新单元已启用后重试。" : progressText}</Notice>}
            {error && <Notice tone="bad">{error}</Notice>}
            <DialogFooter>
              <Button type="button" variant="outline" disabled={busy || restarting} onClick={() => setOpen(false)}>取消</Button>
              <Button type="button" disabled={busy || restarting || (!failed && running)} onClick={() => void confirm()}>
                {busy ? "正在提交…" : failed ? "重试" : running ? progressText : "确认更新"}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
    </div>
  );
}
