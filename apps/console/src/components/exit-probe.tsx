import { useState } from "react";
import { Badge, Button, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Field, Form, Segmented, Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@weir/ui";
import { api } from "../api";
import { useFormAction } from "../lib/action";
import { useConsoleContext } from "../lib/console-context";
import type { ExitItem } from "../lib/core";
import { exitTitle } from "../lib/exits";
import { exitKindName, ProbeReadouts } from "./chain-exit";
import { State } from "./feedback";
import { WaitText } from "./wait";

// 主动实测只更新观测结果；成功后保留弹窗，供用户核对测试位置与读数。
export function ExitProbe({ exit, base, gates, loading, error, retry, close, saved }: {
  exit: ExitItem;
  base: "/admin" | "/me";
  gates: { id: number; name: string }[];
  loading: boolean;
  error: unknown;
  retry: () => void;
  close: () => void;
  saved: () => void;
}) {
  const [where, setWhere] = useState("center");
  const [result, setResult] = useState<ExitItem["probe"] | null>(null);
  const save = useFormAction(async (track) => {
    setResult(null);
    return api(`${base}/exits/${exit.id}/probe`, "POST", where === "center" ? { location: "center" } : { gate_id: Number(where) }, track);
  }, { onSuccess: (r: ExitItem["probe"]) => { setResult(r); saved(); } });
  const choose = (value: string | null) => {
    if (value && value !== where) { setWhere(value); setResult(null); }
  };
  const location = where === "center" ? "中心" : gates.find((g) => String(g.id) === where)?.name ?? "未知";
  const targetName = exit.remark?.trim() || (exit.address ? (exit.port ? `${exit.address}:${exit.port}` : exit.address) : exitTitle(exit));
  const kindLabel = exit.kind === "node" ? "VPS 中转" : exit.kind === "socks5" ? "SOCKS5" : exit.kind === "http" ? "HTTP" : exit.kind === "https" ? "HTTPS" : (exitKindName[exit.kind] ?? exit.kind);
  const existing = (exit.probes ?? []).find((p) => (where === "center" ? p.location === "center" : p.location === "gate" && String(p.gate_id) === where));
  const shownResult = result ?? existing ?? null;
  useConsoleContext({ dialog: { title: "实测代理" } });
  return (
    <Dialog open onOpenChange={(open) => !open && !save.busy && close()}>
      <DialogContent size="md">
        <DialogTitle className="flex flex-wrap items-center gap-2">
          <span className="break-all">实测 · {targetName}</span>
          <Badge variant="soft" tone="neutral">{kindLabel}</Badge>
        </DialogTitle>
        <DialogDescription>发起一次性连通、落地出口与吞吐测试，不建立绑定关系；中心测通不代表指定 VPS 可用。</DialogDescription>
        <Form busy={save.busy} errors={save.errors} onSubmit={() => void save.submit()}>
          <Field label="测试发起位置" name="location">
            {gates.length > 0 && gates.length <= 3 ? (
              <Segmented
                fullWidth="mobile"
                aria-label="测试发起位置"
                value={where}
                onValueChange={choose}
                options={[["center", "控制中心"], ...gates.map((g) => [String(g.id), g.name] as const)]}
              />
            ) : (
              <Select value={where} onValueChange={choose} disabled={save.busy}>
                <SelectTrigger><SelectValue>{location}</SelectValue></SelectTrigger>
                <SelectContent><SelectItem value="center">控制中心</SelectItem>{gates.map((g) => <SelectItem key={g.id} value={String(g.id)}>{g.name}</SelectItem>)}</SelectContent>
              </Select>
            )}
          </Field>
          {(loading || error) ? <State loading={loading && !error} error={error} onRetry={retry} /> : null}
          <ProbeReadouts result={shownResult} location={location} historical={!result && Boolean(existing)} />
          <DialogFooter error={save.error} status={<WaitText wait={save.wait} />}>
            <Button type="button" variant="ghost" onClick={close} disabled={save.busy}>关闭</Button>
            <Button type="submit" busy={save.busy} busyText="正在实测…" disabled={where !== "center" && (loading || !!error)}>开始实测</Button>
          </DialogFooter>
        </Form>
      </DialogContent>
    </Dialog>
  );
}
