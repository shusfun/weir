import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Field, Notice, SectionHeader,
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
import { State } from "./feedback";

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
  pending: "待执行",
  running: "执行中",
  applied: "已生效",
  failed: "失败",
  unknown: "未知",
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
  const uninstall = async (id: number) => {
    setBusy(true);
    setError("");
    try {
      await api(`/admin/devices/${id}/uninstall`, "POST", { confirm: true, mode, scope_confirmed: scopeConfirmed });
      await cache.invalidateQueries({ queryKey: ["release-status"] });
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "卸载请求失败");
    } finally {
      setBusy(false);
    }
  };
  const progress = stageValue[stage] || 10;
  const progressText = stalled ? "安装单元没有开始" : job?.message || "已提交，等待安装单元";
  const running = waitingSince > 0 && !failed && stage !== "idle";
  return (
    <Card>
      <CardContent className="stack">
        <SectionHeader level="h3" title="GitHub Release" description="中心校验安装包后替换自己，再把同一份 Gate 程序推送替换。Gate 不访问 GitHub。" actions={<Badge>{data.status === "ok" && !data.update_available ? "已是最新" : data.status === "unknown" ? "不可用" : "可检查"}</Badge>} />
        {data.status === "unknown" && <State error={data.message || "发布信息不可用"} detail={data.message || "发布信息不可用"} onRetry={() => query.refetch()} />}
        <p>当前版本 {data.current_version || "未知"}。最新正式版本 {data.latest_version || "未知"}。</p>
        {data.release_url && <p><a href={data.release_url}>查看 Release</a></p>}
        <ul>
          {data.gates.map((gate) => (
            <li key={gate.id ?? gate.name}>
              {gate.name || "Gate"}：心跳 {gate.version || "未知"}，{rolloutText[gate.rollout] || "未知"}
              {gate.id ? <Button type="button" disabled={busy || (mode !== "retain" && !scopeConfirmed)} onClick={() => uninstall(gate.id!)}>卸载此 Gate</Button> : null}
            </li>
          ))}
        </ul>
        <Field label="卸载范围">
          <Select value={mode} onValueChange={(value) => { if (value == null) return; setMode(String(value)); setScopeConfirmed(false); }}>
            <SelectTrigger className="w-full" aria-label="卸载范围">
              <SelectValue>{mode === "cores" ? "移除托管内核" : mode === "purge" ? "备份后清除本项目数据" : "保留数据和内核"}</SelectValue>
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="retain">保留数据和内核</SelectItem>
              <SelectItem value="cores">移除托管内核</SelectItem>
              <SelectItem value="purge">备份后清除本项目数据</SelectItem>
            </SelectContent>
          </Select>
        </Field>
        {mode !== "retain" && <Label><Checkbox checked={scopeConfirmed} onCheckedChange={(checked) => setScopeConfirmed(checked === true)} />再次确认这个清除范围</Label>}
        {error && !open && <Notice tone="bad">{error}</Notice>}
        <Button type="button" disabled={busy || !data.update_available} onClick={() => { setOpen(true); setError(""); setWaitingSince(0); }}>确认更新到 {tag}</Button>
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
      </CardContent>
    </Card>
  );
}
