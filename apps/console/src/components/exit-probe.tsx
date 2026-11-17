import { useState } from "react";
import { Button, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Field, Form, Notice, Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@weir/ui";
import { api } from "../api";
import { useFormAction } from "../lib/action";
import { useConsoleContext } from "../lib/console-context";
import type { ExitItem } from "../lib/core";
import { ProbeReadouts } from "./chain-exit";
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
  useConsoleContext({ dialog: { title: "实测代理" } });
  return <Dialog open onOpenChange={(open) => !open && !save.busy && close()}>
    <DialogContent size="lg" className="weir-probe-dialog">
      <DialogTitle>实测代理</DialogTitle>
      <DialogDescription>测试不建立使用关系。中心测通不代表某台 VPS 可用。</DialogDescription>
      <Form busy={save.busy} errors={save.errors} onSubmit={() => void save.submit()}>
        <Field label="测试位置" name="location">
          <Select value={where} onValueChange={choose} disabled={save.busy}>
            <SelectTrigger><SelectValue>{location}</SelectValue></SelectTrigger>
            <SelectContent><SelectItem value="center">中心</SelectItem>{gates.map((g) => <SelectItem key={g.id} value={String(g.id)}>{g.name}</SelectItem>)}</SelectContent>
          </Select>
        </Field>
        {(loading || error) ? <State loading={loading && !error} error={error} onRetry={retry} /> : null}
        {where === "center" && <Notice>中心结果只说明中心到这个代理的连通、落地和速率。</Notice>}
        <ProbeReadouts result={result} location={location} />
        <DialogFooter error={save.error} status={<WaitText wait={save.wait} />}>
          <Button type="button" variant="outline" onClick={close} disabled={save.busy}>完成</Button>
          <Button type="submit" busy={save.busy} busyText="正在实测…" disabled={where !== "center" && (loading || !!error)}>开始实测</Button>
        </DialogFooter>
      </Form>
    </DialogContent>
  </Dialog>;
}
