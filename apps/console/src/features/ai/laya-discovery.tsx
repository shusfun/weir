import { useState } from "react";
import { ActionGroup, Badge, Button, Collapsible, Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, InsetRow, InsetRows, Notice } from "@weir/ui";
import { useQueryClient } from "@tanstack/react-query";
import { api } from "../../api";
import { useConsoleContext } from "../../lib/console-context";

type Instance = { url: string; model: string; state: string; code?: string; runtime_version?: string; model_digest?: string; preview_id?: string; compatibility?: { verified: boolean; enable_background: boolean; reason?: string; verified_range: string }; expires_at?: string };

export function LayaDiscovery({ url, model, apiKey, onAttached }: { url: string; model: string; apiKey: string; onAttached: () => void }) {
  const client = useQueryClient();
  const [items, setItems] = useState<Instance[] | null>(null);
  const [selected, setSelected] = useState<Instance | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  useConsoleContext({ notices: ["发现已有 Laya 实例；不扫描端口、不安装或替换服务。确认后保存为停用配置，推理测试通过后再启用。"], ...(selected ? { dialog: { title: "确认接入已有 Laya" } } : {}) });
  async function discover(defaults: boolean) {
    setBusy(true); setError(""); setNotice(""); setItems(null);
    try { const data = await api("/admin/defense/laya/discover", "POST", defaults ? {} : { url, model, key: apiKey }); setItems(data.items as Instance[]); }
    catch (e) { setError(e instanceof Error ? e.message : "无法发现已有实例"); }
    finally { setBusy(false); }
  }
  async function attach() {
    if (!selected?.preview_id) return;
    setBusy(true); setError("");
    try {
      await api(`/admin/defense/laya/attach/${selected.preview_id}`, "POST");
      setSelected(null); setItems(null); setNotice("已保存接入配置；请测试推理，通过后再启用判断。"); onAttached();
      await Promise.all([client.invalidateQueries({ queryKey: ["laya"] }), client.invalidateQueries({ queryKey: ["observe-system-health"] })]);
    } catch (e) { setError(e instanceof Error ? e.message : "接入失败，请重新发现并核对实例"); }
    finally { setBusy(false); }
  }
  return <Collapsible title="先发现已有实例" data-tour="laya-discovery">
    <div className="stack">
      <Notice>优先连接已有官方运行时。只读取版本和模型信息，不执行推理；已有服务的进程、文件和容器保持原状。</Notice>
      <ActionGroup><Button type="button" variant="outline" busy={busy && !selected} busyText="正在读取实例…" disabled={busy} onClick={() => void discover(true)}>检查已保存及默认地址</Button><Button type="button" variant="ghost" disabled={busy || !url.trim()} onClick={() => void discover(false)}>检查表单中的地址</Button></ActionGroup>
      {items?.map((item) => <div className="stack" key={item.url}>
        <InsetRows>
          <InsetRow><span className="min-w-0 break-all">{item.url}</span><Badge variant="status" tone={item.state === "available" ? "ok" : "neutral"}>{item.state === "available" ? "模型已安装" : "未知"}</Badge></InsetRow>
          <InsetRow><span>实际运行版本</span><span>{item.runtime_version ?? "未知"}</span></InsetRow>
          <InsetRow><span>模型</span><span className="min-w-0 break-all">{item.model}</span></InsetRow>
        </InsetRows>
        {item.code && <Notice>{item.code}</Notice>}
        {item.preview_id && <Button type="button" variant="outline" disabled={busy} onClick={() => { setError(""); setSelected(item); }}>核对并接入</Button>}
      </div>)}
      {items?.length === 0 && <Notice>没有取得实例信息。</Notice>}
      {notice && <Notice>{notice}</Notice>}
      {error && !selected && <Notice tone="bad">{error}</Notice>}
    </div>
    <Dialog open={selected !== null} onOpenChange={(open) => { if (!open && !busy) { setSelected(null); setError(""); } }}>
      <DialogContent><DialogHeader><DialogTitle>确认接入已有 Laya</DialogTitle><DialogDescription>确认会保存此地址、模型和 API Key，判断保持停用；推理测试通过后再启用。</DialogDescription></DialogHeader>
        {selected && <div className="stack"><p className="break-all">地址：{selected.url}</p><p>实际版本：{selected.runtime_version ?? "未知"} · 模型：{selected.model}</p><p className="break-all">模型摘要：{selected.model_digest ?? "未知"}</p><Notice>已验证范围：{selected.compatibility?.verified_range ?? "未知"}。{selected.compatibility?.enable_background ? "版本符合兼容档案。" : selected.compatibility?.reason ?? "后台分析暂停。"} 推理尚未检测，保存后须先测试，再启用判断。</Notice></div>}
        <DialogFooter error={error}><Button type="button" variant="ghost" disabled={busy} onClick={() => { setSelected(null); setError(""); }}>取消</Button><Button type="button" busy={busy} busyText="正在确认接入…" disabled={busy || !selected?.preview_id} onClick={() => void attach()}>确认接入</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  </Collapsible>;
}
